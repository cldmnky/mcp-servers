package server

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// resultText extracts the first text block of a tool result.
func resultText(t *testing.T, res *mcp.CallToolResult) string {
	t.Helper()
	tc, ok := res.Content[0].(*mcp.TextContent)
	if !ok {
		t.Fatalf("unexpected content type: %T", res.Content[0])
	}
	return tc.Text
}

func TestNewRedHatAPIRequiresToken(t *testing.T) {
	t.Setenv("RH_API_OFFLINE_TOKEN", "")
	if _, err := NewRedHatAPI(); err == nil {
		t.Fatal("expected missing token error")
	}
}

// newTestAPI wires a RedHatAPI at the given API server, returning it and the
// count of SSO token refreshes performed.
func newTestAPI(t *testing.T, apiURL string) (*RedHatAPI, *int32) {
	t.Helper()
	var refreshes int32
	sso := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&refreshes, 1)
		if err := r.ParseForm(); err != nil || r.Form.Get("grant_type") != "refresh_token" || r.Form.Get("client_id") != "rhsm-api" {
			t.Errorf("unexpected token request form: %v", r.Form)
		}
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"access_token":"test-access-token","expires_in":900}`)
	}))
	t.Cleanup(sso.Close)

	api := &RedHatAPI{
		BaseURL:      apiURL,
		SSOURL:       sso.URL,
		OfflineToken: "offline-token",
		client:       sso.Client(),
	}
	return api, &refreshes
}

func TestTokenRefreshAndCaching(t *testing.T) {
	var bearer string
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		bearer = r.Header.Get("Authorization")
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{}`)
	}))
	defer ts.Close()

	api, refreshes := newTestAPI(t, ts.URL)

	for i := 0; i < 2; i++ {
		if _, err := api.MakeRequest(context.Background(), http.MethodPost, "/hydra/rest/search/v2/kcs", nil); err != nil {
			t.Fatal(err)
		}
	}
	if got := atomic.LoadInt32(refreshes); got != 1 {
		t.Errorf("token refreshed %d times, want 1 (cached)", got)
	}
	if bearer != "Bearer test-access-token" {
		t.Errorf("Authorization header = %q, want Bearer test-access-token", bearer)
	}
}

func TestTokenRefreshConcurrent(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{}`)
	}))
	defer ts.Close()

	api, refreshes := newTestAPI(t, ts.URL)

	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := api.MakeRequest(context.Background(), http.MethodPost, "/hydra/rest/search/v2/kcs", nil); err != nil {
				t.Error(err)
			}
		}()
	}
	wg.Wait()

	if got := atomic.LoadInt32(refreshes); got != 1 {
		t.Errorf("token refreshed %d times under concurrency, want 1", got)
	}
}

func TestMakeRequestErrorIncludesStatusAndBody(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		fmt.Fprint(w, "{\n  \"error\": \"invalid_token\",\n  \"long\": \""+strings.Repeat("x", 400)+"\"\n}")
	}))
	defer ts.Close()

	api, _ := newTestAPI(t, ts.URL)

	_, err := api.MakeRequest(context.Background(), http.MethodPost, "/hydra/rest/search/v2/kcs", nil)
	if err == nil {
		t.Fatal("expected error")
	}
	msg := err.Error()
	for _, want := range []string{"status 401", "invalid_token"} {
		if !strings.Contains(msg, want) {
			t.Errorf("error %q missing %q", msg, want)
		}
	}
	if strings.Count(msg, "\n") != 0 {
		t.Errorf("error should be single-line, got: %q", msg)
	}
	if !strings.HasSuffix(msg, "...") {
		t.Errorf("long body should be truncated, got: %q", msg)
	}
}

// swapGlobalAPI installs api as the package-level client for the duration of
// the test. Tests using it must not run in parallel.
func swapGlobalAPI(t *testing.T, api *RedHatAPI) {
	t.Helper()
	previous := globalAPI
	globalAPI = api
	t.Cleanup(func() { globalAPI = previous })
}

func kcsSearchHandler(t *testing.T, body string) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/hydra/rest/search/v2/kcs" {
			t.Errorf("unexpected path: %s", r.URL.Path)
			http.NotFound(w, r)
			return
		}
		if r.Header.Get("Authorization") != "Bearer test-access-token" {
			t.Error("missing bearer authentication")
		}
		var req map[string]interface{}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Errorf("invalid request body: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, body)
	}))
}

func TestSearchKCSDefaultsAndDocsShape(t *testing.T) {
	var sent map[string]interface{}
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&sent); err != nil {
			t.Errorf("invalid request body: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"response":{"docs":[
			{"id":"7010411","allTitle":"Example solution","documentKind":"Solution","score":1.5,"view_uri":"https://access.redhat.com/solutions/7010411"},
			{"id":"7010412","title":"Fallback title shape","documentKind":"Article"},
			{"id":"","allTitle":"dropped: no id"}
		]}}`)
	}))
	defer ts.Close()

	api, _ := newTestAPI(t, ts.URL)
	swapGlobalAPI(t, api)

	_, result, err := searchKCS(context.Background(), nil, SearchKCSParams{Query: "gpu passthrough"})
	if err != nil {
		t.Fatal(err)
	}
	if sent["q"] != "gpu passthrough" || sent["clientName"] != "mcp" {
		t.Errorf("unexpected request payload: %v", sent)
	}
	if rows, ok := sent["rows"].(float64); !ok || rows != 10 {
		t.Errorf("default rows = %v, want 10", sent["rows"])
	}
	if _, ok := sent["expression"].(string); !ok {
		t.Errorf("missing required expression field: %v", sent)
	} else if !strings.Contains(sent["expression"].(string), "fq=documentKind:(Solution OR Article)") {
		t.Errorf("expression must filter to Solution/Article documents: %v", sent["expression"])
	}
	if result.Count != 2 {
		t.Fatalf("parsed %d solutions, want 2: %+v", result.Count, result)
	}
	first := result.Solutions[0]
	if first.ID != "7010411" || first.Title != "Example solution" || first.ViewURI == "" || first.Kind != "Solution" {
		t.Errorf("unexpected first solution: %+v", first)
	}
	if result.Solutions[1].Title != "Fallback title shape" {
		t.Errorf("unexpected second solution: %+v", result.Solutions[1])
	}
}

