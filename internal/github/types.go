package github

import "strconv"

// User represents a GitHub user.
type User struct {
	Login string `json:"login"`
}

// ReviewComment represents an inline (line-level) PR comment.
type ReviewComment struct {
	ID                  int    `json:"id"`
	Path                string `json:"path"`
	Line                *int   `json:"line"`
	OriginalLine        *int   `json:"original_line"`
	Body                string `json:"body"`
	User                User   `json:"user"`
	CreatedAt           string `json:"created_at"`
	UpdatedAt           string `json:"updated_at"`
	PullRequestReviewID int    `json:"pull_request_review_id"`
}

// LineDisplay returns the best available line number as a string.
func (c *ReviewComment) LineDisplay() string {
	if c.Line != nil {
		return itoa(*c.Line)
	}
	if c.OriginalLine != nil {
		return itoa(*c.OriginalLine)
	}
	return "?"
}

// LatestTimestamp returns the most recent timestamp for this comment.
func (c *ReviewComment) LatestTimestamp() string {
	if c.UpdatedAt != "" {
		return c.UpdatedAt
	}
	return c.CreatedAt
}

// Review represents a top-level PR review.
type Review struct {
	ID          int    `json:"id"`
	State       string `json:"state"`
	Body        string `json:"body"`
	User        User   `json:"user"`
	SubmittedAt string `json:"submitted_at"`
}

// Issue represents a GitHub issue.
type Issue struct {
	Number      int    `json:"number"`
	Title       string `json:"title"`
	Body        string `json:"body"`
	State       string `json:"state"`
	PullRequest *struct {
		URL string `json:"url"`
	} `json:"pull_request"`
}

// PullRequest represents a GitHub pull request.
type PullRequest struct {
	Number int    `json:"number"`
	State  string `json:"state"`
	Head   struct {
		Ref string `json:"ref"`
	} `json:"head"`
}

// ReplyResponse represents the response from posting a comment reply.
type ReplyResponse struct {
	ID   int  `json:"id"`
	User User `json:"user"`
}

// RepoInfo represents basic repository information.
type RepoInfo struct {
	DefaultBranch string `json:"default_branch"`
}

func itoa(n int) string {
	return strconv.Itoa(n)
}
