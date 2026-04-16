# golink Feature Roadmap (post-MCP, agent-first + LinkedIn-surface expansion)

**Status:** Draft · **Date:** 2026-04-17 · **Author:** research + design pass
**Scope:** Product roadmap, not an implementation plan. Each feature below is a
candidate; concrete implementation plans are produced per-feature via the
`writing-plans` workflow.

---

## 1 · Executive summary

golink today ships a tight core: PKCE OAuth, Posts API create/list/get/delete,
comments/reactions add/list, `search people` gated as `unsupported`, an MCP
stdio server, and a schema-validated JSON envelope for every command.

This roadmap does two things:

1. **Removes the MCP layer** — its 11 tools are 1:1 wrappers over
   `api.Transport` and carry a weaker schema than the CLI's JSON contract, so
   they add maintenance cost without unique value (see §2).
2. **Expands the LinkedIn surface and the agent-operator UX** along three
   tiers — self-serve P0, partner-gated P1, unofficial/Voyager P2 — with an
   explicit out-of-scope list (see §3–6).

The P0 tier is the actionable shipping list. P1 depends on LinkedIn partner
approvals. P2 is marked opt-in and ToS-risky — documented so we don't pretend
it doesn't exist, not endorsed for default implementation.

---

## 2 · ADR: remove the MCP layer (M1)

> **Status: implemented.** Path-B curation landed before git history began —
> `internal/mcp/`, `cmd/mcp.go`, and `mark3labs/mcp-go` never existed on `main`.
> The blast-radius list below is archival; none of the listed files exist
> today.

### Decision

Remove the MCP stdio server and its 11 tool surface. The CLI's `--json` mode
plus `schemas/golink-output.schema.json` remains the single agent-facing
contract.

### Context

The MCP layer (`internal/mcp/server.go`, `tools.go`, `cmd/mcp.go`) declares 11
tools, each of which is a thin pass-through to a method on `api.Transport`:

```go
// internal/mcp/tools.go:55-63 — create_post tool body
summary, err := cfg.Transport.CreatePost(ctx, api.CreatePostRequest{
    Text: text, Visibility: visibility,
})
```

The CLI command `cmd/post.go` makes the same `api.Transport.CreatePost` call.

**Observed gaps in the MCP layer:**

- Weaker schemas than the JSON bundle. Example: `tools.go:37` declares
  `visibility` as a plain string with a description `"PUBLIC|CONNECTIONS|
  LOGGED_IN"`, while `schemas/golink-output.schema.json:51-53` enforces the
  enum strictly. Agents get a *looser* contract via MCP than via
  `golink --json`.
- No session persistence, token amortization, batching, streaming, or
  caching — the reasons MCP typically earns its place over CLI.
- No independent auth — `mcp serve` still requires the CLI to have run
  `auth login`.

### Consequences

Accepted: any agent host that *only* speaks MCP cannot reach golink without a
shim. In 2026 this set is small (most agentic tools — Claude Code, Codex,
Cursor shell, aider, Cline, OpenHands, goose — use shell/Bash). If a
target platform requires MCP later, regenerating the wrapper from
`api.Transport` is ~2h of work.

Gained: one surface to maintain per feature; smaller dep graph (drop
`github.com/mark3labs/mcp-go`); tighter schema contract (CLI JSON envelope is
the single source of truth).

### Removal blast radius (files affected)

**Delete:**
- `internal/mcp/doc.go`
- `internal/mcp/server.go`
- `internal/mcp/server_test.go`
- `internal/mcp/tools.go`
- `cmd/mcp.go`

**Edit:**
- `cmd/root.go:62` — drop `newMCPCommand(a),` from command tree
- `go.mod:7` — drop `github.com/mark3labs/mcp-go v0.48.0`
- `go.sum` — clean via `go mod tidy`
- `internal/output/types.go:336-337, 397-398` — delete
  `MCPToolResultData` struct and `MCPToolOutput` alias
- `internal/output/schema_test.go` — delete MCP fixture + validation case
- `schemas/golink-output.schema.json` — delete `mcpToolOutput` branch in
  top-level `oneOf` and the `mcpToolResultData` / `mcpToolOutput` `$defs`
- `internal/api/transport.go:11`, `internal/api/doc.go` — revise doc comments
  that reference "MCP tooling"
