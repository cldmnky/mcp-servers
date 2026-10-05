package server

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestReflowMarkdownUnterminatedFences(t *testing.T) {
	for _, tt := range []struct{ name, input, want string }{
		{"backticks", "Before ```oc get pods", "Before\n\n```\noc get pods\n```"},
		{"tildes", "Before ~~~oc get nodes-o wide", "Before\n\n~~~\noc get nodes-o wide\n~~~"},
		{"multiple blocks", "```first``` middle ```last", "```\nfirst\n```\n\nmiddle\n\n```\nlast\n```"},
		{"empty remainder", "Before ````", "Before\n\n````\n\n````"},
		{"structured input unchanged", "```bash\noc get pods", "```bash\noc get pods"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			if got := reflowMarkdown(tt.input); got != tt.want {
				t.Errorf("reflowMarkdown(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func FuzzReflowMarkdown(f *testing.F) {
	for _, seed := range []string{"", "Before ```oc get pods", "~~~cmd~~~ tail ```last", "```", "structured\n~~~", "intro ## Heading- bullet"} {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, input string) {
		got := reflowMarkdown(input)
		if strings.Contains(strings.TrimSpace(input), "\n") && got != strings.TrimSpace(input) {
			t.Errorf("already structured input changed: %q -> %q", input, got)
		}
	})
}

func TestGetKCSRejectsMissingAndMalformedDocuments(t *testing.T) {
	for _, tt := range []struct{ name, body, want string }{
		{"not found", `{"response":{"docs":[],"numFound":0}}`, "not found"},
		{"missing response", `{}`, "missing response object"},
		{"null response", `{"response":null}`, "missing response object"},
		{"missing docs", `{"response":{}}`, "missing docs array"},
		{"null docs", `{"response":{"docs":null}}`, "missing docs array"},
		{"wrong docs type", `{"response":{"docs":{}}}`, "missing docs array"},
		{"null document", `{"response":{"docs":[null]}}`, "document is not an object"},
		{"wrong document type", `{"response":{"docs":["bad"]}}`, "document is not an object"},
		{"missing title", `{"response":{"docs":[{"id":"7010411"}]}}`, "document has no title"},
		{"blank title", `{"response":{"docs":[{"id":"7010411","publishedTitle":" "}]}}`, "document has no title"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			ts := kcsSearchHandler(t, tt.body)
			defer ts.Close()
			api, _ := newTestAPI(t, ts.URL)
			swapGlobalAPI(t, api)
			res, details, err := getKCS(context.Background(), nil, GetKCSParams{SolutionID: "7010411"})
			if err == nil || !strings.Contains(err.Error(), tt.want) || res != nil || details != nil {
				t.Fatalf("getKCS = (%+v, %+v, %v), want error containing %q", res, details, err, tt.want)
			}
		})
	}
}

func TestGetKCSEmptyPublishedTitleFallsBack(t *testing.T) {
	ts := kcsSearchHandler(t, `{"response":{"docs":[{"id":"7010411","publishedTitle":"","allTitle":"Unpublished document"}]}}`)
	defer ts.Close()
	api, _ := newTestAPI(t, ts.URL)
	swapGlobalAPI(t, api)
	res, details, err := getKCS(context.Background(), nil, GetKCSParams{SolutionID: "7010411"})
	if err != nil {
		t.Fatal(err)
	}
	if details.Title != "Unpublished document" || !strings.Contains(resultText(t, res), "No body content is available") {
		t.Fatalf("expected a found, bodyless document: %+v", details)
	}
}

func TestGetKCSFenceAndNotFoundOverMCP(t *testing.T) {
	for _, tt := range []struct {
		name, body string
		isError    bool
	}{
		{"unmatched fence", "{\"response\":{\"docs\":[{\"id\":\"7010411\",\"publishedTitle\":\"Broken fence\",\"solution_resolution\":\"Before ```oc get pods\"}]}}", false},
		{"not found", `{"response":{"docs":[]}}`, true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			ts := kcsSearchHandler(t, tt.body)
			defer ts.Close()
			api, _ := newTestAPI(t, ts.URL)
			swapGlobalAPI(t, api)

			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			srv := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "1"}, nil)
			mcp.AddTool(srv, &mcp.Tool{Name: "get_kcs"}, getKCS)
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
			res, err := cs.CallTool(ctx, &mcp.CallToolParams{Name: "get_kcs", Arguments: map[string]any{"solution_id": "7010411", "session_id": "legacy"}})
			if err != nil {
				t.Fatal(err)
			}
			if res.IsError != tt.isError {
				t.Fatalf("IsError = %t, want %t: %+v", res.IsError, tt.isError, res)
			}
			if tt.isError {
				if !strings.Contains(resultText(t, res), "not found") {
					t.Fatalf("missing not-found error: %+v", res)
				}
				return
			}
			data, err := json.Marshal(res.StructuredContent)
			if err != nil {
				t.Fatal(err)
			}
			var details KCSDetails
			if err := json.Unmarshal(data, &details); err != nil {
				t.Fatal(err)
			}
			if details.Resolution != "Before\n\n```\noc get pods\n```" || !strings.Contains(resultText(t, res), "NOT executable as returned") {
				t.Fatalf("fence content/warning lost: %+v", res)
			}
		})
	}
}
