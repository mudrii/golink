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
| `version` | ✅ | ✅ | Reports build metadata |

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
cmd/                       cobra commands (auth, post, comment, react, search, version)
internal/api/              Transport interface + official LinkedIn adapter + NoopTransport
internal/auth/             PKCE login, keyring-backed session store
internal/config/           viper-backed settings with env/flag/file precedence
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
