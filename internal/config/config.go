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
	DockerFile    string // explicit Dockerfile path (DOCKER_FILE config key)
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

const defaultConfTemplate = `# auto-pr watch configuration
# Uncomment and edit values as needed. Defaults are shown.

# Max concurrent worker processes
# MAX_CONCURRENT=2

# Poll interval in seconds
# INTERVAL=30

# Issue labels that trigger auto-processing (comma-separated, OR logic)
# ISSUE_LABELS="auto,claude"

# Directory for git worktrees
# WORKTREE_DIR=".worktrees"

# Base branch for new issue branches (default: repo default branch)
# BASE_BRANCH="main"

# Enable Docker container isolation (true/false)
# DOCKER=false

# Docker image name for worker containers
# DOCKER_IMAGE="auto-pr-worker"

# Custom Dockerfile path (default: auto-resolve)
# Lookup order: DOCKER_FILE -> {repo}/Dockerfile.autopr -> embedded default
# DOCKER_FILE=""
`

// GenerateDefault creates a .pr-watch.conf with commented-out defaults
// if the file does not already exist. Returns true if a file was created.
func GenerateDefault(projectRoot string) bool {
	path := filepath.Join(projectRoot, ".pr-watch.conf")
	if _, err := os.Stat(path); err == nil {
		return false // already exists
	}
	os.WriteFile(path, []byte(defaultConfTemplate), 0644)
	return true
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
		// Strip inline comments and surrounding quotes
		if len(val) > 0 && (val[0] == '"' || val[0] == '\'') {
			q := val[0]
			if end := strings.IndexByte(val[1:], q); end >= 0 {
				val = val[1 : end+1]
			} else {
				val = strings.Trim(val, `"'`)
			}
		} else if i := strings.Index(val, "#"); i > 0 {
			val = strings.TrimSpace(val[:i])
		}

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
		case "DOCKER_FILE":
			cfg.DockerFile = val
		}
	}
	return cfg
}
