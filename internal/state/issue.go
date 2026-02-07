package state

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// IssueStatus represents the lifecycle status of an issue.
type IssueStatus string

const (
	IssuePreexisting IssueStatus = "preexisting"
	IssueInProgress  IssueStatus = "in_progress"
	IssueWatching    IssueStatus = "watching"
	IssueDone        IssueStatus = "done"
	IssueFailed      IssueStatus = "failed"
)

// IssueState represents the persisted state for an issue.
type IssueState struct {
	Status   IssueStatus `json:"status"`
	PID      int         `json:"pid"`
	Branch   string      `json:"branch"`
	PRNumber int         `json:"pr_number"`
}

// ReadIssue reads the state for an issue. Returns nil if not found.
func (d *Dir) ReadIssue(num int) *IssueState {
	path := filepath.Join(d.Root, "issues", fmt.Sprintf("%d.json", num))
	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	var s IssueState
	if err := json.Unmarshal(data, &s); err != nil {
		return nil
	}
	return &s
}

// WriteIssue writes the state for an issue atomically.
func (d *Dir) WriteIssue(num int, s *IssueState) error {
	path := filepath.Join(d.Root, "issues", fmt.Sprintf("%d.json", num))
	data, err := json.Marshal(s)
	if err != nil {
		return err
	}
	return atomicWrite(path, data)
}
