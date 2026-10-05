# Red Hat Jira Issues MCP Server

A Go MCP server for Red Hat Jira Cloud at https://redhat.atlassian.net.

## Authentication

Set both environment variables:

- `RH_JIRA_EMAIL`: your Atlassian account email.
- `RH_JIRA_TOKEN`: your Atlassian API token, **not** the old issues.redhat.com personal access token.

Create an API token at https://id.atlassian.com/manage-profile/security/api-tokens. This server uses email/API-token Basic authentication against the site URL. Scoped tokens requiring the `api.atlassian.com` gateway and OAuth are not supported. Your account must have access to the Red Hat Jira site and the requested issues.

Do not commit credentials to the repository.

## Build and run

Requires Go 1.24.6 or later. From the repository root:

```sh
make build-issues-mcp
export RH_JIRA_EMAIL="you@example.com"
export RH_JIRA_TOKEN="your_atlassian_api_token"
./bin/rh-issues-mcp                       # stdio
./bin/rh-issues-mcp -http localhost:8081   # Streamable HTTP
./bin/rh-issues-mcp -help
```

HTTP mode has no client authentication; keep it on localhost or secure it with an authenticated proxy. Logs are quiet by default (warnings and errors only); `-v` or `LOG_LEVEL=debug` enables per-request detail. The log file (`rh-issues-mcp.log`) is size-rotated, written beside the binary unless `-log-dir` is set, and mirrored to stderr in HTTP mode. The HTTP listener applies connection timeouts and shuts down gracefully on SIGINT/SIGTERM.

## Tools

### `search_issues`

Search using JQL or plain text, which is converted to a JQL `text ~` query.

- `query` (required): e.g. `project = OCPBUGS AND summary ~ "DNS"` or `GPU passthrough errors`.
- `max_results` (optional): page size, default 50.
- `next_page_token` (optional): continuation token returned by the previous search. Omit for the first page; keep the same query when requesting further pages.

Returns `issues` (key, summary, status, priority, view URI), `count`, `query`, `is_last`, and an optional `next_page_token`.

**Migration change:** Jira Cloud's enhanced search uses token-based pagination. `start_at` is no longer supported, and the response no longer contains `total` because the endpoint does not return an exact total. Use `is_last` and `next_page_token` instead.

### `get_issue`

- `issue_key` (required): e.g. `OCPBUGS-55179`.

Returns key, summary, description, status, priority, created/updated dates, and a link to `https://redhat.atlassian.net/browse/{key}`.

## API

Uses the go-jira Cloud client with REST API v2, preserving string-form issue descriptions:

- Search: `/rest/api/2/search/jql` (enhanced search, not the retired `/search` endpoint).
- Issue details: `/rest/api/2/issue/{issueKey}`.

The `/jira/for-you?tab=workedon` URL is a browser landing page, not an API base URL.

For a direct authentication check:

```sh
curl --user "$RH_JIRA_EMAIL:$RH_JIRA_TOKEN" \
  "https://redhat.atlassian.net/rest/api/2/issue/OCPBUGS-55179"
```

## MCP client configuration

Configure the client to launch `/absolute/path/to/mcp-servers/bin/rh-issues-mcp` and pass `RH_JIRA_EMAIL` and `RH_JIRA_TOKEN` through its environment. Both `search_issues` and `get_issue` are registered over stdio and HTTP.

## Development

```sh
make format test vet build-issues-mcp
```

Tests use a local HTTP server to check Cloud authentication, API paths, pagination, and issue decoding without real credentials.

Licensed under [Apache-2.0](../../../LICENSE).