func TestSearchKCSResultsShape(t *testing.T) {
	ts := kcsSearchHandler(t, `{"results":[
		{"documentId":"7010413","title":"Legacy shape","uri":"/solutions/7010413"}
	]}`)
	defer ts.Close()

	api, _ := newTestAPI(t, ts.URL)
	swapGlobalAPI(t, api)

	_, result, err := searchKCS(context.Background(), nil, SearchKCSParams{Query: "legacy", Rows: 5, Start: 5})
	if err != nil {
		t.Fatal(err)
	}
	if result.Count != 1 || result.Solutions[0].ID != "7010413" ||
		result.Solutions[0].ViewURI != "https://access.redhat.com/solutions/7010413" {
		t.Fatalf("unexpected result: %+v", result)
	}
}

func TestSearchKCSRejectsNegativePaging(t *testing.T) {
	swapGlobalAPI(t, &RedHatAPI{BaseURL: "http://unused", SSOURL: "http://unused", client: http.DefaultClient})
	if _, _, err := searchKCS(context.Background(), nil, SearchKCSParams{Query: "q", Rows: -1}); err == nil {
		t.Error("expected error for negative rows")
	}
	if _, _, err := searchKCS(context.Background(), nil, SearchKCSParams{Query: "q", Start: -1}); err == nil {
		t.Error("expected error for negative start")
	}
}

func TestGetKCSExtractsFields(t *testing.T) {
	ts := kcsSearchHandler(t, `{"response":{"docs":[{
		"id":"7010411",
		"publishedTitle":"Configuring the thing",
		"standard_product":["Red Hat OpenShift Container Platform"],
		"issue":["The component fails to start."],
		"solution_resolution":"## Solution- OCP 4.6: Upgrade to 4.6.55 or above- OCP 4.7: Upgrade to 4.7.42 or above ## Workaround Follow these steps.1. Disable the operator ~~~ oc patch clusterversion version--type json ~~~- Scale down",
		"root_cause_listed_but_wrong_key":"ignored"
	}]}}`)
	defer ts.Close()

	api, _ := newTestAPI(t, ts.URL)
	swapGlobalAPI(t, api)

	_, details, err := getKCS(context.Background(), nil, GetKCSParams{SolutionID: "7010411"})
	if err != nil {
		t.Fatal(err)
	}
	if details.Title != "Configuring the thing" {
		t.Errorf("title = %q", details.Title)
	}
	if details.Environment != "Red Hat OpenShift Container Platform" {
		t.Errorf("environment (array form) = %q", details.Environment)
	}
	if details.Issue != "The component fails to start." {
		t.Errorf("issue (array form) = %q", details.Issue)
	}
	// Flattened resolution text must regain its block structure, with the
	// code fence isolated verbatim on its own lines.
	if !strings.HasPrefix(details.Resolution, "## Solution\n- OCP 4.6: Upgrade to 4.6.55 or above\n- OCP 4.7:") {
		t.Errorf("resolution head not reflowed:\n%q", details.Resolution)
	}
	for _, want := range []string{
		"\n\n## Workaround",
		"steps.\n1. Disable",
		"~~~\noc patch clusterversion version--type json\n~~~",
	} {
		if !strings.Contains(details.Resolution, want) {
			t.Errorf("resolution missing %q:\n%q", want, details.Resolution)
		}
	}
	if strings.Count(details.Resolution, "version--type") != 1 {
		t.Errorf("fenced command must stay verbatim: %q", details.Resolution)
	}
	if !fencedCodePresent(details.Resolution) {
		t.Error("fence should be detected for the warning note")
	}
	if details.ViewURI != "https://access.redhat.com/solutions/7010411" {
		t.Errorf("view_uri fallback = %q", details.ViewURI)
	}
}

