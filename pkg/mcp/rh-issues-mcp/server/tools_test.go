package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	jira "github.com/andygrunwald/go-jira/v2/cloud"
	"github.com/modelcontextprotocol/go-sdk/mcp"
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

func TestGetIssueFixAndTargetVersions(t *testing.T) {
	issueJSON := `{
		"key": "TEST-100",
		"names": {
			"customfield_10855": "Target Version"
		},
		"fields": {
			"summary": "Sample clone issue title",
			"description": "Issue description here",
			"status": {"name": "Verified"},
			"priority": {"name": "Major"},
			"created": "2026-09-29T16:06:01.000+0000",
			"updated": "2026-10-01T04:15:58.000+0000",
			"fixVersions": [
				{"name": "4.21.0"}
			],
			"versions": [
				{"name": "4.19.0"}
			],
			"customfield_10855": [
				{"name": "4.20.z"}
			]
		}
	}`

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/rest/api/2/issue/TEST-100" {
			t.Errorf("unexpected path: %s", r.URL.Path)
			http.NotFound(w, r)
			return
		}
		if r.URL.Query().Get("expand") != "names" {
			t.Errorf("expected expand=names query parameter, got %s", r.URL.RawQuery)
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(issueJSON))
	}))
	defer ts.Close()

	client, err := jira.NewClient(ts.URL, ts.Client())
	if err != nil {
		t.Fatal(err)
	}
	previousAPI := globalAPI
	globalAPI = &JiraAPI{client: client}
	t.Cleanup(func() { globalAPI = previousAPI })

	callResult, details, err := getIssue(context.Background(), nil, GetIssueParams{IssueKey: "TEST-100"})
	if err != nil {
		t.Fatal(err)
	}

	if details == nil {
		t.Fatal("expected non-nil details")
	}

	// Verify Target Version / Release
	if len(details.TargetVersions) != 1 || details.TargetVersions[0] != "4.20.z" {
		t.Errorf("unexpected TargetVersions: %v", details.TargetVersions)
	}
	if details.TargetRelease != "4.20.z" {
		t.Errorf("unexpected TargetRelease: %q", details.TargetRelease)
	}

	// Verify Fix Version
	if len(details.FixVersions) != 1 || details.FixVersions[0] != "4.21.0" {
		t.Errorf("unexpected FixVersions: %v", details.FixVersions)
	}
	if details.FixVersion != "4.21.0" {
		t.Errorf("unexpected FixVersion: %q", details.FixVersion)
	}

	// Verify Affects Version
	if len(details.AffectsVersions) != 1 || details.AffectsVersions[0] != "4.19.0" {
		t.Errorf("unexpected AffectsVersions: %v", details.AffectsVersions)
	}

	// Verify text response representation
	if len(callResult.Content) == 0 {
		t.Fatal("expected non-empty tool result content")
	}
	text := callResult.Content[0].(*mcp.TextContent).Text
	for _, expected := range []string{
		"**Target Version(s):** 4.20.z",
		"**Fix Version(s):** 4.21.0",
		"**Affects Version(s):** 4.19.0",
	} {
		if !jsonContainsSubstring(text, expected) {
			t.Errorf("expected responseText to contain %q, got:\n%s", expected, text)
		}
	}

	// Verify JSON marshaling
	encoded, err := json.Marshal(details)
	if err != nil {
		t.Fatal(err)
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(encoded, &fields); err != nil {
		t.Fatal(err)
	}
	if string(fields["target_release"]) != `"4.20.z"` {
		t.Errorf("expected target_release to be \"4.20.z\", got %s", fields["target_release"])
	}
	if string(fields["target_versions"]) != `["4.20.z"]` {
		t.Errorf("expected target_versions to be [\"4.20.z\"], got %s", fields["target_versions"])
	}
	if string(fields["fix_versions"]) != `["4.21.0"]` {
		t.Errorf("expected fix_versions to be [\"4.21.0\"], got %s", fields["fix_versions"])
	}
	if string(fields["fix_version"]) != `"4.21.0"` {
		t.Errorf("expected fix_version to be \"4.21.0\", got %s", fields["fix_version"])
	}
}

func TestGetIssueEmptyVersionsAreArrays(t *testing.T) {
	issueJSON := `{
		"key": "TEST-200",
		"fields": {
			"summary": "Issue without versions",
			"description": "Description",
			"status": {"name": "New"},
			"priority": {"name": "Normal"}
		}
	}`

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(issueJSON))
	}))
	defer ts.Close()

	client, err := jira.NewClient(ts.URL, ts.Client())
	if err != nil {
		t.Fatal(err)
	}
	previousAPI := globalAPI
	globalAPI = &JiraAPI{client: client}
	t.Cleanup(func() { globalAPI = previousAPI })

	callResult, details, err := getIssue(context.Background(), nil, GetIssueParams{IssueKey: "TEST-200"})
	if err != nil {
		t.Fatal(err)
	}

	if details.TargetVersions == nil || len(details.TargetVersions) != 0 {
		t.Errorf("expected empty non-nil TargetVersions, got %v", details.TargetVersions)
	}
	if details.FixVersions == nil || len(details.FixVersions) != 0 {
		t.Errorf("expected empty non-nil FixVersions, got %v", details.FixVersions)
	}
	if details.TargetRelease != "" {
		t.Errorf("expected empty TargetRelease, got %q", details.TargetRelease)
	}
	if details.FixVersion != "" {
		t.Errorf("expected empty FixVersion, got %q", details.FixVersion)
	}

	text := callResult.Content[0].(*mcp.TextContent).Text
	if jsonContainsSubstring(text, "Target Version(s):") {
		t.Errorf("responseText should not have Target Version(s): %s", text)
	}
	if jsonContainsSubstring(text, "Fix Version(s):") {
		t.Errorf("responseText should not have Fix Version(s): %s", text)
	}

	// Verify JSON serialization produces [] for arrays and omits empty strings
	encoded, err := json.Marshal(details)
	if err != nil {
		t.Fatal(err)
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(encoded, &fields); err != nil {
		t.Fatal(err)
	}
	if string(fields["target_versions"]) != "[]" {
		t.Errorf("target_versions must serialize as [], got %s", fields["target_versions"])
	}
	if string(fields["fix_versions"]) != "[]" {
		t.Errorf("fix_versions must serialize as [], got %s", fields["fix_versions"])
	}
	if _, exists := fields["target_release"]; exists {
		t.Errorf("target_release should be omitted when empty, got %s", fields["target_release"])
	}
	if _, exists := fields["fix_version"]; exists {
		t.Errorf("fix_version should be omitted when empty, got %s", fields["fix_version"])
	}
}

