package cmd

import (
	"context"
	"fmt"
	"os"
	"strconv"

	"auto-pr/internal/ghcli"
	"auto-pr/internal/github"
)

// RunReply implements the "reply" subcommand.
func RunReply(args []string) int {
	if len(args) == 0 {
		printReplyUsage()
		return 1
	}

	if args[0] == "--help" || args[0] == "-h" {
		printReplyUsage()
		return 0
	}

	ctx := context.Background()

	if err := ghcli.Detect(); err != nil {
		fmt.Fprintln(os.Stderr, "Error:", err)
		return 1
	}

	repo, err := ghcli.RepoSlug(ctx)
	if err != nil {
		fmt.Fprintln(os.Stderr, "Error:", err)
		return 1
	}

	// --list mode
	if args[0] == "--list" {
		prNum := 0
		if len(args) > 1 {
			n, err := strconv.Atoi(args[1])
			if err != nil {
				fmt.Fprintf(os.Stderr, "Error: Invalid PR number '%s'\n", args[1])
				return 1
			}
			prNum = n
		}

		if prNum == 0 {
			branch, err := github.CurrentBranch()
			if err != nil {
				fmt.Fprintln(os.Stderr, "Error:", err)
				return 1
			}
			prNum, err = github.FindPRForBranch(ctx, repo, branch)
			if err != nil {
				fmt.Fprintf(os.Stderr, "Error: %v\n", err)
				return 1
			}
		}

		comments, err := github.FetchReviewComments(ctx, repo, prNum)
		if err != nil {
			fmt.Fprintln(os.Stderr, "Error:", err)
			return 1
		}

		fmt.Printf("Comments on PR #%d that can be replied to:\n\n", prNum)
		for _, c := range comments {
			firstLine := firstLineOf(c.Body)
			fmt.Printf("  ID: %d  @%s  %s:%s\n  %s\n\n",
				c.ID, c.User.Login, c.Path, c.LineDisplay(), firstLine)
		}
		return 0
	}

	// Reply mode: pr-reply <comment_id> "body"
	if len(args) < 2 {
		fmt.Fprintln(os.Stderr, "Error: Missing reply body.")
		fmt.Fprintln(os.Stderr, "Usage: auto-pr reply <comment_id> \"reply body\"")
		return 1
	}

	commentID, err := strconv.Atoi(args[0])
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: comment_id must be a number, got '%s'.\n", args[0])
		return 1
	}
	replyBody := args[1]

	// Post reply
	endpoint := fmt.Sprintf("repos/%s/pulls/comments/%d/replies", repo, commentID)
	var resp github.ReplyResponse
	err = ghcli.APITyped(ctx, endpoint, &resp, "-f", "body="+replyBody)
	if err != nil {
		fmt.Fprintln(os.Stderr, "Error: Failed to post reply. Check comment ID and permissions.")
		fmt.Fprintln(os.Stderr, err)
		return 1
	}

	fmt.Printf("Reply posted (ID: %d) by @%s\n", resp.ID, resp.User.Login)
	return 0
}

func printReplyUsage() {
	fmt.Println("Usage:")
	fmt.Println("  auto-pr reply <comment_id> \"reply body\"   Reply to a review comment")
	fmt.Println("  auto-pr reply --list [PR_NUMBER]           List comment IDs available for reply")
	fmt.Println("  auto-pr reply --help                       Show this help")
}

func firstLineOf(s string) string {
	for i, ch := range s {
		if ch == '\n' {
			return s[:i]
		}
	}
	return s
}
