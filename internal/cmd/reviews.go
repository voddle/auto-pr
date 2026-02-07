package cmd

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"strconv"

	"auto-pr/internal/ghcli"
	"auto-pr/internal/github"
)

// RunReviews implements the "reviews" subcommand.
func RunReviews(args []string) int {
	fs := flag.NewFlagSet("reviews", flag.ContinueOnError)
	latest := fs.Bool("latest", false, "Only show the latest review round")
	jsonOut := fs.Bool("json", false, "Raw JSON output")
	help := fs.Bool("help", false, "Show help")
	h := fs.Bool("h", false, "Show help")

	if err := fs.Parse(args); err != nil {
		return 1
	}

	if *help || *h {
		fmt.Println("Usage: auto-pr reviews [PR_NUMBER] [--latest] [--json]")
		fmt.Println()
		fmt.Println("  auto-pr reviews          Auto-detect PR for current branch")
		fmt.Println("  auto-pr reviews 123      Show reviews for PR #123")
		fmt.Println("  auto-pr reviews --latest Only show the latest review round")
		fmt.Println("  auto-pr reviews --json   Raw JSON output")
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

	// Parse optional PR number from remaining args
	var prNum int
	for _, arg := range fs.Args() {
		n, err := strconv.Atoi(arg)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error: Unknown argument '%s'\n", arg)
			return 1
		}
		prNum = n
	}

	// Auto-detect PR from branch if not specified
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
		fmt.Printf("Detected PR #%d for branch '%s'\n", prNum, branch)
	}

	// Fetch data
	comments, err := github.FetchReviewComments(ctx, repo, prNum)
	if err != nil {
		fmt.Fprintln(os.Stderr, "Error:", err)
		return 1
	}
	reviews, err := github.FetchReviews(ctx, repo, prNum)
	if err != nil {
		fmt.Fprintln(os.Stderr, "Error:", err)
		return 1
	}

	// JSON output mode
	if *jsonOut {
		out := struct {
			Reviews  []github.Review        `json:"reviews"`
			Comments []github.ReviewComment `json:"comments"`
		}{
			Reviews:  reviews,
			Comments: comments,
		}
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		enc.Encode(out)
		return 0
	}

	// Filter latest if requested
	if *latest {
		reviews, comments = github.FilterLatestReview(reviews, comments)
	}

	// Pretty-print
	fmt.Println()
	fmt.Printf("═══ PR #%d Reviews ═══\n", prNum)
	fmt.Println()

	// Top-level reviews
	for _, r := range reviews {
		if r.Body == "" && r.State == "COMMENTED" {
			continue
		}
		ts := r.SubmittedAt
		if ts == "" {
			ts = "pending"
		}
		fmt.Printf("── %s by @%s (%s) ──\n%s\n\n", r.State, r.User.Login, ts, r.Body)
	}

	// Inline comments
	if len(comments) > 0 {
		fmt.Printf("── Inline Comments (%d) ──\n\n", len(comments))
		for _, c := range comments {
			fmt.Printf("  %s:%s  @%s\n  %s\n  ID: %d\n\n",
				c.Path, c.LineDisplay(), c.User.Login, c.Body, c.ID)
		}
	}

	fmt.Println("Done.")
	return 0
}
