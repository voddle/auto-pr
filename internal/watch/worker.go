package watch

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"time"

	"auto-pr/internal/claude"
	"auto-pr/internal/container"
	"auto-pr/internal/github"
	"auto-pr/internal/state"
	"auto-pr/internal/worktree"
)

// RunWorker runs the full lifecycle for a single issue:
// Phase 1: Create worktree, implement issue via Claude
// Phase 2: Watch PR reviews, handle them via Claude --continue
func RunWorker(ctx context.Context, repo, projectRoot string, issueNum, interval int, once bool, cfg WorkerConfig, stateDir *state.Dir, dockerMgr *container.Manager) error {
	logFile, err := os.OpenFile(stateDir.LogPath(issueNum), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		return fmt.Errorf("open log file: %w", err)
	}
	defer logFile.Close()

	log := func(format string, args ...interface{}) {
		msg := fmt.Sprintf("[worker #%d] %s", issueNum, fmt.Sprintf(format, args...))
		fmt.Println(msg)
		fmt.Fprintln(logFile, msg)
	}

	branch := fmt.Sprintf("auto/issue-%d", issueNum)

	log("Starting worker for issue #%d in repo %s", issueNum, repo)

	// Phase 0: If Docker is enabled, start a container for this worker
	var containerID string
	if dockerMgr != nil {
		containerName := fmt.Sprintf("worker-issue-%d", issueNum)
		log("Starting Docker container %s...", containerName)
		cid, err := dockerMgr.Start(ctx, containerName, container.GetWorkerEnv())
		if err != nil {
			log("Failed to start container: %v", err)
			stateDir.WriteIssue(issueNum, &state.IssueState{
				Status: state.IssueFailed, Branch: branch,
			})
			return err
		}
		containerID = cid
		defer func() {
			log("Stopping container %s...", containerName)
			dockerMgr.Stop(context.Background(), containerID)
		}()
	}

	// Phase 1: Create worktree and implement issue
	log("Phase 1: Creating worktree...")
	wtPath, err := worktree.CreateForIssue(ctx, projectRoot, cfg.WorktreeDir, repo, issueNum, cfg.BaseBranch)
	if err != nil {
		log("Failed to create worktree: %v", err)
		stateDir.WriteIssue(issueNum, &state.IssueState{
			Status: state.IssueFailed, Branch: branch,
		})
		return err
	}

	// Fetch issue details
	issue, err := github.GetIssue(ctx, repo, issueNum)
	if err != nil {
		log("Failed to fetch issue: %v", err)
		stateDir.WriteIssue(issueNum, &state.IssueState{
			Status: state.IssueFailed, Branch: branch,
		})
		return err
	}

	log("Phase 1: Implementing issue — %s", issue.Title)

	prompt := buildImplementPrompt(repo, issueNum, issue.Title, issue.Body, branch)
	if err := runClaude(ctx, dockerMgr, containerID, wtPath, prompt, logFile); err != nil {
		log("Warning: claude exited with error during implementation: %v", err)
		stateDir.WriteIssue(issueNum, &state.IssueState{
			Status: state.IssueFailed, Branch: branch,
		})
		return err
	}

	log("Phase 1 complete.")

	// Detect PR created by claude
	log("Detecting PR...")
	prNum, err := detectPR(ctx, repo, issueNum)
	if err != nil || prNum == 0 {
		log("No PR found. Claude may not have created one.")
		stateDir.WriteIssue(issueNum, &state.IssueState{
			Status: state.IssueFailed, Branch: branch,
		})
		return fmt.Errorf("no PR created for issue #%d", issueNum)
	}

	log("PR #%d detected.", prNum)
	stateDir.WriteIssue(issueNum, &state.IssueState{
		Status: state.IssueWatching, Branch: branch, PRNumber: prNum,
	})

	// Phase 2: Watch reviews
	if err := watchReviews(ctx, repo, wtPath, prNum, issueNum, interval, once, stateDir, logFile, dockerMgr, containerID); err != nil {
		return err
	}

	// Done
	stateDir.WriteIssue(issueNum, &state.IssueState{
		Status: state.IssueDone, Branch: branch, PRNumber: prNum,
	})
	log("PR #%d closed/merged, worker exiting.", prNum)
	return nil
}