- `README.md` — delete the "MCP server" section and related capability-matrix
  rows
- `CLAUDE.md` — revise the "Architecture" section to drop the MCP line
- `PROMPT_golink.md` — remove MCP references (28 occurrences): the `mcp serve`
  bullet in Product Scope, the MCP Tools section, MCP rows in Agent-Mode
  Requirements, the MCP input-schema reference table, and MCP entries in the
  validation checklist

### Out-of-scope for this ADR

- Rebuilding MCP as a *useful* layer (option M2 from the brainstorm) is
  deferred. If re-adopted, it ships features the CLI structurally can't —
  session-scoped auth refresh, batch/streaming tools, strictly typed schemas
  matching the JSON bundle. Not part of this roadmap.

---

## 3 · Tiers and tier meaning

| Tier | Definition | Prereqs |
|---|---|---|
| **P0** | Shippable with a self-serve LinkedIn developer app *or* purely client-side (no LinkedIn dependency) | `w_member_social`, `openid`, `profile`, `email` (self-serve) |
| **P1** | Requires LinkedIn partner approval or adds heavyweight content/organization surface | Community Management API approval, `w_organization_social`, Marketing Developer Platform, etc. |
| **P2** | Uses LinkedIn's internal Voyager API or equivalent scraping path | Violates LinkedIn ToS § 8.2; opt-in only behind `--transport=unofficial --accept-unofficial-risk` |

Transport legend in tables: 🟢 official · 🟡 partner-official · 🔴 unofficial · 🤖 agent-UX (no LinkedIn dep).

---

## 4 · Feature roadmap

### 4.1 · Auth & session (P0)

| Feature | Tier | Transport | Rationale |
|---|---|---|---|
| Auto refresh before access-token expiry | P0 | 🟢 | LinkedIn issues 60d access + 365d refresh tokens (Programmatic Refresh Tokens program). Currently golink re-prompts `auth login` on 401. |
| `auth refresh` explicit subcommand | P0 | 🟢 | Agents can force pre-batch refresh. |
| Silent re-auth fallback when programmatic refresh unavailable | P0 | 🟢 | LinkedIn bypasses the authz screen if member still logged in. |
| `auth list` / `auth switch <profile>` — first-class multi-account | P0 | 🟢 | `--profile` flag is partial; promote to subcommands. |
| `auth status --verbose` with scope-gated feature map | P0 | 🟢 | Prevents blind `unsupported` errors for agents planning a batch. |

### 4.2 · Posts — content types

| Feature | Tier | Transport | Rationale |
|---|---|---|---|
| Single image post (`post create --image path`) | P0 | 🟢 | Assets API `registerUpload` + SYNCHRONOUS_UPLOAD, then Posts API. Self-serve scope. |
| Edit post (`post edit <urn>`) | P0 | 🟢 | Posts API supports update where allowed. |
| Reshare / repost with commentary | P0 | 🟢 | Posts API ≥ `Linkedin-Version: 202209`. |
| Draft (`--lifecycle-state DRAFT`) | P0 | 🟢 | Lets agents stage then publish. |
| Client-side scheduled queue (`post schedule --at <rfc3339>`) | P0 | 🤖 | LinkedIn has no native scheduled-post API; golink stores queue locally and publishes on trigger. |
| `post preview` (URL unfurl preview, char count, mention resolution) | P0 | 🤖 | Pre-flight for agents. |
| Carousel / multi-image post | P1 | 🟢 | Posts API `multiImage` content type. |
| Video post + thumbnail + captions | P1 | 🟢 | Videos API multi-part upload with ETags. |
| Document post (PDF carousel) | P1 | 🟢 | Documents API recipe. |
| Poll post | P1 | 🟢 | `content.poll` with 2–4 options; duration ONE/THREE/SEVEN/FOURTEEN_DAYS. |
| Article / long-form post | P1 | 🟢 | Where Posts API supports; otherwise partner-only. |
| Posts with @mentions | P1 | 🟢 | People Typeahead API + commentary annotations. |

### 4.3 · Organizations / pages

