# Deep TDD/ATDD and Quality Audit

Date: 2026-05-11
Scope: `/Users/mudrii/src/golink` at `bf58ddb3e9a023fb17bd5a299e6d26271fff6e52`

## Executive Summary

- Audited 35 Go test files, 44 non-test Go source files, 13 packages, and 448 test/subtest runs.
- Live gates passed: `go test ./... -coverprofile=/tmp/golink-cover.out`, `go test -race ./...`, `go vet ./...`, `golangci-lint run ./...`, and `govulncheck ./...`.
- Overall statement coverage is 72.2%. Package coverage ranges from 63.0% to 85.5%.
- Repo-stated coverage floors are not fully met: `cmd` is 71.1% against a 75% floor, and `internal/approval` plus `internal/schedule` are below the general risk level expected for persistence/state-machine packages.
- Critical issues: 0. High issues: 5. Medium issues: 12. Low issues: 8.
- Overall health score: 7/10. The codebase is functionally well tested, but several tests were added as coverage ballast and the command layer carries too much business logic.

## Test Correctness Findings

### Per-file Breakdown

| Test file | Assessment | Notes |
|---|---:|---|
| `cmd/approval_test.go` | Good, with weak spots | Strong end-to-end approval tests; three tests exceed 78 lines and mix staging, grant, run, transport, and audit assertions. |
| `cmd/batch_test.go` | Good | Covers happy path, dry run, resume, strict, approval, validation, and helper dispatch. Missing direct tests for rate-limit pacing and progress write failures. |
| `cmd/command_test.go` | Good, with weak helper design | Good schema validation. Shared helper still uses `context.Background()` at lines 744 and 779 instead of `t.Context()`. |
| `cmd/coverage_regression_test.go` | WEAK | Several tests exercise private helpers mostly for coverage, not user-visible behavior. `TestBatchRunnerPendingApprovalAndCachedResult` validates progress text but not emitted JSONL shape. |
| `cmd/doctor_test.go` | Good | Covers health states, strict exits, feature map, schema, text output, and non-audit behavior. |
| `cmd/org_test.go` | Good | Covers scope gates, dry-run, live fake transport, and org author in payload. |
| `cmd/plan_test.go` | Good | Covers generated plan envelopes, execution path, profile/transport propagation. |
| `cmd/preflight_test.go` | Good | Data-driven flag parsing coverage. |
| `cmd/schedule_coverage_test.go` | WEAK | Name is explicitly coverage-oriented; tests conversion helpers rather than CLI behavior. |
| `cmd/schedule_test.go` | Good | Strong state-machine behavior; missing retrying transition and file-store malformed entry coverage. |
| `cmd/scopes_test.go` | Good | Simple table-like assertions, adequate scope helper coverage. |
| `cmd/social_test.go` | Good | Covers args, max URNs, success, and table output. |
| `cmd/transport_test.go` | Good | Broad command/transport behavior coverage through fake transport. |
| `internal/api/*_test.go` | Good | Httptest-backed official transport coverage is strong. Upload retry/backoff and cancellation remain weak. |
| `internal/approval/approval_test.go` | Good, with gaps | Covers memory and file store basics but not corruption, duplicate stage, resolve path, or staged path helper. |
| `internal/audit/audit_test.go` | Good | Covers file sink, permissions, concurrency, redaction, resolve path. |
| `internal/auth/oauth_test.go` | WEAK/MISLEADING in callback timing | Tests use fixed `time.Sleep(25 * time.Millisecond)` before callback requests at lines 376, 459, 523, and 592; this is a timing assumption, not a readiness signal. |
| `internal/auth/session_test.go` | Good | Covers validation and auth state. |
| `internal/auth/store_coverage_test.go` | WEAK | `TestKeyringHelpers` only checks private key formatting and constructor defaults; real keyring load/save/delete are 0% covered. |
| `internal/config/*_test.go` | Good | Covers precedence, env overrides, output conflicts, audit settings. |
| `internal/httprecord/httprecord_test.go` | Good | Covers record/replay, redaction, oversized response, mutually exclusive envs. |
| `internal/idempotency/idempotency_test.go` | Good | Covers hit/miss/mismatch/expiry/prune/path. Missing no-op store methods and corrupted-line behavior. |
| `internal/output/enums_test.go` | Good | Table-driven enum parsing. |
| `internal/output/format_coverage_test.go` | WEAK | `TestTabularRowsCoverage` asserts only header/row counts for most types, not actual cell values. |
| `internal/output/render_test.go` | Good | Covers all output modes, table fallback, error render, truncation. |
| `internal/output/schema_test.go` | Good but hard to maintain | 45 fixtures in one 992-line test function; high value, low readability. |
| `internal/plan/plan_test.go` | Good | Covers schema, command allowlist, malformed JSON, deterministic hash. |
| `internal/privacy/redact_test.go` | WEAK | Negative leak assertions are useful, but `Form`, nested arrays, `SensitiveKey` variants, invalid URL, and malformed JSON are under-covered. |
| `internal/schedule/schedule_test.go` | Good, with state gaps | Covers main transitions; `MarkRetrying`, file-store `Due`, cancellation, and invalid file content are uncovered. |