func watchReviews(ctx context.Context, repo, wtPath string, prNum, issueNum, interval int, once bool, stateDir *state.Dir, logFile io.Writer, dockerMgr *container.Manager, containerID string) error {
	log := func(format string, args ...interface{}) {
		msg := fmt.Sprintf("[worker #%d] %s", issueNum, fmt.Sprintf(format, args...))
		fmt.Println(msg)
		fmt.Fprintln(logFile, msg)
	}

	branch := fmt.Sprintf("auto/issue-%d", issueNum)

	log("Phase 2: Watching reviews on PR #%d", prNum)

	lastTS, _ := github.GetLatestCommentTimestamp(ctx, repo, prNum)
	if lastTS == "" {
		lastTS = "1970-01-01T00:00:00Z"
	}
	log("Baseline review timestamp: %s", lastTS)

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(time.Duration(interval) * time.Second):
		}

		// Check if PR is still open
		prState, err := github.GetPRState(ctx, repo, prNum)
		if err != nil {
			log("Warning: could not check PR state: %v", err)
			continue
		}
		if prState != "open" {
			log("PR #%d is %s, exiting review loop.", prNum, prState)
			break
		}

		// Check for new comments
		newData, err := github.FetchNewComments(ctx, repo, prNum, lastTS)
		if err != nil {
			log("Warning: %v", err)
			continue
		}
		if newData == nil {
			continue
		}

		log("PR #%d: %d new inline comment(s), %d new review(s)",
			prNum, len(newData.InlineComments), len(newData.TopLevelReviews))

		dataJSON, _ := json.Marshal(newData)
		prompt := buildReviewPrompt(repo, prNum, branch, string(dataJSON))

		// --continue reuses session context from Phase 1
		if err := runClaudeContinue(ctx, dockerMgr, containerID, wtPath, prompt, logFile); err != nil {
			log("Warning: claude exited with error during review handling: %v", err)
		}

		// Update timestamp
		ts, _ := github.GetLatestCommentTimestamp(ctx, repo, prNum)
		if ts != "" {
			lastTS = ts
		}
		log("Updated review timestamp to: %s", lastTS)

		if once {
			log("--once mode, exiting review loop.")
			break
		}
	}

	stateDir.WriteIssue(issueNum, &state.IssueState{
		Status: state.IssueDone, Branch: branch, PRNumber: prNum,
	})
	return nil
}

// runClaude runs claude either locally or in a Docker container.
func runClaude(ctx context.Context, dockerMgr *container.Manager, containerID, dir, prompt string, logWriter io.Writer) error {
	if dockerMgr != nil && containerID != "" {
		// Convert host worktree path to container path
		workDir := toContainerPath(dir, dockerMgr.ProjectRoot)
		return claude.RunInContainer(ctx, dockerMgr, containerID, workDir, prompt, logWriter)
	}
	return claude.Run(ctx, dir, prompt, logWriter)
}

// runClaudeContinue runs claude --continue either locally or in a Docker container.
func runClaudeContinue(ctx context.Context, dockerMgr *container.Manager, containerID, dir, prompt string, logWriter io.Writer) error {
	if dockerMgr != nil && containerID != "" {
		workDir := toContainerPath(dir, dockerMgr.ProjectRoot)
		return claude.RunContinueInContainer(ctx, dockerMgr, containerID, workDir, prompt, logWriter)
	}
	return claude.RunContinue(ctx, dir, prompt, logWriter)
}

// toContainerPath converts a host path to the corresponding container path.
// Host project root is bind-mounted at /workspace in the container.
func toContainerPath(hostPath, projectRoot string) string {
	// Get relative path from project root
	rel := hostPath
	if len(hostPath) > len(projectRoot) && hostPath[:len(projectRoot)] == projectRoot {
		rel = hostPath[len(projectRoot):]
	}
	// Normalize path separators for Linux container
	result := "/workspace"
	for _, ch := range rel {
		if ch == '\\' {
			result += "/"
		} else {
			result += string(ch)
		}
	}
	return result
}

func detectPR(ctx context.Context, repo string, issueNum int) (int, error) {
	branch := fmt.Sprintf("auto/issue-%d", issueNum)
	prNum, err := github.FindPRForBranch(ctx, repo, branch)
	if err != nil {
		return 0, err
	}
	return prNum, nil
}

func buildImplementPrompt(repo string, issueNum int, title, body, branch string) string {
	return fmt.Sprintf(`You are working in a git worktree for issue #%d in repo %s.
Issue title: %s
Issue body:
%s

Your task:
1. Read the issue and understand the requirement
2. Explore the codebase, implement the solution
3. Commit with message referencing the issue (e.g. "fix #%d: ...")
4. git push -u origin %s
5. Create a PR with: gh pr create --title "<descriptive title>" --body "Fixes #%d"

Constraints: Only modify relevant files. Do not touch CLAUDE.md, .claude/, scripts/, .gitignore, CI configs.`,
		issueNum, repo, title, body, issueNum, branch, issueNum)
}

func buildReviewPrompt(repo string, prNum int, branch, data string) string {
	return fmt.Sprintf(`New review comments on PR #%d (branch: %s) in repo %s:

%s

【Edit scope constraints — MUST strictly follow】
- You may ONLY modify files explicitly mentioned in the review comments (the 'path' field of inline comments defines your editing scope). Do NOT edit any file not referenced by a review comment.
- Only change code related to the reviewer's feedback — do not refactor, reformat, or "improve" surrounding code beyond what the reviewer requested.
- Do NOT modify project infrastructure files: CLAUDE.md, .claude/, scripts/, .gitignore, CI configs.
- If a review comment is ambiguous or references files not in the PR, use ./scripts/pr-reply to ask for clarification instead of guessing.

For each inline comment (items in inline_comments array):
1. Read the file mentioned in the comment (path field) at the code location (line field)
2. Modify the code per the reviewer's feedback (only that file)
3. After all modifications, commit and push with a single commit
4. For each inline comment, reply using: ./scripts/pr-reply <comment_id> "brief description of what you changed"

For top_level_reviews, if they contain specific modification suggestions, handle them too (same edit scope constraints).

Note: The 'id' field of each comment is the comment_id needed for pr-reply.`,
		prNum, branch, repo, data)
}
