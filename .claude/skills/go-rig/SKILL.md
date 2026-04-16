---
name: go-rig
description: Use this skill when building, reviewing, or refactoring Go code that must follow strict design discipline — ATDD/TDD workflow, explicit dependency injection, package-boundary discipline, and structured code review. Complements CLAUDE.md by focusing on process and design judgment rather than version-specific Go features.
metadata:
  short-description: Go design, workflow, and review discipline
  slash-command: enabled
---

# Go Rig

Process and review discipline for Go work in this repo.

Scope split (do not duplicate across layers):

- `CLAUDE.md` — toolchain, commands, env vars, exit codes, style pointers
- `.claude/rules/go-idioms.md` — Go 1.26 idioms and modernizers
- `.claude/rules/go-patterns.md` — style, API, docs, and testing patterns
- This skill — ATDD/TDD workflow, DI discipline, coverage floors, schema-first, review gate, reject list

If `CLAUDE.md` is stricter on a shared point, follow `CLAUDE.md`.

## When to use

- implementing a new feature or behavior increment
- refactoring for clearer ownership or testability
- reviewing package boundaries or dependency flow
- replacing hidden collaborator construction with explicit injection
- tightening tests around user-visible or integration behavior

## ATDD/TDD workflow

Test-first is a design tool, not an afterthought. Every meaningful behavior change follows red → green → refactor.

- **ATDD** (acceptance-first) for any user- or agent-visible change — drive from `cmd/command_test.go` or `cmd/transport_test.go` so the test speaks in "what the CLI does", not "what the function does"
- **TDD** (unit-first) for internal invariants, validation rules, and edge cases — drive from a package-local `*_test.go`

1. Define the boundary behavior you want
2. Add/update the closest consumer-level test (ATDD when user-visible; unit otherwise)
3. Write the smallest failing assertion for the next increment
4. Minimal implementation to pass
5. Refactor while green; commit often
6. Repeat

If repo policy blocks automatic test execution, still design test-first and ask before running. "Test everything you write" — no behavior ships without at least one consumer-level test pointing at it.

## Coverage floors

Measured via `go test -cover ./...`. Touched packages must meet the floor or the PR must explicitly note the gap.

- `internal/api`, `internal/auth`, `internal/output`, `cmd/` ≥ 75%
- `internal/config`, `internal/mcp` ≥ 70%
- Every branch of `cmd.mapTransportError` (401/403/404/422/429/5xx/unknown) has a test
- Every `Transport` method called by the CLI or MCP has an httptest-backed case in `internal/api/official_test.go`
- `main.go` exempt (pure wiring)

## Schema-first contract changes

`schemas/golink-output.schema.json` is the source of truth for every `--json` envelope. Order of operations when a shape changes:

1. Edit the schema (`$defs` entry + the `oneOf` entry if it's a new envelope)
2. Update or add the fixture in `internal/output/schema_test.go`
3. Run `go test ./internal/output/...` and watch it fail the new expectation
4. Then edit the Go struct and command/tool handler
5. Run the full suite to prove the envelope flows end-to-end

This turns schema drift into a test failure instead of silent runtime mismatch.

## Definition of done

- Acceptance behavior specified at the right boundary
- Smallest relevant unit behavior covered
- Failure and edge paths covered
- Code refactored back to clarity while green
- `make ci` (or the equivalent vet + lint + test + race + vuln gate) passes locally
- Coverage floors met for any touched package, or gap explicitly noted
- Schema and fixture moved first if a `--json` shape changed

## Design principles

Apply without ceremony — these guide decisions, not generate boilerplate.

- **SRP** — one reason to change per package, type, function
- **DRY** — extract repeated rules/validation/mapping, but not incidental similarity
- **OCP** — a concrete type with a small seam at the consumer beats an abstract framework

When these conflict, fix boundaries before introducing interfaces.

## Abstraction and function discipline

- Start concrete; add an interface only when a real consumer needs substitution
- A function does one thing: validate, transform, orchestrate, persist, or render
- Split when business rules get mixed with transport/storage/logging
- Early returns over nested pyramids; whitespace separates phases
- Options struct when params > 3; named types when booleans muddy intent

## Dependency injection

- Constructors for long-lived collaborators; parameters for short-lived/pure logic
- Never construct HTTP clients, keyring stores, loggers, or browsers inside domain methods
- No DI framework. No hidden globals. No package-level mutable state.
- Pass dependencies from the composition root (`main` → `cmd.ExecuteContext` → `cmd.Dependencies`)
- Inject seams for: time (`Now`), randomness, HTTP (`HTTPClient`), filesystem, browser launch, interactive TTY check, transport selection (`TransportFactory`), session storage (`SessionStore`)

```go
// constructor injection for long-lived deps
func NewOrderService(store OrderStore, clock Clock) *OrderService {
    return &OrderService{store: store, clock: clock}
}

// function parameter for short-lived / pure logic
func ValidateOrder(order Order, now time.Time) error {
    if order.ExpiresAt.Before(now) {
        return fmt.Errorf("order %s expired: %w", order.ID, ErrExpired)
    }
    return nil
}
```

## Hardcoding

- No URLs, ports, credentials, file paths, timeouts, feature flags, or collaborator selection in core logic
- Exceptions: protocol-mandated constants and explicit contract/test fixtures
- Operational values come from typed config structs validated at startup

## Review checklist

Before finishing any change — treat a match in *Reject these patterns* as a blocker.

- [ ] Package boundaries coherent; no cross-domain leaks
- [ ] Interfaces have real consumers; no interface-per-struct
- [ ] Dependencies injected from the composition root
- [ ] No hardcoded URLs/ports/credentials/timeouts
- [ ] Types explicit where they protect domain correctness
- [ ] Functions readable in one pass, one responsibility each
- [ ] Errors wrapped with `%w`; no swallowed errors; no panic on expected failures
- [ ] Tests cover happy path, validation, edges, errors; concurrency where relevant
- [ ] Coverage floors met for touched packages
- [ ] JSON schema and Go struct in sync; fixture exists for every envelope change
- [ ] Nil-vs-empty behavior intentional for slices/maps/pointers/JSON
- [ ] Goroutines have a shutdown path and observable ownership
- [ ] Exported docs updated when public behavior changed
- [ ] `make ci` passes locally

## Reject these patterns

- Interface-per-struct without a consumer need
- Functions mixing validation, orchestration, and persistence
- Hardcoded configuration or collaborator selection
- Weakly typed domain data (raw maps / generic blobs) when a struct would do
- Comments that restate code
- Brittle mock-only tests (prefer fakes with real behavior)
- Transport concerns embedded in core domain logic
- Production design distorted to satisfy a mocking framework
- Refactors that add indirection without improving correctness, ownership, or testability
- Schema drift — Go struct changed without the matching schema fixture

## Success criteria

The skill is being followed correctly when:

- changes are small, test-backed, and easy to review
- dependency flow is explicit from `main` down
- package responsibilities get cleaner, not blurrier
- tests speak in behavior terms, not implementation vocabulary
- the resulting code reads clearly without comments explaining control flow
