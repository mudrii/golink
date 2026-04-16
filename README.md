# golink

LinkedIn CLI for humans and LLM agents.

> **Status: alpha / requires LinkedIn app credentials.** The command tree, OAuth PKCE login, JSON envelope contract, retryable HTTP client, and LinkedIn REST adapter are implemented and tested. Live use requires a LinkedIn developer app with the documented scopes; without credentials you can still exercise every `--dry-run` and schema-validated envelope.

## Overview

golink talks to LinkedIn through a transport-pluggable architecture. The default **official** transport wraps LinkedIn's Posts API and Community Management API with retry + rate-limit awareness. An **unofficial** transport slot exists behind `--transport=unofficial` but ships as a typed `ErrFeatureUnavailable` no-op until wired; `--transport=auto` falls back transparently where the official adapter is out of scope.

## Capability matrix

| Command | Implemented | Live-tested | Notes |
|---|---|---|---|
| `auth login` | ✅ | via httptest | Native PKCE S256 with loopback callback |
| `auth status` | ✅ | ✅ | Reports unauthenticated when token is missing or expired; shows refresh expiry when available |
| `auth logout` | ✅ | ✅ | Clears the keyring entry |
| `auth refresh` | ✅ | via httptest | Exchanges refresh token for new access token; silently auto-runs within 5 min of expiry |
| `profile me` | ✅ | ✅ | Reads cached OIDC claims from the session |
| `post create` | ✅ | httptest | `--dry-run` previews the exact payload |
| `post create --dry-run` | ✅ | ✅ | Schema-validated envelope, no network |
| `post list` | ✅ | httptest | Defaults author to the session member URN |
| `post get <urn>` | ✅ | httptest | Entitlement-gated by LinkedIn |
| `post delete <urn>` | ✅ | httptest | `--dry-run` supported |
| `comment add <urn>` | ✅ | httptest | `--dry-run` supported |
| `comment list <urn>` | ✅ | httptest | |
| `react add <urn>` | ✅ | httptest | `--dry-run` supported |
| `react list <urn>` | ✅ | httptest | |
| `search people` | Official transport: `unsupported` | ✅ | Returns `ErrFeatureUnavailable` by design |
| `doctor` | ✅ | ✅ | Env vars, session state, userinfo probe, feature map, audit log state |
| `version` | ✅ | ✅ | Reports build metadata |
| `batch <ops.jsonl>` | ✅ | httptest | Reads JSONL ops file, dispatches each op, streams JSONL results; supports `--fail-fast`, `--strict`, `--concurrency`, `--resume` |

"httptest" means the code path is covered by an integration test against a local HTTP server that mimics the LinkedIn endpoint; a real request to `api.linkedin.com` requires your own developer app.

## Installation

```sh
go install github.com/mudrii/golink@latest
```

Requires Go 1.26.2+.

## Quick start

```sh
# Set your LinkedIn app client ID
export GOLINK_CLIENT_ID=your_client_id

# Authenticate (opens the system browser, waits on loopback)
golink auth login

# Everything works in JSON mode for scripts and agents
golink --json auth status
golink --json auth refresh   # manually exchange a refresh token for a new access token
golink --json profile me
golink --json post create --text "Hello from golink" --visibility PUBLIC
golink --json post list --count 5
```

## Non-interactive / agent mode

Without a TTY, all interactive prompts are disabled and required inputs come from flags:

```sh
golink --json post create --text "Hello" --visibility PUBLIC
golink --json comment add urn:li:share:123 --text "nice"
golink --json react add urn:li:share:123 --type LIKE
golink --json post list --count 10
```

Use `--dry-run` to preview the exact request payload without sending it. Supported on `post create`, `post delete`, `comment add`, `react add`.

## Output modes

golink supports five output modes via `--output`:

| Flag | Mode | Description |
|---|---|---|
| _(default)_ | `text` | Human-readable plain text |
| `--json` or `--output=json` | `json` | Full JSON envelope, schema-validated |
| `--output=jsonl` | `jsonl` | One JSON object per line; list commands emit one item per line, scalar commands emit a single envelope line |
| `--output=compact` or `--compact` | `compact` | Stripped envelope (no `command_id`, `generated_at`, `rate_limit`); useful for LLM context budgets |
| `--output=table` | `table` | Tabwriter-based columnar output for list commands; scalar commands fall back to text |

