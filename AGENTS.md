# Repository-specific guidance

## Build and verify
- Use Go 1.24.6+; this is one module with two standalone binaries, not the original `ocp-agent` workspace.
- Run commands from the root: `make` builds both binaries into ignored `bin/`; `make build-issues-mcp` builds Jira, while `make build-mcp` builds **KCS only**.
- Checks: `make format` (`go fmt ./...`), `make test` (`go test ./...`), `make vet` (`go vet ./...`). Root `go build` has no Go package; use the Make targets.
- Focused tests: `go test ./pkg/mcp/rh-issues-mcp/server -run '^TestCloudAPI$' -count=1`; package scope `go test ./pkg/mcp/rh-issues-mcp/...` (or `rhkcs-mcp/...`). Add `-race` to exercise the KCS token-refresh lock.
- All tests use `httptest` with dummy credentials; no external service or real tokens needed.

## Wiring and API contracts
- Each `pkg/mcp/{rh-issues-mcp,rhkcs-mcp}/main.go` owns CLI flags, logging init, transports, and HTTP timeouts/graceful shutdown. Its `server/tools.go` constructs the MCP server, initializes a package-global API client, and registers tools; `server/api.go` owns upstream requests/authentication.
- Tool parameter/result structs and their `json`/`jsonschema` tags define SDK-inferred schemas via `mcp.AddTool`. The SDK generates `additionalProperties: false` — removing a param field breaks existing clients that still send it. Keep removed-behavior params (e.g. KCS `session_id`) in the struct with a "Deprecated ... ignored" description instead of deleting them.
- Handlers return both text content and typed structured results. Keep both representations aligned when changing output. Empty `issues`/array results must stay `[]`, not `null`, to satisfy the MCP array schema (see `TestSearchIssuesEmptyResultsAreArray`).
- Jira uses `github.com/andygrunwald/go-jira/v2/cloud` against `https://redhat.atlassian.net`: email/API-token Basic auth, not legacy PATs or OAuth gateway tokens. Preserve REST v2 for string descriptions and `/rest/api/2/search/jql` (`SearchV2JQL`), not retired `/search`.
- Jira pagination is `next_page_token`/`is_last`, not offsets or totals.
- Both KCS tools POST to `/hydra/rest/search/v2/kcs`; `get_kcs` is an `id:` search with field selection, not a separate solution endpoint. Preserve the request's `expression` field. KCS `rows` defaults to 10 and `session_id` is accepted but ignored.
- KCS `RedHatAPI` caches the access token behind a mutex; concurrent requests must refresh exactly once. Tests assert this — keep it that way.
- API clients are package globals; tests replacing `globalAPI` must not run in parallel and restore via `t.Cleanup` (see `swapGlobalAPI`).

## Logging
- `internal/logging` is the shared leveled logger (default **warn**; `-v` or `LOG_LEVEL=debug|info|warn|error`). Per-request detail goes to `Debugf`, real failures to `Errorf`. Never dump full API responses into logs.
- Rotation is `lumberjack` (`<service>.log`, 10 MB, 5 backups, 30 days, compressed), default directory beside the binary, overridable with `-log-dir`. Log files are created lazily — a quiet server creates none.
- stdio mode logs to the file only (stdout is the MCP protocol); HTTP mode mirrors to stderr.

## Runtime gotchas
- Jira requires `RH_JIRA_EMAIL` and `RH_JIRA_TOKEN`; KCS requires `RH_API_OFFLINE_TOKEN` (exchanged through Red Hat SSO). Variables are read directly from the environment; `.env` is not loaded.
- HTTP has no client authentication. Prefer `./bin/rhkcs-mcp -http localhost:8080` or `./bin/rh-issues-mcp -http localhost:8081`; `make run-mcp-http` binds **all interfaces** (`:8080`). Both `run-mcp*` targets are KCS-only.
- A `rhkcs-mcp` may run from `~/bin` as the user's deployed MCP server — never kill or "clean up" processes outside this repo's `bin/`.
- Tool/client examples live in each server's README; `pkg/mcp/rhkcs-mcp/rhkcs-mcp-config.json` is a placeholder client example, not server-side configuration. Never put real tokens in it.
