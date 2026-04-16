# golink

Production-grade LinkedIn CLI for humans and LLM agents.

## Overview

golink provides a command-line interface to LinkedIn's official APIs (Posts API, Community Management API) with optional unofficial transport for features not available through self-serve access. It supports both interactive (TTY) and non-interactive (agent/CI) modes with structured JSON output.

## Features

- **OAuth native PKCE** — browser-based login with loopback callback, no client secret required
- **Post management** — create, list, get, delete posts via LinkedIn Posts API
- **Social actions** — add/list comments and reactions
- **People search** — via unofficial transport when official access is unavailable
- **MCP server** — expose all operations as Model Context Protocol tools for LLM agents
- **Dual transport** — official (default), unofficial (opt-in), or auto-fallback
- **JSON-first** — every command produces strict, schema-validated JSON output with `--json`
- **Dry-run** — preview exact API payloads without sending with `--dry-run`
- **Secure** — tokens stored in OS keyring, never on disk or in logs

## Installation

```sh
go install github.com/mudrii/golink@latest
```

Requires Go 1.26.2+.

## Quick Start

```sh
# Set your LinkedIn app client ID
export GOLINK_CLIENT_ID=your_client_id

# Authenticate (opens browser)
golink auth login

# Check auth status
golink auth status

# View your profile
golink profile me

# Create a post
golink post create --text "Hello from golink!"

# List your recent posts
golink post list

# JSON mode for automation
golink --json profile me
golink --json post create --text "Automated post" --visibility PUBLIC
```

## Agent / Non-Interactive Mode

When running without a TTY (CI, scripts, LLM agents), all interactive prompts are disabled. Required arguments must be passed as flags:

```sh
golink --json post create --text "Hello" --visibility PUBLIC
golink --json comment add urn:li:share:123 --text "Great post!"
golink --json react add urn:li:share:123 --type LIKE
golink --json search people --keywords "Go engineer"
```

## MCP Server

Expose golink as an MCP tool server for LLM agent integration:

```sh
golink mcp serve
```

Registers 11 tools: `golink_profile_me`, `golink_create_post`, `golink_list_posts`, `golink_get_post`, `golink_delete_post`, `golink_add_comment`, `golink_list_comments`, `golink_add_reaction`, `golink_list_reactions`, `golink_search_people`, `golink_auth_status`.

## Transport Modes

| Mode | Flag | Behavior |
|------|------|----------|
| Official | `--transport=official` (default) | LinkedIn REST APIs with OAuth bearer token |
| Unofficial | `--transport=unofficial` | Experimental web-adjacent endpoints (requires acknowledgement) |
| Auto | `--transport=auto` | Official first, unofficial fallback for read-only features |

## Configuration

| Source | Priority |
|--------|----------|
| CLI flags | Highest |
| `GOLINK_*` env vars | |
| `~/.config/golink/config.yaml` | |
| Default values | Lowest |

**Environment variables:**

| Variable | Required | Description |
|----------|----------|-------------|
| `GOLINK_CLIENT_ID` | Yes | LinkedIn app client ID |
| `GOLINK_API_VERSION` | No | Override `Linkedin-Version` header (YYYYMM format) |
| `GOLINK_REDIRECT_PORT` | No | Preferred OAuth callback port |

## Exit Codes

| Code | Meaning |
|------|---------|
| 0 | Success |
| 2 | Validation / usage error |
| 4 | Auth / session error |
| 5 | API / transport error |

## Prerequisites

- A [LinkedIn Developer App](https://www.linkedin.com/developers/) with native PKCE enabled (contact LinkedIn support to enable)
- The `w_member_social` scope (self-serve via developer portal)
- Go 1.26.2+ for building from source

## License

[MIT](LICENSE)
