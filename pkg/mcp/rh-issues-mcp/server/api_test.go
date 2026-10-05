package server

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	jira "github.com/andygrunwald/go-jira/v2/cloud"
)

func TestNewJiraAPIRequiresCloudCredentials(t *testing.T) {
	t.Setenv("RH_JIRA_TOKEN", "test-token")
	t.Setenv("RH_JIRA_EMAIL", "")
	if _, err := NewJiraAPI(); err == nil {
		t.Fatal("expected missing email error")
	}
	t.Setenv("RH_JIRA_EMAIL", "user@example.com")
	t.Setenv("RH_JIRA_TOKEN", "")
	if _, err := NewJiraAPI(); err == nil {
		t.Fatal("expected missing token error")
	}
}

func TestCloudAPI(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		email, token, ok := r.BasicAuth()
		if !ok || email != "user@example.com" || token != "test-token" {
			t.Error("missing Cloud basic authentication")
		}
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/rest/api/2/search/jql":
			q := r.URL.Query()
			if q.Get("jql") != "project = PROJ" || q.Get("nextPageToken") != "page-2" || q.Get("maxResults") != "10" {
				t.Errorf("unexpected query: %v", q)
			}
			w.Write([]byte(`{"issues":[{"key":"PROJ-1"}],"isLast":false,"nextPageToken":"page-3"}`))
		case "/rest/api/2/issue/PROJ-1":
			w.Write([]byte(`{"key":"PROJ-1","fields":{"description":"Issue description"}}`))
		default:
			t.Errorf("unexpected path: %s", r.URL.Path)
			http.NotFound(w, r)
		}
	}))
	defer ts.Close()
	tp := jira.BasicAuthTransport{Username: "user@example.com", APIToken: "test-token"}
	client, err := jira.NewClient(ts.URL, tp.Client())
	if err != nil {
		t.Fatal(err)
	}
	api := &JiraAPI{client: client}
	issues, resp, err := api.SearchIssues(context.Background(), "project = PROJ", &jira.SearchOptionsV2{MaxResults: 10, NextPageToken: "page-2"})
	if err != nil {
		t.Fatal(err)
	}
	if len(issues) != 1 || resp.IsLast || resp.NextPageToken != "page-3" {
		t.Fatalf("unexpected search response: %+v %+v", issues, resp)
	}
	issue, err := api.GetIssue(context.Background(), "PROJ-1")
	if err != nil {
		t.Fatal(err)
	}
	if issue.Fields.Description != "Issue description" {
		t.Fatalf("unexpected issue: %+v", issue)
	}
}
