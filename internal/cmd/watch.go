package cmd

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"

	"auto-pr/internal/claude"
	"auto-pr/internal/config"
	"auto-pr/internal/container"
	"auto-pr/internal/ghcli"
	"auto-pr/internal/github"
	"auto-pr/internal/state"
	"auto-pr/internal/watch"
)

// RunWatch implements the "watch" subcommand.
func RunWatch(args []string) int {
	// Detect project root
	projectRoot, err := findProjectRoot()
	if err != nil {
		fmt.Fprintln(os.Stderr, "Error:", err)
		return 1
	}

	// Auto-generate default config if missing
	if config.GenerateDefault(projectRoot) {
		fmt.Println("[auto-pr] Generated default .pr-watch.conf (edit as needed)")
	}

	// Load config
	cfg := config.Load(projectRoot)

	fs := flag.NewFlagSet("watch", flag.ContinueOnError)
	repoMode := fs.Bool("repo", false, "Enable repo-level watching mode")
	intervalFlag := fs.Int("interval", 0, "Poll interval in seconds")
	maxConcurrentFlag := fs.Int("max-concurrent", 0, "Max concurrent worker processes")
	dockerFlag := fs.Bool("docker", false, "Run workers in Docker containers for isolation")
	once := fs.Bool("once", false, "Check once and exit")
	help := fs.Bool("help", false, "Show help")
	h := fs.Bool("h", false, "Show help")

	if err := fs.Parse(args); err != nil {
		return 1
	}

	if *help || *h {
		fmt.Println("Usage:")
		fmt.Println("  auto-pr watch [PR_NUMBER] [--interval N] [--once]")
		fmt.Println("      Single-PR mode: watch one PR (backward compatible)")
		fmt.Println()
		fmt.Println("  auto-pr watch --repo [--interval N] [--once] [--max-concurrent N]")
		fmt.Println("      Repo mode: watch all issues with worktree isolation (spawns workers)")
		fmt.Println()
		fmt.Println("Options:")
		fmt.Println("  --interval N        Poll interval in seconds (default: 30)")
		fmt.Println("  --max-concurrent N  Max concurrent worker processes (default: 2)")
		fmt.Println("  --docker            Run workers in Docker containers for isolation")
		fmt.Println("  --once              Check once and exit (for debugging)")
		fmt.Println("  --repo              Enable repo-level watching mode")
		fmt.Println("  --help, -h          Show this help")
		return 0
	}

	// CLI flags override config
	interval := cfg.Interval
	if *intervalFlag > 0 {
		interval = *intervalFlag
	}
	maxConcurrent := cfg.MaxConcurrent
	if *maxConcurrentFlag > 0 {
		maxConcurrent = *maxConcurrentFlag
	}

	// Determine Docker mode: CLI flag overrides config
	dockerEnabled := cfg.DockerEnabled || *dockerFlag

	// Detect tools
	if err := ghcli.Detect(); err != nil {
		fmt.Fprintln(os.Stderr, "Error:", err)
		return 1
	}
	if !dockerEnabled {
		// Only need claude CLI on host if not using Docker
		if err := claude.Detect(); err != nil {
			fmt.Fprintln(os.Stderr, "Error:", err)
			return 1
		}
	}

	// Detect Docker if enabled
	var dockerMgr *container.Manager
	if dockerEnabled {
		if err := container.Detect(); err != nil {
			fmt.Fprintln(os.Stderr, "Error:", err)
			return 1
		}
		dockerMgr = container.NewManager(cfg.DockerImage, projectRoot, cfg.DockerFile)
	}

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
	defer cancel()

	repo, err := ghcli.RepoSlug(ctx)
	if err != nil {
		fmt.Fprintln(os.Stderr, "Error:", err)
		return 1
	}

	// Initialize state directory
	stateDir := state.New(projectRoot)
	if err := stateDir.Init(); err != nil {
		fmt.Fprintln(os.Stderr, "Error initializing state:", err)
		return 1
	}

	// Ensure .gitignore covers state and worktree dirs
	state.EnsureGitignore(projectRoot, []string{
		".pr-watch-state/",
		cfg.WorktreeDir + "/",
	})

	if *repoMode {
		wcfg := watch.WorkerConfig{
			WorktreeDir:   cfg.WorktreeDir,
			BaseBranch:    cfg.BaseBranch,
			IssueLabels:   cfg.IssueLabels,
			DockerEnabled: dockerEnabled,
			DockerImage:   cfg.DockerImage,
		}
		err := watch.Repo(ctx, repo, projectRoot, interval, maxConcurrent, *once, wcfg, stateDir, dockerMgr)
		if err != nil && err != context.Canceled {
			fmt.Fprintln(os.Stderr, "Error:", err)
			return 1
		}
		return 0
	}

	// Single-PR mode
	prNum := 0
	for _, arg := range fs.Args() {
		n, err := strconv.Atoi(arg)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error: Unknown argument '%s'\n", arg)
			return 1
		}
		prNum = n
	}

	if prNum == 0 {
		branch, err := github.CurrentBranch()
		if err != nil {
			fmt.Fprintln(os.Stderr, "Error:", err)
			return 1
		}
		prNum, err = github.FindPRForBranch(ctx, repo, branch)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			return 1
		}
		fmt.Printf("Detected PR #%d for branch '%s'\n", prNum, branch)
	}

	err = watch.SinglePR(ctx, repo, projectRoot, prNum, interval, *once, stateDir, dockerMgr)
	if err != nil && err != context.Canceled {
		fmt.Fprintln(os.Stderr, "Error:", err)
		return 1
	}
	return 0
}

func findProjectRoot() (string, error) {
	// Use current working directory, then walk up to find .git
	dir, err := os.Getwd()
	if err != nil {
		return "", err
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, ".git")); err == nil {
			return dir, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	// Fallback to cwd
	return os.Getwd()
}
