# Curate Initial Commit — Land golink v3 Without MCP (Path B)

> **Status: Completed (shipped in commit `fbcd7c0` and prior).** The MCP layer
> never landed in git history (`internal/mcp/`, `cmd/mcp.go` do not exist).
> Keeping this plan as an archival record of the Path-B approach. Do not
> re-execute — there is nothing left to curate.

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Curate the existing working-tree WIP so the MCP layer never lands in git history. Ship the golink v3 implementation, CI config, and planning docs across three clean commits.

**Architecture:** Path B — the MCP code in the working tree (`cmd/mcp.go`, untracked `internal/mcp/`, MCP-tagged sections in schema/types/tests/docs) gets removed from the working tree *before* staging. The final commits contain no MCP. HEAD goes from scaffold (`8eceb1e Prepare repo for open source`) directly to "golink v3, CLI-only".

**Tech Stack:** Go 1.26.2, cobra, `golangci-lint`, `govulncheck`, `gotestsum`. No new libraries.

---

## Context for implementers

**Current state (HEAD: `8eceb1e`):**
- `cmd/mcp.go` tracked but is a 16-line stub with no implementation
- `internal/mcp/` does not exist in HEAD
- The full v3 implementation (including MCP expansion) lives in the working tree as modifications + untracked files
- See `docs/superpowers/specs/2026-04-17-golink-feature-roadmap.md` §2 for the decision to drop MCP

**Path-B delta from the original removal plan:**
- No `git rm` + commit cycle per file — we curate the working tree pre-stage
- `internal/mcp/` untracked — plain `rm -r` drops it with no git cost
- `cmd/mcp.go` tracked — `git rm` vs. HEAD stub, folded into the single implementation commit
- Three commits total: implementation, CI config, docs