### Incorrect / Weak / Misleading Tests

1. WEAK: `internal/output/format_coverage_test.go:59-107`
   - Intent: tabular data coverage.
   - Problem: most assertions check only `len(Headers())` and `len(Rows())`, so swapped columns or wrong labels can pass.
   - Replacement sketch:
     ```go
     got := scheduleRows.Rows()
     want := [][]string{{"cmd_1", "pending", "2026-04-17T12:00:00Z", "hello"}}
     if diff := cmp.Diff(want, got); diff != "" { t.Fatal(diff) }
     ```
   - Effort: S.

2. WEAK: `cmd/coverage_regression_test.go:202-254`
   - Intent: pending approval and cached batch result behavior.
   - Problem: combines two unrelated behaviors and only checks the approval count plus a raw progress substring. It does not assert emitted JSONL status, line numbers, or cached envelope data.
   - Replacement sketch:
     ```go
     lines := decodeJSONLLines(t, stdout.Bytes())
     assertStatus(t, lines[0], "pending_approval")
     assertStatus(t, lines[1], "ok")
     assertCommandID(t, lines[1], "cmd_cached")
     ```
   - Effort: S.

3. WEAK/FLAKY: `internal/auth/oauth_test.go:376`, `:459`, `:523`, `:592`
   - Intent: drive the loopback callback after `CompleteLogin` starts waiting.
   - Problem: fixed sleeps are a hidden scheduling assumption. On a loaded runner, the callback can race the server startup or waste time.
   - Replacement sketch:
     ```go
     callbackDone := fireCallbackUntilReady(t, request.RedirectURI, request.State)
     session, err := CompleteLogin(ctx, request, "default", "official", opts)
     ```
   - Effort: M.

4. WEAK: `internal/auth/store_coverage_test.go:48-57`
   - Intent: keyring helper coverage.
   - Problem: tests constructor internals and key formatting, while `KeyringStore.LoadSession`, `SaveSession`, and `DeleteSession` remain 0% covered.
   - Replacement sketch:
     ```go
     // Extract keyring backend behind a tiny interface, then fake it here.
     store := NewKeyringStoreWithBackend("svc", fakeKeyring)
     require.NoError(t, store.SaveSession(t.Context(), session))
     ```
   - Effort: M.

5. WEAK: `internal/privacy/redact_test.go:8-45`
   - Intent: prove PII redaction.
   - Problem: leak-only assertions can pass while structure is destroyed. `Form` is 0% covered.
   - Replacement sketch:
     ```go
     got := string(Form([]byte("client_secret=s&count=10")))
     if got != "client_secret=REDACTED&count=10" { t.Fatal(got) }
     ```
   - Effort: S.

## Missing Tests Report

