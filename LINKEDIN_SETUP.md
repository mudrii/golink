# golink Product and LinkedIn Setup Guide

golink is a command-line tool for publishing and managing LinkedIn content from
terminals, scripts, CI jobs, and AI agents. It uses LinkedIn OAuth to store a
member session in the operating-system keyring, then sends requests through the
official LinkedIn REST APIs.

This guide explains what the product is for, what you need installed, how to
create the LinkedIn developer app, and how to verify the setup end to end.

## What golink Is For

Use golink when you need a scriptable LinkedIn workflow:

- Authenticate a LinkedIn member from the CLI.
- Create, edit, delete, reshare, list, and inspect LinkedIn posts.
- Add comments and reactions.
- Upload one image with a post.
- Post as an organization page when the authenticated member is an administrator.
- Queue client-side scheduled posts and run them later from cron, launchd,
  systemd timers, or an agent loop.
- Generate plan files for review before execution.
- Require approval before mutating commands are sent.
- Emit stable JSON, JSONL, compact, table, or text output for humans and agents.
- Record and replay HTTP exchanges with personal data redacted.

golink is not a LinkedIn account automation tool. It uses OAuth and the
permissions granted to your LinkedIn developer app. LinkedIn may gate some APIs
or scopes behind product approval.

## Local Dependencies

Install these before setting up LinkedIn:

| Dependency | Purpose |
|---|---|
| Go 1.26.2 or newer | Build and test golink |
| git | Clone the repository |
| make | Run the project build/test gates |
| golangci-lint | Required by `make ci` |
| govulncheck | Run through `make vuln` / `make ci` via `go run` |
| OS keyring backend | Stores OAuth session tokens |
| Browser | Opens the LinkedIn OAuth consent page |

Optional but useful:

| Dependency | Purpose |
|---|---|
| jq | Inspect JSON output |
| cron, launchd, or systemd timer | Run scheduled posts later |

macOS example:

```sh
brew install go golangci-lint gotestsum govulncheck
```

Linux keyring note: `github.com/zalando/go-keyring` requires a working Secret
Service provider such as GNOME Keyring or KWallet. Headless Linux environments
may need a session keyring configured before `golink auth login` can persist a
session.

## Build From Source

```sh
git clone https://github.com/mudrii/golink.git
cd golink

make build
make ci
```

To install the CLI from the local checkout:

```sh
go install .
```

To install from a published module version:

```sh
go install github.com/mudrii/golink@latest
```

Confirm the binary is available:

```sh
golink version
```

## LinkedIn Developer Setup

### 1. Create or Select a LinkedIn Developer App

1. Open `https://developer.linkedin.com/`.
2. Go to **My Apps**.
3. Create a new app, or open an existing app you control.
4. Fill in the required app name, company page, logo, and legal/contact fields.
5. Save the app.

Keep this browser tab open. You will need the app credentials and product tabs.

### 2. Request LinkedIn Products and Permissions

Request the products that match the commands you want to use.

Minimum for login and profile:

| Need | Product / Permission |
|---|---|
| OAuth login and profile identity | Sign in with LinkedIn using OpenID Connect |
| OIDC scopes | `openid profile email` |

Member posting:

| Need | Scope |
|---|---|
| Create, edit, delete, reshare, comment, react as member | `w_member_social_feed` |
| Older apps may grant | `w_member_social` |

Organization posting:

| Need | Scope |
|---|---|
| List administered organizations and post as an organization | `w_organization_social_feed` |
| Older apps may grant | `w_organization_social` |

The authenticated member must be an administrator of the organization page to
post as that organization.

Some read APIs are entitlement-gated by LinkedIn. `golink doctor` reports which
features are supported by the scopes in the current session.

### 3. Configure Redirect URLs

LinkedIn requires the callback URL to match exactly. Scheme, host, port, and
path all matter.

Recommended local callback:

```text
http://127.0.0.1:8080/callback
```

In the LinkedIn developer app:

