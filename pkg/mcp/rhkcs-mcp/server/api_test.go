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
)

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
	// Flattened resolution text must regain its block structure.
	if !strings.HasPrefix(details.Resolution, "## Solution\n- OCP 4.6: Upgrade to 4.6.55 or above\n- OCP 4.7:") {
		t.Errorf("resolution head not reflowed:\n%q", details.Resolution)
	}
	for _, want := range []string{
		"\n\n## Workaround",
		"steps.\n1. Disable",
		"operator\n\n~~~ oc patch",
	} {
		if !strings.Contains(details.Resolution, want) {
			t.Errorf("resolution missing %q:\n%q", want, details.Resolution)
		}
	}
	if strings.Contains(details.Resolution, "version--type") == false {
		t.Errorf("inline double-dash must not be split: %q", details.Resolution)
	}
	if details.ViewURI != "https://access.redhat.com/solutions/7010411" {
		t.Errorf("view_uri fallback = %q", details.ViewURI)
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
		{"ordered break", "Follow these steps.1. Disable it.2. Restart it.", "Follow these steps.\n1. Disable it.\n2. Restart it."},
		{"version numbers untouched", "Upgrade to 4.6.55 or 4.7.42 before upgrading", "Upgrade to 4.6.55 or 4.7.42 before upgrading"},
		{"inline dash untouched", "run version--type json-p flags", "run version--type json-p flags"},
		{"fence break", "before ~~~ oc get pods ~~~ after", "before\n\n~~~ oc get pods\n\n~~~ after"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := reflowMarkdown(tt.in); got != tt.want {
				t.Errorf("reflowMarkdown(%q) =\n%q\nwant:\n%q", tt.in, got, tt.want)
			}
		})
	}
}