| Module | Missing Coverage | Severity | Suggested Test |
|---|---|---:|---|
| `cmd/app.go:98-100` | Public `Execute` wrapper is 0%. | Low | Set `os.Args` with `t.Setenv`/restore and assert `Execute(t.Context(), BuildInfo{})` returns version success. |
| `cmd/app.go:283-305` | `openBrowser` is 0%; platform command mapping and error wrapping untested. | Medium | Extract command starter seam, table-test darwin/linux/windows/default behavior. |
| `cmd/app.go:308-319` | `defaultIsInteractive` is 0%; TTY detection can drift. | Low | Extract stat funcs or test via injected dependency only; not critical. |
| `cmd/app.go:750-753` | `idempotencyRecord` failure logging is 0%. | Medium | Use store fake returning error and log sink assertion that primary command does not fail. |
| `cmd/batch.go:365-380` | `batchErrorCode` only 25%; 403/404/429/422 branches missing. | Medium | Table-test all `api.Error` statuses, mirror `mapTransportError` cases. |
| `cmd/batch.go:880-908` | `paceRateLimit` only 15.8%; no no-op, malformed reset, context-cancel, or sleep path tests. | High | Inject `Now`/timer or a sleep function; assert low remaining with future reset waits/cancels deterministically. |
| `cmd/approval.go:196-507` | `newApprovalRunCommand` only 34.9% and carries six command execution paths. | High | Split per-command replay into `runApprovedPostCreate`, `runApprovedPostEdit`, etc.; add one focused test per unsupported/invalid branch. |
| `cmd/plan.go:105-123` | `newPlanPostEditCommand` 42.9%; missing patch combinations and invalid visibility tests. | Medium | Table-test text-only, visibility-only, both, no changes, invalid visibility. |
| `internal/api/official.go:680-693` | `uploadRetryWait` 0%; retry/cancel behavior untested. | High | Inject sleeper/backoff or call with canceled context and max attempts; add upload 500->201 retry test. |
| `internal/api/official.go:698-790` | `EditPost` 52.4%; validation and response variants under-covered. | Medium | Test missing URN, no fields, invalid response body, missing header fallback. |
| `internal/approval/approval.go:499-502` | `MemoryStore.StagedPath` 0%; helper is test-only but public. | Low | Either unexport or test via staged entry path. |
| `internal/approval/approval.go:531-542` | `ResolvePath` 0%; env/XDG/home fallback untested. | Medium | Mirror audit/idempotency path tests with `t.Setenv`. |
| `internal/audit/audit.go:120-124` | `NoopSink.Append` 0%. | Low | Direct no-op assertion or accept as trivial. |
| `internal/auth/keyring.go:29-77` | Keyring load/save/delete are 0%. | High | Introduce backend seam; fake success, not-found, corrupt JSON, and delete error. |
| `internal/idempotency/idempotency.go:293-318` | Memory/no-op store helper methods are 0%. | Low | Direct assertions for no-op behavior and Entries copy semantics. |
| `internal/privacy/redact.go:73-87` | `Form` is 0%. | Medium | Cover sensitive key, PII value, invalid form, and preserved non-sensitive fields. |
| `internal/schedule/schedule.go:285-322` | FileStore `Due` 0%. | High | Add pending/running/completed files and malformed JSON; assert sorted due subset and limit. |
| `internal/schedule/schedule.go:384-405`, `:667-684` | File and memory `MarkRetrying` are 0%. | High | Test failed->pending clears `LastError`, invalid states fail, missing IDs fail. |
| `internal/schedule/schedule.go:398-405` | File `MarkCancelled` is 0%. | Medium | Test pending->cancelled and running rejection. |

## Code Quality Assessment

| Component | Rating | Evidence |
|---|---:|---|
| `cmd` | Needs Improvement | High command constructor complexity: `newApprovalRunCommand` cyclomatic 66 / cognitive 191, `newPostCreateCommand` cognitive 59, `newDoctorCommand` cognitive 57. |
| `internal/api` | Good | Strong transport seam and httptest coverage. Some large methods and raw `map[string]any` payload assembly are acceptable at REST boundary but should be localized further. |
| `internal/auth` | Good | Clear OAuth seams and tests; callback tests use timing sleeps and keyring lacks injectable backend tests. |
| `internal/output` | Good | Schema-first contract is strong. Main schema fixture test is too large to maintain easily. |
| `internal/config` | Excellent | Good validation and precedence coverage. |
| `internal/httprecord` | Good | Good redaction/record/replay tests; silent malformed-line behavior should be documented if intentional. |
| `internal/approval` | Needs Improvement | State machine is clean, but file store lacks corruption/path/duplicate tests. |
| `internal/schedule` | Needs Improvement | Good model, but state transitions are duplicated between file and memory stores and several transitions are uncovered. |
| `internal/idempotency` | Good | Append-only design is simple; corrupted lines are silently skipped without explicit tests/documentation. |
| `internal/audit` | Good | Redaction and concurrency covered. |
| `internal/privacy` | Needs Improvement | Security-sensitive helpers need broader data-shape tests. |
| `internal/plan` | Good | Focused API and adequate coverage. |
| `main.go` | Good | Thin wiring; no direct test needed. |