1. Open the app **Auth** settings.
2. Add `http://127.0.0.1:8080/callback` to Authorized Redirect URLs.
3. Save changes.

Then configure golink to use the same port:

```sh
export GOLINK_REDIRECT_PORT=8080
```

Do not register `localhost` and run `127.0.0.1`, or the reverse. LinkedIn treats
those as different redirect URLs.

### 4. Choose an OAuth Flow

golink supports two login flows.

#### Option A: Native PKCE (default)

Use this when your LinkedIn app is allowed to use the native PKCE authorization
endpoint. This flow needs only the client ID.

```sh
export GOLINK_CLIENT_ID="<your LinkedIn app Client ID>"
export GOLINK_REDIRECT_PORT=8080
```

Default requested scopes:

```text
openid profile email w_member_social_feed
```

If LinkedIn rejects OIDC scopes on the native PKCE endpoint, request only the
posting/profile scopes your app has:

```sh
export GOLINK_AUTH_SCOPES="w_member_social r_profile_basicinfo"
export GOLINK_MEMBER_URN="urn:li:person:<your_member_id>"
```

`GOLINK_MEMBER_URN` is a fallback used when OIDC userinfo and profile lookup are
not available for the app.

#### Option B: Standard OAuth 2.0

Use this if LinkedIn reports that the app does not have access to native PKCE.
This flow requires both client ID and client secret.

```sh
export GOLINK_AUTH_FLOW=oauth2
export GOLINK_CLIENT_ID="<your LinkedIn app Client ID>"
export GOLINK_CLIENT_SECRET="<your LinkedIn app Client Secret>"
export GOLINK_REDIRECT_PORT=8080
export GOLINK_AUTH_SCOPES="openid profile email w_member_social_feed"
```

Keep `GOLINK_CLIENT_SECRET` out of source control. Store it in your shell
profile, CI secret manager, or local secret manager.

## Authenticate

Run:

```sh
golink auth login
```

What happens:

1. golink starts a loopback callback server.
2. golink opens a LinkedIn authorization URL in your browser.
3. You approve the requested permissions.
4. LinkedIn redirects to `http://127.0.0.1:8080/callback`.
5. golink exchanges the authorization code for tokens.
6. golink stores the session in the OS keyring.

Check the session:

```sh
golink --json auth status
golink doctor
```

For strict release or CI verification:

```sh
golink --json doctor --strict
```

## First Commands

Profile:

```sh
golink --json profile me
```

Dry-run a member post without sending it:

```sh
golink --json --dry-run post create \
  --text "Hello from golink" \
  --visibility PUBLIC
```

Create a member post:

```sh
golink --json post create \
  --text "Hello from golink" \
  --visibility PUBLIC
```

List posts for the authenticated member:

```sh
golink --json post list --count 5
```

List organizations you administer:

```sh
golink --json org list
```

Post as an organization:

```sh
golink --json post create \
  --text "Company update from golink" \
  --as-org "urn:li:organization:<organization_id>"
```

Create a post with one image:

```sh
golink --json post create \
  --text "Image post from golink" \
  --image "/absolute/path/to/image.jpg" \
  --image-alt "Short accessible image description"
```

## Configuration Reference

Configuration priority:

1. CLI flags
2. `GOLINK_*` environment variables
3. `~/.config/golink/config.yaml`
4. Built-in defaults

Important environment variables:

