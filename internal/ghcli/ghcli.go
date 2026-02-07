package ghcli

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"time"
)

// DefaultTimeout for gh CLI commands.
const DefaultTimeout = 30 * time.Second

var ghPath string

// Detect finds the gh CLI binary and returns an error if not found.
func Detect() error {
	// Check PATH first
	if p, err := exec.LookPath("gh"); err == nil {
		ghPath = p
		return nil
	}

	// Windows-specific paths
	if runtime.GOOS == "windows" {
		candidates := []string{
			`C:\Program Files\GitHub CLI\gh.exe`,
			`C:\Program Files (x86)\GitHub CLI\gh.exe`,
		}
		for _, c := range candidates {
			if _, err := os.Stat(c); err == nil {
				ghPath = c
				return nil
			}
		}
	}

	return fmt.Errorf("gh CLI not found. Install from https://cli.github.com")
}

// Path returns the detected gh binary path.
func Path() string {
	return ghPath
}

// Run executes a gh command with the given arguments and returns stdout.
func Run(ctx context.Context, args ...string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, DefaultTimeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, ghPath, args...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("gh %s: %w\n%s", strings.Join(args, " "), err, stderr.String())
	}
	return stdout.Bytes(), nil
}

// RunWithStdin executes a gh command with stdin input.
func RunWithStdin(ctx context.Context, stdin []byte, args ...string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, DefaultTimeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, ghPath, args...)
	cmd.Stdin = bytes.NewReader(stdin)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("gh %s: %w\n%s", strings.Join(args, " "), err, stderr.String())
	}
	return stdout.Bytes(), nil
}

// API calls gh api with the given endpoint and options.
func API(ctx context.Context, endpoint string, opts ...string) ([]byte, error) {
	args := append([]string{"api", endpoint}, opts...)
	return Run(ctx, args...)
}

// APIPaginate calls gh api with --paginate.
func APIPaginate(ctx context.Context, endpoint string, opts ...string) ([]byte, error) {
	args := append([]string{"api", endpoint, "--paginate"}, opts...)
	return Run(ctx, args...)
}

// APITyped calls gh api and unmarshals the JSON response into v.
func APITyped(ctx context.Context, endpoint string, v interface{}, opts ...string) error {
	data, err := API(ctx, endpoint, opts...)
	if err != nil {
		return err
	}
	return json.Unmarshal(data, v)
}

// APIPaginateTyped calls gh api with --paginate and unmarshals.
func APIPaginateTyped(ctx context.Context, endpoint string, v interface{}, opts ...string) error {
	data, err := APIPaginate(ctx, endpoint, opts...)
	if err != nil {
		return err
	}
	return json.Unmarshal(data, v)
}

// RepoSlug returns the "owner/repo" for the current repository.
func RepoSlug(ctx context.Context) (string, error) {
	data, err := Run(ctx, "repo", "view", "--json", "nameWithOwner", "--jq", ".nameWithOwner")
	if err != nil {
		return "", fmt.Errorf("not inside a GitHub repository: %w", err)
	}
	return strings.TrimSpace(string(data)), nil
}