func TestGetKCSArticleUsesAbstract(t *testing.T) {
	// Articles (documentKind=Article) carry an abstract instead of
	// solution_resolution/issue; drafts may have no body at all.
	ts := kcsSearchHandler(t, `{"response":{"docs":[{
		"id":"1240753",
		"documentKind":"Article",
		"publishedTitle":"Allocate Floating IP Addresses in OpenStack Networking",
		"standard_product":"Red Hat OpenStack Platform",
		"abstract":"Floating IP addresses allow you to direct ingress network traffic to your OpenStack instances.",
		"view_uri":"https://access.redhat.com/articles/1240753"
	}]}}`)
	defer ts.Close()

	api, _ := newTestAPI(t, ts.URL)
	swapGlobalAPI(t, api)

	toolResult, details, err := getKCS(context.Background(), nil, GetKCSParams{SolutionID: "1240753"})
	if err != nil {
		t.Fatal(err)
	}
	if details.Kind != "Article" || details.Abstract == "" || details.Title == "" {
		t.Fatalf("article details incomplete: %+v", details)
	}
	if !strings.Contains(resultText(t, toolResult), "**Abstract:**") {
		t.Errorf("text output missing abstract section: %s", resultText(t, toolResult))
	}
	if strings.Contains(resultText(t, toolResult), "No body content") {
		t.Errorf("abstract present, empty-body note must not appear")
	}
}

func TestGetKCSEmptyBodyNote(t *testing.T) {
	ts := kcsSearchHandler(t, `{"response":{"docs":[{
		"id":"9999999",
		"documentKind":"Article",
		"publishedTitle":"Draft with no body",
		"abstract":"",
		"issue":null,
		"solution_resolution":null
	}]}}`)
	defer ts.Close()

	api, _ := newTestAPI(t, ts.URL)
	swapGlobalAPI(t, api)

	toolResult, _, err := getKCS(context.Background(), nil, GetKCSParams{SolutionID: "9999999"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(resultText(t, toolResult), "No body content is available in the search index") {
		t.Errorf("missing empty-body note: %s", resultText(t, toolResult))
	}
}

func TestGetKCSWarnsAboutFencedCommands(t *testing.T) {
	ts := kcsSearchHandler(t, `{"response":{"docs":[{
		"id":"7010411",
		"publishedTitle":"With commands",
		"solution_resolution":"Run this: ~~~oc get nodes-o wide~~~ then stop."
	}]}}`)
	defer ts.Close()

	api, _ := newTestAPI(t, ts.URL)
	swapGlobalAPI(t, api)

	toolResult, _, err := getKCS(context.Background(), nil, GetKCSParams{SolutionID: "7010411"})
	if err != nil {
		t.Fatal(err)
	}
	text := resultText(t, toolResult)
	if !strings.Contains(text, "Treat fenced commands as reference") {
		t.Errorf("missing mangled-command warning: %s", text)
	}
}

func TestGetKCSRejectsNonNumericID(t *testing.T) {
	swapGlobalAPI(t, &RedHatAPI{BaseURL: "http://unused", SSOURL: "http://unused", client: http.DefaultClient})
	for _, bad := range []string{
		"https://docs.redhat.com/en/documentation/openshift_container_platform/",
		"labs-rearconfighelper",
		"7010411 ",
		"",
	} {
		if _, _, err := getKCS(context.Background(), nil, GetKCSParams{SolutionID: bad}); err == nil {
			t.Errorf("expected error for non-numeric solution_id %q", bad)
		}
	}
}

func TestReflowMarkdown(t *testing.T) {
	tests := []struct {
		name, in, want string
	}{
		{"empty", "", ""},
		{"already structured", "line one\nline two", "line one\nline two"},
		{"heading break", "intro text ## Workaround do this", "intro text\n\n## Workaround do this"},
		{"bullet break", "first item- second item- third", "first item\n- second item\n- third"},
		{"double dash never split", "oc debug node/-- chroot /host bash-c 'x'", "oc debug node/-- chroot /host bash-c 'x'"},
		{"ordered break", "Follow these steps.1. Disable it.2. Restart it.", "Follow these steps.\n1. Disable it.\n2. Restart it."},
		{"version numbers untouched", "Upgrade to 4.6.55 or 4.7.42 before upgrading", "Upgrade to 4.6.55 or 4.7.42 before upgrading"},
		{"inline dash untouched", "run version--type json-p flags", "run version--type json-p flags"},
		{"fence with own lines", "before ```oc get pods``` after", "before\n\n```\noc get pods\n```\n\nafter"},
		{"fence content verbatim", "intro ```# cmd --flag- yaml: value``` tail- bullet", "intro\n\n```\n# cmd --flag- yaml: value\n```\n\ntail\n- bullet"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := reflowMarkdown(tt.in); got != tt.want {
				t.Errorf("reflowMarkdown(%q) =\n%q\nwant:\n%q", tt.in, got, tt.want)
			}
		})
	}
}

func TestFencedCodePresent(t *testing.T) {
	if !fencedCodePresent("text ```cmd``` more") {
		t.Error("should detect backtick fence")
	}
	if !fencedCodePresent("text ~~~cmd~~~ more") {
		t.Error("should detect tilde fence")
	}
	if fencedCodePresent("plain text only") {
		t.Error("plain text has no fence")
	}
}
