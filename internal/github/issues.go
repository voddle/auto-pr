package github

import (
	"context"
	"fmt"
	"net/url"

	"auto-pr/internal/ghcli"
)

// FetchIssuesWithLabels fetches open issues with the given comma-separated labels.
// Filters out pull requests (which GitHub API returns as issues too).
func FetchIssuesWithLabels(ctx context.Context, repo, labels string) ([]Issue, error) {
	encoded := url.QueryEscape(labels)
	endpoint := fmt.Sprintf("repos/%s/issues?labels=%s&state=open&sort=created&direction=asc", repo, encoded)

	var issues []Issue
	if err := ghcli.APIPaginateTyped(ctx, endpoint, &issues); err != nil {
		return nil, fmt.Errorf("fetch issues: %w", err)
	}

	// Filter out pull requests
	var filtered []Issue
	for _, issue := range issues {
		if issue.PullRequest == nil {
			filtered = append(filtered, issue)
		}
	}
	return filtered, nil
}

// GetIssue fetches a single issue by number.
func GetIssue(ctx context.Context, repo string, num int) (*Issue, error) {
	var issue Issue
	err := ghcli.APITyped(ctx, fmt.Sprintf("repos/%s/issues/%d", repo, num), &issue)
	if err != nil {
		return nil, err
	}
	return &issue, nil
}
