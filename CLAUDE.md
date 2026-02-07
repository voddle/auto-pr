# Project Workflow — GitHub PR Review

## Overview

This project provides a single Go binary (`auto-pr`) for reviewing and replying to GitHub PR comments locally via Claude Code, with support for automated issue implementation and repo-level watching.

**Build:** `go build -o auto-pr.exe .` (or `go build -o auto-pr .` on Linux/macOS)

## Commands

| Command | Purpose |
|---------|---------|
| `auto-pr reviews` | Read PR review comments |
| `auto-pr reply` | Reply to PR review comments |
| `auto-pr watch` | Auto-watch PR/repo for new reviews and issues, process them |

## Workflow

1. **Read reviews** — Run `auto-pr reviews` to see all review comments on the current branch's PR.
2. **Address feedback** — Edit code based on the review comments.
3. **Reply to reviewers** — Use `auto-pr reply <comment_id> "message"` to respond.
4. **Push changes** — Commit and push to update the PR.

## Quick Reference

```bash
# See reviews for current branch's PR
auto-pr reviews

# See reviews for a specific PR
auto-pr reviews 123

# Only latest review round
auto-pr reviews --latest

# Raw JSON output
auto-pr reviews --json

# List comment IDs you can reply to
auto-pr reply --list

# Reply to a specific comment
auto-pr reply <comment_id> "Fixed in latest commit"
```

## Automated Watch Mode

`auto-pr watch` supports two modes: **Single-PR mode** (default) and **Repo mode** (worker-based).

### Single-PR Mode (default)

Watches one PR for new review comments and processes them synchronously in the foreground.

```bash
# Watch current branch's PR (auto-detect)
auto-pr watch

# Watch a specific PR
auto-pr watch 123

# Custom poll interval (60 seconds)
auto-pr watch --interval 60

# Single check, no loop (for debugging)
auto-pr watch --once
```

**How it works:**
1. On first run, it snapshots existing comments to avoid re-processing history
2. Every 30 seconds (configurable), it checks for new inline comments and top-level reviews
3. When new comments are found, it calls `claude -p` with the comment details
4. Claude Code reads the relevant files, makes changes, commits, pushes, and replies to each comment
5. The loop continues until you stop it (Ctrl+C)

### Repo Mode (worker-based)

Watches the entire repo for new issues (with configured labels). Each issue gets a dedicated **worker goroutine** that runs in its own worktree, implements the issue, creates a PR, and then watches that PR for reviews — all with continuous context via `claude -p --continue`.

```bash
# Watch entire repo
auto-pr watch --repo

# Watch with custom settings
auto-pr watch --repo --interval 60 --max-concurrent 4

# Single scan, no loop (for debugging)
auto-pr watch --repo --once
```

**How it works:**
1. On first run, it snapshots all existing issues as "pre-existing" (skipped)
2. Each poll cycle, the main goroutine:
   - Monitors worker status (detects completed/failed workers)
   - Cleans up worktrees for closed issues
   - Scans for new issues with configured labels → spawns worker goroutines
3. Concurrency is limited to `MAX_CONCURRENT` simultaneous workers (semaphore channel)
4. The loop continues until you stop it (Ctrl+C); all workers are cancelled on exit via context

**Worker lifecycle** (one per issue):

| Phase | What happens |
|-------|-------------|
| **Phase 1: Implement** | Worker creates branch + worktree, calls `claude -p` to implement the issue, pushes, and creates a PR |
| **Phase 2: Watch reviews** | Worker polls for new review comments on its PR, handles them with `claude -p --continue` (preserving context from Phase 1) |
| **Exit** | When the PR is merged or closed, the worker exits cleanly |

**Context continuity:** `claude -p --continue` is directory-scoped ("continue the most recent conversation in the current directory"). Since each worker runs in its own worktree directory, context is naturally isolated per issue. The Claude session remembers the code it wrote in Phase 1 when handling reviews in Phase 2.

**Worker logs:** Each worker's output is written to `.pr-watch-state/logs/issue-N.log`.

## Docker Container Isolation

Workers can optionally run inside Docker containers for process, network, and environment isolation. This prevents port conflicts, process interference, and environment pollution when multiple workers run concurrently.

```bash
# Repo mode with Docker isolation
auto-pr watch --repo --docker

# Single-PR mode with Docker isolation
auto-pr watch --docker
```

**How it works:**
- Each worker gets its own Docker container (started on demand, stopped on exit)
- The project root is bind-mounted at `/workspace` inside the container
- `GH_TOKEN` and `ANTHROPIC_API_KEY` are passed as environment variables
- `claude -p --continue` session continuity works because each worktree directory is unique
- Without `--docker`, behavior is identical to before (backward compatible)

