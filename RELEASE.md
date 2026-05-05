# Release Checklist

This checklist prepares date-based golink releases using `YY.MM.DD`.

## Release scope

- Product: LinkedIn CLI for humans and LLM agents.
- Module: `github.com/mudrii/golink`.
- Binary: `golink`.
- License: MIT.
- Supported build path: Go 1.26.2+.
- Release tag format: `vYY.MM.DD`, for example `v26.05.05`.
- Displayed release version: `YY.MM.DD`, for example `26.05.05`.
- Homebrew formula: `Formula/golink.rb`.

## Pre-release review

Run the full gate from a clean worktree:

```sh
make ci
git diff --check
git status --short
```

Optional coverage visibility:

```sh
go test -cover ./...
```

Coverage is informational until it is wired into CI. The current release gate is
`make ci`, which runs vet, lint, tests, race tests, and govulncheck.

## Documentation review

Before tagging, verify these files match the command surface:

- `README.md` - product overview, install paths, command matrix, workflows,
  persistence, privacy, diagnostics, technical architecture, and development.
- `LINKEDIN_SETUP.md` - LinkedIn Developer app setup, scopes, OAuth flows,
  redirect URI setup, and troubleshooting.
- `CLAUDE.md` - authoritative engineering contract for agents.
- `AGENTS.md` - pointer for agents that delegates to `CLAUDE.md`.
- `.agents/skills/golink-cli/` - reusable CLI skill for OpenAI/Codex agents,
  OpenClaw, Hermes, and other CLI-capable agents.

Useful alignment checks:

```sh
go run . --help
go run . auth --help
go run . post --help
go run . schedule --help
go run . doctor
```

## Privacy and artifact review

Run these checks before publishing:

```sh
rg -n "(AKIA|ASIA|BEGIN (RSA|OPENSSH|PRIVATE)|client_secret|access_token|refresh_token|password|api[_-]?key)" \
  -g'!go.sum' -g'!RELEASE.md' -g'!.git/**' .

rg -n "^(<<<<<<<|=======|>>>>>>>|MERGE_HEAD|CHERRY_PICK_HEAD|REBASE_HEAD)" \
  -g'!go.sum' -g'!.git/**' .

rg --files \
  -g'*.jsonl' -g'*.cassette' -g'*.har' -g'*.env' -g'*.pem' -g'*.key' \
  -g'*.tmp' -g'*.log' -g'*.out' -g'coverage*' -g'*.prof' -g'golink' \
  -g'!vendor/**' -g'!.git/**'
```

Expected result:

- no secrets or credentials;
- no unresolved merge markers;
- no generated cassettes, logs, profiles, env files, keys, or binaries.

Synthetic test fixtures may contain fake URNs and emails; persisted runtime
artifacts must not.

## Homebrew release

The formula uses the Homebrew tap flow documented by Homebrew: users can install
directly from a tap, and maintainers can validate with `brew install
--build-from-source`.

After the release commit is ready:

```sh
git tag -a v26.05.05 -m "golink 26.05.05"
git push origin main
git push origin v26.05.05
```

Validate the formula:

```sh
brew tap mudrii/golink "$PWD"
brew style Formula/golink.rb
brew audit --strict mudrii/golink/golink
brew install --build-from-source mudrii/golink/golink
brew test golink
brew uninstall golink
```

Homebrew 5.x rejects direct formula paths for install/audit, so local validation
must tap the repository first. Use `brew untap mudrii/golink` after testing if
you do not want to keep the local tap.

For a separate tap repository, copy `Formula/golink.rb` into the tap and pin the
tag revision after the tag exists:

```ruby
url "https://github.com/mudrii/golink.git",
    tag:      "v26.05.05",
    revision: "<git rev-parse v26.05.05>"
```

Users can install from this repository as a tap after the tag is pushed:

```sh
brew install mudrii/golink/golink
```

## GitHub release

Create a GitHub release for `v26.05.05` with:

- product summary from `README.md`;
- setup link to `LINKEDIN_SETUP.md`;
- Homebrew install command;
- source install command: `go install github.com/mudrii/golink@v26.05.05`;
- known LinkedIn prerequisites and scope requirements;
- note that unsupported envelopes may reflect LinkedIn entitlement or scope
  limits rather than a CLI failure.

## Final verification

Run after tagging and before announcing:

```sh
go install github.com/mudrii/golink@v26.05.05
golink version
golink doctor
brew install mudrii/golink/golink
brew test golink
```
