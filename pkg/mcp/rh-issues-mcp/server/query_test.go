package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	jira "github.com/andygrunwald/go-jira/v2/cloud"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestJQLQueryDetection(t *testing.T) {
	for _, query := range []string{
		"project=OCPBUGS",
		`summary~"crash"`,
		`fixVersion = "4.21.0"`,
		"assignee IS EMPTY",
		"assignee IS NOT EMPTY",
		"status NOT IN (Open, Closed)",
		"assignee IN membersOf(\"developers\")",
		`status WAS "Open"`,
		"status CHANGED AFTER -1w",
		"created>=-7d",
		`"Target Release" = "4.21.0"`,
		"cf[10855] IN (12345)",
		"(project=OCPBUGS OR project=OCPENG)",
		"NOT (status=Closed)",
		"order by updated DESC",
		"  project\t= OCPBUGS",
	} {
		t.Run(query, func(t *testing.T) {
			if got := buildJQLQuery(query); got != query {
				t.Errorf("JQL rewritten: %q -> %q", query, got)
			}
		})
	}
	for _, query := range []string{
		"etcd and ovnkube failures",
		"timeout OR crashloop",
		"NOT responding after reboot",
		"ovnkube crashloop",
		`"project = OCPBUGS"`,
		"look for status = Closed in logs",
		"somewhere in the network",
		"status is unavailable",
		"everything is not ready",
	} {
		t.Run(query, func(t *testing.T) {
			if got := buildJQLQuery(query); got != buildTextJQLQuery(query) {
				t.Errorf("plain text mistaken for JQL: %q -> %q", query, got)
			}
		})
	}
}

func TestResolveJQLQueryModes(t *testing.T) {
	for _, tt := range []struct{ query, mode, want string }{
		{"project=OCPBUGS", "", "project=OCPBUGS"},
		{"project=OCPBUGS", "auto", "project=OCPBUGS"},
		{"issue.property[example].value = true", "jql", "issue.property[example].value = true"},
		{"project=OCPBUGS", "text", `text ~ "project=OCPBUGS"`},
		{`path C:\temp "name"`, "text", `text ~ "path C:\\temp \"name\""`},
		{`trailing\`, "auto", `text ~ "trailing\\"`},
	} {
		got, err := resolveJQLQuery(tt.query, tt.mode)
		if err != nil || got != tt.want {
			t.Errorf("resolveJQLQuery(%q, %q) = (%q, %v), want %q", tt.query, tt.mode, got, err, tt.want)
		}
	}
	if _, err := resolveJQLQuery("query", "invalid"); err == nil {
		t.Error("invalid query_mode must return an error")
	}
}

func TestSearchIssuesQueryModesOverMCP(t *testing.T) {
	sent := make(chan string, 10)
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sent <- r.URL.Query().Get("jql")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"issues":[],"isLast":true}`))
	}))
	defer ts.Close()
	apiClient, err := jira.NewClient(ts.URL, ts.Client())
	if err != nil {
		t.Fatal(err)
	}
	previous := globalAPI
	globalAPI = &JiraAPI{client: apiClient}
	t.Cleanup(func() { globalAPI = previous })

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	srv := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "1"}, nil)
	mcp.AddTool(srv, &mcp.Tool{Name: "search_issues"}, searchIssues)
	st, ct := mcp.NewInMemoryTransports()
	ss, err := srv.Connect(ctx, st, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer ss.Close()
	client := mcp.NewClient(&mcp.Implementation{Name: "test-client", Version: "1"}, nil)
	cs, err := client.Connect(ctx, ct, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer cs.Close()

	for _, tt := range []struct{ name, query, mode, want string }{
		{"legacy client", "etcd and ovnkube failures", "", `text ~ "etcd and ovnkube failures"`},
		{"auto", "project=OCPBUGS", "auto", "project=OCPBUGS"},
		{"literal", "project=OCPBUGS", "text", `text ~ "project=OCPBUGS"`},
		{"explicit JQL", "issue.property[example].value = true", "jql", "issue.property[example].value = true"},
		{"invalid mode", "query", "invalid", ""},
	} {
		t.Run(tt.name, func(t *testing.T) {
			args := map[string]any{"query": tt.query}
			if tt.mode != "" {
				args["query_mode"] = tt.mode
			}
			res, err := cs.CallTool(ctx, &mcp.CallToolParams{Name: "search_issues", Arguments: args})
			if err != nil {
				t.Fatal(err)
			}
			if tt.mode == "invalid" {
				if !res.IsError || !strings.Contains(res.Content[0].(*mcp.TextContent).Text, "query_mode") {
					t.Fatalf("expected invalid-mode tool error: %+v", res)
				}
				select {
				case q := <-sent:
					t.Fatalf("invalid mode sent an upstream request: %q", q)
				default:
				}
				return
			}
			if res.IsError {
				t.Fatalf("unexpected tool error: %+v", res)
			}
			select {
			case got := <-sent:
				if got != tt.want {
					t.Errorf("upstream JQL = %q, want %q", got, tt.want)
				}
			case <-ctx.Done():
				t.Fatal("missing upstream request")
			}
			data, err := json.Marshal(res.StructuredContent)
			if err != nil {
				t.Fatal(err)
			}
			var result SearchIssuesResult
			if err := json.Unmarshal(data, &result); err != nil {
				t.Fatal(err)
			}
			if result.Query != tt.want {
				t.Errorf("result query = %q, want %q", result.Query, tt.want)
			}
		})
	}
}
