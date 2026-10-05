# MCP servers

Standalone Go MCP servers ported from `ocp-agent`:

| Binary | Tools | Required environment variable |
| --- | --- | --- |
| `rh-issues-mcp` | `search_issues`, `get_issue` | `RH_JIRA_EMAIL`, `RH_JIRA_TOKEN` |
| `rhkcs-mcp` | `search_kcs`, `get_kcs` | `RH_API_OFFLINE_TOKEN` |

## Build

Requires Go 1.24.6 or later.

```sh
make                  # Build both servers into bin/
make build-issues-mcp # Build only rh-issues-mcp
make build-mcp        # Build only rhkcs-mcp
```

`bin/` is ignored by Git. The module depends only on the MCP SDK, the JIRA client, and their transitive dependencies; the original agent is not required.

## Run

Set the required token in your environment, then launch the corresponding binary:

```sh
# stdio transport (default, for MCP clients)
./bin/rh-issues-mcp
./bin/rhkcs-mcp

# Streamable HTTP transport
./bin/rh-issues-mcp -http localhost:8081
./bin/rhkcs-mcp -http localhost:8080
```

Use `-help` for CLI options. Logs are quiet by default (warnings and errors only); use `-v` or `LOG_LEVEL=debug` for per-request detail. The log file (`rh-issues-mcp.log`) is size-rotated and written beside the binary unless `-log-dir` is set. HTTP mode also mirrors logs to stderr, applies connection timeouts, and shuts down gracefully on SIGINT/SIGTERM. HTTP listeners do not provide client authentication, so bind to localhost or secure access with an authenticated proxy.

See the [JIRA server documentation](pkg/mcp/rh-issues-mcp/README.md) and [KCS server documentation](pkg/mcp/rhkcs-mcp/README.md) for tool parameters and MCP client configuration. Keep tokens out of committed configuration files.

## Development

```sh
make format
make test
make vet
make clean # Remove the two built binaries (preserves logs)
make install # Build both servers and copy them to ~/bin (override with INSTALL_DIR=...)
```

Licensed under [Apache-2.0](LICENSE).
