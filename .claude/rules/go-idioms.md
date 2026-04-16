# Go 1.26 Idioms and Modernizers

Prefer modern Go patterns when the project's toolchain supports them and they improve clarity. Do not introduce older patterns by default when better standard forms are available.

## Language (1.26)

```go
type yearsSince int
age := new(yearsSince(born))     // *yearsSince — allocates and initializes from expression

// Self-referential generic constraint — useful when a method must return
// the same concrete type that implements the interface (not `any`).
type Adder[A Adder[A]] interface {
    Add(A) A
}
```

`new(expr)` is available in Go 1.26. The return type is `*T` where `T` is the type of the expression. Use it when it improves clarity for optional scalar pointer values. Do not force it where `&T{...}` or a plain local variable is clearer.

## Iterators (1.23+)

Use `iter.Seq`/`iter.Seq2` and range-over-func. Prefer stdlib iterator APIs:
- `slices.Collect`, `slices.Sorted`, `slices.SortedFunc`, `slices.Concat`
- `maps.Keys`, `maps.Values`, `maps.Collect`, `maps.Insert`

Iterator-based string/byte splitting (1.24+):
- `bytes.Lines`, `bytes.SplitSeq`, `strings.Lines`, `strings.SplitSeq`

## Struct Tags (1.24+)

- `omitzero` for struct-typed fields and types with `IsZero()` (e.g., `time.Time`)
- `omitempty` for slices, maps, strings, and other empty-value cases
- Use both when the wire format should omit either: `json:",omitzero,omitempty"`
- JSON tag changes are behavior changes — review carefully
- Generic type aliases are fully supported

## go fix Modernizers (1.26)

`go fix` applies modernizations in-place. Always review the git diff before committing — some rewrites change observable behavior.

Useful analyzers:
- `rangeint` — 3-clause `for` → `for range`
- `minmax` — if/else clamp → `min`/`max`
- `slicessort` — `sort.Slice` → `slices.Sort` for basic ordered types
- `any` — `interface{}` → `any`
- `fmtappendf` — `[]byte(fmt.Sprintf(...))` → `fmt.Appendf`
- `testingcontext` — simple cancellable test context setup → `t.Context()`
- `omitzero` — suggests `omitzero` for struct fields where `omitempty` has no effect
- `mapsloop` — map update loops → `maps.Copy`/`maps.Insert`/`maps.Clone`/`maps.Collect`
- `newexpr` — wrappers returning `&x` or call sites → `new(expr)`; result type is `*T` matching the expression's type
- `stringsseq` / `stditerators` — loops over eager APIs → iterator-based forms
- `waitgroupgo` — `wg.Add(1)`/`go`/`wg.Done()` → `wg.Go` (stdlib `sync.WaitGroup`, Go 1.25+); prefer `errgroup.Group.Go` from `golang.org/x/sync/errgroup` when error propagation is needed
- `//go:fix inline` — source-level inliner for API migrations

## Applied in this repo

| Modernizer | Status | Notes |
|---|---|---|
| `omitzero` | ✅ | `internal/auth/session.go` — `ExpiresAt`, `ConnectedAt` |
| `any` | ✅ | No `interface{}` in source |
| `rangeint` | ⚠️ | `cmd/app.go:commandLookupArgs` keeps a C-style loop because the body advances `i` for flag-with-value pairs |
| `testingcontext` | ❌ | Tests still use `context.Background()` |

Not applicable today (no callers to migrate): `mapsloop`, `stringsseq`, `stditerators`, `newexpr`, `waitgroupgo`, `minmax`, `slicessort`, `fmtappendf`.

Workflow: `go fix -diff ./...` shows pending changes; apply with `go fix ./...`; review diff; run `make ci`.
