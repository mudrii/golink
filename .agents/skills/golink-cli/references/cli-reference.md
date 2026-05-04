# golink CLI Reference for Agents

Load this file when you need concrete commands, scope mapping, troubleshooting,
or examples beyond the main skill workflow.

## Output Modes

Use structured output by default:

```sh
golink --json auth status
golink --output=json post list --count 5
golink --output=jsonl batch ops.jsonl
golink --compact post list --count 5
golink --output=table post list --count 10
```

Precedence: `--compact` > `--output` > `--json` > text.

## Auth

```sh
golink auth login
golink --json auth status
golink --json auth refresh
golink auth logout
```

Required local env:

```sh
export GOLINK_CLIENT_ID="<client id>"
export GOLINK_REDIRECT_PORT=8080
```

Standard OAuth 2.0 fallback:

```sh
export GOLINK_AUTH_FLOW=oauth2
export GOLINK_CLIENT_SECRET="<client secret>"
export GOLINK_REDIRECT_PORT=8080
```

Do not echo secret values. Use `test -n "$VAR"` style checks.

## LinkedIn Scopes

Default login scopes:

```text
openid profile email w_member_social_feed
```

Accepted member write scopes:

```text
w_member_social_feed
w_member_social
```

Accepted organization write scopes:

```text
w_organization_social_feed
w_organization_social
```

Native PKCE fallback for apps without OIDC:

```sh
export GOLINK_AUTH_SCOPES="w_member_social r_profile_basicinfo"
export GOLINK_MEMBER_URN="urn:li:person:<id>"
```

## Diagnostics

```sh
golink doctor
golink --json doctor --strict
```

Interpretation:

- `health: ok`: ready.
- `health: warnings`: usable, but inspect warnings.
- `health: error`: fix auth/config before live mutations.
- feature `unsupported`: often a LinkedIn scope/entitlement issue.

## Safe Publishing Pattern

```sh
golink --json --dry-run post create \
  --text "Post body" \
  --visibility PUBLIC

golink --json post create \
  --text "Post body" \
  --visibility PUBLIC \
  --idempotency-key "post-$(date +%Y%m%d)-topic"
```

Use `--visibility PUBLIC`, `CONNECTIONS`, or `LOGGED_IN`.

## Plan and Execute

Use plans when another human/agent should review before execution:

```sh
golink --json plan post create \
  --text "Post body" \
  --visibility PUBLIC > plan.json

golink --json --dry-run execute plan.json
golink --json execute plan.json
```

Plannable commands:

```text
post create
post delete
post edit
post reshare
post schedule
comment add
react add
```

## Approval Gate

Use approval when the command should be staged locally:

```sh
golink --json post create \
  --text "Post body" \
  --visibility PUBLIC \
  --require-approval

golink --json approval list
golink --json approval show <command_id>
golink --json approval grant <command_id>
golink --json approval run <command_id>
```

Cancel or deny:

```sh
golink --json approval deny <command_id>
golink --json approval cancel <command_id>
```

## Posts

```sh
golink --json post create --text "Hello" --visibility PUBLIC
golink --json post create --text "Hello" --image "/absolute/path/image.jpg" --image-alt "Alt text"
golink --json post list --count 5
golink --json post get "urn:li:share:<id>"
golink --json post edit "urn:li:share:<id>" --text "Updated text"
golink --json post reshare "urn:li:share:<id>" --text "Commentary"
golink --json post delete "urn:li:share:<id>"
```

Prefer `--dry-run` before create, edit, reshare, delete, or schedule.

## Organization Posting

```sh
golink --json org list

golink --json post create \
  --text "Company update" \
  --as-org "urn:li:organization:<id>"
```

Requirements:

- app has org write scope;
- authenticated member is an organization administrator;
- `org list` returns the organization.

## Comments and Reactions

```sh
golink --json comment add "urn:li:share:<id>" --text "Comment"
golink --json comment list "urn:li:share:<id>"

golink --json react add "urn:li:share:<id>" --type LIKE
golink --json react list "urn:li:share:<id>"
```

Reaction types:

```text
LIKE PRAISE EMPATHY INTEREST APPRECIATION ENTERTAINMENT
```

## Scheduling

golink scheduling is client-side. It does not run a daemon.

```sh
golink --json post schedule \
  --at "2026-05-06T09:00:00Z" \
  --text "Scheduled post" \
  --visibility PUBLIC

golink --json schedule list
golink --json schedule next
golink --json schedule show <command_id>
golink --json schedule run --limit 20
golink --json schedule cancel <command_id>
```

For scheduled images, use absolute paths because the image is read later.

## Batch

`ops.jsonl` example:

```jsonl
{"command":"post create","args":{"text":"Hello batch","visibility":"PUBLIC"},"idempotency_key":"batch-1"}
{"command":"comment add","args":{"post_urn":"urn:li:share:123","text":"Nice"},"dry_run":true}
```

Run:

```sh
golink --output=jsonl batch ops.jsonl --concurrency 1 --strict
```

Use `--resume` to skip completed operations via the sidecar progress file.

## Record and Replay

For offline reproduction:

```sh
GOLINK_RECORD=cassette.jsonl golink --json post list
GOLINK_REPLAY=cassette.jsonl golink --json post list
```

Cassettes are redacted for tokens and common personal data, but still treat them
as sensitive review artifacts.

## Troubleshooting

Missing client ID:

```sh
export GOLINK_CLIENT_ID="<client id>"
```

Redirect mismatch:

- register `http://127.0.0.1:<port>/callback` in LinkedIn;
- set `GOLINK_REDIRECT_PORT=<port>`;
- do not mix `localhost` and `127.0.0.1`.

Missing scopes:

```sh
golink auth logout
export GOLINK_AUTH_SCOPES="openid profile email w_member_social_feed w_organization_social_feed"
golink auth login
golink doctor
```

No refresh token:

- run `golink auth login` again when needed;
- or apply for LinkedIn refresh-token capability.

Keyring failure:

- unlock macOS Keychain;
- ensure Linux Secret Service/KWallet is available;
- avoid headless sessions without a keyring provider.
