package mcphttp

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

const initializeBody = `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18","capabilities":{},"clientInfo":{"name":"test-client","version":"1"}}}`

func httpRequest(t *testing.T, client *http.Client, url, method, sessionID, body string) (int, http.Header) {
	t.Helper()
	req, err := http.NewRequest(method, url, strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")
	req.Header.Set("MCP-Protocol-Version", "2025-06-18")
	if sessionID != "" {
		req.Header.Set("Mcp-Session-Id", sessionID)
	}
	// Exercise cleanup independently of TCP connection reuse.
	req.Close = true
	res, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	if _, err := io.Copy(io.Discard, res.Body); err != nil {
		t.Fatal(err)
	}
	return res.StatusCode, res.Header
}

func closeSessions(server *mcp.Server) {
	for session := range server.Sessions() {
		session.Close()
	}
}

func sessionCount(server *mcp.Server) int {
	n := 0
	for range server.Sessions() {
		n++
	}
	return n
}

func TestNewHandlerSupportsStatefulClients(t *testing.T) {
	if sessionTimeout != 10*time.Minute {
		t.Fatalf("unexpected production session timeout: %v", sessionTimeout)
	}
	server := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "1"}, nil)
	defer closeSessions(server)
	ts := httptest.NewServer(NewHandler(server))
	defer ts.Close()
	client := ts.Client()
	client.Timeout = 3 * time.Second

	status, headers := httpRequest(t, client, ts.URL, http.MethodPost, "", initializeBody)
	id := headers.Get("Mcp-Session-Id")
	if status != http.StatusOK || id == "" || sessionCount(server) != 1 {
		t.Fatalf("stateful initialization failed: status=%d, headers=%v", status, headers)
	}
	status, _ = httpRequest(t, client, ts.URL, http.MethodPost, id, `{"jsonrpc":"2.0","id":2,"method":"ping"}`)
	if status != http.StatusOK {
		t.Fatalf("existing session ping status = %d, want 200", status)
	}
	status, _ = httpRequest(t, client, ts.URL, http.MethodDelete, id, "")
	if status != http.StatusNoContent {
		t.Fatalf("session deletion status = %d, want 204", status)
	}
}

func TestHandlerExpiresAbandonedSessions(t *testing.T) {
	server := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "1"}, nil)
	defer closeSessions(server)
	// Use the same factory as production with a shorter timeout for testing.
	ts := httptest.NewServer(newHandler(server, 100*time.Millisecond))
	defer ts.Close()
	client := ts.Client()
	client.Timeout = 3 * time.Second
	var ids []string
	for i := 0; i < 3; i++ {
		status, headers := httpRequest(t, client, ts.URL, http.MethodPost, "", initializeBody)
		id := headers.Get("Mcp-Session-Id")
		if status != http.StatusOK || id == "" {
			t.Fatalf("initialization failed: status=%d, headers=%v", status, headers)
		}
		ids = append(ids, id)
	}
	client.CloseIdleConnections()

	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()
	deadline := time.NewTimer(5 * time.Second)
	defer deadline.Stop()
	for sessionCount(server) != 0 {
		select {
		case <-ticker.C:
		case <-deadline.C:
			t.Fatalf("abandoned sessions still retained: %d", sessionCount(server))
		}
	}
	for _, id := range ids {
		status, _ := httpRequest(t, client, ts.URL, http.MethodPost, id, `{"jsonrpc":"2.0","id":2,"method":"ping"}`)
		if status != http.StatusNotFound {
			t.Fatalf("expired session status = %d, want 404", status)
		}
	}
}
