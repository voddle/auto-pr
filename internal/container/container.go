package container

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"auto-pr/internal/ghcli"
)

// defaultDockerfile is embedded into the binary as a fallback when no external
// Dockerfile is found.  It provides a fat development environment with common
// toolchains so Claude Code can build most projects out of the box.
const defaultDockerfile = `FROM ubuntu:22.04

ENV DEBIAN_FRONTEND=noninteractive

# Base tools
RUN apt-get update && apt-get install -y \
    git curl wget jq unzip \
    build-essential pkg-config \
    ca-certificates gnupg lsb-release \
    software-properties-common \
    && rm -rf /var/lib/apt/lists/*

# Python 3 + pip + venv
RUN apt-get update && apt-get install -y \
    python3 python3-pip python3-venv \
    && rm -rf /var/lib/apt/lists/*

# Node.js 20 (for claude CLI and JS/TS projects)
RUN curl -fsSL https://deb.nodesource.com/setup_20.x | bash - \
    && apt-get install -y nodejs \
    && rm -rf /var/lib/apt/lists/*

# Go 1.22
RUN curl -fsSL https://go.dev/dl/go1.22.5.linux-amd64.tar.gz \
    | tar -C /usr/local -xzf -
ENV PATH="/usr/local/go/bin:/root/go/bin:${PATH}"

# Rust (via rustup, minimal profile)
RUN curl --proto '=https' --tlsv1.2 -sSf https://sh.rustup.rs \
    | sh -s -- -y --default-toolchain stable --profile minimal
ENV PATH="/root/.cargo/bin:${PATH}"

# gh CLI
RUN curl -fsSL https://cli.github.com/packages/githubcli-archive-keyring.gpg \
    | dd of=/usr/share/keyrings/githubcli-archive-keyring.gpg \
    && chmod go+r /usr/share/keyrings/githubcli-archive-keyring.gpg \
    && echo "deb [arch=$(dpkg --print-architecture) signed-by=/usr/share/keyrings/githubcli-archive-keyring.gpg] https://cli.github.com/packages stable main" \
    | tee /etc/apt/sources.list.d/github-cli.list > /dev/null \
    && apt-get update && apt-get install -y gh \
    && rm -rf /var/lib/apt/lists/*

# Claude CLI
RUN npm install -g @anthropic-ai/claude-code

WORKDIR /workspace
`

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
	ImageName      string
	ProjectRoot    string
	DockerfilePath string // optional: explicit Dockerfile path from config
}

// NewManager creates a new container manager.
func NewManager(imageName, projectRoot, dockerfilePath string) *Manager {
	return &Manager{
		ImageName:      imageName,
		ProjectRoot:    projectRoot,
		DockerfilePath: dockerfilePath,
	}
}

// resolveDockerfile determines which Dockerfile to use in priority order:
//  1. Manager.DockerfilePath (from DOCKER_FILE config)
//  2. {projectRoot}/Dockerfile.autopr
//  3. Embedded default written to a temp file
//
// Returns the path and whether it's a temp file that the caller should remove.
func (m *Manager) resolveDockerfile() (path string, isTempFile bool, err error) {
	// 1. Explicit config path
	if m.DockerfilePath != "" {
		if _, err := os.Stat(m.DockerfilePath); err != nil {
			return "", false, fmt.Errorf("configured DOCKER_FILE not found: %s", m.DockerfilePath)
		}
		return m.DockerfilePath, false, nil
	}

	// 2. Project-local Dockerfile.autopr
	autoprPath := filepath.Join(m.ProjectRoot, "Dockerfile.autopr")
	if _, err := os.Stat(autoprPath); err == nil {
		return autoprPath, false, nil
	}

	// 3. Embedded default → temp file
	tmp, err := os.CreateTemp("", "auto-pr-dockerfile-*")
	if err != nil {
		return "", false, fmt.Errorf("failed to create temp Dockerfile: %w", err)
	}
	if _, err := tmp.WriteString(defaultDockerfile); err != nil {
		tmp.Close()
		os.Remove(tmp.Name())
		return "", false, fmt.Errorf("failed to write temp Dockerfile: %w", err)
	}
	tmp.Close()
	return tmp.Name(), true, nil
}

// EnsureImage checks if the Docker image exists; if not, builds it using
// the resolved Dockerfile (config path → Dockerfile.autopr → embedded default).
func (m *Manager) EnsureImage(ctx context.Context) error {
	// Check if image already exists
	cmd := exec.CommandContext(ctx, dockerPath, "image", "inspect", m.ImageName)
	if err := cmd.Run(); err == nil {
		return nil // image exists
	}

	dockerfilePath, isTmp, err := m.resolveDockerfile()
	if err != nil {
		return err
	}
	if isTmp {
		defer os.Remove(dockerfilePath)
	}

	fmt.Printf("[docker] Building image %s from %s...\n", m.ImageName, dockerfilePath)
	cmd = exec.CommandContext(ctx, dockerPath, "build", "-t", m.ImageName, "-f", dockerfilePath, ".")
	cmd.Dir = filepath.Dir(dockerfilePath)
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

	// Mount host ~/.claude/ into container so subscription login session is inherited
	if claudeDir := claudeConfigDir(); claudeDir != "" {
		args = append(args, "-v", claudeDir+":/root/.claude")
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

// claudeConfigDir returns the path to ~/.claude/ if it exists, or empty string.
func claudeConfigDir() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	dir := filepath.Join(home, ".claude")
	if info, err := os.Stat(dir); err == nil && info.IsDir() {
		return dir
	}
	return ""
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
