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
| `approval list` | ✅ | unit | List staged approval entries |
| `approval show <id>` | ✅ | unit | Show a single staged entry |
| `approval grant <id>` | ✅ | unit | Approve a pending entry |
| `approval deny <id>` | ✅ | unit | Deny a pending entry |
| `approval run <id>` | ✅ | unit | Execute an approved entry via transport |
| `approval cancel <id>` | ✅ | unit | Remove a pending entry |
| `--require-approval` | ✅ | unit | Stage any mutating command for review (exits 3) |
| `social metadata <urn>...` | ✅ | httptest | Batch engagement read: comment count, reaction totals per type, comments-state for up to 100 post URNs in one call |
| `post schedule --at ...` | ✅ | unit | Queue a post for later publication (client-side queue) |
| `schedule list / show / run / cancel / next` | ✅ | unit | Manage the scheduled-post queue |
| `post edit <urn>` | ✅ | httptest | Update commentary or visibility of an existing post |
| `post reshare <urn>` | ✅ | httptest | Reshare an existing share with optional added commentary (requires `Linkedin-Version >= 202209`) |
| `post create --image <path>` | ✅ | httptest | Upload a local image and attach it to a new post |
| `plan post create / delete / edit / reshare / schedule` | ✅ | unit | Generate a plan document without calling LinkedIn |
| `plan comment add` | ✅ | unit | Generate a comment-add plan document |
| `plan react add` | ✅ | unit | Generate a react-add plan document |
| `execute <plan.json>` | ✅ | unit | Execute a golink.plan/v1 document via the normal Transport path |
| `org list` | ✅ | httptest | List organizations where the session member is ADMINISTRATOR; requires `w_organization_social` scope |
| `post create --as-org <urn>` | ✅ | httptest | Post as an organization; requires `w_organization_social` scope |

"httptest" means the code path is covered by an integration test against a local HTTP server that mimics the LinkedIn endpoint; a real request to `api.linkedin.com` requires your own developer app.

## Organization posting

To post as a LinkedIn organization page you need the `w_organization_social` scope added to your LinkedIn app. The session member must be an ADMINISTRATOR of the organization.

```sh
# List organizations you administer
golink --json org list

# Post as an org page
golink --json post create \
  --text "Exciting news from Acme Corp" \
  --as-org urn:li:organization:12345678

# Preview without sending
golink --json --dry-run post create \
  --text "Draft post" \
  --as-org urn:li:organization:12345678

# Plan for review, then execute
golink --json plan post create \
  --text "Queued org post" \
  --as-org urn:li:organization:12345678 > plan.json
golink execute plan.json
```

`golink doctor` reflects `org list` and `post create --as-org` availability based on whether `w_organization_social` is in the session scopes.

## Scheduling

LinkedIn has no native scheduled-post API. golink implements a client-side queue — scheduled posts are stored on disk and executed on demand:

```sh
# Queue a post for tomorrow 9am UTC
golink post schedule --at 2026-04-18T09:00:00Z --text "Good morning"

# Inspect the queue
golink schedule list
golink schedule next        # prints the earliest pending scheduled_at

# Execute past-due entries (typically invoked from cron)
golink schedule run --limit 20
```

golink does NOT run a daemon. Operators invoke `schedule run` via cron, launchd, systemd-timer, or an agent loop. Queue location: `$GOLINK_SCHEDULE_DIR` (override), else `$XDG_STATE_HOME/golink/schedule/`. `--image` paths must be absolute because they resolve at run time. `--require-approval` is not supported on scheduled posts in this release.

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

## Plan / Execute

`golink plan` generates a `golink.plan/v1` JSON document that describes a mutating operation without calling LinkedIn. The document can be inspected, stored, or version-controlled before being executed.

```sh
# Generate a plan (no network call, no auth required)
golink plan post create --text "Hello from golink" --visibility PUBLIC > plan.json

# Review the plan
cat plan.json

# Execute it (auth required, all middleware applies)
golink execute plan.json

# Preview without executing
golink --dry-run execute plan.json

# Pipe directly
golink plan comment add --post-urn urn:li:share:123 --text "nice" | golink execute -
```

