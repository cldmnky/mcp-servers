# Red Hat KCS MCP Server

A Model Context Protocol (MCP) server that provides tools to search and retrieve Red Hat Knowledge Centered Service (KCS) solutions using the Go MCP SDK.

## Features

This MCP server provides two main tools for accessing Red Hat KCS:

### 1. Search KCS Solutions (`search_kcs`)
Search for Red Hat KCS Solutions and Articles, returning a list with Solution IDs.

**Parameters:**
- `query` (string, required): Search query string
- `rows` (int, optional): Number of results to return (default: 10)
- `start` (int, optional): Starting index for pagination (default: 0)
- `session_id` (string, optional): Deprecated; accepted for backwards compatibility and ignored.

**Filtering:**
Results are filtered server-side (`fq`) to documents where `documentKind` is "Solution" or "Article". Product documentation pages, labs, vulnerability entries, and container catalog rows are excluded — their identifiers are not numeric solution IDs and cannot be fetched with `get_kcs`; excluding them also removes per-translation duplicates of documentation pages.

**Returns:**
- List of solutions with numeric ID, kind (Solution or Article), title, score, and view URI

### 2. Get KCS Solution (`get_kcs`)
Get a specific solution by ID and extract structured content with title, Environment, Issue, Resolution, and Root Cause.

**Parameters:**
- `solution_id` (string, required): Numeric solution ID from a `search_kcs` result, e.g. `7010411`. Documentation URLs and other identifiers are rejected with an error.
- `session_id` (string, optional): Deprecated; accepted for backwards compatibility and ignored.

**Returns:**
- Solutions: title, kind, environment, issue, resolution, and root cause.
- Articles: title, kind, environment, and abstract only — the full article body is not in the search index; use the `view_uri` link for the complete page. The response notes this. The index's raw `abstract` field is stored duplicated (publishedAbstract repeated, title appended); the server returns the clean `publishedAbstract` copy, or a deduplicated fallback for drafts, and never shows the auto-generated abstract on Solutions (it duplicates their issue text).

**Formatting:**
The search index flattens line breaks out of stored text, so articles arrive as single lines. The server re-inserts line breaks around Markdown block elements (headings, list items, numbered steps) to restore readable structure. Code fences are kept byte-for-byte verbatim — the index also eats spaces where line breaks used to be, so fenced commands come back mangled (`oc get nodes-o` for `oc get nodes -o`) — and the response includes a warning that they are not executable as returned. Unterminated fences in flattened text are closed for display without attempting to repair their code content. Inline constructs such as links, `--flags`, and version numbers are left untouched.

## Setup

### Prerequisites

1. **Red Hat API Offline Token**: You need a Red Hat API offline token to authenticate with the Red Hat APIs.
   - Obtain this from [Red Hat API Tokens](https://access.redhat.com/management/api)

### Environment Variables

- `RH_API_OFFLINE_TOKEN` (required): Your Red Hat API offline token

### Installation

From the root of the mcp-servers repository:

1. Build the server:

```bash
make build-mcp
```

This will build the MCP server binary to `bin/rhkcs-mcp`.

## Usage

### Running over stdio (for MCP clients)

```bash
export RH_API_OFFLINE_TOKEN="your_offline_token_here"
make run-mcp
```

Or directly:

```bash
export RH_API_OFFLINE_TOKEN="your_offline_token_here"
./bin/rhkcs-mcp
```

### Running as HTTP server

```bash
export RH_API_OFFLINE_TOKEN="your_offline_token_here"
make run-mcp-http
```

Or directly:

```bash
export RH_API_OFFLINE_TOKEN="your_offline_token_here"
./bin/rhkcs-mcp -http :8080
```

The HTTP server will be available at `http://localhost:8080` and can be used with MCP clients that support HTTP transport.

### Command Line Options

- `-http <address>`: Run as HTTP server on the specified address (e.g., `localhost:8080`)
- `-log-dir <dir>`: Directory for the rotated log file (default: beside the binary)
- `-v`: Verbose (debug) logging; equivalent to `LOG_LEVEL=debug`
- `-help`: Show help message

HTTP mode has no client authentication; bind to localhost or use an authenticated proxy. The HTTP server applies read/write/idle timeouts, expires stateful MCP sessions after 10 minutes without new requests, and shuts down gracefully on SIGINT/SIGTERM. Reinitialize after HTTP 404 for an expired session.

### Logging

Logs are quiet by default: only warnings and errors are recorded. `LOG_LEVEL=debug` (or `-v`) enables per-request detail. The log file (`rhkcs-mcp.log`) is size-rotated (10 MB), compressed, and old backups are pruned (5 backups, 30 days). HTTP mode mirrors log output to stderr; stdio mode normally writes to the file only. If opening, writing, or rotating the log fails, messages fall back to stderr in either mode, keeping stdout clean for the MCP protocol.

### Example Usage with Claude Desktop

Add this configuration to your Claude Desktop MCP settings:

```json
{
  "mcpServers": {
    "redhat-kcs": {
      "command": "/path/to/rhkcs-mcp",
      "env": {
        "RH_API_OFFLINE_TOKEN": "your_offline_token_here"
      }
    }
  }
}
```

### Example Usage with OpenCode

Add to `opencode.json` (global or project):

```json
{
  "mcp": {
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

### Example Usage with pi

Add to `~/.pi/agent/mcp.json`, or run
`pi mcp add rhkcs --env RH_API_OFFLINE_TOKEN=... -- /absolute/path/to/bin/rhkcs-mcp`:

```json
{
  "mcpServers": {
    "rhkcs": {
      "command": "/absolute/path/to/bin/rhkcs-mcp",
      "env": {
        "RH_API_OFFLINE_TOKEN": "${RH_API_OFFLINE_TOKEN}"
      }
    }
  }
}
```

## API Integration

This server integrates with the Red Hat KCS Search API:

- **Red Hat KCS Search API**: `/hydra/rest/search/v2/kcs`

Authentication is handled automatically using OAuth2 with the offline token.

## Architecture

The server is built using:

- **[Go MCP SDK](https://github.com/modelcontextprotocol/go-sdk)**: Official Go SDK for Model Context Protocol
- **Red Hat Access APIs**: For accessing KCS solutions and support cases
- **OAuth2 Authentication**: Using Red Hat SSO for secure API access

## Error Handling

The server includes error handling for authentication failures, network connectivity issues, invalid parameters, and missing resources. API errors returned to MCP clients include the HTTP status and a bounded excerpt of the response body. `get_kcs` returns a tool error when no matching document is found or the response shape is unexpected; a found document with no indexed body still returns its metadata with an explanatory note.

## Development

### Project Structure

```
pkg/mcp/rhkcs-mcp/
├── main.go           # Main entry point and CLI handling
├── server/
│   ├── api.go        # Red Hat API client implementation
│   ├── api_test.go   # httptest-based client and tool tests
│   └── tools.go      # MCP tool implementations
└── README.md         # This file
```

Tests run without real credentials or network access: `go test ./pkg/mcp/rhkcs-mcp/...` (add `-race` to exercise the token-refresh lock).

### Contributing

1. Ensure you have Go 1.24.6 or later installed
2. Set up your Red Hat API offline token
3. Make your changes
4. Test with `go build` and run the server
5. Submit a pull request

## License

This project is licensed under Apache-2.0; see [LICENSE](../../../LICENSE).