| Feature | Tier | Transport | Rationale |
|---|---|---|---|
| `org list` (orgs where member is admin) | P0 | 🟢 | Organization Access Control API. |
| `org me <urn>` detail | P0 | 🟢 | |
| Post as organization (`--as-org urn:li:organization:…`) | P0 | 🟢 | Requires admin role + `w_organization_social`. |
| Toggle comments on org post (`post moderation <urn> --close-comments`) | P1 | 🟢 | Social Metadata API `commentsState=CLOSED`. |
| `org followers` count + list | P1 | 🟢 | Community Management entitlement. |
| `org admins` list + roles | P1 | 🟢 | |
| Page messaging inbox | P1 | 🟡 | Partner-gated Pages Messaging API. |

### 4.4 · Social engagement (expands existing)

| Feature | Tier | Transport | Rationale |
|---|---|---|---|
| `social metadata <urn...>` — batch engagement state | P0 | 🟢 | One call returns counts + reaction summary — far cheaper than N× (list-comments + list-reactions). |
| Reply to comment (threaded) | P0 | 🟢 | Currently only top-level. |
| React to a comment (not just posts) | P0 | 🟢 | Comment URN as target. |
| `comment edit` / `comment delete` (own) | P0 | 🟢 | |
| `mentions list` (via Social Actions Notifications) | P1 | 🟢 | Key agent feature: poll and respond. |
| People typeahead for @mention lookup | P1 | 🟢 | Powers mention insertion. |
| Pin comment (org posts) | P1 | 🟢 | |

### 4.5 · Analytics

| Feature | Tier | Transport | Rationale |
|---|---|---|---|
| `post analytics <urn>` — impressions, engagement breakdown | P1 | 🟢 | Community Management analytics. |
| `post video-analytics <urn>` — VIDEO_PLAY, VIDEO_WATCH_TIME, VIDEO_VIEWER | P1 | 🟢 | `memberCreatorVideoAnalytics` endpoint, `Linkedin-Version ≥ 202506`. |
| `org analytics` — page follower growth, content performance | P1 | 🟢 | |
| Profile analytics (views, appearances) | P1 | 🟡 | Restricted. |
| Ad analytics (campaigns, spend, CPM) | P1 | 🟡 | Marketing Developer Platform. |

### 4.6 · People / network

| Feature | Tier | Transport | Rationale |
|---|---|---|---|
| `connections list` (own 1st-degree) | P1 | 🟡 | Connections API, partner-gated, 50-page max. |
| `connections size` | P1 | 🟡 | |
| Send invitation | P1 | 🟡 | Invitation API + LinkedIn 100/week platform cap. |
| `search people` (keywords, title, company, location) | P2 | 🔴 | Voyager `/search/cluster` — golink already returns `unsupported` on official. |
| `search companies` | P2 | 🔴 | Voyager. |
| Profile by vanity URL | P2 | 🔴 | Voyager. |
| Profile contact info | P2 | 🔴 | Voyager. |

### 4.7 · Messaging

| Feature | Tier | Transport | Rationale |
|---|---|---|---|
| `msg send --to <urn>` (user-initiated only) | P1 | 🟡 | Messages API — recipients OR threadUrn (not both); strict member-consent rules. |
| `msg threads list` / `msg read <threadUrn>` | P1 | 🟡 | Partner-gated. |
| `msg send` via unofficial transport | **out of scope** | 🔴 | Highest ban risk; refuse by design. |

### 4.8 · Jobs / Events

| Feature | Tier | Transport | Rationale |
|---|---|---|---|
| `events create / update / list / attendees` | P1 | 🟢 | Events management via Developer Solutions. |
| `jobs search` (keywords, location, remote, date) | P2 | 🔴 | Voyager — no self-serve equivalent. |
| `jobs get <urn>` + required skills | P2 | 🔴 | Voyager. |
| `jobs apply` | **out of scope** | 🔴 | Real-world consequences + ToS. |

### 4.9 · Feed reading

| Feature | Tier | Transport | Rationale |
|---|---|---|---|
| `feed timeline` (home feed) | P2 | 🔴 | Voyager — no official equivalent. |
| `feed posts --by <person\|org>` for any member | P2 | 🔴 | Voyager; current `post list` is self-only. |

### 4.10 · Agent-operator UX (the "C" cut)

The narrow list that makes golink uniquely good for LLM agents. All P0 and
all 🤖 (no LinkedIn dependency).