### Maintainability Findings

1. High: `cmd/approval.go:196-507` violates SRP.
   - Current:
     ```go
     switch cmdName {
     case "post create":
         text, _ := payloadMap["text"].(string)
         visStr := "PUBLIC"
     ```
   - Recommended:
     ```go
     runner := approvedCommandRunner(cmdName)
     result, audit, err := runner.Run(cmd.Context(), approvedRunInput{entry, session, transport})
     ```
   - Rationale: approval replay mixes payload decoding, validation, upload orchestration, transport dispatch, idempotency, audit, and rendering in one closure.
   - Effort: L.

2. High: `cmd/post.go:66-240` duplicates validation/preview/upload behavior used by approval replay.
   - Current:
     ```go
     if imagePath != "" {
         if _, statErr := os.Stat(imagePath); statErr != nil {
             return a.validationFailure(cmd, "cannot read image file", statErr.Error())
         }
     ```
   - Recommended:
     ```go
     req, preview, err := buildPostCreateIntent(flags, session)
     if err != nil { return validationErr(err) }
     ```
   - Rationale: one builder should define text length, visibility, org author, image preview, and create request.
   - Effort: M.

3. High: `cmd/batch.go:880-908` has real-time sleep logic that is hard to test.
   - Current:
     ```go
     wait := time.Until(sleepUntil)
     select {
     case <-ctx.Done():
     case <-time.After(wait):
     }
     ```
   - Recommended:
     ```go
     wait := r.clock.Until(sleepUntil)
     r.sleeper.Sleep(ctx, wait)
     ```
   - Rationale: deterministic rate-limit pacing tests without wall-clock delays.
   - Effort: M.

4. Medium: `internal/schedule/schedule.go` duplicates state-transition rules in file and memory stores.
   - Current:
     ```go
     if e.State != StateFailed {
         return fmt.Errorf("%w: %s (state=%s, want failed)", ErrInvalidState, commandID, e.State)
     }
     e.State = StatePending
     ```
   - Recommended:
     ```go
     if err := transition(&e, StateFailed, StatePending); err != nil { return err }
     ```
   - Rationale: one transition helper keeps file and memory stores aligned.
   - Effort: M.

5. Medium: `internal/output/schema_test.go:18-1011` is a 45-fixture monolith.
   - Current:
     ```go
     fixtures := []schemaFixture{
         {name: "auth status", payload: []byte(`{...}`)},
     }
     ```
   - Recommended:
     ```go
     fixtures := loadSchemaFixtures(t, "testdata/schema/*.json")
     ```
   - Rationale: fixture files are easier to diff, regenerate, and map to command outputs.
   - Effort: M.

6. Medium: `internal/privacy/redact.go:73-87` is security-sensitive but untested.
   - Current:
     ```go
     values, err := url.ParseQuery(string(body))
     if err != nil { return []byte(redacted) }
     ```
   - Recommended:
     ```go
     require.Equal(t, "client_secret=REDACTED&count=10", string(Form(body)))
     ```
   - Rationale: record/replay token safety depends on this path.
   - Effort: S.

7. Low: test code frequently uses `context.Background()` despite repo idiom preferring `t.Context()`.
   - Current:
     ```go
     ExecuteContext(context.Background(), args, Dependencies{...}, BuildInfo{...})
     ```
   - Recommended:
     ```go
     ExecuteContext(t.Context(), args, Dependencies{...}, BuildInfo{...})
     ```
   - Rationale: tests inherit test cancellation and reduce leak risk.
   - Effort: S.

## Prioritized Fix Roadmap

### Immediate (Fix this week)

| Issue | Fix | Effort |
|---|---|---:|
| `cmd` coverage below required 75% | Add tests for `batchErrorCode`, `idempotencyRecord`, `plan post edit`, and approval-run invalid branches. | M |
| `internal/auth/keyring.go` 0% store coverage | Add injectable keyring backend and fake-backed tests. | M |
| `internal/privacy.Form` 0% coverage | Add form redaction tests for secrets, PII values, malformed input, and preserved fields. | S |
| `internal/schedule` transition gaps | Cover `MarkRetrying`, file `Due`, and file `MarkCancelled`. | M |
| Timing sleeps in OAuth tests | Replace fixed sleeps with retry-until-server-ready helper. | M |

