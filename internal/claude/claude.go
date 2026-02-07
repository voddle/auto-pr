package claude

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"

	"auto-pr/internal/container"
)

var claudePath string

// Detect finds the claude CLI binary.
func Detect() error {
	p, err := exec.LookPath("claude")
	if err != nil {
		return fmt.Errorf("claude CLI not found. Ensure 'claude' is in PATH")
	}
	claudePath = p
	return nil
}

// Run executes "claude -p <prompt>" in the given directory.
// Output is written to both stdout and the provided writer (if non-nil).
func Run(ctx context.Context, dir, prompt string, logWriter io.Writer) error {
	args := []string{"-p", prompt, "--verbose"}
	cmd := exec.CommandContext(ctx, claudePath, args...)
	cmd.Dir = dir

	if logWriter != nil {
		cmd.Stdout = io.MultiWriter(os.Stdout, logWriter)
		cmd.Stderr = io.MultiWriter(os.Stderr, logWriter)
	} else {
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
	}

	return cmd.Run()
}

// RunContinue executes "claude -p <prompt> --continue" in the given directory.
// This continues the most recent conversation in that directory.
func RunContinue(ctx context.Context, dir, prompt string, logWriter io.Writer) error {
	args := []string{"-p", prompt, "--continue", "--verbose"}
	cmd := exec.CommandContext(ctx, claudePath, args...)
	cmd.Dir = dir

	if logWriter != nil {
		cmd.Stdout = io.MultiWriter(os.Stdout, logWriter)
		cmd.Stderr = io.MultiWriter(os.Stderr, logWriter)
	} else {
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
	}

	return cmd.Run()
}

// RunInContainer executes "claude -p <prompt>" inside a Docker container.
func RunInContainer(ctx context.Context, mgr *container.Manager, containerID, workDir, prompt string, logWriter io.Writer) error {
	return mgr.Exec(ctx, containerID, workDir, []string{"claude", "-p", prompt, "--verbose"}, logWriter)
}

// RunContinueInContainer executes "claude -p <prompt> --continue" inside a Docker container.
func RunContinueInContainer(ctx context.Context, mgr *container.Manager, containerID, workDir, prompt string, logWriter io.Writer) error {
	return mgr.Exec(ctx, containerID, workDir, []string{"claude", "-p", prompt, "--continue", "--verbose"}, logWriter)
}
