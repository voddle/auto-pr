package watch

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sync"
	"time"

	"auto-pr/internal/container"
	"auto-pr/internal/github"
	"auto-pr/internal/state"
	"auto-pr/internal/worktree"
)

// Repo runs the repo-level watcher that scans for new issues and spawns worker goroutines.
func Repo(ctx context.Context, repo, projectRoot string, interval, maxConcurrent int, once bool, cfg WorkerConfig, stateDir *state.Dir, dockerMgr *container.Manager) error {
	fmt.Printf("[pr-watch] Repo mode — watching %s\n", repo)
	fmt.Printf("[pr-watch] Config: interval=%ds, max_concurrent=%d, issue_labels=%s\n", interval, maxConcurrent, cfg.IssueLabels)
	fmt.Printf("[pr-watch] Worktree dir: %s\n", cfg.WorktreeDir)
	if dockerMgr != nil {
		fmt.Printf("[pr-watch] Docker isolation: enabled (image: %s)\n", dockerMgr.ImageName)
	}
	fmt.Println("[pr-watch] Workers handle: Issue implementation → PR creation → Review watching")
	fmt.Println()

	// Ensure Docker image exists if Docker mode is enabled
	if dockerMgr != nil {
		if err := dockerMgr.EnsureImage(ctx); err != nil {
			return fmt.Errorf("docker image build failed: %w", err)
		}
	}

	sem := make(chan struct{}, maxConcurrent)
	var wg sync.WaitGroup
	activeWorkers := make(map[int]context.CancelFunc) // issueNum -> cancel
	var mu sync.Mutex

	defer func() {
		fmt.Println()
		fmt.Println("[pr-watch] Shutting down, terminating workers...")
		mu.Lock()
		for num, cancel := range activeWorkers {
			fmt.Printf("[pr-watch] Cancelling worker for issue #%d\n", num)
			cancel()
		}
		mu.Unlock()
		wg.Wait()
		fmt.Println("[pr-watch] Goodbye.")
	}()

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		fmt.Printf("[pr-watch] %s Scanning...\n", time.Now().Format("15:04:05"))

		// 1. Monitor workers — check for completed/failed
		mu.Lock()
		for num, cancel := range activeWorkers {
			issueState := stateDir.ReadIssue(num)
			if issueState != nil && (issueState.Status == state.IssueDone || issueState.Status == state.IssueFailed) {
				fmt.Printf("[pr-watch] Worker for issue #%d finished (%s)\n", num, issueState.Status)
				cancel()
				delete(activeWorkers, num)
			}
		}
		activeCount := len(activeWorkers)
		mu.Unlock()

		// 2. Clean up stale worktrees
		cleanupStaleWorktrees(ctx, repo, projectRoot, cfg.WorktreeDir, stateDir)

		// 3. Scan for new issues
		scanAndSpawnWorkers(ctx, repo, projectRoot, interval, once, cfg, stateDir, sem, &wg, activeWorkers, &mu, dockerMgr)

		// Mark initialized after first scan
		if !stateDir.IsInitialized() {
			stateDir.MarkInitialized()
			fmt.Println("[pr-watch] First scan complete, future events will be processed.")
		}

		mu.Lock()
		activeCount = len(activeWorkers)
		mu.Unlock()
		fmt.Printf("[pr-watch] Active workers: %d/%d\n", activeCount, maxConcurrent)

		if once {
			if activeCount > 0 {
				fmt.Printf("[pr-watch] --once mode, waiting for %d active worker(s) to finish...\n", activeCount)
				wg.Wait()
			}
			fmt.Println("[pr-watch] --once mode, exiting.")
			return nil
		}

		fmt.Printf("[pr-watch] Sleeping %ds...\n", interval)
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(time.Duration(interval) * time.Second):
		}
	}
}