**Three planned commits:**
1. `feat: implement golink v3 (CLI, JSON contract, LinkedIn REST adapter)` — all of cmd/, internal/api, internal/auth, internal/config, internal/output, schemas, main.go, Makefile, README.md, CLAUDE.md, PROMPT_golink.md, .claude/rules/*, .claude/skills/*, cmd/mcp.go deletion, go.mod/go.sum
2. `ci: add GitHub Actions workflow and golangci-lint config` — `.github/`, `.golangci.yml`
3. `docs: add feature roadmap and Path-B execution plan` — `docs/superpowers/specs/`, `docs/superpowers/plans/`

---

## File Structure

| Path | Change relative to HEAD | Included in commit |
|---|---|---|
| `cmd/mcp.go` | **git rm** (deleted) | C1 (implementation) |
| `internal/mcp/` (untracked) | `rm -r` (never added) | — |
| `cmd/root.go` | modify: drop `newMCPCommand(a),` from command tree | C1 |
| `go.mod` | modify: drop `github.com/mark3labs/mcp-go v0.48.0` line | C1 |
| `go.sum` | regenerate via `go mod tidy` | C1 |
| `internal/output/types.go` | modify: drop `encoding/json` import, `MCPToolResultData`, `MCPToolOutput` | C1 |
| `internal/output/schema_test.go` | modify: drop the "mcp tool" fixture | C1 |
| `schemas/golink-output.schema.json` | modify: drop `mcpToolOutput` from top-level `oneOf`, drop `mcpToolResultData` and `mcpToolOutput` `$defs` | C1 |
| `internal/api/transport.go` (untracked in HEAD) | new file; ensure doc comment does not mention MCP | C1 |
| `internal/api/doc.go` (untracked in HEAD) | new file; ensure package doc does not mention MCP | C1 |
| `README.md` | modify: drop MCP capability-matrix row, "MCP server" section, `mcp` from cobra list, `internal/mcp/` layout row | C1 |
| `CLAUDE.md` | modify: drop MCP from transport-seam bullet, delete MCP architecture bullet, drop `mcp` from cobra list, drop `internal/mcp/` row | C1 |
| `PROMPT_golink.md` (untracked in HEAD) | modify working-tree copy to strip 28 MCP refs before committing | C1 |
| All other WIP (cmd/*, internal/auth/*, internal/config/*, internal/output/{format,schema,enums,…}, etc.) | carry forward unchanged | C1 |
| `.github/` (untracked) | carry forward unchanged | C2 |
| `.golangci.yml` (untracked) | carry forward unchanged | C2 |
| `docs/superpowers/specs/*`, `docs/superpowers/plans/*` (untracked) | carry forward unchanged | C3 |

---

## Task 1: Establish baseline

**Files:** none modified.

- [ ] **Step 1: Confirm HEAD and working-tree state match expectations**

```bash
cd /Users/mudrii/src/golink
git status --short | wc -l
git log --oneline -2
```

Expected: `git status` lists ~35+ paths (24 modified + 14 untracked as of this plan's authoring). `git log` shows HEAD as `8eceb1e Prepare repo for open source` with `b780ef6` as the parent. If HEAD is different, stop and reconcile with the user.

- [ ] **Step 2: Confirm the MCP implementation is present in WIP**

```bash
cd /Users/mudrii/src/golink
ls internal/mcp/
wc -l cmd/mcp.go
```

Expected: `internal/mcp/` contains `doc.go`, `server.go`, `server_test.go`, `tools.go`. `cmd/mcp.go` is 80+ lines (the real implementation — HEAD stub is 16). If either check fails, the working tree is not in the state this plan assumes; stop.

- [ ] **Step 3: Run the full CI gate on the current working tree**

```bash
cd /Users/mudrii/src/golink
make ci
```

Expected: exits 0. If anything fails here, the baseline is not green and MCP curation is not the cause — surface the failure and stop.

---

## Task 2: Remove MCP from source code

**Rationale:** Edit all Go / JSON source files so the working tree reflects the post-MCP shape. No commits yet — we stage at the end.

**Files:**
- Delete: `cmd/mcp.go` (tracked — use `git rm`)
- Delete: `internal/mcp/` (untracked — use `rm -r`)
- Modify: `cmd/root.go` (line 62)
- Modify: `internal/output/types.go` (lines 4, 336-340, 397-398)
- Modify: `internal/output/schema_test.go` (lines 391-404)
- Modify: `schemas/golink-output.schema.json` (lines 25, 405-413, 630-640)

- [ ] **Step 1: Delete `cmd/mcp.go`**

```bash
cd /Users/mudrii/src/golink
git rm cmd/mcp.go
```

Expected: stages the deletion. `git status` shows `D cmd/mcp.go`.

- [ ] **Step 2: Delete the untracked `internal/mcp/` package**

```bash
cd /Users/mudrii/src/golink
rm -r internal/mcp
```

Expected: directory gone. `git status` drops the `?? internal/mcp/` row.

- [ ] **Step 3: Drop `newMCPCommand(a),` from the root command tree**

Edit `cmd/root.go` lines 55-64. Current block:

```go
	rootCmd.AddCommand(
		newAuthCommand(a),
		newProfileCommand(a),
		newPostCommand(a),
		newCommentCommand(a),
		newReactCommand(a),
		newSearchCommand(a),
		newMCPCommand(a),
		newVersionCommand(a),
	)
```

Delete only line 62 (`		newMCPCommand(a),`). Resulting block:

```go
	rootCmd.AddCommand(
		newAuthCommand(a),
		newProfileCommand(a),
		newPostCommand(a),
		newCommentCommand(a),
		newReactCommand(a),
		newSearchCommand(a),
		newVersionCommand(a),
	)
```

- [ ] **Step 4: Delete `MCPToolResultData` and drop the now-unused `encoding/json` import**

Edit `internal/output/types.go`. At lines 336-340, delete:

```go
// MCPToolResultData is the standard wrapper for MCP tool JSON.
type MCPToolResultData struct {
	Tool       string          `json:"tool"`
	ResultJSON json.RawMessage `json:"result_json"`
}
```

At lines 397-398, delete:

```go
// MCPToolOutput is the schema-aligned MCP tool envelope.
type MCPToolOutput = SuccessEnvelope[MCPToolResultData]
```

Then confirm `encoding/json` is now unused in this file:

```bash
cd /Users/mudrii/src/golink
grep -n "json\." internal/output/types.go
```

Expected: no matches (only struct-tag `json:"…"` strings remain, which do not require the import). If matches surface, leave the import.

If unused, delete line 4 (`	"encoding/json"`). Tidy the import block so there is no trailing blank inside the parens.

- [ ] **Step 5: Delete the "mcp tool" fixture from the schema round-trip test**

Edit `internal/output/schema_test.go` lines 391-404. Delete the block:

```go
		{
			name: "mcp tool",
			payload: []byte(`{
				"status": "ok",
				"command_id": "cmd_mcp_tool_01",
				"command": "mcp serve",
				"transport": "official",
				"generated_at": "2026-04-16T10:30:00Z",
				"data": {
					"tool": "golink_create_post",
					"result_json": {"status":"ok","id":"urn:li:share:3"}
				}
			}`),
		},
```

(14 lines including the trailing comma.) Preceding "validation error" fixture and following "version" fixture remain unchanged.

- [ ] **Step 6: Remove `mcpToolOutput` from the schema's top-level `oneOf`**

Edit `schemas/golink-output.schema.json` line 25:

```json
    { "$ref": "#/$defs/mcpToolOutput" },
```

Delete the line. Preceding `searchPeopleOutput` `$ref` and following `versionOutput` `$ref` remain unchanged.

- [ ] **Step 7: Remove the `mcpToolResultData` `$def`**

At lines 405-413, delete:

```json
    "mcpToolResultData": {
      "type": "object",
      "additionalProperties": false,
      "required": ["tool", "result_json"],
      "properties": {
        "tool": { "type": "string", "minLength": 1 },
        "result_json": { "description": "Embedded JSON value (object, array, etc.) — not a string-encoded blob" }
      }
    },
```

- [ ] **Step 8: Remove the `mcpToolOutput` `$def`**

At lines 630-640, delete:

```json
    "mcpToolOutput": {
      "type": "object",
      "unevaluatedProperties": false,
      "allOf": [{ "$ref": "#/$defs/baseEnvelopeProperties" }],
      "required": ["status", "command_id", "command", "transport", "generated_at", "data"],
      "properties": {
        "status": { "const": "ok" },
        "command": { "const": "mcp serve" },
        "data": { "$ref": "#/$defs/mcpToolResultData" }
      }
    },
```

- [ ] **Step 9: Verify schema is valid JSON and no internal `$ref` dangles**

```bash
cd /Users/mudrii/src/golink
python3 -c "import json; s=json.load(open('schemas/golink-output.schema.json')); print('OK')"
grep -n "mcpTool" schemas/golink-output.schema.json || echo "CLEAN_OK"
```

Expected: `OK` then `CLEAN_OK`. If either step fails, the deletions above left a dangling reference.

- [ ] **Step 10: Verify Go compiles and output tests pass**

```bash
cd /Users/mudrii/src/golink
go build ./...
go test ./internal/output/...
```

Expected: build exits 0; schema round-trip tests pass (fixture count decreased by one).

---

## Task 3: Remove MCP from `internal/api` doc comments

**Rationale:** `internal/api/` is untracked — this is the first time these files enter git. Make sure the package doc and `Transport` interface doc reflect CLI-only, not CLI+MCP.

**Files:**
- Modify: `internal/api/transport.go` (doc lines for `Transport`)
- Modify: `internal/api/doc.go` (package doc)

- [ ] **Step 1: Rewrite the `Transport` interface doc comment**

Edit `internal/api/transport.go` lines 9-11:

```go
// Transport is the contract that both official and unofficial LinkedIn
// adapters must implement. Each method returns domain types that the CLI and
// MCP tooling can render directly into response envelopes.
```

Replace with:

```go
// Transport is the contract that both official and unofficial LinkedIn
// adapters must implement. Each method returns domain types that the CLI
// renders directly into --json response envelopes.
```

- [ ] **Step 2: Rewrite the package doc**

Edit `internal/api/doc.go` lines 1-5:

```go
// Package api defines the transport abstraction over LinkedIn's APIs and
// houses the official REST adapter plus a pluggable fallback for unofficial
// transports. Commands and the MCP server interact with LinkedIn exclusively
// through the Transport interface so the wire details stay here.
package api
```

Replace with:

```go
// Package api defines the transport abstraction over LinkedIn's APIs and
// houses the official REST adapter plus a pluggable fallback for unofficial
// transports. Commands interact with LinkedIn exclusively through the
// Transport interface so the wire details stay here.
package api
```

- [ ] **Step 3: Verify vet and lint still clean**

```bash
cd /Users/mudrii/src/golink
go vet ./...
golangci-lint run ./...
```

Expected: both exit 0.

---

## Task 4: Drop `mcp-go` dependency and run `go mod tidy`

**Files:**
- Modify: `go.mod` (line 7)
- Modify: `go.sum` (regenerated)

- [ ] **Step 1: Confirm no Go source references `mark3labs/mcp-go`**

```bash
cd /Users/mudrii/src/golink
grep -R "mark3labs/mcp-go" --include="*.go" . || echo "NO_CODE_REF_OK"
```

Expected: prints `NO_CODE_REF_OK`. If any file still imports `mcp-go`, Task 2 or Task 3 missed a spot.

- [ ] **Step 2: Remove the `mcp-go` line from `go.mod`**

Edit `go.mod` line 7:

```
	github.com/mark3labs/mcp-go v0.48.0
```

Delete the line.

- [ ] **Step 3: Run `go mod tidy`**

```bash
cd /Users/mudrii/src/golink
go mod tidy
```

Expected: exits 0. `go.sum` updates in-place to drop `mcp-go` and any transitives only `mcp-go` needed.

- [ ] **Step 4: Confirm `mcp` vanished from module metadata**

```bash
cd /Users/mudrii/src/golink
grep -c "mcp" go.mod go.sum
```

Expected: both print `0`.

---

## Task 5: Remove MCP from user-facing docs

**Files:**
- Modify: `README.md` (lines 29, 71-87, 139, 143)
- Modify: `CLAUDE.md` (lines 8, 11, 18, 22)
- Modify: `PROMPT_golink.md` (28 occurrences — mapped below)

- [ ] **Step 1: `README.md` — remove the `mcp serve` capability-matrix row**

At line 29, delete:

```
| `mcp serve` | ✅ | unit | Stdio MCP server with 11 tools |
```

- [ ] **Step 2: `README.md` — delete the entire "MCP server" section**

At lines 71-88, delete the H2 header through the blank line before "Transport modes":

```markdown
## MCP server

```sh
golink mcp serve
```

Starts a stdio MCP server exposing these tools:

```
golink_auth_status, golink_profile_me,
golink_create_post, golink_list_posts, golink_get_post, golink_delete_post,
golink_add_comment, golink_list_comments,
golink_add_reaction, golink_list_reactions,
golink_search_people
```

Each tool returns JSON text matching golink's envelope shapes, with `status:"unsupported"` for features that are not available in the selected transport.

```

- [ ] **Step 3: `README.md` — update the cobra commands layout line**

At line 139, replace:

```
cmd/                       cobra commands (auth, post, comment, react, search, mcp, version)
```

with:

```
cmd/                       cobra commands (auth, post, comment, react, search, version)
```

- [ ] **Step 4: `README.md` — delete the `internal/mcp/` layout row**

At line 143, delete:

```
internal/mcp/              stdio MCP server wiring all 11 tools to Transport
```

- [ ] **Step 5: `README.md` — verify clean**

```bash
cd /Users/mudrii/src/golink
grep -in "mcp" README.md || echo "CLEAN_OK"
```

Expected: `CLEAN_OK`.

- [ ] **Step 6: `CLAUDE.md` — rewrite the transport-seam bullet**

At line 8, replace:

```
- **Transport seam**: `internal/api/transport.go` (interface) → `official.go` (live LinkedIn adapter) / `noop.go` (fallback). Every CLI command and every MCP tool goes through `Transport`.
```

with:

```
- **Transport seam**: `internal/api/transport.go` (interface) → `official.go` (live LinkedIn adapter) / `noop.go` (fallback). Every CLI command goes through `Transport`.
```

- [ ] **Step 7: `CLAUDE.md` — delete the MCP architecture bullet**

At line 11, delete:

```
- **MCP**: `mcp-go` v0.48+ functional-options style; 11 tools in `internal/mcp/tools.go`
```

- [ ] **Step 8: `CLAUDE.md` — update the cobra commands line**

At line 18, replace:

```
cmd/               cobra commands (auth, post, comment, react, search, mcp, version)
```

with:

```
cmd/               cobra commands (auth, post, comment, react, search, version)
```

- [ ] **Step 9: `CLAUDE.md` — delete the `internal/mcp/` row**

At line 22, delete:

```
internal/mcp/      stdio MCP server
```

- [ ] **Step 10: `CLAUDE.md` — verify clean**

```bash
cd /Users/mudrii/src/golink
grep -in "mcp" CLAUDE.md || echo "CLEAN_OK"
```

Expected: `CLEAN_OK`.

- [ ] **Step 11: `PROMPT_golink.md` — remove Product Scope bullet**

At line 79, delete:

```
- `mcp serve` with tool registration
```

- [ ] **Step 12: `PROMPT_golink.md` — remove `mcp.go` from `cmd/` layout**

At line 133, delete:

```
│   ├── mcp.go             # MCP server entry
```

- [ ] **Step 13: `PROMPT_golink.md` — remove `internal/mcp/` subtree from layout**

At lines 151-153, delete:

```
│   └── mcp/
│       ├── server.go
│       └── tools.go
```

- [ ] **Step 14: `PROMPT_golink.md` — rewrite `CLI/MCP` language**

At line 233, replace:

```
  - Default CLI/MCP behavior: list the authenticated member's own posts. Resolve `authorURN` from the current session/profile; do not require a user-supplied `--author-urn` flag for the default case.
```

with:

```
  - Default CLI behavior: list the authenticated member's own posts. Resolve `authorURN` from the current session/profile; do not require a user-supplied `--author-urn` flag for the default case.
```

- [ ] **Step 15: `PROMPT_golink.md` — remove `mcp-go` dependency row**

At line 301, delete:

```
github.com/mark3labs/mcp-go           v0.48.0
```

- [ ] **Step 16: `PROMPT_golink.md` — delete the "## MCP Tools" section**

At lines 499-546, delete the H2 header through the final "Verify the exact helper names…" paragraph (48 lines inclusive of the trailing blank). The preceding "Error type" section and the following "## JSON Schema Bundle" section remain unchanged.

- [ ] **Step 17: `PROMPT_golink.md` — update JSON Schema Bundle lead-in**

At line 551, replace:

```
Use this as the canonical output contract for all command payloads and MCP tool outputs.
```

with:

```
Use this as the canonical output contract for all command payloads.
```

- [ ] **Step 18: `PROMPT_golink.md` — remove `MCPToolResultData` + `MCPToolOutput` from the struct bundle**

At lines 796-800, delete:

```go
type MCPToolResultData struct {
    Tool       string          `json:"tool"`
    ResultJSON json.RawMessage `json:"result_json"`
}
```

Plus the preceding blank separator. At line 827, delete:

```go
type MCPToolOutput          = SuccessEnvelope[MCPToolResultData]
```

- [ ] **Step 19: `PROMPT_golink.md` — delete the "### MCP tool input schemas" reference table**

At lines 831-859, delete the H3 subsection through the final row of the MCP input-schema table (29 lines).

- [ ] **Step 20: `PROMPT_golink.md` — remove `mcp` from validated command families**

At line 877, replace:

```
- Validate `auth`, `profile`, `post`, `comment`, `react`, `search`, `mcp`, `version`.
```

with:

```
- Validate `auth`, `profile`, `post`, `comment`, `react`, `search`, `version`.
```

- [ ] **Step 21: `PROMPT_golink.md` — delete Section 13 block**

At lines 931-936, delete:

```
### Section 13 — MCP Tools
- Ensure MCP stdio server runs and tool registry matches required list (all 11 tools).
- Ensure MCP payloads reuse standard envelopes.
- Verify `mcp-go` API compatibility with pinned version.

```

- [ ] **Step 22: `PROMPT_golink.md` — renumber Validation Checklist entry**

At line 954, delete:

```
13. MCP Tools: all required tools registered and reuse output envelopes.
```

Then renumber what was `14.`:

```
14. Validation & Tests: test plan includes success/error/dry-run for every schema type.
```

becomes:

```
13. Validation & Tests: test plan includes success/error/dry-run for every schema type.
```

- [ ] **Step 23: `PROMPT_golink.md` — verify clean**

```bash
cd /Users/mudrii/src/golink
grep -in "mcp" PROMPT_golink.md || echo "CLEAN_OK"
```

Expected: `CLEAN_OK`.

---

## Task 6: Pre-commit verification

**Files:** none modified — verification only.

- [ ] **Step 1: Run the full CI gate**

```bash
cd /Users/mudrii/src/golink
make ci
```

Expected: exits 0. `vet`, `golangci-lint`, `go test ./...`, `go test -race ./...`, `govulncheck ./...` all green.

- [ ] **Step 2: Confirm the binary builds and has no MCP subcommand**

```bash
cd /Users/mudrii/src/golink
go build -o /tmp/golink-path-b .
/tmp/golink-path-b --help 2>&1 | grep -i mcp || echo "NO_MCP_OK"
/tmp/golink-path-b mcp serve 2>&1 | head -3
```

Expected: first grep prints `NO_MCP_OK`. Second invocation fails with cobra's `unknown command "mcp"`.

- [ ] **Step 3: Audit for orphan MCP references across the repo (excluding plan docs)**

```bash
cd /Users/mudrii/src/golink
grep -RIn "mcp\|MCP" --include="*.go" --include="*.md" --include="*.json" --include="*.mod" --include="*.sum" . 2>/dev/null \
  | grep -v "^\./\.claude" \
  | grep -v "^\./docs/superpowers/"
```

Expected: no output. Survivors under `.claude/` (general rules) and `docs/superpowers/` (this plan + the spec) are intentional and excluded by the pipe.

- [ ] **Step 4: Survey what will go into each planned commit**

```bash
cd /Users/mudrii/src/golink
echo "=== Commit 1 candidates (implementation) ==="
git status --short | grep -vE "^\?\? \.github/|^\?\? \.golangci\.yml|^\?\? docs/"
echo
echo "=== Commit 2 candidates (CI) ==="
git status --short | grep -E "^\?\? \.github/|^\?\? \.golangci\.yml"
echo
echo "=== Commit 3 candidates (docs) ==="
git status --short | grep -E "^\?\? docs/"
```

Expected: C1 lists all code + Makefile + README + CLAUDE + PROMPT + .claude/* + go.mod/go.sum + the `D cmd/mcp.go` deletion. C2 lists `.github/` and `.golangci.yml`. C3 lists `docs/`. If any path is miscategorized, address before staging.

---

## Task 7: Commit 1 — golink v3 implementation

**Files:** everything surveyed as "Commit 1 candidates" in Task 6 Step 4.

- [ ] **Step 1: Stage all implementation paths**

```bash
cd /Users/mudrii/src/golink
git add -u                       # stage modifications + the cmd/mcp.go deletion
git add .claude/                  # untracked updates to rules/skills
git add internal/api/             # new package
git add internal/auth/oauth_test.go internal/auth/session_test.go
git add internal/config/config_test.go internal/config/loader_test.go
git add internal/output/enums.go internal/output/enums_test.go
git add cmd/preflight_test.go cmd/transport_test.go
```

Confirm nothing unexpected is staged:

```bash
cd /Users/mudrii/src/golink
git status --short
```

Expected: `D cmd/mcp.go` and every modification / added file for the implementation is in the index. Only `.github/`, `.golangci.yml`, and `docs/` remain unstaged.

- [ ] **Step 2: Create the implementation commit**

```bash
cd /Users/mudrii/src/golink
git commit -m "$(cat <<'EOF'
feat: implement golink v3 (CLI, JSON contract, LinkedIn REST adapter)

Lands the full golink v3 surface on top of the open-source scaffold:

- cmd/: cobra commands for auth login/logout/status, profile me,
  post create/list/get/delete (with --dry-run), comment add/list,
  react add/list, search people, version. Signal-aware root
  context, strict --json envelope emission, global flags
  (--json, --dry-run, --verbose, --profile, --transport,
  --accept-unofficial-risk, --timeout).
- internal/api/: Transport interface, retryable HTTP client with
  Linkedin-Version + Rest.li headers and rate-limit parsing,
  official LinkedIn adapter for Posts API + Community Management
  API + OIDC userinfo, and NoopTransport fallback.
- internal/auth/: native PKCE S256 login with loopback callback,
  keyring-backed session store, profile-aware token resolution.
- internal/config/: viper settings with flag/env/file precedence.
- internal/output/: SuccessEnvelope generic, typed error codes,
  schema round-trip validator, and enum parsers. JSON schema at
  schemas/golink-output.schema.json is the single machine-readable
  contract for every command output.
- Documentation: README, CLAUDE.md, PROMPT_golink.md aligned with
  the implementation. Makefile adds lint/test/race/vuln/ci targets.

The MCP layer that appeared in an earlier iteration is intentionally
left out — the CLI's --json mode plus the JSON schema is the single
agent-facing contract. See the ADR in
docs/superpowers/specs/2026-04-17-golink-feature-roadmap.md §2.
EOF
)"
```

Expected: commit lands. `git log --oneline -3` shows the new commit on top of `8eceb1e`.

- [ ] **Step 3: Confirm CI still passes on the new HEAD**

```bash
cd /Users/mudrii/src/golink
make ci
```

Expected: exits 0. If this fails but Task 6 Step 1 passed, something happened during staging — investigate.

---

## Task 8: Commit 2 — CI configuration

**Files:** `.github/`, `.golangci.yml`.

- [ ] **Step 1: Stage CI paths only**

```bash
cd /Users/mudrii/src/golink
git add .github/ .golangci.yml
git status --short
```

Expected: only `.github/*` and `.golangci.yml` appear staged. `docs/` remains untracked.

- [ ] **Step 2: Create the CI commit**

```bash
cd /Users/mudrii/src/golink
git commit -m "$(cat <<'EOF'
ci: add GitHub Actions workflow and golangci-lint config

Wires the make ci gate (vet, golangci-lint v2, go test with race,
govulncheck) into a reusable GitHub Actions workflow. The .golangci.yml
is the authoritative lint config — matches CLAUDE.md guidance.
EOF
)"
```

Expected: commit lands.

---

## Task 9: Commit 3 — roadmap + execution plan docs

**Files:** `docs/superpowers/specs/2026-04-17-golink-feature-roadmap.md`, `docs/superpowers/plans/2026-04-17-remove-mcp-layer.md`.

- [ ] **Step 1: Stage the docs**

```bash
cd /Users/mudrii/src/golink
git add docs/
git status --short
```

Expected: only `docs/superpowers/specs/*` and `docs/superpowers/plans/*` appear staged. Working tree is now fully clean after this commit.

- [ ] **Step 2: Create the docs commit**

```bash
cd /Users/mudrii/src/golink
git commit -m "$(cat <<'EOF'
docs: add feature roadmap and Path-B execution plan

- specs/2026-04-17-golink-feature-roadmap.md: tiered feature list
  (P0 self-serve + agent UX, P1 partner-gated, P2 Voyager opt-in)
  with the MCP-removal ADR and LinkedIn API prereqs per tier.
- plans/2026-04-17-remove-mcp-layer.md: Path-B curation plan that
  keeps the MCP layer out of git history entirely rather than
  committing-then-removing.
EOF
)"
```

Expected: commit lands. `git status` reports a clean tree (modulo any still-WIP paths outside this plan's scope).

---

## Task 10: Final verification

**Files:** none modified.

- [ ] **Step 1: Inspect the 3-commit series**

```bash
cd /Users/mudrii/src/golink
git log --oneline -5
git log --stat -3 | head -120
```

Expected: three fresh commits on top of `8eceb1e`, with reasonable diffstat per commit. No fix-ups.

- [ ] **Step 2: Confirm the working tree is clean**

```bash
cd /Users/mudrii/src/golink
git status
```

Expected: "nothing to commit, working tree clean" (or only unexpected WIP not in scope of this plan, which should be surfaced and discussed).

- [ ] **Step 3: Record post-change schema digest**

```bash
cd /Users/mudrii/src/golink
shasum -a 256 schemas/golink-output.schema.json
```

Save the digest to the PR description or release notes. This is the committed, MCP-free contract.

- [ ] **Step 4: Smoke-test every remaining command**

```bash
cd /Users/mudrii/src/golink
go build -o /tmp/golink-final .
for cmd in auth profile post comment react search version; do
  echo "=== $cmd ==="
  /tmp/golink-final $cmd --help 2>&1 | head -3
done
```

Expected: each command prints its `Short` description + usage. `mcp` is not listed anywhere.

- [ ] **Step 5: Final CI pass on the committed tree**

```bash
cd /Users/mudrii/src/golink
make ci
```

Expected: exits 0. This is the handoff-ready state.

---

## Self-review

**Spec coverage** — mapping tasks to the Path-B goal and the roadmap §2 removal ADR:

| Roadmap ADR deletion | Task covering it |
|---|---|
| `internal/mcp/{doc,server,server_test,tools}.go` | Task 2 Step 2 |
| `cmd/mcp.go` | Task 2 Step 1 |
| `cmd/root.go:62` `newMCPCommand(a),` | Task 2 Step 3 |
| `go.mod:7` `mcp-go` line | Task 4 Step 2 |
| `go.sum` regenerate | Task 4 Step 3 |
| `MCPToolResultData`, `MCPToolOutput`, `encoding/json` import | Task 2 Step 4 |
| `schema_test.go` "mcp tool" fixture | Task 2 Step 5 |
| Schema `oneOf` branch + both `$defs` | Task 2 Steps 6, 7, 8 |
| `internal/api` doc comments | Task 3 Steps 1, 2 |
| `README.md` | Task 5 Steps 1–4 |
| `CLAUDE.md` | Task 5 Steps 6–9 |
| `PROMPT_golink.md` (28 refs) | Task 5 Steps 11–22 |

**Commit coverage** — Path B's three-commit history:

| Commit | Task |
|---|---|
| C1: implementation | Task 7 |
| C2: CI config | Task 8 |
| C3: docs | Task 9 |

**Placeholder scan** — no `TBD`, `TODO`, `add error handling`, `similar to Task N`. Every step has exact file paths, exact line numbers (where applicable), and exact edits shown.

**Type consistency** — `MCPToolResultData` and `MCPToolOutput` used consistently; lower-case `mcpToolResultData`/`mcpToolOutput` reserved for JSON-schema `$def` keys; `newMCPCommand` referenced only in Task 2 Step 3.

**Commit cadence** — 3 commits, each compiling + CI-green, clean separation of implementation / CI / docs.
