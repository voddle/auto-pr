package watch

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"time"

	"auto-pr/internal/claude"
	"auto-pr/internal/container"
	"auto-pr/internal/github"
	"auto-pr/internal/state"
)

// SinglePR watches a single PR for new review comments and processes them with Claude.
func SinglePR(ctx context.Context, repo, projectRoot string, prNum, interval int, once bool, stateDir *state.Dir, dockerMgr *container.Manager) error {
	// Read or init state
	prState := stateDir.ReadPR(prNum)
	var lastTS string
	if prState != nil {
		lastTS = prState.LastCommentTS
	}

	if lastTS == "" {
		fmt.Println("[pr-watch] First run — recording current comment state...")
		ts, err := github.GetLatestCommentTimestamp(ctx, repo, prNum)
		if err != nil {
			ts = ""
		}
		if ts != "" {
			stateDir.WritePR(prNum, &state.PRState{LastCommentTS: ts})
			fmt.Printf("[pr-watch] Baseline timestamp: %s\n", ts)
			lastTS = ts
		} else {
			lastTS = "1970-01-01T00:00:00Z"
			stateDir.WritePR(prNum, &state.PRState{LastCommentTS: lastTS})
			fmt.Println("[pr-watch] No existing comments found, watching for new ones.")
		}
	} else {
		fmt.Printf("[pr-watch] Resuming from timestamp: %s\n", lastTS)
	}

	fmt.Printf("[pr-watch] Watching PR #%d on %s (interval: %ds)\n\n", prNum, repo, interval)

	// If Docker mode is enabled, start a container for this PR
	var containerID string
	if dockerMgr != nil {
		if err := dockerMgr.EnsureImage(ctx); err != nil {
			return fmt.Errorf("docker image build failed: %w", err)
		}
		containerName := fmt.Sprintf("worker-pr-%d", prNum)
		fmt.Printf("[pr-watch] Starting Docker container %s...\n", containerName)
		cid, err := dockerMgr.Start(ctx, containerName, container.GetWorkerEnv())
		if err != nil {
			return fmt.Errorf("failed to start container: %w", err)
		}
		containerID = cid
		defer func() {
			fmt.Printf("[pr-watch] Stopping container %s...\n", containerName)
			dockerMgr.Stop(context.Background(), containerID)
		}()
	}

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		fmt.Printf("[pr-watch] %s Checking for new comments...\n", time.Now().Format("15:04:05"))

		newData, err := github.FetchNewComments(ctx, repo, prNum, lastTS)
		if err != nil {
			fmt.Fprintf(os.Stderr, "[pr-watch] Warning: %v\n", err)
		}

		if newData == nil {
			fmt.Println("[pr-watch] No new comments.")
		} else {
			fmt.Printf("[pr-watch] Found %d new inline comment(s), %d new review(s).\n",
				len(newData.InlineComments), len(newData.TopLevelReviews))

			// Print previews
			for _, c := range newData.InlineComments {
				fmt.Printf("  -> @%s on %s:%s: %s\n", c.User.Login, c.Path, c.LineDisplay(), firstLine(c.Body))
			}
			for _, r := range newData.TopLevelReviews {
				fmt.Printf("  -> @%s [%s]: %s\n", r.User.Login, r.State, firstLine(r.Body))
			}

			fmt.Println()
			fmt.Println("[pr-watch] Dispatching to Claude Code...")

			dataJSON, _ := json.Marshal(newData)
			prompt := buildSinglePRPrompt(repo, prNum, string(dataJSON))

			if err := runClaudeSinglePR(ctx, dockerMgr, containerID, projectRoot, prompt); err != nil {
				fmt.Fprintf(os.Stderr, "[pr-watch] Warning: Claude Code exited with non-zero status: %v\n", err)
			}

			fmt.Println()
			fmt.Println("[pr-watch] Claude Code finished processing.")

			// Update timestamp
			ts, _ := github.GetLatestCommentTimestamp(ctx, repo, prNum)
			if ts != "" {
				lastTS = ts
				stateDir.WritePR(prNum, &state.PRState{LastCommentTS: lastTS})
				fmt.Printf("[pr-watch] Updated timestamp to: %s\n", lastTS)
			}
		}

		if once {
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

func buildSinglePRPrompt(repo string, prNum int, data string) string {
	return fmt.Sprintf(`New review comments on GitHub PR #%d (repo: %s). Process each one:

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

Note: The 'id' field of each comment is the comment_id needed for pr-reply.`, prNum, repo, data)
}

// runClaudeSinglePR runs claude for single-PR mode, either locally or in a Docker container.
func runClaudeSinglePR(ctx context.Context, dockerMgr *container.Manager, containerID, projectRoot, prompt string) error {
	if dockerMgr != nil && containerID != "" {
		return claude.RunInContainer(ctx, dockerMgr, containerID, "/workspace", prompt, nil)
	}
	return claude.Run(ctx, ".", prompt, nil)
}

func firstLine(s string) string {
	for i, ch := range s {
		if ch == '\n' {
			return s[:i]
		}
	}
	return s
}