**Dockerfile resolution order** (first match wins):
1. `DOCKER_FILE=/path/to/Dockerfile` in `.pr-watch.conf` — explicit path
2. `{projectRoot}/Dockerfile.autopr` — project-specific customization
3. Embedded default — fat dev image with Go, Python, Node.js, Rust, gh, claude CLI

The embedded default image provides a comprehensive development environment (~2.5GB) so workers can build most projects out of the box. To customize, place a `Dockerfile.autopr` in the target repo root.

**Prerequisites for Docker mode:**
- Docker Desktop installed and running
- The `docker` CLI in PATH

## Configuration

`auto-pr watch --repo` reads settings from `.pr-watch.conf` in the project root:

```bash
MAX_CONCURRENT=2          # Max concurrent claude processes
INTERVAL=30               # Poll interval (seconds)
ISSUE_LABELS="auto,claude" # Issue labels that trigger auto-processing (comma-separated, OR logic)
WORKTREE_DIR=".worktrees"  # Worktree directory
# BASE_BRANCH="main"      # Base branch for new issue branches (default: repo default branch)
DOCKER=false              # Enable Docker container isolation (true/false)
DOCKER_IMAGE="auto-pr-worker"  # Docker image name for worker containers
# DOCKER_FILE="/path/to/Dockerfile"  # Custom Dockerfile path (default: auto-resolve)
```

CLI flags (`--interval`, `--max-concurrent`, `--docker`) override config file values.

## State Management

State is stored in `.pr-watch-state/` (directory, git-ignored):

```
.pr-watch-state/
  .initialized              # Sentinel: first scan completed
  issues/
    42.json                  # {"status":"in_progress|watching|done|failed|preexisting","branch":"auto/issue-42","pr_number":99}
  prs/
    101.json                 # {"last_comment_ts":"2026-...","branch":"feature-x"}
  logs/
    issue-42.log             # Worker stdout/stderr for issue #42
```

Issue status lifecycle: `preexisting` (skipped) | `in_progress` (Phase 1) → `watching` (Phase 2, PR created) → `done` (PR merged/closed) | `failed` (error).

Old flat-file `.pr-watch-state` is automatically migrated on first run.

## Editing Scope Rules

When processing PR review comments (via `auto-pr watch` or manually), you MUST follow these rules:

1. **Only modify files explicitly mentioned in the review comments** — the `path` field of inline comments defines your editing scope. Do NOT edit any file that is not referenced by a review comment.
2. **Only change code related to the reviewer's feedback** — do not refactor, reformat, or "improve" surrounding code beyond what the reviewer requested.
3. **Never modify project infrastructure files** — do not edit `CLAUDE.md`, `.claude/`, `internal/`, `main.go`, `.gitignore`, or any CI/CD config unless a reviewer explicitly asks for it.
4. **If a review comment is ambiguous or requests changes to files not in the PR**, reply to the comment asking for clarification instead of guessing.

## Project Structure

```
auto-pr/
  go.mod
  main.go                       # Entry point, subcommand dispatch
  Dockerfile.example             # Example Dockerfile for reference (embedded default is used at runtime)
  internal/
    ghcli/ghcli.go              # gh CLI detection + execution wrapper
    config/config.go            # .pr-watch.conf parsing + CLI flag merging
    container/container.go      # Docker container lifecycle management
    state/
      state.go                  # State directory init, migration
      issue.go                  # Issue state CRUD
      pr.go                     # PR state CRUD
    github/
      types.go                  # ReviewComment, Review, Issue, User types
      reviews.go                # Fetch/filter review comments
      issues.go                 # Fetch issues by label
      pr.go                     # PR resolution (branch → PR)
    worktree/worktree.go        # Git worktree create, validate, cleanup
    claude/claude.go            # Claude CLI detection + execution (+ container variants)
    cmd/
      reviews.go                # reviews subcommand
      reply.go                  # reply subcommand
      watch.go                  # watch subcommand entry + flag parsing
    watch/
      config.go                 # WorkerConfig type
      singlepr.go               # Single-PR watch mode
      repo.go                   # Repo scheduler mode
      worker.go                 # Single issue worker lifecycle
```

## Prerequisites

- Go 1.21+ (for building)
- `gh` CLI installed and authenticated (`gh auth login`)
- Inside a git repository with a GitHub remote
- For `auto-pr watch`: `claude` CLI in PATH and `ANTHROPIC_API_KEY` set
- For `auto-pr watch --docker`: Docker Desktop installed and running
