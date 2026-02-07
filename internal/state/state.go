package state

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Dir manages the .pr-watch-state directory.
type Dir struct {
	Root string // e.g., /project/.pr-watch-state
}

// New creates a Dir for the given project root.
func New(projectRoot string) *Dir {
	return &Dir{Root: filepath.Join(projectRoot, ".pr-watch-state")}
}

// Init creates the state directory structure and migrates old format if needed.
func (d *Dir) Init() error {
	if err := d.migrateOldState(); err != nil {
		fmt.Fprintf(os.Stderr, "[pr-watch] Warning: migration failed: %v\n", err)
	}

	dirs := []string{
		filepath.Join(d.Root, "issues"),
		filepath.Join(d.Root, "prs"),
		filepath.Join(d.Root, "logs"),
	}
	for _, dir := range dirs {
		if err := os.MkdirAll(dir, 0755); err != nil {
			return fmt.Errorf("create state dir %s: %w", dir, err)
		}
	}
	return nil
}

// migrateOldState handles migration from old flat-file format.
func (d *Dir) migrateOldState() error {
	info, err := os.Stat(d.Root)
	if err != nil {
		return nil // doesn't exist, nothing to migrate
	}
	if info.IsDir() {
		return nil // already a directory
	}

	// Old format: flat file with lines of "PR_NUMBER_TIMESTAMP"
	fmt.Println("[pr-watch] Migrating old state file to directory structure...")
	content, err := os.ReadFile(d.Root)
	if err != nil {
		return err
	}

	if err := os.Remove(d.Root); err != nil {
		return fmt.Errorf("remove old state file: %w", err)
	}

	prsDir := filepath.Join(d.Root, "prs")
	if err := os.MkdirAll(prsDir, 0755); err != nil {
		return err
	}

	scanner := bufio.NewScanner(strings.NewReader(string(content)))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		idx := strings.Index(line, "_")
		if idx < 0 {
			continue
		}
		prNum := line[:idx]
		ts := line[idx+1:]
		if prNum == "" || ts == "" {
			continue
		}
		state := PRState{
			LastCommentTS: ts,
			PID:           0,
			Branch:        "",
		}
		data, _ := json.Marshal(state)
		if err := os.WriteFile(filepath.Join(prsDir, prNum+".json"), data, 0644); err != nil {
			fmt.Fprintf(os.Stderr, "[pr-watch] Warning: could not migrate PR %s: %v\n", prNum, err)
		}
	}
	fmt.Println("[pr-watch] Migration complete.")
	return nil
}

// IsInitialized returns true if the first scan has been completed.
func (d *Dir) IsInitialized() bool {
	_, err := os.Stat(filepath.Join(d.Root, ".initialized"))
	return err == nil
}

// MarkInitialized creates the .initialized sentinel file.
func (d *Dir) MarkInitialized() error {
	return os.WriteFile(filepath.Join(d.Root, ".initialized"), []byte(""), 0644)
}

// LogPath returns the log file path for an issue worker.
func (d *Dir) LogPath(issueNum int) string {
	return filepath.Join(d.Root, "logs", fmt.Sprintf("issue-%d.log", issueNum))
}

// EnsureGitignore appends entries to .gitignore if they are not already present.
func EnsureGitignore(projectRoot string, entries []string) {
	gitignorePath := filepath.Join(projectRoot, ".gitignore")

	existing := ""
	if data, err := os.ReadFile(gitignorePath); err == nil {
		existing = string(data)
	}

	var toAdd []string
	for _, entry := range entries {
		// Check if line is already present (exact line match)
		found := false
		for _, line := range strings.Split(existing, "\n") {
			if strings.TrimSpace(line) == entry {
				found = true
				break
			}
		}
		if !found {
			toAdd = append(toAdd, entry)
		}
	}

	if len(toAdd) == 0 {
		return
	}

	f, err := os.OpenFile(gitignorePath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return
	}
	defer f.Close()

	// Ensure we start on a new line
	if existing != "" && !strings.HasSuffix(existing, "\n") {
		f.WriteString("\n")
	}

	f.WriteString("\n# auto-pr state (auto-generated)\n")
	for _, entry := range toAdd {
		f.WriteString(entry + "\n")
	}
}

// atomicWrite writes data to a file atomically using a temp file + rename.
func atomicWrite(path string, data []byte) error {
	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, ".tmp-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()

	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		os.Remove(tmpName)
		return err
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmpName)
		return err
	}
	if err := os.Rename(tmpName, path); err != nil {
		os.Remove(tmpName)
		return err
	}
	return nil
}
