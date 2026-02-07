package github

import (
	"context"
	"encoding/json"
	"fmt"

	"auto-pr/internal/ghcli"
)

// FetchReviewComments fetches all inline (line-level) comments on a PR.
func FetchReviewComments(ctx context.Context, repo string, prNum int) ([]ReviewComment, error) {
	data, err := ghcli.APIPaginate(ctx, fmt.Sprintf("repos/%s/pulls/%d/comments", repo, prNum))
	if err != nil {
		return nil, fmt.Errorf("fetch review comments: %w", err)
	}
	return parseComments(data)
}

// FetchReviews fetches all top-level reviews on a PR.
func FetchReviews(ctx context.Context, repo string, prNum int) ([]Review, error) {
	data, err := ghcli.APIPaginate(ctx, fmt.Sprintf("repos/%s/pulls/%d/reviews", repo, prNum))
	if err != nil {
		return nil, fmt.Errorf("fetch reviews: %w", err)
	}
	return parseReviews(data)
}

// parseComments handles the gh api --paginate output which may be concatenated JSON arrays.
func parseComments(data []byte) ([]ReviewComment, error) {
	// Try parsing as a single array first
	var comments []ReviewComment
	if err := json.Unmarshal(data, &comments); err == nil {
		return comments, nil
	}
	// gh --paginate can concatenate multiple JSON arrays; try decoding sequentially
	dec := json.NewDecoder(jsonReader(data))
	var all []ReviewComment
	for dec.More() {
		var batch []ReviewComment
		if err := dec.Decode(&batch); err != nil {
			return nil, fmt.Errorf("parse comments: %w", err)
		}
		all = append(all, batch...)
	}
	return all, nil
}

func parseReviews(data []byte) ([]Review, error) {
	var reviews []Review
	if err := json.Unmarshal(data, &reviews); err == nil {
		return reviews, nil
	}
	dec := json.NewDecoder(jsonReader(data))
	var all []Review
	for dec.More() {
		var batch []Review
		if err := dec.Decode(&batch); err != nil {
			return nil, fmt.Errorf("parse reviews: %w", err)
		}
		all = append(all, batch...)
	}
	return all, nil
}

// FilterLatestReview filters comments and reviews to only the latest review round.
func FilterLatestReview(reviews []Review, comments []ReviewComment) ([]Review, []ReviewComment) {
	if len(reviews) == 0 {
		return reviews, comments
	}
	maxID := 0
	for _, r := range reviews {
		if r.ID > maxID {
			maxID = r.ID
		}
	}
	var filteredReviews []Review
	for _, r := range reviews {
		if r.ID == maxID {
			filteredReviews = append(filteredReviews, r)
		}
	}
	var filteredComments []ReviewComment
	for _, c := range comments {
		if c.PullRequestReviewID == maxID {
			filteredComments = append(filteredComments, c)
		}
	}
	return filteredReviews, filteredComments
}

// GetLatestCommentTimestamp returns the latest timestamp across all comments and reviews.
func GetLatestCommentTimestamp(ctx context.Context, repo string, prNum int) (string, error) {
	comments, err := FetchReviewComments(ctx, repo, prNum)
	if err != nil {
		comments = nil
	}
	reviews, err := FetchReviews(ctx, repo, prNum)
	if err != nil {
		reviews = nil
	}

	var maxTS string
	for _, c := range comments {
		ts := c.LatestTimestamp()
		if ts > maxTS {
			maxTS = ts
		}
	}
	for _, r := range reviews {
		if r.SubmittedAt > maxTS {
			maxTS = r.SubmittedAt
		}
	}
	return maxTS, nil
}

// NewComments holds new inline comments and top-level reviews since a given timestamp.
type NewComments struct {
	InlineComments  []ReviewComment `json:"inline_comments"`
	TopLevelReviews []Review        `json:"top_level_reviews"`
}

// FetchNewComments fetches comments and reviews newer than 'since'.
func FetchNewComments(ctx context.Context, repo string, prNum int, since string) (*NewComments, error) {
	comments, err := FetchReviewComments(ctx, repo, prNum)
	if err != nil {
		comments = nil
	}
	reviews, err := FetchReviews(ctx, repo, prNum)
	if err != nil {
		reviews = nil
	}

	var newComments []ReviewComment
	for _, c := range comments {
		if c.LatestTimestamp() > since {
			newComments = append(newComments, c)
		}
	}

	var newReviews []Review
	for _, r := range reviews {
		if r.SubmittedAt > since && r.Body != "" {
			newReviews = append(newReviews, r)
		}
	}

	if len(newComments) == 0 && len(newReviews) == 0 {
		return nil, nil
	}

	return &NewComments{
		InlineComments:  newComments,
		TopLevelReviews: newReviews,
	}, nil
}