func TestGetIssueVariousTargetFields(t *testing.T) {
	tests := []struct {
		name            string
		jsonBody        string
		expectedTargets []string
	}{
		{
			name: "customfield_10886 single version object",
			jsonBody: `{
				"key": "TEST-301",
				"fields": {
					"summary": "Single version object test",
					"customfield_10886": {"name": "42.0.0.Beta1"}
				}
			}`,
			expectedTargets: []string{"42.0.0.Beta1"},
		},
		{
			name: "customfield_10878 backport versions array",
			jsonBody: `{
				"key": "TEST-302",
				"fields": {
					"summary": "Backport versions array test",
					"customfield_10878": [{"name": "rhel-9.6.z"}, {"name": "rhel-10.2.z"}]
				}
			}`,
			expectedTargets: []string{"rhel-9.6.z", "rhel-10.2.z"},
		},
		{
			name: "customfield_10495 bz target release option",
			jsonBody: `{
				"key": "TEST-303",
				"fields": {
					"summary": "BZ target release test",
					"customfield_10495": {"value": "8.3"}
				}
			}`,
			expectedTargets: []string{"8.3"},
		},
		{
			name: "customfield_10813 zstream target release option",
			jsonBody: `{
				"key": "TEST-304",
				"fields": {
					"summary": "ZStream target release test",
					"customfield_10813": {"value": "8.2.0"}
				}
			}`,
			expectedTargets: []string{"8.2.0"},
		},
		{
			name: "dynamic name lookup for target version",
			jsonBody: `{
				"key": "TEST-305",
				"names": {
					"customfield_54321": "Target Version"
				},
				"fields": {
					"summary": "Dynamic target test",
					"customfield_54321": [{"name": "4.20.z"}]
				}
			}`,
			expectedTargets: []string{"4.20.z"},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				w.Write([]byte(tc.jsonBody))
			}))
			defer ts.Close()

			client, err := jira.NewClient(ts.URL, ts.Client())
			if err != nil {
				t.Fatal(err)
			}
			previousAPI := globalAPI
			globalAPI = &JiraAPI{client: client}
			t.Cleanup(func() { globalAPI = previousAPI })

			_, details, err := getIssue(context.Background(), nil, GetIssueParams{IssueKey: "KEY-1"})
			if err != nil {
				t.Fatal(err)
			}

			if len(details.TargetVersions) != len(tc.expectedTargets) {
				t.Fatalf("expected %d target versions, got %v", len(tc.expectedTargets), details.TargetVersions)
			}
			for i, v := range tc.expectedTargets {
				if details.TargetVersions[i] != v {
					t.Errorf("target version mismatch at index %d: expected %q, got %q", i, v, details.TargetVersions[i])
				}
			}
		})
	}
}

func TestSearchIssuesFixAndTargetVersions(t *testing.T) {
	searchBody := `{
		"isLast": true,
		"issues": [
			{
				"key": "TEST-400",
				"fields": {
					"summary": "Sample clone issue",
					"status": {"name": "Verified"},
					"priority": {"name": "Major"},
					"fixVersions": [{"name": "4.21.0"}],
					"customfield_10855": [{"name": "4.20.z"}]
				}
			}
		]
	}`

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(searchBody))
	}))
	defer ts.Close()

	client, err := jira.NewClient(ts.URL, ts.Client())
	if err != nil {
		t.Fatal(err)
	}
	previousAPI := globalAPI
	globalAPI = &JiraAPI{client: client}
	t.Cleanup(func() { globalAPI = previousAPI })

	callResult, result, err := searchIssues(context.Background(), nil, SearchIssuesParams{Query: "project = TEST"})
	if err != nil {
		t.Fatal(err)
	}

	if len(result.Issues) != 1 {
		t.Fatalf("expected 1 issue, got %d", len(result.Issues))
	}
	issue := result.Issues[0]
	if len(issue.TargetVersions) != 1 || issue.TargetVersions[0] != "4.20.z" {
		t.Errorf("unexpected TargetVersions: %v", issue.TargetVersions)
	}
	if issue.TargetRelease != "4.20.z" {
		t.Errorf("unexpected TargetRelease: %q", issue.TargetRelease)
	}
	if len(issue.FixVersions) != 1 || issue.FixVersions[0] != "4.21.0" {
		t.Errorf("unexpected FixVersions: %v", issue.FixVersions)
	}

	text := callResult.Content[0].(*mcp.TextContent).Text
	if !jsonContainsSubstring(text, "Target: 4.20.z") {
		t.Errorf("expected text to contain 'Target: 4.20.z', got:\n%s", text)
	}
	if !jsonContainsSubstring(text, "Fix: 4.21.0") {
		t.Errorf("expected text to contain 'Fix: 4.21.0', got:\n%s", text)
	}
}

func jsonContainsSubstring(s, substr string) bool {
	return strings.Contains(s, substr)
}