### Short-term (Next sprint)

| Issue | Fix | Effort |
|---|---|---:|
| `approval run` complexity | Extract command-specific approved runners and shared payload decoding. | L |
| `post create` duplication | Extract post-create intent builder shared by live, approval, batch, and plan paths where appropriate. | M |
| Rate-limit pacing untestable | Inject clock/sleeper into `batchRunner`; cover cancel/no-op/sleep branches. | M |
| Schema fixture monolith | Move 45 schema fixtures into `internal/output/testdata/schema/`. | M |
| Coverage-oriented tests | Replace count-only assertions with full row/envelope comparisons. | S |

### Long-term (Backlog)

| Issue | Fix | Effort |
|---|---|---:|
| Command constructors remain long across CLI | Move validation/intent construction into package-local pure functions. | L |
| File and memory stores duplicate state machines | Centralize transition validation and state mutation helpers. | M |
| Integration coverage is local-only | Add optional record/replay cassettes for high-value LinkedIn adapter flows. | M |
| Documentation drift risk | Add a README capability-matrix test or generated doc check for command list/status. | M |

## Appendix: Full Issue List

1. HIGH: `cmd` coverage 71.1% violates the repo's 75% floor for touched/user-facing command code.
2. HIGH: `cmd/approval.go:196-507` has cyclomatic complexity 66 and cognitive complexity 191.
3. HIGH: `cmd/post.go:66-240` has cognitive complexity 59 and duplicates create/preview/upload rules.
4. HIGH: `cmd/batch.go:880-908` rate-limit pacing is mostly untested and wall-clock coupled.
5. HIGH: `internal/auth/keyring.go:29-77` keyring load/save/delete are 0% covered.
6. HIGH: `internal/schedule/schedule.go:285-322` file-store due selection is 0% covered.
7. HIGH: `internal/schedule/schedule.go:384-405`, `:667-684` retry transitions are 0% covered.
8. MEDIUM: `internal/privacy/redact.go:73-87` form redaction is 0% covered.
9. MEDIUM: `internal/api/official.go:680-693` upload retry wait is 0% covered.
10. MEDIUM: `cmd/batch.go:365-380` batch error classification is only 25% covered.
11. MEDIUM: `cmd/plan.go:105-123` plan post edit is only 42.9% covered.
12. MEDIUM: `cmd/app.go:750-753` idempotency record failure path is 0% covered.
13. MEDIUM: `internal/approval/approval.go:531-542` approval path resolution is 0% covered.
14. MEDIUM: `internal/api/official.go:698-790` edit-post validation/response branches are under-covered.
15. MEDIUM: `internal/output/schema_test.go:18-1011` is a 992-line schema fixture monolith.
16. MEDIUM: `internal/auth/oauth_test.go:376`, `:459`, `:523`, `:592` use fixed sleeps.
17. MEDIUM: `cmd/coverage_regression_test.go:202-254` mixes two concerns and asserts raw substrings.
18. MEDIUM: `internal/output/format_coverage_test.go:59-107` uses count-only tabular assertions.
19. LOW: `cmd/app.go:98-100` public `Execute` wrapper is 0% covered.
20. LOW: `cmd/app.go:283-305` browser launcher is 0% covered.
21. LOW: `cmd/app.go:308-319` default TTY detection is 0% covered.
22. LOW: `internal/approval/approval.go:499-502` exported test helper `StagedPath` is 0% covered.
23. LOW: `internal/audit/audit.go:120-124` no-op sink append is 0% covered.
24. LOW: `internal/idempotency/idempotency.go:293-318` memory/no-op helper paths are 0% covered.
25. LOW: widespread test `context.Background()` use diverges from repo's `t.Context()` idiom.

## Wave-2 Review-and-Fix Cycle

Date: 2026-05-13
Scope: review/wave-fixes branch at worktree `review-fix-wave-2`
Gates: `make ci` clean (vet + golangci-lint 0 issues + test + race + govulncheck).

