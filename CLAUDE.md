# golink — Go Development Standards

golink is a LinkedIn CLI for humans and LLM agents. Go 1.26.2 on darwin/arm64. See `PROMPT_golink.md` for the full implementation spec. `go.mod`, `toolchain`, and CI config are the source of truth.

## Architecture

- **CLI**: cobra (`github.com/spf13/cobra`); auth subcommands: `login`, `status`, `logout`, `refresh`; org subcommands: `list` (requires `w_organization_social`); top-level: `doctor` (read-only, not audited), `version`
- **Transport seam**: `internal/api/transport.go` (interface) → `official.go` (live LinkedIn adapter) / `noop.go` (fallback). Every CLI command goes through `Transport`.
- **HTTP**: `internal/api/client.go` — `go-retryablehttp`, 429/5xx retry, `Linkedin-Version` + `X-Restli-Protocol-Version` headers, rate-limit parsing, typed `api.Error`
- **Auth**: native PKCE OAuth (`internal/auth/oauth.go`) + `go-keyring` session store — tokens never touch disk or logs
- **Output contract**: `schemas/golink-output.schema.json` — every `--json` response must validate; fixtures in `internal/output/schema_test.go`

## Packages

```
main.go                entry point + signal handling
cmd/                   cobra commands (auth, org, post, comment, react, search, social, batch, approval, schedule, plan, execute, doctor, version)
internal/api/          Transport interface, official adapter, retry client, typed errors
internal/approval/     approval gate (Store interface, FileStore, MemoryStore; states: pending/approved/denied/completed)
internal/audit/        append-only JSONL audit log (Sink interface, FileSink, MemorySink, NoopSink)
internal/auth/         PKCE OAuth + keyring session store
internal/config/       viper settings with env/flag/file precedence
internal/httprecord/   HTTP record/replay cassette (RecordTransport, ReplayTransport, Wrap); activated by GOLINK_RECORD / GOLINK_REPLAY
internal/idempotency/  append-only JSONL idempotency store (FileStore, MemoryStore, NoopStore)
internal/output/       JSON envelopes, schema validator, enum parsers
internal/plan/         golink.plan/v1 document type — Load (envelope-aware), SHA256, IsPlannableCommand
internal/schedule/     client-side post queue (Store interface, FileStore, MemoryStore; states: pending/running/completed/failed/cancelled)
schemas/               golink-output.schema.json (the --json contract)
```

Domain-organized. Avoid `helpers`, `util`, `common`.

## Commands

```sh
make build lint test race vuln    # individual gates
make ci                           # vet + lint + test + race + vuln (full)
make fmt                          # gofmt -w .
```

Tooling (installed via `brew install golangci-lint goimports govulncheck gotestsum`):

```sh
gotestsum --format short-verbose  # nicer test output
golangci-lint run ./...           # .golangci.yml (v2 schema) is authoritative
govulncheck ./...                 # dep-bump and pre-release gate
goimports -w .                    # import grouping
go fix ./...                      # Go 1.26 modernizers — review diff before commit
```

Run `go vet ./...` and `go test -race ./...` after any code change. Only run `go mod tidy` when imports changed.

## Environment variables

| Variable | Required | Purpose |
|---|---|---|
| `GOLINK_CLIENT_ID` | yes, for `auth login` | LinkedIn app client ID (PKCE flow) |
| `GOLINK_API_VERSION` | no | `Linkedin-Version` header value (`YYYYMM`) |
| `GOLINK_REDIRECT_PORT` | no | Preferred OAuth loopback port; `0` picks any free port |
| `GOLINK_JSON`, `GOLINK_TRANSPORT` | no | Preflight overrides for `--json` / `--transport` |
| `GOLINK_IDEMPOTENCY_PATH` | no | Override idempotency store path (default: `$XDG_STATE_HOME/golink/idempotency.jsonl`) |
| `GOLINK_AUDIT` | no | `on` (default) or `off` to disable the audit log |
| `GOLINK_AUDIT_PATH` | no | Override audit log file path (default: `$XDG_STATE_HOME/golink/audit.jsonl`) |
| `GOLINK_RECORD` | no | Path to a JSONL cassette file; wraps the HTTP client to record every exchange |
| `GOLINK_REPLAY` | no | Path to a JSONL cassette file; serves responses from cassette without network access (mutually exclusive with `GOLINK_RECORD`) |

No client secret. Tokens via keyring only. Config file stores non-sensitive settings.

## Exit codes