func scanAndSpawnWorkers(ctx context.Context, repo, projectRoot string, interval int, once bool, cfg WorkerConfig, stateDir *state.Dir, sem chan struct{}, wg *sync.WaitGroup, activeWorkers map[int]context.CancelFunc, mu *sync.Mutex, dockerMgr *container.Manager) {
	if cfg.IssueLabels == "" {
		return
	}

	issues, err := github.FetchIssuesWithLabels(ctx, repo, cfg.IssueLabels)
	if err != nil {
		fmt.Fprintf(os.Stderr, "[pr-watch] Warning: Failed to fetch issues: %v\n", err)
		return
	}

	for _, issue := range issues {
		// Check if already known
		if s := stateDir.ReadIssue(issue.Number); s != nil {
			continue
		}

		// First run guard — mark as preexisting
		if !stateDir.IsInitialized() {
			fmt.Printf("[pr-watch] First run: marking issue #%d as pre-existing (skipping)\n", issue.Number)
			stateDir.WriteIssue(issue.Number, &state.IssueState{
				Status: state.IssuePreexisting,
			})
			continue
		}

		fmt.Printf("[pr-watch] New issue #%d: %s\n", issue.Number, issue.Title)

		// Try to acquire a slot
		select {
		case sem <- struct{}{}:
			// Got a slot — spawn worker
		default:
			fmt.Printf("[pr-watch] No slots available, deferring issue #%d\n", issue.Number)
			continue
		}

		issueNum := issue.Number
		branch := fmt.Sprintf("auto/issue-%d", issueNum)

		stateDir.WriteIssue(issueNum, &state.IssueState{
			Status: state.IssueInProgress,
			Branch: branch,
		})

		workerCtx, cancel := context.WithCancel(ctx)
		mu.Lock()
		activeWorkers[issueNum] = cancel
		mu.Unlock()

		wg.Add(1)
		go func() {
			defer wg.Done()
			defer func() { <-sem }()
			defer func() {
				mu.Lock()
				delete(activeWorkers, issueNum)
				mu.Unlock()
			}()

			fmt.Printf("[pr-watch] Spawned worker for issue #%d\n", issueNum)

			if err := RunWorker(workerCtx, repo, projectRoot, issueNum, interval, once, cfg, stateDir, dockerMgr); err != nil {
				fmt.Fprintf(os.Stderr, "[pr-watch] Worker for issue #%d failed: %v\n", issueNum, err)
				stateDir.WriteIssue(issueNum, &state.IssueState{
					Status: state.IssueFailed, Branch: branch,
				})
			}
		}()

		fmt.Printf("[pr-watch] Spawned worker for issue #%d (log: %s)\n", issueNum, stateDir.LogPath(issueNum))
	}
}

var issueWorktreeRE = regexp.MustCompile(`^issue-(\d+)$`)
var prWorktreeRE = regexp.MustCompile(`^pr-(\d+)$`)

func cleanupStaleWorktrees(ctx context.Context, repo, projectRoot, worktreeDir string, stateDir *state.Dir) {
	wtRoot := filepath.Join(projectRoot, worktreeDir)
	entries, err := os.ReadDir(wtRoot)
	if err != nil {
		return
	}

	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		name := entry.Name()

		if m := issueWorktreeRE.FindStringSubmatch(name); m != nil {
			issueNum := parseInt(m[1])
			issueState := stateDir.ReadIssue(issueNum)
			if issueState != nil && (issueState.Status == state.IssueInProgress || issueState.Status == state.IssueWatching) {
				continue // active worker
			}

			issue, err := github.GetIssue(ctx, repo, issueNum)
			if err != nil {
				continue
			}
			if issue.State == "closed" {
				fmt.Printf("[pr-watch] Issue #%d is closed, removing worktree...\n", issueNum)
				wtPath := filepath.Join(wtRoot, name)
				if err := worktree.Remove(projectRoot, wtPath); err != nil {
					fmt.Fprintf(os.Stderr, "[pr-watch] Warning: %v\n", err)
				}
			}
		} else if m := prWorktreeRE.FindStringSubmatch(name); m != nil {
			prNum := parseInt(m[1])
			prState, err := github.GetPRState(ctx, repo, prNum)
			if err != nil {
				continue
			}
			if prState == "closed" || prState == "merged" {
				fmt.Printf("[pr-watch] PR #%d is %s, removing worktree...\n", prNum, prState)
				wtPath := filepath.Join(wtRoot, name)
				if err := worktree.Remove(projectRoot, wtPath); err != nil {
					fmt.Fprintf(os.Stderr, "[pr-watch] Warning: %v\n", err)
				}
			}
		}
	}
}

func parseInt(s string) int {
	n := 0
	for _, ch := range s {
		if ch >= '0' && ch <= '9' {
			n = n*10 + int(ch-'0')
		}
	}
	return n
}
