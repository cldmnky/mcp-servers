package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	jira "github.com/andygrunwald/go-jira/v2/cloud"
)

func TestSearchIssuesEmptyResultsAreArray(t *testing.T) {
	for _, body := range []string{
		`{"issues":[],"isLast":true}`,
		`{"issues":null,"isLast":true}`,
	} {
		t.Run(body, func(t *testing.T) {
			query := `text ~ "\"enable-dynamic-udn-allocation\""`
			ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != "/rest/api/2/search/jql" || r.URL.Query().Get("jql") != query {
					t.Errorf("unexpected search request: %s", r.URL)
				}
				w.Header().Set("Content-Type", "application/json")
				w.Write([]byte(body))
			}))
			defer ts.Close()
			client, err := jira.NewClient(ts.URL, ts.Client())
			if err != nil {
				t.Fatal(err)
			}
			previousAPI := globalAPI
			globalAPI = &JiraAPI{client: client}
			t.Cleanup(func() { globalAPI = previousAPI })

			toolResult, result, err := searchIssues(context.Background(), nil, SearchIssuesParams{Query: query})
			if err != nil {
				t.Fatal(err)
			}
			if toolResult == nil || result.Issues == nil || len(result.Issues) != 0 || result.Count != 0 || !result.IsLast || result.Query != query {
				t.Fatalf("unexpected empty result: %+v", result)
			}
			encoded, err := json.Marshal(result)
			if err != nil {
				t.Fatal(err)
			}
			var fields map[string]json.RawMessage
			if err := json.Unmarshal(encoded, &fields); err != nil {
				t.Fatal(err)
			}
			if string(fields["issues"]) != "[]" {
				t.Fatalf("issues must serialize as [], got %s", fields["issues"])
			}
		})
	}
}
