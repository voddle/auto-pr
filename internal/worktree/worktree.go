package worktree

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"

	"auto-pr/internal/github"
)

// Ensure creates or validates a git worktree.
// Returns the absolute path to the worktree.
func Ensure(projectRoot, worktreeDir, branch, name string) (string, error) {
	wtPath := filepath.Join(projectRoot, worktreeDir, name)

	if info, err := os.Stat(wtPath); err == nil && info.IsDir() {
		// Check if it's a valid worktree
		if isValidWorktree(wtPath) {
			fmt.Printf("[pr-watch] Worktree '%s' exists, pulling latest...\n", name)
			gitInDir(wtPath, "fetch", "origin", branch)
			if err := gitInDir(wtPath, "reset", "--hard", "origin/"+branch); err != nil {
				gitInDir(wtPath, "checkout", branch)
			}
			return wtPath, nil
		}
		// Corrupted — remove and recreate
		fmt.Printf("[pr-watch] Worktree '%s' corrupted, recreating...\n", name)
		gitInDir(projectRoot, "worktree", "remove", "--force", wtPath)
		os.RemoveAll(wtPath)
	}

	// Create new worktree
	fmt.Printf("[pr-watch] Creating worktree '%s' on branch '%s'...\n", name, branch)
	os.MkdirAll(filepath.Join(projectRoot, worktreeDir), 0755)

	if err := gitInDir(projectRoot, "worktree", "add", wtPath, branch); err != nil {
		// Branch might not exist locally — try fetching
		gitInDir(projectRoot, "fetch", "origin", branch)
		if err := gitInDir(projectRoot, "worktree", "add", wtPath, branch); err != nil {
			// Try creating/resetting branch from remote (-B forces if branch already exists)
			if err := gitInDir(projectRoot, "worktree", "add", "-B", branch, wtPath, "origin/"+branch); err != nil {
				return "", fmt.Errorf("failed to create worktree '%s': %w", name, err)
			}
		}
	}
	return wtPath, nil
}

// CreateForIssue creates a worktree for an issue, branching from the base branch.
func CreateForIssue(ctx context.Context, projectRoot, worktreeDir, repo string, issueNum int, baseBranch string) (string, error) {
	branch := fmt.Sprintf("auto/issue-%d", issueNum)

	if baseBranch == "" {
		var err error
		baseBranch, err = github.GetDefaultBranch(ctx, repo)
		if err != nil {
			baseBranch = "main"
		}
	}

	// Prune stale worktree references before creating new ones
	gitInDir(projectRoot, "worktree", "prune")

	// Fetch latest base
	gitInDir(projectRoot, "fetch", "origin", baseBranch)

	// Create branch from base (ignore error if already exists)
	gitInDir(projectRoot, "branch", branch, "origin/"+baseBranch)

	return Ensure(projectRoot, worktreeDir, branch, fmt.Sprintf("issue-%d", issueNum))
}

// Remove removes a worktree.
func Remove(projectRoot, wtPath string) error {
	if err := gitInDir(projectRoot, "worktree", "remove", "--force", wtPath); err != nil {
		return fmt.Errorf("could not remove worktree '%s': %w", wtPath, err)
	}
	return nil
}

func isValidWorktree(path string) bool {
	cmd := exec.Command("git", "-C", path, "rev-parse", "--git-dir")
	return cmd.Run() == nil
}

func gitInDir(dir string, args ...string) error {
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("git %v: %w (%s)", args, err, stderr.String())
	}
	return nil
}
