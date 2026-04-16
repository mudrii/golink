# Go Patterns and Style Reference

Language-level conventions that complement `CLAUDE.md`. The `go-rig` skill owns process and review discipline; this file owns code-level patterns.

## Style

- Package names short, lowercase; no `util`/`common`/`helpers`
- Initialisms uppercase: `ID`, `URL`, `HTTP`, `JSON`, `API`, `SQL`
- No stutter: `orders.Service`, `api.Error` (not `api.APIError`)
- Accept interfaces, return structs
- Constructors only when they enforce invariants or own resources
- Options struct when params > 3
- Named types over `string`/`int` for IDs, states, units
- `map[string]any` only at boundaries (MCP tool output, REST decoding, HTTP envelopes); never in core domain

## API design

- Make the zero value useful when practical; when not (e.g. `auth.Session{}` requires a profile), document the invariant on the type
- Nil slices by default; distinguish nil vs empty only when the API requires it
- Pointer receivers when mutating or when copying is non-trivial; keep receiver choice consistent per type
- Never copy types with mutex-like fields
- JSON shape and error behavior are part of the public API — treat changes as compatibility work

## Consumer-side interfaces

Interfaces belong where they are consumed. Example from this repo:

```go
// cmd/app.go — consumer-side seam
type TransportFactory func(ctx context.Context, settings config.Settings,
    session auth.Session, logger *slog.Logger) (api.Transport, error)

// internal/api/official.go — concrete type, returned by constructor
func NewOfficial(cfg OfficialConfig) *Official { ... }
```

The `Transport` interface (`internal/api/transport.go`) is the rare large interface — it mirrors the command/tool surface. Do not build interface-per-struct; if there is no substitution need, keep the concrete type.

## Documentation

- Every exported name and package gets a doc comment that starts with the declared name
- Describe caller-visible behavior, invariants, and ownership — not the implementation
- Update docs in the same change as the behavior/config/wire shift
- TODOs carry a reason and (when one exists) a ticket reference

## Comments

Write a comment only when it adds something the code cannot. Identifiers explain WHAT; comments explain WHY.

Write comments for:

- a tradeoff or design choice that's not obvious from the call sites
- a hidden invariant, lifetime, or concurrency ownership rule
- a workaround for a specific bug or upstream quirk (link it)
- boundary assumptions at package or API edges

Do not write comments that:

- restate the code (`// increment i`)
- narrate your own thinking (`// we could maybe refactor this`)
- duplicate the doc comment with less precision
- carry vague TODOs without a reason or ticket reference

## Testing patterns

- Table-driven with `t.Run`; subtest names read as sentences
- `t.Helper()` on helpers
- `t.Context()` for test-lifetime context (Go 1.24+)
- `t.Setenv("KEY", "val")` for env-backed config tests (auto-reset)
- `httptest.NewServer` is the seam for transport-level tests — every `Transport` method has a matching case in `internal/api/official_test.go`
- `testing/synctest` for deterministic concurrent tests (Go 1.25+)
- `for b.Loop() { ... }` for new benchmarks (Go 1.24+)
- `t.ArtifactDir()` for test output files (Go 1.26)
- `cmp.Equal` / `cmp.Diff` for nested struct comparisons; hand-written asserts for simple values
- Error-string assertions are acceptable ONLY when the string is part of the user-visible contract (golink's "Re-run: golink auth login" qualifies)

### Goroutine-leak pattern for test-side servers

When a test runs a server in a goroutine (e.g. `WaitForOAuthCallback` in `internal/auth/oauth_test.go`), close the listener with `defer`, use a buffered error channel, and wait for the server goroutine to exit before the test returns. Never leak a serve goroutine.

## Common patterns

- **CLI**: cobra only (matches the rest of `cmd/`). Check `cmd/` layout before adding a command.
- **HTTP**: go through `internal/api.Client`; set timeouts deliberately; always close bodies (`errcheck` is on)
- **Modules**: justify every new dependency; `go mod tidy` only when imports actually changed; keep `replace` directives intentional and short-lived
- **Linting**: `.golangci.yml` (v2 schema) is authoritative. Fix findings at the root; `//nolint` only with a precise justification on the same line.
