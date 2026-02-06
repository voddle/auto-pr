# Project Workflow — GitHub PR Review

## Overview

This project provides scripts for reviewing and replying to GitHub PR comments locally via Claude Code, with support for automated issue implementation and repo-level watching.

## Scripts

| Script | Purpose |
|--------|---------|
| `scripts/pr-reviews` | Read PR review comments |
| `scripts/pr-reply` | Reply to PR review comments |
| `scripts/pr-watch` | Auto-watch PR/repo for new reviews and issues, process them |

## Workflow

1. **Read reviews** — Run `./scripts/pr-reviews` to see all review comments on the current branch's PR.
2. **Address feedback** — Edit code based on the review comments.
3. **Reply to reviewers** — Use `./scripts/pr-reply <comment_id> "message"` to respond.
4. **Push changes** — Commit and push to update the PR.

## Quick Reference

```bash
# See reviews for current branch's PR
./scripts/pr-reviews

# See reviews for a specific PR
./scripts/pr-reviews 123

# Only latest review round
./scripts/pr-reviews --latest

# Raw JSON output
./scripts/pr-reviews --json

# List comment IDs you can reply to
./scripts/pr-reply --list

# Reply to a specific comment
./scripts/pr-reply <comment_id> "Fixed in latest commit"
```

## Automated Watch Mode

`pr-watch` supports three modes: **Single-PR mode** (original behavior), **Repo mode** (lightweight scheduler), and **Worker mode** (internal, per-issue).

### Single-PR Mode (backward compatible)

Watches one PR for new review comments and processes them synchronously in the foreground.

```bash
# Watch current branch's PR (auto-detect)
./scripts/pr-watch

# Watch a specific PR
./scripts/pr-watch 123

# Custom poll interval (60 seconds)
./scripts/pr-watch --interval 60

# Single check, no loop (for debugging)
./scripts/pr-watch --once
```

**How it works:**
1. On first run, it snapshots existing comments to avoid re-processing history
2. Every 30 seconds (configurable), it checks for new inline comments and top-level reviews
3. When new comments are found, it calls `claude -p` with the comment details
4. Claude Code reads the relevant files, makes changes, commits, pushes, and replies to each comment
5. The loop continues until you stop it (Ctrl+C)

### Repo Mode (worker-based)

Watches the entire repo for new issues (with configured labels). Each issue gets a dedicated **worker process** that runs in its own worktree, implements the issue, creates a PR, and then watches that PR for reviews — all with continuous context via `claude -p --continue`.

```bash
# Watch entire repo
./scripts/pr-watch --repo

# Watch with custom settings
./scripts/pr-watch --repo --interval 60 --max-concurrent 4

# Single scan, no loop (for debugging)
./scripts/pr-watch --repo --once
```

**How it works:**
1. On first run, it snapshots all existing issues as "pre-existing" (skipped)
2. Each poll cycle, the lightweight main process:
   - Monitors worker health (reaps exited workers, updates state)
   - Cleans up worktrees for closed issues
   - Scans for new issues with configured labels → spawns a worker subprocess
3. Concurrency is limited to `MAX_CONCURRENT` simultaneous worker processes
4. The loop continues until you stop it (Ctrl+C); all worker processes are terminated on exit

**Worker lifecycle** (one per issue):

| Phase | What happens |
|-------|-------------|
| **Phase 1: Implement** | Worker creates branch + worktree, calls `claude -p` to implement the issue, pushes, and creates a PR |
| **Phase 2: Watch reviews** | Worker polls for new review comments on its PR, handles them with `claude -p --continue` (preserving context from Phase 1) |
| **Exit** | When the PR is merged or closed, the worker exits cleanly |

**Context continuity:** `claude -p --continue` is directory-scoped ("continue the most recent conversation in the current directory"). Since each worker runs in its own worktree directory, context is naturally isolated per issue. The Claude session remembers the code it wrote in Phase 1 when handling reviews in Phase 2.

**Worker logs:** Each worker's output is written to `.pr-watch-state/logs/issue-N.log`.

## Configuration

`pr-watch --repo` reads settings from `.pr-watch.conf` in the project root:

```bash
MAX_CONCURRENT=2          # Max concurrent claude processes
INTERVAL=30               # Poll interval (seconds)
ISSUE_LABELS="auto,claude" # Issue labels that trigger auto-processing (comma-separated)
WORKTREE_DIR=".worktrees"  # Worktree directory
# BASE_BRANCH="main"      # Base branch for new issue branches (default: repo default branch)
```

CLI flags (`--interval`, `--max-concurrent`) override config file values.

## State Management

State is stored in `.pr-watch-state/` (directory, git-ignored):

```
.pr-watch-state/
  .initialized              # Sentinel: first scan completed
  issues/
    42.json                  # {"status":"in_progress|watching|done|failed|preexisting","pid":1234,"branch":"auto/issue-42","pr_number":99}
  prs/
    101.json                 # {"last_comment_ts":"2026-...","pid":0,"branch":"feature-x"}
  pids/
    1234.json                # {"type":"worker","number":42,"worktree":".worktrees/issue-42","started_at":"..."}
  logs/
    issue-42.log             # Worker stdout/stderr for issue #42
```

Issue status lifecycle: `preexisting` (skipped) | `in_progress` (Phase 1) → `watching` (Phase 2, PR created) → `done` (PR merged/closed) | `failed` (error).

Old flat-file `.pr-watch-state` is automatically migrated on first run.

## Editing Scope Rules

When processing PR review comments (via `pr-watch` or manually), you MUST follow these rules:

1. **Only modify files explicitly mentioned in the review comments** — the `path` field of inline comments defines your editing scope. Do NOT edit any file that is not referenced by a review comment.
2. **Only change code related to the reviewer's feedback** — do not refactor, reformat, or "improve" surrounding code beyond what the reviewer requested.
3. **Never modify project infrastructure files** — do not edit `CLAUDE.md`, `.claude/`, `scripts/pr-watch`, `scripts/pr-reviews`, `scripts/pr-reply`, `.gitignore`, or any CI/CD config unless a reviewer explicitly asks for it.
4. **If a review comment is ambiguous or requests changes to files not in the PR**, reply to the comment asking for clarification instead of guessing.

## Prerequisites

- `gh` CLI installed and authenticated (`gh auth login`)
- Inside a git repository with a GitHub remote
- For `pr-watch`: `claude` CLI in PATH and `ANTHROPIC_API_KEY` set