| Feature | Tier | Rationale |
|---|---|---|
| `--require-approval` — writes proposed payload to `$GOLINK_APPROVAL_DIR`, exits with new code `3`, resumes when operator signs off by touching a file | P0 | Supervised-agent gate. Pattern from gh-aw. |
| Append-only audit log at `$XDG_STATE_HOME/golink/audit.jsonl` — every mutating call + request_id + payload diff | P0 | Forensics, compliance, post-mortems. |
| `--idempotency-key <k>` — client dedup + server-side request ID | P0 | Agent retries don't double-post. |
| `golink batch <ops.jsonl>` — one process runs many operations with rate-limit coordination, emits `results.jsonl` | P0 | Agents naturally produce lists; one invocation beats 50 shell calls. |
| In-process rate-limit pacing — respect `X-RateLimit-Remaining` within a single process | P0 | Headers already parsed; just need the pacer. |
| `--compact` output mode — strips envelope noise for LLM context budget | P0 | rtk-style filtering. |
| `--output <json\|jsonl\|compact\|table>` | P0 | Table for humans, jsonl for agent streams. |
| Batch resume-on-partial-failure | P0 | Skip already-successful lines. |
| `golink doctor` — probes `/userinfo`, checks granted scopes, reports which features the current `Linkedin-Version` exposes, warns if token expires within 7d | P0 | First call an agent should make. |
| `golink open <urn>` — opens the LinkedIn URL for human spot-check | P0 | Useful in human-in-the-loop flows. |
| Plan-then-execute split: `golink plan post create --text "…"` emits a JSON plan; `golink execute plan.json` runs it unchanged | P0 | Prompt-injection-resistant pattern (ReversecLabs). |
| Request record/replay via `GOLINK_RECORD=file.json` / `GOLINK_REPLAY=file.json` | P0 | Deterministic tests; reproducible agent runs. |
| `--explain` — emit the exact HTTP request an invocation would make (more detailed than `--dry-run`) | P0 | Debugging agent batches. |
| `--trace` — OpenTelemetry spans across retries | P0 | Observability for long agent runs. |
| Structured warnings channel in stderr JSON (`{"level":"warn",…}`) | P0 | Agents can grep-filter. |
| Shell completions (zsh/bash/fish) | P0 | Human ergonomics. |
| `config set / get / list` subcommands | P0 | Replaces manual YAML editing. |

### 4.11 · Safety & ToS guardrails (P0)

| Feature | Rationale |
|---|---|
| Per-command allow-list in config (`allowed_commands: [post.create, comment.add]`) | Operator locks down which commands an agent can run. |
| Refuse automated messaging in unofficial transport by construction | Highest ban-risk operation; research says don't. |
| `--transport=unofficial` + `--accept-unofficial-risk` must display ToS § 8.2 warning | Tighten current scaffold. |
| `slog` secret redactor for Authorization / cookie headers | Defensive baseline; logs must survive verbose mode. |

---

## 5 · Out of scope (explicitly not implementing)

- **Voyager messaging** — ban risk is high; no agent use case justifies it.
- **Browser-automation fallback** (Patchright-style) — turns golink from a
  CLI into an infra project; `stickerdaniel/linkedin-mcp-server` covers that
  niche.
- **Managed scraping proxy** (Apify-style) — out of scope for a local CLI.
- **TUI long-form editor** — humans use LinkedIn's UI; agents produce text.
- **AI content rewriter / hashtag optimizer** — content generation is the
  calling LLM's job, not the CLI's.
- **Rebuilt MCP layer** — deferred. If needed later, regenerate from the
  `Transport` interface.
- **`jobs apply`** — automated job applications have real-world consequences
  and violate multiple ToS surfaces.

---

## 6 · LinkedIn prereqs per tier

Captured here so every implementation plan can check access before work.

**P0 self-serve (should work on a new developer app):**
- `openid`, `profile`, `email` (Sign In with LinkedIn)
- `w_member_social` (Share on LinkedIn)
- PKCE enabled on the developer app (requires LinkedIn support ticket)

**P1 approval-gated (LinkedIn partner application required):**
- `w_organization_social`, `r_organization_social` (Community Management API)
- `r_member_social` — **currently closed** per LinkedIn; `post list` of own
  posts already gated as `unsupported`