```sh
# Schema-validated JSON (agent contract — byte-for-byte stable)
golink --output=json post list --count 5
golink --json post list --count 5   # identical to above

# Compact — lower token cost for LLM pipelines
golink --compact auth status
golink --output=compact post list --count 5

# JSONL — one object per line, pipeable to jq / stream processors
golink --output=jsonl post list --count 10 | jq '.text'

# Table — human-readable columns
golink --output=table post list --count 10
golink --output=table react list urn:li:share:123
```

**Schema validation**: only `--output=json` (and legacy `--json`) is validated against `schemas/golink-output.schema.json`. Compact, JSONL, and table modes are lossy renderings of the same data.

**Precedence**: `--compact` > `--output` > `--json` > text default. Using both `--compact` and `--output=<non-compact>` is a validation error.

## Transport modes

| Mode | Flag | Behavior |
|---|---|---|
| Official | `--transport=official` (default) | LinkedIn REST APIs with OAuth bearer token, retry on 429/5xx, rate-limit header parsing |
| Unofficial | `--transport=unofficial` | Stub until a concrete adapter lands; requires `--accept-unofficial-risk` |
| Auto | `--transport=auto` | Official first, best-effort fallback |

## Configuration

| Source | Priority |
|---|---|
| CLI flags | Highest |
| `GOLINK_*` env vars | |
| `~/.config/golink/config.yaml` | |
| Defaults | Lowest |

### Environment variables

| Variable | Required | Description |
|---|---|---|
| `GOLINK_CLIENT_ID` | Yes | LinkedIn app client ID (used for `auth login` and `auth refresh`) |
| `GOLINK_API_VERSION` | No | `Linkedin-Version` header, e.g. `202604` |
| `GOLINK_REDIRECT_PORT` | No | Preferred OAuth loopback port; `0` picks any free port |
| `GOLINK_JSON`, `GOLINK_TRANSPORT` | No | Preflight overrides for `--json` / `--transport` |

Tokens are stored in the OS keyring — never on disk or in logs.

## Idempotency keys

Any mutating command (`post create`, `post delete`, `comment add`, `react add`) accepts `--idempotency-key <k>`. On the first successful call the result is cached locally; subsequent calls with the same key within 24 hours replay the cached result and set `from_cache: true` in the envelope — the transport is never called again.

```sh
golink --json post create --text "Hello" --idempotency-key my-post-1
# Second call returns from_cache:true, no network request
golink --json post create --text "Hello" --idempotency-key my-post-1
```

**Store location** (first match wins):

1. `GOLINK_IDEMPOTENCY_PATH=/custom/path.jsonl`
2. `$XDG_STATE_HOME/golink/idempotency.jsonl`
3. `~/.local/state/golink/idempotency.jsonl`

The store is an append-only JSONL file created with mode `0600` in a `0700` directory. Keys expire after 24 hours. Using a key with a different command than it was first recorded against returns a `validation_error`.

## Batch operations

`golink batch <ops.jsonl>` reads a JSONL file where each line is an operation:

```jsonl
{"command":"post create","args":{"text":"Hello batch","visibility":"PUBLIC"},"idempotency_key":"b-1"}
{"command":"post delete","args":{"post_urn":"urn:li:share:123"}}
{"command":"comment add","args":{"post_urn":"urn:li:share:456","text":"nice"},"dry_run":true}
{"command":"react add","args":{"post_urn":"urn:li:share:789","type":"LIKE"},"idempotency_key":"r-1"}
```

Results stream to stdout as JSONL — one `BatchOpResultOutput` envelope per input line.

Supported commands: `post create`, `post delete`, `comment add`, `react add`.

```sh
# Basic run
golink --json batch ops.jsonl

# Stop on first error
golink --json batch --fail-fast ops.jsonl

# Exit 2 if any op is non-ok (CI gate)
golink --json batch --strict ops.jsonl

# Up to 4 parallel workers
golink --json batch --concurrency 4 ops.jsonl

# Skip ops already recorded in ops.jsonl.progress
golink --json batch --resume ops.jsonl

# Read from stdin
cat ops.jsonl | golink --json batch -
```

**Flags**:

