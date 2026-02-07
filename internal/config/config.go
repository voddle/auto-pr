package config

import (
	"bufio"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// Config holds pr-watch configuration.
type Config struct {
	MaxConcurrent int
	Interval      int
	IssueLabels   string
	WorktreeDir   string
	BaseBranch    string
	DockerEnabled bool
	DockerImage   string
}

// DefaultConfig returns the default configuration.
func DefaultConfig() Config {
	return Config{
		MaxConcurrent: 2,
		Interval:      30,
		IssueLabels:   "auto,claude",
		WorktreeDir:   ".worktrees",
		BaseBranch:    "",
		DockerEnabled: false,
		DockerImage:   "auto-pr-worker",
	}
}

// Load reads .pr-watch.conf from projectRoot and returns the config.
// Missing file is not an error; defaults are used.
func Load(projectRoot string) Config {
	cfg := DefaultConfig()

	f, err := os.Open(filepath.Join(projectRoot, ".pr-watch.conf"))
	if err != nil {
		return cfg
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		idx := strings.Index(line, "=")
		if idx < 0 {
			continue
		}
		key := strings.TrimSpace(line[:idx])
		val := strings.TrimSpace(line[idx+1:])
		// Strip surrounding quotes
		val = strings.Trim(val, `"'`)

		switch key {
		case "MAX_CONCURRENT":
			if n, err := strconv.Atoi(val); err == nil && n > 0 {
				cfg.MaxConcurrent = n
			}
		case "INTERVAL":
			if n, err := strconv.Atoi(val); err == nil && n > 0 {
				cfg.Interval = n
			}
		case "ISSUE_LABELS":
			cfg.IssueLabels = val
		case "WORKTREE_DIR":
			cfg.WorktreeDir = val
		case "BASE_BRANCH":
			cfg.BaseBranch = val
		case "DOCKER":
			cfg.DockerEnabled = val == "true" || val == "1" || val == "yes"
		case "DOCKER_IMAGE":
			if val != "" {
				cfg.DockerImage = val
			}
		}
	}
	return cfg
}
