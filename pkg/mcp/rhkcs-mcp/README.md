# Red Hat KCS MCP Server

A Model Context Protocol (MCP) server that provides tools to search and retrieve Red Hat Knowledge Centered Service (KCS) solutions using the Go MCP SDK.

## Features

This MCP server provides two main tools for accessing Red Hat KCS:

### 1. Search KCS Solutions (`search_kcs`)
Search for Red Hat KCS Solutions and Articles, returning a list with Solution IDs.

**Parameters:**
- `query` (string, required): Search query string
- `rows` (int, optional): Number of results to return (default: 50)
- `start` (int, optional): Starting index for pagination (default: 0)
- `session_id` (string, optional): Optional session ID

**Filtering:**
By default, returns only documents where:
- `documentKind` is either "Article" or "Solution"
- `accessState` is either "active" or "private"

**Returns:**
- List of solutions with ID, title, score, and view URI

### 2. Get KCS Solution (`get_kcs`)
Get a specific solution by ID and extract structured content with title, Environment, Issue, Resolution, and Root Cause.

**Parameters:**
- `solution_id` (string, required): The ID of the solution to retrieve
- `session_id` (string, optional): Optional session ID

**Returns:**
- Detailed solution with title, environment, issue, resolution, and root cause

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

- `-http <address>`: Run as HTTP server on the specified address (e.g., `:8080`)
- `-help`: Show help message

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

The server includes comprehensive error handling for:

- Authentication failures
- API rate limiting
- Network connectivity issues
- Invalid parameters
- Missing resources

## Development

### Project Structure

```
pkg/mcp/rhkcs-mcp/
├── main.go           # Main entry point and CLI handling
├── server/
│   ├── api.go        # Red Hat API client implementation
│   └── tools.go      # MCP tool implementations
└── README.md         # This file
```

### Contributing

1. Ensure you have Go 1.24.6 or later installed
2. Set up your Red Hat API offline token
3. Make your changes
4. Test with `go build` and run the server
5. Submit a pull request

## License

This project is licensed under Apache-2.0; see [LICENSE](../../../LICENSE).