| Flag | Default | Description |
|---|---|---|
| `--fail-fast` | false | Stop on the first op error (exit 5) |
| `--continue-on-error` | true | Continue after op errors |
| `--concurrency` | 1 | Parallel workers (max 4) |
| `--strict` | false | Exit 2 if any op is non-ok |
| `--resume` | true | Skip ops already in the `.progress` sidecar file |

Each op may include a per-op `dry_run: true` field to preview that op without executing it, regardless of the global `--dry-run` flag.

Resume: on each successful op, a line is appended to `<ops.jsonl>.progress`. Re-running with `--resume` skips those lines and emits `from_cache:true` envelopes for them.

## Audit log

Every mutating command (`post create`, `post delete`, `comment add`, `react add`, `auth login`, `auth logout`, `auth refresh`) appends one JSONL line to an audit log after the command completes. Read commands are not audited.

**File location** (first match wins):

1. `GOLINK_AUDIT_PATH=/custom/path.jsonl`
2. `$XDG_STATE_HOME/golink/audit.jsonl`
3. `~/.local/state/golink/audit.jsonl`

The file is created with mode `0600` in a `0700` directory on first write.

**Opt-out**: set `GOLINK_AUDIT=off` (or `audit: false` in `~/.config/golink/config.yaml`).

**What is recorded**: timestamp, profile, transport, command, command ID, mode (`normal`/`dry_run`), outcome status, request ID when available, HTTP status, error code, and (for dry-run) the would-be payload preview.

**What is never recorded**: access tokens, refresh tokens, PKCE codes or state, email addresses, or any other secrets.

Audit-write failures are logged at WARN and never abort the command.

## Diagnostics

`golink doctor` reports the health of your environment in one shot — useful before filing a bug or after a new install.

```sh
golink doctor
```

```
Environment
  GOLINK_CLIENT_ID   set
  GOLINK_API_VERSION not set (default: 202504)
  GOLINK_TRANSPORT   not set (default: official)
  GOLINK_AUDIT       not set (default: on)
  GOLINK_AUDIT_PATH  not set

Session
  status     authenticated
  profile    urn:li:member:12345678 (Ada Lovelace)
  scopes     openid profile email w_member_social
  expires_at 2026-04-24 12:00:00 UTC

Probe: GET /v2/userinfo
  status     ok (200)
  latency    142ms

Features
  auth login      supported
  auth refresh    supported
  profile me      supported
  post create     supported
  post list       supported
  post get        supported
  post delete     supported
  comment add     supported
  comment list    supported
  react add       supported
  react list      supported
  search people   unsupported

Audit
  enabled    true
  path       /Users/ada/.local/state/golink/audit.jsonl
  exists     true
```

Add `--strict` to treat warnings (token expiring in < 7 days, missing `GOLINK_CLIENT_ID`) as exit 2 and errors (probe failure, no active session) as exit 5. Without `--strict` the command always exits 0.

`golink doctor` is read-only and is not audited.

## Exit codes

| Code | Meaning |
|---|---|
| 0 | Success |
| 2 | Validation / usage error |
| 4 | Auth / session error |
| 5 | API / transport error |

`status:"unsupported"` envelopes still exit `0` — the envelope `status` field is the machine-readable signal.

## Prerequisites for live API access

- A [LinkedIn Developer App](https://www.linkedin.com/developers/) with native PKCE enabled (contact LinkedIn support to enable PKCE for your app)
- The `w_member_social` scope (self-serve via the developer portal)
- Endpoint-family specific scopes as required (`r_member_social`, `w_member_social_feed`, organization-scoped permissions)
- Go 1.26.2+ for building from source

## Project layout

```
main.go                    entry point + signal handling
cmd/                       cobra commands (auth, post, comment, react, search, batch, doctor, version)
internal/api/              Transport interface + official LinkedIn adapter + NoopTransport
internal/auth/             PKCE login, keyring-backed session store
internal/config/           viper-backed settings with env/flag/file precedence
internal/idempotency/      append-only JSONL idempotency store (FileStore, MemoryStore, NoopStore)
internal/output/           JSON envelope types, schema validation, enum parsers
schemas/                   golink-output.schema.json — contract for every --json response
```

## Development

```sh
make test          # go test ./...
make race          # go test -race ./...
make vet           # go vet ./...
make lint          # golangci-lint run ./...
make vuln          # govulncheck ./...
make ci            # vet lint test race vuln
```

## License

[MIT](LICENSE)