| Variable | Required | Description |
|---|---|---|
| `GOLINK_CLIENT_ID` | Yes for login | LinkedIn app Client ID |
| `GOLINK_CLIENT_SECRET` | Only with `GOLINK_AUTH_FLOW=oauth2` | LinkedIn app Client Secret |
| `GOLINK_AUTH_FLOW` | No | `pkce` default, or `oauth2` |
| `GOLINK_AUTH_SCOPES` | No | Space- or comma-separated OAuth scopes |
| `GOLINK_MEMBER_URN` | No | Manual member URN fallback |
| `GOLINK_REDIRECT_PORT` | Recommended | Local callback port registered in LinkedIn |
| `GOLINK_API_VERSION` | No | LinkedIn API version header, for example `202604` |
| `GOLINK_OUTPUT` | No | `text`, `json`, `jsonl`, `compact`, or `table` |
| `GOLINK_AUDIT` | No | `on` by default; set `off` to disable audit log |
| `GOLINK_AUDIT_PATH` | No | Override audit JSONL path |
| `GOLINK_IDEMPOTENCY_PATH` | No | Override idempotency JSONL path |
| `GOLINK_APPROVAL_DIR` | No | Override approval directory |
| `GOLINK_SCHEDULE_DIR` | No | Override schedule queue directory |
| `GOLINK_RECORD` | No | Record redacted HTTP cassette |
| `GOLINK_REPLAY` | No | Replay from redacted HTTP cassette |

Example `~/.config/golink/config.yaml`:

```yaml
profile: default
transport: official
redirect_port: 8080
auth_flow: pkce
auth_scopes: "openid profile email w_member_social_feed"
api_version: "202604"
output: json
audit: true
```

Do not put `client_secret`, access tokens, or refresh tokens in config files.
Tokens are stored in the OS keyring by golink.

## Release and Operations Checklist

Before handing golink to a user or agent:

1. `make ci` passes.
2. LinkedIn app has required products/scopes approved.
3. Redirect URL in LinkedIn exactly matches `GOLINK_REDIRECT_PORT`.
4. `GOLINK_CLIENT_ID` is set.
5. If using `oauth2`, `GOLINK_CLIENT_SECRET` is set through a secret manager.
6. `golink auth login` succeeds.
7. `golink doctor` reports expected feature support.
8. Run the first mutating command with `--dry-run`.
9. Use `--require-approval` for agent-driven publishing workflows.
10. Use `--idempotency-key` for retryable publishing workflows.

## Troubleshooting

### `missing required environment variable: GOLINK_CLIENT_ID`

Set the app Client ID:

```sh
export GOLINK_CLIENT_ID="<your LinkedIn app Client ID>"
```

### Redirect URI mismatch

Verify all of these match exactly:

- LinkedIn app Authorized Redirect URL
- `GOLINK_REDIRECT_PORT`
- Host: `127.0.0.1`
- Path: `/callback`

Recommended:

```text
http://127.0.0.1:8080/callback
```

### Native PKCE permission error

Switch to standard OAuth 2.0:

```sh
export GOLINK_AUTH_FLOW=oauth2
export GOLINK_CLIENT_SECRET="<your LinkedIn app Client Secret>"
export GOLINK_REDIRECT_PORT=8080
```

Then re-run:

```sh
golink auth login
```

### Scope is missing after login

Change `GOLINK_AUTH_SCOPES`, then re-authenticate:

```sh
golink auth logout
export GOLINK_AUTH_SCOPES="openid profile email w_member_social_feed w_organization_social_feed"
golink auth login
golink doctor
```

### Organization posting is unsupported

Check:

1. Your app has `w_organization_social_feed` or `w_organization_social`.
2. The authenticated member is an administrator of the organization page.
3. `golink --json org list` returns the organization.

### Refresh token unavailable

Not every LinkedIn app receives refresh tokens. If `golink auth refresh` reports
that no refresh token is stored, run `golink auth login` again when the access
token expires or apply for the LinkedIn product capability that issues refresh
tokens to your app.

### Keyring errors

Make sure your OS keyring is available:

- macOS: Keychain should be unlocked.
- Linux desktop: Secret Service should be running.
- Headless Linux: configure a keyring provider or run in an environment with
  Secret Service support.

## Documentation References

This guide was checked against current LinkedIn documentation using Context7:

- LinkedIn Developer portal: `https://developer.linkedin.com/`
- OAuth authorization code flow:
  `https://learn.microsoft.com/en-us/linkedin/shared/authentication/authorization-code-flow`
- Sign in with LinkedIn using OpenID Connect:
  `https://learn.microsoft.com/en-us/linkedin/consumer/integrations/self-serve/sign-in-with-linkedin-v2`
