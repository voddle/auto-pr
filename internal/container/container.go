package container

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"

	"auto-pr/internal/ghcli"
)

var dockerPath string

// Detect checks whether the docker CLI is available.
func Detect() error {
	p, err := exec.LookPath("docker")
	if err != nil {
		return fmt.Errorf("docker CLI not found. Install Docker Desktop from https://www.docker.com")
	}
	dockerPath = p
	return nil
}

// Manager manages Docker containers for worker isolation.
type Manager struct {
	ImageName   string
	ProjectRoot string
}

// NewManager creates a new container manager.
func NewManager(imageName, projectRoot string) *Manager {
	return &Manager{
		ImageName:   imageName,
		ProjectRoot: projectRoot,
	}
}

// EnsureImage checks if the Docker image exists; if not, builds it from the Dockerfile.
func (m *Manager) EnsureImage(ctx context.Context, dockerfilePath string) error {
	// Check if image already exists
	cmd := exec.CommandContext(ctx, dockerPath, "image", "inspect", m.ImageName)
	if err := cmd.Run(); err == nil {
		return nil // image exists
	}

	fmt.Printf("[docker] Building image %s from %s...\n", m.ImageName, dockerfilePath)
	cmd = exec.CommandContext(ctx, dockerPath, "build", "-t", m.ImageName, "-f", dockerfilePath, m.ProjectRoot)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("docker build failed: %w", err)
	}
	fmt.Printf("[docker] Image %s built successfully.\n", m.ImageName)
	return nil
}

// Start launches a long-running container (sleep infinity) with the project root bind-mounted.
// Returns the container ID.
func (m *Manager) Start(ctx context.Context, name string, env map[string]string) (string, error) {
	// Remove any existing container with the same name (leftover from previous run)
	stopCmd := exec.CommandContext(ctx, dockerPath, "rm", "-f", name)
	stopCmd.Run() // ignore error — container may not exist

	args := []string{
		"run", "-d",
		"--name", name,
		"-v", m.ProjectRoot + ":/workspace",
	}
	for k, v := range env {
		args = append(args, "-e", k+"="+v)
	}
	args = append(args, m.ImageName, "sleep", "infinity")

	cmd := exec.CommandContext(ctx, dockerPath, args...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("docker run failed: %w\n%s", err, stderr.String())
	}

	containerID := strings.TrimSpace(stdout.String())
	fmt.Printf("[docker] Started container %s (id: %.12s)\n", name, containerID)
	return containerID, nil
}

// Exec runs a command inside a running container, streaming output to logWriter.
func (m *Manager) Exec(ctx context.Context, containerID, workDir string, cmdArgs []string, logWriter io.Writer) error {
	args := []string{"exec"}
	if workDir != "" {
		args = append(args, "-w", workDir)
	}
	args = append(args, containerID)
	args = append(args, cmdArgs...)

	cmd := exec.CommandContext(ctx, dockerPath, args...)

	if logWriter != nil {
		cmd.Stdout = io.MultiWriter(os.Stdout, logWriter)
		cmd.Stderr = io.MultiWriter(os.Stderr, logWriter)
	} else {
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
	}

	return cmd.Run()
}

// Stop stops and removes a container.
func (m *Manager) Stop(ctx context.Context, containerID string) error {
	cmd := exec.CommandContext(ctx, dockerPath, "stop", containerID)
	cmd.Run() // best-effort stop

	cmd = exec.CommandContext(ctx, dockerPath, "rm", "-f", containerID)
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("docker rm failed: %w", err)
	}
	return nil
}

// IsRunning checks if a container is currently running.
func (m *Manager) IsRunning(ctx context.Context, containerID string) bool {
	cmd := exec.CommandContext(ctx, dockerPath, "inspect", "-f", "{{.State.Running}}", containerID)
	var stdout bytes.Buffer
	cmd.Stdout = &stdout
	if err := cmd.Run(); err != nil {
		return false
	}
	return strings.TrimSpace(stdout.String()) == "true"
}

// GetWorkerEnv collects environment variables needed inside the container.
func GetWorkerEnv() map[string]string {
	env := map[string]string{}

	// Anthropic API key
	if key := os.Getenv("ANTHROPIC_API_KEY"); key != "" {
		env["ANTHROPIC_API_KEY"] = key
	}

	// GitHub token: prefer GH_TOKEN env var, fall back to gh auth token
	if token := os.Getenv("GH_TOKEN"); token != "" {
		env["GH_TOKEN"] = token
	} else if token := os.Getenv("GITHUB_TOKEN"); token != "" {
		env["GH_TOKEN"] = token
	} else {
		out, err := exec.Command(ghcli.Path(), "auth", "token").Output()
		if err == nil {
			env["GH_TOKEN"] = strings.TrimSpace(string(out))
		}
	}

	return env
}
