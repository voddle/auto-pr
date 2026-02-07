package worktree

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

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
	fixWorktreeRelPaths(wtPath)
	return wtPath, nil
}

// fixWorktreeRelPaths rewrites the .git pointer file in a worktree and the
// corresponding gitdir file in the main repo to use relative paths. This is
// necessary for Docker mode: the project root is bind-mounted into the
// container at a different absolute path, but relative paths work in both
// the host and the container since the directory structure is identical.
func fixWorktreeRelPaths(wtPath string) {
	// 1. Read {wtPath}/.git — should contain "gitdir: <abs-path>"
	dotGitPath := filepath.Join(wtPath, ".git")
	data, err := os.ReadFile(dotGitPath)
	if err != nil {
		return
	}
	content := strings.TrimSpace(string(data))
	if !strings.HasPrefix(content, "gitdir: ") {
		return
	}
	gitdirTarget := strings.TrimPrefix(content, "gitdir: ")

	// Normalise to OS path separators for filepath.Rel
	gitdirTarget = filepath.FromSlash(gitdirTarget)

	// Only fix if the path is absolute
	if filepath.IsAbs(gitdirTarget) {
		rel, err := filepath.Rel(wtPath, gitdirTarget)
		if err == nil {
			// Git requires forward slashes
			rel = filepath.ToSlash(rel)
			os.WriteFile(dotGitPath, []byte("gitdir: "+rel+"\n"), 0644)
		}
	}

	// 2. Read {gitdirTarget}/gitdir — should contain the abs path back to wtPath/.git
	//    Use the original (possibly absolute) target so we can find the file
	gitdirFile := filepath.Join(filepath.FromSlash(gitdirTarget), "gitdir")
	data2, err := os.ReadFile(gitdirFile)
	if err != nil {
		return
	}
	backPointer := strings.TrimSpace(string(data2))
	backPointer = filepath.FromSlash(backPointer)

	if filepath.IsAbs(backPointer) {
		gitdirDir := filepath.Dir(gitdirFile)
		rel, err := filepath.Rel(gitdirDir, backPointer)
		if err == nil {
			rel = filepath.ToSlash(rel)
			os.WriteFile(gitdirFile, []byte(rel+"\n"), 0644)
		}
	}
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