| Code | Meaning |
|---|---|
| 0 | Success (includes `status:"unsupported"` envelopes) |
| 2 | Validation / usage error |
| 3 | Approval required — command staged; use `approval grant` + `approval run` |
| 4 | Auth / session error |
| 5 | API / transport error |

## Style (pointers; details in `.claude/rules/go-patterns.md`)

- `gofmt` / `goimports` — never hand-format; indentation is mechanical
- Standard library first; `x/` before third-party; justify every dependency
- Concrete types over premature abstractions; unexport by default
- Interfaces: consumer-side, 1–3 methods; accept interfaces, return structs
- Early returns; whitespace separates phases; functions readable in one pass
- Strict types: named types for IDs/states/units; `map[string]any` only at I/O boundaries
- Comments explain WHY; identifiers explain WHAT (see `go-patterns.md §Comments`)

## Errors

Wrap with `%w`: `return fmt.Errorf("create order: %w", err)`. Branch with `errors.Is` / `errors.As`. Lowercase, no trailing punctuation. Never swallow. Never panic for expected failures. Context in, internals never out.

## Context

First parameter for I/O. Never in struct. Never nil. Cancellation + deadlines + request metadata only.

## Concurrency

Only when it measurably helps. Every goroutine needs a shutdown path. Context cancellation for background work. No concurrent map writes without sync. Race detector in CI.

## Testing

- Table-driven with `t.Run`; helpers call `t.Helper()`
- Fakes over mocks; test behavior not implementation
- Inject seams (`cmd.Dependencies` shows the pattern: `Stdout`, `Now`, `HTTPClient`, `SessionStore`, `BrowserOpener`, `IsInteractive`, `TransportFactory`, `IdempotencyStore`, `ApprovalStore`)
- `t.Context()` for test-lifetime context; `t.Setenv` for env; `httptest.NewServer` for transport tests
- `cmp.Equal` / `cmp.Diff` for nested struct comparisons
- **Schema-first contract changes**: edit `schemas/golink-output.schema.json` + add a fixture in `internal/output/schema_test.go` FIRST, then change the Go struct and command code. The schema is the contract.

## Output modes

Five modes via `--output`: `text` (default), `json`, `jsonl`, `compact`, `table`. Also `--compact` (shorthand for `--output=compact`) and `--json` (shorthand for `--output=json`, preserved for back-compat).

**Precedence**: `--compact` > `--output` > `--json` > text default. `--compact` combined with `--output=<non-compact>` is a validation error.

Implementation:
- `internal/config/config.go`: `Settings.Output` holds the resolved mode; `Settings.JSON` kept in sync for back-compat checks.
- `internal/output/render.go`: `RenderSuccess` / `RenderError` dispatch on mode. `TabularData` interface (`Headers() []string`, `Rows() [][]string`) implemented by `PostListData`, `CommentListData`, `ReactionListData`, `SearchPeopleData`, `OrgListData`.
- `internal/output/format.go`: `BuildBase` and `ExtractErrorEnvelope` helpers used by `cmd/app.go`.
- `cmd/app.go:writeSuccess` / `writeDryRun` / `writeUnsupported`: delegate to renderer for non-json modes.
- `cmd/app.go:preflightFlags`: also parses `--output` and `--compact` so pre-cobra failures can honor the mode.

**Schema validation**: only `--output=json` (and `--json`) is schema-validated. Compact, JSONL, and table are lossy renderings — documented in code comments and README.

## Common patterns

- **Logging**: `slog` (configured in `cmd/root.go:newLogger`). Never log secrets.
- **Config**: loaded and validated at startup via `internal/config.Loader`; no hardcoded runtime values
- **HTTP**: all LinkedIn calls go through `internal/api.Client`; always close bodies (`errcheck` is enabled)
- **Security**: `govulncheck` on dep changes and before release; gated by `make ci`
- **Audit**: every mutating command's `RunE` must call `a.auditMutation(cmd, cmdID, status, mode, ...)` before returning. Opt-out via `GOLINK_AUDIT=off` or `audit: false` in config. Tokens/secrets must never appear in audit entries. `doctor` is read-only and is never audited.

## Review rejects (blocking)

- Premature abstractions; interface-per-struct with no consumer need
- Swallowed errors; panic on expected failures; string-parsed errors
- `context.Context` in structs or `nil` contexts
- Goroutine leaks; unsafe shared state
- Secrets in code, logs, or config files
- Transport details embedded in domain code
- Giant mixed-responsibility functions
- Magic runtime values outside protocol constants / test fixtures
- JSON schema and Go struct out of sync
- Missing tests on a user-visible behavior change
- `go fix` applied blindly without reviewing the diff
