# Red Hat MCP Servers

Two standalone [Model Context Protocol](https://modelcontextprotocol.io) (MCP) servers, written in Go, that let AI assistants and other MCP clients search Red Hat's Jira issue tracker and Customer Portal knowledge base. All tools are read-only: the servers fetch and search, they never create or modify anything.

| Server | What it covers | Tools | Credential needed |
| --- | --- | --- | --- |
| **[`rh-issues-mcp`](pkg/mcp/rh-issues-mcp/README.md)** | Red Hat Jira Cloud ([redhat.atlassian.net](https://redhat.atlassian.net)) — engineering bugs, features, and escalations in projects such as OCPBUGS, OCPENG, and OCPTRL | `search_issues`, `get_issue` | Atlassian account email + API token |
| **[`rhkcs-mcp`](pkg/mcp/rhkcs-mcp/README.md)** | Red Hat knowledge base (KCS) — Solutions and Articles for OpenShift, RHEL, Ansible, and other Red Hat products | `search_kcs`, `get_kcs` | Red Hat API offline token |

The two servers are independent: build and run only the one you need, or both side by side.

## Tools at a glance

### `rh-issues-mcp`

| Tool | What it does |
| --- | --- |
| `search_issues` | Search issues with JQL (`project = OCPBUGS AND text ~ "GPU passthrough"`) or plain text (`ovnkube crashloop`), which is converted to a JQL full-text query across summary, description, comments, and environment. Returns key, summary, status, priority, and a browse URL, one page at a time. Pagination is token-based (`next_page_token` / `is_last`); the API provides no exact total. |
| `get_issue` | Fetch one issue by key (e.g. `OCPBUGS-55179`): summary, full description (converted from Jira wiki markup to Markdown), status, priority, created/updated dates, and URL. |

### `rhkcs-mcp`

| Tool | What it does |
| --- | --- |
| `search_kcs` | Search the knowledge base for Solutions and Articles. Product documentation pages, labs, vulnerability entries, and catalog rows are excluded, so every numeric result ID can be fetched with `get_kcs`. Returns numeric ID, kind, title, relevance score, and an access.redhat.com URL. |
| `get_kcs` | Fetch one document by numeric ID (e.g. `7010411`). Solutions return environment, issue, resolution, and root cause; Articles return their abstract only (the index has no full article body — follow the link for the complete page). Long text is re-formatted with restored line breaks; fenced commands can come back mangled from the index and are flagged as not executable as returned. |

Full parameter references and examples: [Jira server README](pkg/mcp/rh-issues-mcp/README.md) · [KCS server README](pkg/mcp/rhkcs-mcp/README.md).

## Requirements

- **Go 1.24.6 or later** — only needed to build; the binaries have no runtime dependencies.
- Credentials for the server(s) you run — see below.

## Getting credentials

### `rh-issues-mcp` — Atlassian email and API token

1. Create an API token at [id.atlassian.com/manage-profile/security/api-tokens](https://id.atlassian.com/manage-profile/security/api-tokens).
2. Export your Atlassian account email and the token:

```sh
export RH_JIRA_EMAIL="you@example.com"
export RH_JIRA_TOKEN="your_atlassian_api_token"
```

This is an Atlassian API token — not the retired issues.redhat.com personal access token, and not a scoped/OAuth gateway token. Your account must have access to the Red Hat Jira site and the issues you request.

### `rhkcs-mcp` — Red Hat API offline token

1. Create an offline token at [access.redhat.com/management/api](https://access.redhat.com/management/api).
2. Export it:

```sh
export RH_API_OFFLINE_TOKEN="your_offline_token"
```

The server exchanges the offline token for a short-lived access token via Red Hat SSO automatically and refreshes it as needed.

Variables are read directly from the process environment — `.env` files are not loaded. Keep tokens out of committed files: MCP clients can pull secrets from the environment instead of inlining them (shown below). A missing token is reported at startup and the server exits.

## Build

From the repository root:

```sh
make                  # Build both servers into bin/
make build-issues-mcp # Build only rh-issues-mcp
make build-mcp        # Build only rhkcs-mcp
make install          # Build both and copy them to ~/bin (override with INSTALL_DIR=...)
```

`bin/` is ignored by Git.

## Run

### stdio (default — for MCP clients that launch the server)

```sh
./bin/rh-issues-mcp    # uses RH_JIRA_EMAIL and RH_JIRA_TOKEN
./bin/rhkcs-mcp        # uses RH_API_OFFLINE_TOKEN
```

In stdio mode, stdout is reserved for the MCP protocol; logs go to the log file only.

### Streamable HTTP (for MCP clients that connect over HTTP)

```sh
./bin/rh-issues-mcp -http localhost:8081
./bin/rhkcs-mcp -http localhost:8080
```

KCS-only shortcuts: `make run-mcp` (stdio) and `make run-mcp-http` (HTTP on `:8080`, which binds **all interfaces**). HTTP mode has **no client authentication** — bind to localhost or put an authenticated proxy in front before exposing it further. It applies connection timeouts and shuts down gracefully on SIGINT/SIGTERM.

Run `<binary> -help` for the full option list, including copy-paste MCP client setup snippets.

## Configure an MCP client

Use absolute paths to the binaries. The examples reference secrets from the environment instead of inlining tokens.

### OpenCode

Add to `opencode.json` (global or project):

```json
{
  "mcp": {
    "rh-issues": {
      "type": "local",
      "command": ["/absolute/path/to/bin/rh-issues-mcp"],
      "environment": {
        "RH_JIRA_EMAIL": "{env:RH_JIRA_EMAIL}",
        "RH_JIRA_TOKEN": "{env:RH_JIRA_TOKEN}"
      }
    },
    "rhkcs": {
      "type": "local",
      "command": ["/absolute/path/to/bin/rhkcs-mcp"],
      "environment": {
        "RH_API_OFFLINE_TOKEN": "{env:RH_API_OFFLINE_TOKEN}"
      }
    }
  }
}
```

### pi

```sh
pi mcp add rh-issues --env RH_JIRA_EMAIL=... --env RH_JIRA_TOKEN=... -- /absolute/path/to/bin/rh-issues-mcp
pi mcp add rhkcs --env RH_API_OFFLINE_TOKEN=... -- /absolute/path/to/bin/rhkcs-mcp
```

or in `~/.pi/agent/mcp.json`:

```json
{
  "mcpServers": {
    "rh-issues": {
      "command": "/absolute/path/to/bin/rh-issues-mcp",
      "env": {
        "RH_JIRA_EMAIL": "${RH_JIRA_EMAIL}",
        "RH_JIRA_TOKEN": "${RH_JIRA_TOKEN}"
      }
    },
    "rhkcs": {
      "command": "/absolute/path/to/bin/rhkcs-mcp",
      "env": {
        "RH_API_OFFLINE_TOKEN": "${RH_API_OFFLINE_TOKEN}"
      }
    }
  }
}
```

### Claude Desktop and other `mcpServers` clients

```json
{
  "mcpServers": {
    "redhat-kcs": {
      "command": "/absolute/path/to/bin/rhkcs-mcp",
      "env": {
        "RH_API_OFFLINE_TOKEN": "your_offline_token"
      }
    }
  }
}
```

Add `rh-issues-mcp` the same way with `RH_JIRA_EMAIL` and `RH_JIRA_TOKEN`.

## Command-line options and logging

| Option | Effect |
| --- | --- |
| *(none)* | stdio transport (default) |
| `-http <addr>` | Streamable HTTP transport, e.g. `localhost:8080` (logs also mirror to stderr) |
| `-log-dir <dir>` | Directory for the log file (default: beside the binary) |
| `-v` | Verbose (debug) logging; same as `LOG_LEVEL=debug` |
| `-help` | Full help, including MCP client setup snippets |

Logs are quiet by default: only warnings and errors. Set `LOG_LEVEL` to `debug`, `info`, `warn` (default), or `error`. The log file (`rh-issues-mcp.log` / `rhkcs-mcp.log`) is size-rotated (10 MB, 5 backups, 30 days, compressed) and created lazily — a quiet server writes no log file.

## Development

```sh
make format
make test
make vet
make clean # Remove the built binaries (preserves logs)
```

Tests use `httptest` with dummy credentials — no network or real tokens needed. Focused runs: `go test ./pkg/mcp/rh-issues-mcp/...` or `go test ./pkg/mcp/rhkcs-mcp/...` (add `-race` to exercise the KCS token-refresh lock).

Licensed under [Apache-2.0](LICENSE).
