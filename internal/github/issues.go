package github

import (
	"context"
	"fmt"
	"net/url"
	"strings"

	"auto-pr/internal/ghcli"
)

// FetchIssuesWithLabels fetches open issues matching ANY of the given
// comma-separated labels (OR logic). Each label triggers a separate API call;
// results are deduplicated by issue number.
func FetchIssuesWithLabels(ctx context.Context, repo, labels string) ([]Issue, error) {
	seen := map[int]bool{}
	var result []Issue

	for _, label := range strings.Split(labels, ",") {
		label = strings.TrimSpace(label)
		if label == "" {
			continue
		}
		encoded := url.QueryEscape(label)
		endpoint := fmt.Sprintf("repos/%s/issues?labels=%s&state=open&sort=created&direction=asc", repo, encoded)

		var issues []Issue
		if err := ghcli.APIPaginateTyped(ctx, endpoint, &issues); err != nil {
			return nil, fmt.Errorf("fetch issues (label %q): %w", label, err)
		}

		for _, issue := range issues {
			if issue.PullRequest != nil || seen[issue.Number] {
				continue
			}
			seen[issue.Number] = true
			result = append(result, issue)
		}
	}
	return result, nil
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
