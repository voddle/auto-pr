package state

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// PRState represents the persisted state for a PR being watched.
type PRState struct {
	LastCommentTS string `json:"last_comment_ts"`
	PID           int    `json:"pid"`
	Branch        string `json:"branch"`
}

// ReadPR reads the state for a PR. Returns nil if not found.
func (d *Dir) ReadPR(num int) *PRState {
	path := filepath.Join(d.Root, "prs", fmt.Sprintf("%d.json", num))
	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	var s PRState
	if err := json.Unmarshal(data, &s); err != nil {
		return nil
	}
	return &s
}

// WritePR writes the state for a PR atomically.
func (d *Dir) WritePR(num int, s *PRState) error {
	path := filepath.Join(d.Root, "prs", fmt.Sprintf("%d.json", num))
	data, err := json.Marshal(s)
	if err != nil {
		return err
	}
	return atomicWrite(path, data)
}
