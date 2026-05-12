---
name: golink-cli
description: |
  Use this skill when an agent needs to operate the golink LinkedIn CLI: setup
  verification, auth login/status/refresh/logout, dry-runs, JSON/JSONL output,
  creating/editing/deleting/resharing posts, comments, reactions, organization
  posting, scheduled posts, approval gates, plan/execute workflows, record/
  replay, privacy-safe automation, or troubleshooting LinkedIn app scopes and
  redirect configuration. Applies to Codex, OpenClaw, Hermes, and other CLI-
  capable agents.
---

# golink CLI

Use `golink` as the source of truth for LinkedIn automation. Prefer CLI commands
over editing local state files directly. Default to non-destructive inspection
and dry-runs until the user explicitly asks to publish, delete, approve, or run a
queued mutation.

For command examples and troubleshooting, read `references/cli-reference.md`.

## Operating Rules

- Verify the binary first: `golink version`.
- Prefer `--json` for single results and `--output=jsonl` for streams.
- Run `golink doctor` before diagnosing auth, scope, redirect, or API support issues.
- Use `--dry-run` for mutating commands unless the user clearly wants live LinkedIn changes.
- Use `--require-approval` for agent-driven publishing workflows.
- Use `--idempotency-key` for retryable mutations.
- Never print or store access tokens, refresh tokens, client secrets, PKCE codes, or OAuth callback URLs containing `code=`.
- Never edit keyring, audit, approval, idempotency, schedule, or cassette files by hand unless the user explicitly asks for repair.
- Treat `unsupported` as an expected LinkedIn entitlement/scope result, not necessarily a CLI bug.

## Setup Workflow

1. Check local readiness:
   ```sh
   golink version
   golink doctor
   ```
2. If unauthenticated, confirm required env vars without revealing values:
   ```sh
   test -n "$GOLINK_CLIENT_ID" && echo GOLINK_CLIENT_ID=set || echo GOLINK_CLIENT_ID=missing
   test -n "$GOLINK_REDIRECT_PORT" && echo GOLINK_REDIRECT_PORT=set || echo GOLINK_REDIRECT_PORT=missing
   ```
3. Authenticate only when user expects an interactive browser flow:
   ```sh
   golink auth login
   golink --json auth status
   golink doctor
   ```
4. If the user needs LinkedIn app setup instructions, point them to
   `LINKEDIN_SETUP.md` in this repo.

## Mutation Workflow

1. Inspect state: `golink --json auth status` and `golink doctor`.
2. Build the command with explicit flags; do not rely on prompts in agent mode.
3. Dry-run:
   ```sh
   golink --json --dry-run post create --text "..." --visibility PUBLIC
   ```
4. For reviewable work, create a plan or approval entry:
   ```sh
   golink --json plan post create --text "..." --visibility PUBLIC > plan.json
   golink --json execute plan.json --dry-run
   ```
5. Execute live only after user confirmation or an existing approved plan:
   ```sh
   golink --json execute plan.json
   ```

## Agent-Specific Guidance

- **Codex**: use this skill normally; run shell commands in the repo root or the user's requested working directory.
- **OpenClaw**: use the same command contract; prefer JSON output and approval gates because the agent may not preserve long interactive context.
- **Hermes**: keep commands deterministic and idempotent; include `--json`, explicit flags, and stable idempotency keys for live mutations.

All agents should summarize command outcomes without exposing sensitive env vars
or OAuth/token material.

## Common Commands

Read `references/cli-reference.md` for examples covering auth, posting, org
posting, comments, reactions, scheduling, approval, plan/execute, output modes,
and record/replay.