Plannable commands: `post create`, `post delete`, `post edit`, `post reshare`, `post schedule`, `comment add`, `react add`.

The plan document is the full output envelope from `golink plan` (or a bare `golink.plan/v1` JSON — both are accepted by `execute`).

**Plan SHA-256**: every time `execute` dispatches a plan, the SHA-256 of the plan document is recorded in the audit log (`plan_sha256` field). This makes it possible to trace which exact plan produced a given mutation.

**Transport mismatch**: if the plan's `transport` field differs from the `--transport` CLI flag, a warning is logged but execution proceeds with the CLI value.

**Idempotency**: the plan's `idempotency_key` (if set) is used as the idempotency key unless `--idempotency-key` is supplied on the CLI.

**Dry-run**: the plan's `dry_run` field is OR'd with the `--dry-run` CLI flag — either source can enable dry-run.

## Record / Replay

golink can record HTTP exchanges to a cassette file and replay them later without network access. This is useful for offline testing, CI environments, and sharing reproduction cases.

```sh
# Record all LinkedIn API calls to a cassette
GOLINK_RECORD=cassette.jsonl golink --json post list

# Replay from the cassette (no network)
GOLINK_REPLAY=cassette.jsonl golink --json post list
```

`GOLINK_RECORD` and `GOLINK_REPLAY` are mutually exclusive — setting both is a startup error.

**Cassette format**: newline-delimited JSON (JSONL). Each line records one HTTP exchange:
- `method`, `url`, `body_sha256` — request identity (match key for replay)
- `request_body` — request body inlined if ≤ 1 KB, otherwise omitted
- `response.status`, `response.headers`, `response.body_base64` — full response

**Security**: `Authorization`, `Cookie`, and `Set-Cookie` headers are redacted from cassettes before writing. Access tokens never appear in cassette files.

**Replay semantics**: responses are matched by `method + url + body_sha256`. A miss aborts with an error — the replayer never falls back to the network.

## Approval gate

`--require-approval` stages a mutating command for operator review instead of executing it immediately. The command exits with code 3 and emits a `pending_approval` JSON envelope. An operator then uses the `approval` subcommands to review and dispatch.

```sh
# Stage a post for review
golink --json post create --text "Hello from golink" --require-approval
# exits 3; envelope contains staged_path and command_id

# List pending approvals
golink --json approval list

# Inspect a specific entry
golink --json approval show <command_id>

# Approve and dispatch
golink --json approval grant <command_id>
golink --json approval run <command_id>

# Or reject / cancel
golink --json approval deny <command_id>
golink --json approval cancel <command_id>
```

Supported on: `post create`, `post delete`, `comment add`, `react add`.

Also supported in batch ops via `require_approval: true` on a per-op basis:

```jsonl
{"command":"post create","args":{"text":"Hello batch"},"require_approval":true}
```

**Store location** (first match wins):

1. `GOLINK_APPROVAL_DIR=/custom/dir`
2. `$XDG_STATE_HOME/golink/approvals/`
3. `~/.local/state/golink/approvals/`

Each staged entry is a `<command_id>.<state>.json` file. States: `pending` → `approved` → `completed` (or `denied`/cancelled via removal).

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
| 3 | Approval required — command staged; use `approval grant` + `approval run` |
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
cmd/                       cobra commands (auth, post, comment, react, search, social, batch, approval, schedule, plan, execute, doctor, version)
internal/api/              Transport interface + official LinkedIn adapter + NoopTransport
internal/approval/         approval gate (Store interface, FileStore, MemoryStore; states: pending/approved/denied/completed)
internal/audit/            append-only JSONL audit log (Sink interface, FileSink, MemorySink, NoopSink)
internal/auth/             PKCE login, keyring-backed session store
internal/config/           viper-backed settings with env/flag/file precedence
internal/httprecord/       HTTP record/replay cassette (RecordTransport, ReplayTransport, Wrap)
internal/idempotency/      append-only JSONL idempotency store (FileStore, MemoryStore, NoopStore)
internal/output/           JSON envelope types, schema validation, enum parsers
internal/plan/             golink.plan/v1 document type, Load, SHA256
internal/schedule/         client-side post queue (Store interface, FileStore, MemoryStore)
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