- `w_organization_social_feed`, `r_organization_social_feed` (newer feed
  permissions)
- Marketing Developer Platform (for ad analytics, conversion, lead sync)
- Partner approval for Messages API (user-initiated, opt-in, consent-first)
- Connections API / Invitation API (restricted partner)

**P2 no LinkedIn approval (but violates ToS):**
- `li_at` cookie from an authenticated member account
- Accepts account suspension risk

---

## 7 · Implementation sequencing (suggested order for P0)

This is advisory; each feature still gets its own brainstorm → plan → execute
cycle. The ordering reflects dependency + quick-win priority.

> **Status update (2026-04-17):** items 1–12 have all shipped (see `git log`).
> The next P0 candidates are from §4.10: `--explain`/`--trace`, `golink open`,
> `config set/get/list`, shell completions, `auth list`/`auth switch`,
> `post preview`.

1. ✅ **MCP removal** — never landed in git history; Path-B curation shipped.
2. ✅ **Refresh tokens + auto-refresh** — `auth refresh` + silent pre-expiry refresh.
3. ✅ **`--output` modes + `--compact`** — text/json/jsonl/compact/table all wired.
4. ✅ **Audit log** — `internal/audit/` JSONL sink, enforced in every mutating `RunE`.
5. ✅ **`golink doctor`** — env + session + scope probe; read-only, never audited.
6. ✅ **`--idempotency-key` + resumable batch** — 24h file store + batch `--resume`.
7. ✅ **`--require-approval` gate** — exit code 3, `approval grant`/`run`/`deny`.
8. ✅ **`social metadata` batch read** — `social metadata` command via Reactions/Comments count API.
9. ✅ **Image post + edit post + reshare** — `post create --image`, `post edit`, `post reshare`.
10. ✅ **`post schedule` (client-side queue)** — `schedule list/show/run/cancel/next`.
11. ✅ **Plan/execute split + record/replay** — `golink.plan/v1` + `GOLINK_RECORD`/`GOLINK_REPLAY`.
12. ✅ **Org posting + `org list`** — `post create --as-org`, `org list`.

P1 features are scheduled *after* partner approval lands (outside engineering
control). P2 features wait on a conscious decision to own the ToS risk.

---

## 8 · Non-goals of this document

- Not a per-feature technical spec. Each feature above is a candidate that
  will get its own `docs/superpowers/specs/<date>-<feature>.md` when
  prioritized.
- Not an implementation plan. The `writing-plans` workflow turns a prioritized
  feature into a task-ordered plan.
- Not a commitment to P1 or P2 work. Those tiers exist to capture research
  and shape the `Transport` interface — not to schedule.

---

## 9 · Open questions for owner

- **Which LinkedIn partner programs is the golink developer app enrolled in?**
  Answer shifts the P1 features from "roadmap" to "actionable now."
- **Is there a target agent platform** that would pin the MCP decision?
  (Claude Code / Codex / Cursor / aider all use shell; if a non-shell
  platform is in-scope, revisit M1.)
- **Do we care about organization posting as a first-class flow?** If yes,
  `org list` + post-as-org jumps into P0 sequence.

---

## Sources consulted

LinkedIn official (Microsoft Learn):
- Community Management Overview (li-lms-2026-02), Posts API (li-lms-2026-03),
  Assets API (li-lms-2026-01), Images API, Videos API, Poll Post API, Social
  Metadata API, Connections API, Connections Size API, Messages API,
  Programmatic Refresh Tokens, Recent Marketing API Changes (li-lms-2026-03).

Industry / community:
- `linkedin-api` (PyPI, Voyager wrapper) and community guides for internal
  Voyager endpoints.
- `stickerdaniel/linkedin-mcp-server`, `felipfr/linkedin-mcpserver`, Apify
  LinkedIn MCP, and the 2026 LinkedIn-MCP landscape writeup (morphllm).
- GitHub Agentic Workflows (`github/gh-aw`) for approval-gate / safe-outputs
  patterns.
- ReversecLabs "Design Patterns for Securing LLM Agents" for map-reduce
  batching and plan-then-execute separation.
- `rtk-ai/rtk` for the compact-output-for-LLM pattern.
