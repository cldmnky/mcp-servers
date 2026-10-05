# Repository-specific guidance

## Build and verify
- Use Go 1.24.6+; this is one module with two standalone binaries, not the original `ocp-agent` workspace.
- Run commands from the root: `make` builds both binaries into ignored `bin/`; `make build-issues-mcp` builds Jira, while `make build-mcp` builds **KCS only**.
- Checks: `make format` (`go fmt ./...`), `make test` (`go test ./...`), `make vet` (`go vet ./...`). Root `go build` has no Go package; use the Make targets.
- Focused Jira test: `go test ./pkg/mcp/rh-issues-mcp/server -run '^TestCloudAPI$' -count=1`. Package scope: `go test ./pkg/mcp/rh-issues-mcp/...` (or `rhkcs-mcp/...`).
- Existing tests are Jira-only, use `httptest` and dummy credentials, and need no external service or real tokens. KCS currently has no tests.

## Wiring and API contracts
- Each `pkg/mcp/{rh-issues-mcp,rhkcs-mcp}/main.go` owns CLI, logging, and transport. Its `server/tools.go` constructs the MCP server, initializes a package-global API client, and registers tools; `server/api.go` owns upstream requests/authentication.
- Tool parameter/result structs and their `json`/`jsonschema` tags define SDK-inferred schemas via `mcp.AddTool`; handlers return both text content and typed structured results. Keep both representations aligned when changing output.
- Jira uses `github.com/andygrunwald/go-jira/v2/cloud` against `https://redhat.atlassian.net`: email/API-token Basic auth, not legacy PATs or OAuth gateway tokens. Preserve REST v2 for string descriptions and `/rest/api/2/search/jql` (`SearchV2JQL`), not retired `/search`.
- Jira pagination is `next_page_token`/`is_last`, not offsets or totals. Empty `issues` must remain `[]`, not `null`, to satisfy the MCP array schema.
- Both KCS tools POST to `/hydra/rest/search/v2/kcs`; `get_kcs` is an `id:` search with field selection, not a separate solution endpoint. Preserve the request's `expression` field.
- KCS docs/schema descriptions overstate current behavior: omitted `rows` actually defaults to **10**, `session_id` is unused, and requests do not enforce the claimed document-kind/access-state filters. Check `server/tools.go` before relying on those claims.
- API clients are package globals; tests replacing `globalAPI` or credentials must not run in parallel and should restore any replaced client.

## Runtime gotchas
- Jira requires `RH_JIRA_EMAIL` and `RH_JIRA_TOKEN`; KCS requires `RH_API_OFFLINE_TOKEN` (exchanged through Red Hat SSO). Variables are read directly from the environment; `.env` is not loaded.
- Default transport is stdio: keep stdout protocol-only and use the configured logger for diagnostics. Logs append to dated files **beside the executable**, so its directory must be writable; HTTP mode also logs to stderr.
- HTTP has no client authentication. Prefer `./bin/rhkcs-mcp -http localhost:8080` or `./bin/rh-issues-mcp -http localhost:8081`; `make run-mcp-http` binds **all interfaces** (`:8080`). Both `run-mcp*` targets are KCS-only.
- Tool/client examples live in each server's README; `pkg/mcp/rhkcs-mcp/rhkcs-mcp-config.json` is a placeholder client example, not server-side configuration. Never put real tokens in it.
