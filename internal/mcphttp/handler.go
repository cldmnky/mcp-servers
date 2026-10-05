// Package mcphttp shares the HTTP transport policy of the MCP binaries.
package mcphttp

import (
	"net/http"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

const sessionTimeout = 10 * time.Minute

// NewHandler retains stateful client compatibility while expiring abandoned
// sessions. HTTP connection timeouts alone do not release MCP session state.
// This handler does not authenticate clients; bind locally or use an auth proxy.
func NewHandler(server *mcp.Server) *mcp.StreamableHTTPHandler {
	return newHandler(server, sessionTimeout)
}

func newHandler(server *mcp.Server, timeout time.Duration) *mcp.StreamableHTTPHandler {
	return mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server {
		return server
	}, &mcp.StreamableHTTPOptions{SessionTimeout: timeout})
}