### Summary

- Initial review surfaced 17 findings: H1-H4 (high), M1-M14 (medium), L1-L2 (low). All 17 landed as atomic commits.
- A post-validation re-scan surfaced 4 additional issues — H1 approval data-loss, H2 idempotency cross-process race, M6 schedule state-transition race, plus crash-durability residuals. All resolved.
- No new findings were carried forward; the wave is closed.

### Highest-impact findings

1. HIGH (post-validation): `internal/approval` persisted payload data loss.
   - Symptom: `approval.Stage` ran `privacy.JSON` over the payload before disk write. On `approval run`, dispatch read the stored payload back literally and posted `"REDACTED"` strings to LinkedIn.
   - Fix: removed the redaction at persist time. File mode `0o600` on the approval directory remains the access-control boundary; redaction stays scoped to audit previews and HTTP cassettes.
   - Commit: `c4faef4`.

2. HIGH (post-validation): `internal/idempotency` cross-process race between Lookup and Record.
   - Symptom: callers performed `Lookup` then `Record` as two separate calls. Two concurrent processes could both miss, both execute the side-effect, and both record.
   - Fix: extended `Store` with `Acquire(ctx, key) (release, error)` taking a per-key sidecar `flock`. Justified the interface expansion in code comments on the consumer-side seam; `MemoryStore.Acquire` is documented as non-reentrant.
   - Commits: `c911813`, `48b4222`.

3. MEDIUM (M3/M4): refresh hard-fail policy change in `cmd` token refresh.
   - Before: `maybeRefreshSession` logged a warning and proceeded when the sidecar flock or `SaveSession` failed.
   - After: both failures are now fatal. Rationale: refresh-token rotation makes a silent half-state (new token issued, not persisted) strictly worse than an upfront error the caller can retry.
   - Commits: `7cc797c`, `d277e79`.

### Full commit list (most recent first)

```
e073fe1 fix(auth): surface oauth callback server Shutdown errors (M5)
48b4222 docs(idempotency): document MemoryStore.Acquire is non-reentrant
646f5e1 fix(batch): log idempotency marshal/record errors instead of dropping (L2)
60ded12 fix(batch): fsync progress sidecar to survive kernel crash (M8)
7cc797c fix(cmd): hard-fail token refresh on lock or save error (M3, M4)
0d3c625 fix(httprecord): fsync cassette writes and serialise with sidecar flock (M1, M2)
c911813 fix(idempotency): hold sidecar flock during Lookup to close cross-process race (H2-followup)
d45ba33 fix(schedule): serialize state transitions across processes with sidecar flock (M6-followup)
c4faef4 fix(approval): do not redact persisted payload — dispatch reads it back literally (H1)
ec33b22 fix(schedule): fsync writes and directories for crash durability (M4/M5 follow-up)
9fd04f8 test(api): widen HTTP-date Retry-After window to 2s to deflake test
14ccf1c style: gofmt + gocritic fixes in execute and redact
ebe7bd1 refactor(output): use typed ScheduleRunStatus in ScheduleRunResult (M7)
d277e79 fix(cmd): serialize token refresh to prevent concurrent-process race (M13)
22f3bc3 refactor(output): centralize output-mode validation (M12)
14ff2dc refactor(schedule): add Now clock seam on stores for test injection (L2)
fe4459a fix: fsync append-only stores before close to survive kernel crash (M4, M5)
af74487 docs(batch): document results-channel ownership invariant (L1)
b46eef5 fix(httprecord): canonicalize URL for record/replay matching (M6)
c1fb7ce fix(api): decode percent-encoded URN keys in SocialMetadata (M10)
89c1568 refactor(schedule): drop redundant duplicate-key pre-check in Add (M14)
ab4ce0c fix(privacy): redact inline "Bearer <token>" strings (M11)
ef95aea fix(execute): use settings.RequireApproval, honor env/config (M3)
adf4b21 fix(api): honor Retry-After header on 429/503 retries (H2)
29a2c5b docs(execute): document settings mutation invariant (H4)
708b012 fix(idempotency): hold sidecar flock across Prune read+rewrite (H3)
d87a4d4 fix(plan): canonicalize Args through json.Number for stable SHA256 (M8)
8ed8a32 fix(plan): register --notes persistent flag (H1)
```

