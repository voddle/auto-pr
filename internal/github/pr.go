package github

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"strings"

	"auto-pr/internal/ghcli"
)

// CurrentBranch returns the current git branch name.
func CurrentBranch() (string, error) {
	cmd := exec.Command("git", "rev-parse", "--abbrev-ref", "HEAD")
	var out bytes.Buffer
	cmd.Stdout = &out
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("not inside a git repository: %w", err)
	}
	return strings.TrimSpace(out.String()), nil
}

// FindPRForBranch finds the open PR number for the given branch.
func FindPRForBranch(ctx context.Context, repo, branch string) (int, error) {
	var pulls []PullRequest
	if err := ghcli.APIPaginateTyped(ctx, fmt.Sprintf("repos/%s/pulls", repo), &pulls); err != nil {
		return 0, fmt.Errorf("fetch PRs: %w", err)
	}
	for _, pr := range pulls {
		if pr.Head.Ref == branch {
			return pr.Number, nil
		}
	}
	return 0, fmt.Errorf("no open PR found for branch '%s'", branch)
}

// GetPRState returns the state of a PR ("open", "closed", "merged").
func GetPRState(ctx context.Context, repo string, prNum int) (string, error) {
	var pr PullRequest
	err := ghcli.APITyped(ctx, fmt.Sprintf("repos/%s/pulls/%d", repo, prNum), &pr)
	if err != nil {
		return "", err
	}
	return pr.State, nil
}

// GetDefaultBranch returns the default branch of the repo.
func GetDefaultBranch(ctx context.Context, repo string) (string, error) {
	var info RepoInfo
	err := ghcli.APITyped(ctx, fmt.Sprintf("repos/%s", repo), &info)
	if err != nil {
		return "main", nil
	}
	if info.DefaultBranch == "" {
		return "main", nil
	}
	return info.DefaultBranch, nil
}
