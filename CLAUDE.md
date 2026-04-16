# golink — Go Development Standards

## Project

golink is a LinkedIn CLI for humans and LLM agents. See `PROMPT_golink.md` for the full implementation spec.

- **CLI framework**: cobra (`github.com/spf13/cobra`)
- **Output contract**: `schemas/golink-output.schema.json` — all `--json` output must validate against this schema
- **Schema tests**: `internal/output/schema_test.go` — add a fixture for every new command envelope
- **Transport pattern**: target architecture uses `internal/api/transport.go` for the `Transport` interface; official and unofficial adapters implement it once the CLI scaffold is in place
- **Secret storage**: `go-keyring` only — never persist tokens to disk or logs
- **MCP server**: `mcp-go` supports both struct-based schemas and functional-options tool registration; use one style consistently and verify against the pinned version

## Environment

- Go 1.26.2 on darwin/arm64
- Treat `go.mod`, `toolchain`, CI config as the source of truth
- Prefer `make` targets over raw commands when a Makefile exists

## Commands

If a `Makefile` exists, check it for exact targets. Common conventions:

```sh
make build        # build binary
make test         # go test ./...
make lint         # golangci-lint
make fmt          # gofmt / gofumpt
make ci           # full gate (or make check, make all)
```

After writing code:

```sh
go vet ./...         # always run first — catches misuse before lint
go test ./...
go test -race ./...  # when the package has race support
go mod tidy          # only when imports or dependencies changed
```

Periodically (not after every edit):

```sh
go fix ./...         # applies modernizations in-place; always review git diff before committing
```

## Go 1.26 Idioms

Write modern Go when it improves clarity and the module toolchain supports it. Key features: `new(expr)`, self-referential generics, `iter.Seq`/`iter.Seq2`, range-over-func, `omitzero` struct tags, generic type aliases. See `.claude/rules/go-idioms.md` for the full catalog and go fix modernizers.

## Layout

golink is expected to use a single-binary layout with cobra commands:

```
main.go                     # entry point (single binary)
cmd/                        # cobra command files (root.go, auth.go, post.go, …)
internal/<domain>/          # domain-organized, not layer-organized
schemas/                    # JSON schema for output contract
*_test.go                   # next to the package under test
```

For multi-binary repos, use `cmd/<binary>/main.go` instead.

Domain-oriented packages as default. Avoid `helpers`, `util`, `common`.

## Style

- Formatting is mechanical: `gofmt`/`go fmt` — do not hand-format
- Standard library first; `x/` before third-party; justify every dependency
- Concrete types over premature abstractions; unexport by default
- Interfaces: consumer-side, 1–3 methods, accept interfaces return structs
- Early returns; whitespace to separate phases; functions readable in one pass
- See `.claude/rules/go-patterns.md` for naming, API design, and documentation rules

## Errors

```go
return fmt.Errorf("create order: %w", err)
```

Wrap with `%w`. Branch with `errors.Is`/`errors.As`. Lowercase, no trailing punctuation. Never swallow. Never panic for expected failures. Context in, internals never out.

## Context

First parameter for I/O. Never in struct. Never nil. Cancellation + deadlines + request metadata only.

## Concurrency

Only when it measurably helps. Every goroutine needs a shutdown path. Context cancellation for background work. No concurrent map writes without sync. Race detector in CI.

## Testing

- Table-driven with `t.Run`, helpers with `t.Helper()`
- Fakes/stubs over mocks — test behavior not implementation
- Deterministic seams for time, randomness, I/O
- `cmp.Equal`/`cmp.Diff` for complex comparisons
- See `.claude/rules/go-patterns.md` for detailed testing APIs

## Common Patterns

- **Logging**: prefer `slog` (stdlib, structured) for new code; match existing convention in existing projects. Never log secrets.
- **Config**: no hardcoded values. Load + validate at startup. Secrets from env or secret manager.
- **HTTP / DB**: set timeouts deliberately. Always close rows/bodies.
- **Security**: `govulncheck ./...` when deps changed or before release; include in CI.

## Review Rejects

Premature abstractions · swallowed errors · context misuse · goroutine leaks · unsafe shared state · unnecessary deps · behavior-changing go fix applied blindly · transport in domain · missing tests · secrets in code · outdated patterns when the toolchain clearly supports better ones · magic values outside protocol/contracts · giant mixed-responsibility functions
