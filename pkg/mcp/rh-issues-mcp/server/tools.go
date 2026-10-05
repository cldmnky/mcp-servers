package server

import (
	"context"
	"fmt"
	"regexp"
	"strings"
	"time"

	jira "github.com/andygrunwald/go-jira/v2/cloud"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/trivago/tgo/tcontainer"

	"github.com/cldmnky/mcp-servers/internal/logging"
)

// Global API client instance
var globalAPI *JiraAPI

// Initialize the global API client
func initGlobalAPI() error {
	var err error
	globalAPI, err = NewJiraAPI()
	return err
}

// Search Issues tool parameters
type SearchIssuesParams struct {
	Query         string `json:"query" jsonschema:"Search terms or a JQL expression. By default, leading field/operator clauses or ORDER BY are detected as JQL; other input is wrapped in a full-text search. Use query_mode to disambiguate. Examples: 'project = OCPBUGS AND text ~ \"GPU passthrough\"' or 'ovnkube crashloop'"`
	QueryMode     string `json:"query_mode,omitempty" jsonschema:"Query interpretation: auto (default), jql (pass through unchanged), or text (always wrap in a full-text search). Reuse the same mode when paginating"`
	MaxResults    int    `json:"max_results,omitempty" jsonschema:"Page size, 1-100; defaults to 50. Keep modest to limit response size"`
	NextPageToken string `json:"next_page_token,omitempty" jsonschema:"Continuation token returned by a previous search_issues call. Omit on the first page and reuse the exact same query and query_mode when paginating"`
}

// JIRA Issue response
type JiraIssue struct {
	Key            string   `json:"key"`
	Summary        string   `json:"summary"`
	Status         string   `json:"status"`
	Priority       string   `json:"priority"`
	ViewURI        string   `json:"view_uri"`
	TargetRelease  string   `json:"target_release,omitempty"`
	TargetVersions []string `json:"target_versions"`
	FixVersions    []string `json:"fix_versions"`
}

// Search Issues result wrapper
type SearchIssuesResult struct {
	Issues        []JiraIssue `json:"issues" jsonschema:"List of JIRA issues found"`
	NextPageToken string      `json:"next_page_token,omitempty" jsonschema:"Continuation token for the next page"`
	IsLast        bool        `json:"is_last" jsonschema:"Whether this is the last page of results"`
	Count         int         `json:"count" jsonschema:"Number of issues returned in this response"`
	Query         string      `json:"query" jsonschema:"The JQL query used"`
}

// Detect a leading JQL clause, not boolean words embedded in plain text.
// Field names may be identifiers, quoted names, or custom-field references;
// whitespace around symbolic operators is optional. Auto-detection remains a
// heuristic, so query_mode offers an explicit override for ambiguous input.
var (
	jqlClause = regexp.MustCompile(`(?i)^\s*(?:(?:NOT\b\s*|\()\s*)*` +
		`(?:[a-z][a-z0-9_.]*|"(?:\\.|[^"\\])*"|'(?:\\.|[^'\\])*'|cf\[\d+\])\s*` +
		`(?:[<>]=?|!=|!?~|=|\b(?:` +
		`(?:NOT\s+IN|IN)\b\s*(?:\(|[a-z][a-z0-9_]*\s*\()` +
		`|IS\s+(?:NOT\s+)?(?:EMPTY|NULL)\b|WAS\b|CHANGED\b))`)
	jqlOrder = regexp.MustCompile(`(?i)^\s*ORDER\s+BY\s+\S`)
)

func isJQLQuery(query string) bool {
	return jqlClause.MatchString(query) || jqlOrder.MatchString(query)
}

func resolveJQLQuery(query, mode string) (string, error) {
	switch mode {
	case "", "auto":
		return buildJQLQuery(query), nil
	case "jql":
		return query, nil
	case "text":
		return buildTextJQLQuery(query), nil
	default:
		return "", fmt.Errorf("query_mode must be auto, jql, or text")
	}
}

// buildJQLQuery converts a natural language query to JQL
func buildJQLQuery(query string) string {
	// If it already looks like JQL, return as-is
	if isJQLQuery(query) {
		return query
	}

	return buildTextJQLQuery(query)
}

func buildTextJQLQuery(query string) string {
	// Escape backslashes before quotes so literal text cannot break out of
	// the JQL string. text ~ searches summary, description, comments, and environment.
	escapedQuery := strings.ReplaceAll(query, `\`, `\\`)
	escapedQuery = strings.ReplaceAll(escapedQuery, `"`, `\"`)

	jql := fmt.Sprintf(`text ~ "%s"`, escapedQuery)

	logging.Debugf("[buildJQLQuery] converted natural language query %q to JQL: %q", query, jql)
	return jql
}

// Known field IDs in Red Hat Jira used for target release / version
var knownTargetFieldIDs = []string{
	"customfield_10855", // Target Version (OpenShift, array of version objects)
	"customfield_10886", // Target Release (WildFly/middleware, version object)
	"customfield_10878", // Target Backport Versions (RHEL, array of version objects)
	"customfield_10495", // BZ Target Release (Bugzilla migrated, option object)
	"customfield_10813", // ZStream Target Release (option object)
	"customfield_10820", // Target Upstream Version (string)
	"target_version",
	"target_versions",
	"target_release",
	"targetVersion",
	"targetRelease",
}

func isTargetVersionFieldName(name string) bool {
	switch strings.ToLower(strings.TrimSpace(name)) {
	case "target version", "target versions",
		"target release", "target releases",
		"target backport versions",
		"bz target release", "zstream target release",
		"target upstream version":
		return true
	default:
		return false
	}
}

func extractVersionsFromValue(val interface{}) []string {
	if val == nil {
		return nil
	}

	var results []string

	switch v := val.(type) {
	case string:
		if s := strings.TrimSpace(v); s != "" {
			results = append(results, s)
		}
	case []interface{}:
		for _, item := range v {
			results = append(results, extractVersionsFromValue(item)...)
		}
	case []string:
		for _, s := range v {
			if trimmed := strings.TrimSpace(s); trimmed != "" {
				results = append(results, trimmed)
			}
		}
	case map[string]interface{}:
		if name, ok := v["name"].(string); ok && strings.TrimSpace(name) != "" {
			results = append(results, strings.TrimSpace(name))
		} else if value, ok := v["value"].(string); ok && strings.TrimSpace(value) != "" {
			results = append(results, strings.TrimSpace(value))
		}
	case tcontainer.MarshalMap:
		if name, err := v.String("name"); err == nil && strings.TrimSpace(name) != "" {
			results = append(results, strings.TrimSpace(name))
		} else if value, err := v.String("value"); err == nil && strings.TrimSpace(value) != "" {
			results = append(results, strings.TrimSpace(value))
		}
	}

	return results
}

func extractTargetVersions(issue *jira.Issue) []string {
	if issue == nil || issue.Fields == nil {
		return make([]string, 0)
	}

	var raw []string
	seen := make(map[string]bool)

	addVersions := func(versions []string) {
		for _, v := range versions {
			v = strings.TrimSpace(v)
			if v != "" && !seen[v] {
				seen[v] = true
				raw = append(raw, v)
			}
		}
	}

	// 1. Check issue.Names if available (expanded)
	if len(issue.Names) > 0 && len(issue.Fields.Unknowns) > 0 {
		for fieldID, fieldName := range issue.Names {
			if isTargetVersionFieldName(fieldName) {
				if val, exists := issue.Fields.Unknowns.Value(fieldID); exists {
					addVersions(extractVersionsFromValue(val))
				}
			}
		}
	}

	// 2. Check known field IDs in Unknowns
	if len(issue.Fields.Unknowns) > 0 {
		for _, fieldID := range knownTargetFieldIDs {
			if val, exists := issue.Fields.Unknowns.Value(fieldID); exists {
				addVersions(extractVersionsFromValue(val))
			}
		}
	}

	if raw == nil {
		return make([]string, 0)
	}
	return raw
}

func extractFixVersions(issue *jira.Issue) []string {
	if issue == nil || issue.Fields == nil {
		return make([]string, 0)
	}

	var raw []string
	seen := make(map[string]bool)

	addVersion := func(v string) {
		v = strings.TrimSpace(v)
		if v != "" && !seen[v] {
			seen[v] = true
			raw = append(raw, v)
		}
	}

	// Standard go-jira FixVersions field
	for _, fv := range issue.Fields.FixVersions {
		if fv != nil {
			addVersion(fv.Name)
		}
	}

	// Fallback to Unknowns in case it was unmarshalled there
	if len(raw) == 0 && len(issue.Fields.Unknowns) > 0 {
		for _, key := range []string{"fixVersions", "fix_versions", "fixVersion"} {
			if val, exists := issue.Fields.Unknowns.Value(key); exists {
				for _, v := range extractVersionsFromValue(val) {
					addVersion(v)
				}
			}
		}
	}

	if raw == nil {
		return make([]string, 0)
	}
	return raw
}

func extractAffectsVersions(issue *jira.Issue) []string {
	if issue == nil || issue.Fields == nil {
		return make([]string, 0)
	}

	var raw []string
	seen := make(map[string]bool)

	addVersion := func(v string) {
		v = strings.TrimSpace(v)
		if v != "" && !seen[v] {
			seen[v] = true
			raw = append(raw, v)
		}
	}

	for _, av := range issue.Fields.AffectsVersions {
		if av != nil {
			addVersion(av.Name)
		}
	}

	if len(raw) == 0 && len(issue.Fields.Unknowns) > 0 {
		for _, key := range []string{"versions", "affects_versions", "affectsVersions"} {
			if val, exists := issue.Fields.Unknowns.Value(key); exists {
				for _, v := range extractVersionsFromValue(val) {
					addVersion(v)
				}
			}
		}
	}

	if raw == nil {
		return make([]string, 0)
	}
	return raw
}

// Search JIRA Issues
func searchIssues(ctx context.Context, req *mcp.CallToolRequest, params SearchIssuesParams) (*mcp.CallToolResult, *SearchIssuesResult, error) {
	if params.MaxResults < 0 {
		return nil, nil, fmt.Errorf("max_results must be positive")
	}

	if globalAPI == nil {
		return nil, nil, fmt.Errorf("JIRA API client not initialized")
	}

	// Set defaults
	if params.MaxResults == 0 {
		params.MaxResults = 50
	}
	// Jira Cloud enhanced search caps page size at 100.
	if params.MaxResults > 100 {
		params.MaxResults = 100
	}

	// Convert query to JQL if needed, honoring an explicit interpretation.
	jqlQuery, err := resolveJQLQuery(params.Query, params.QueryMode)
	if err != nil {
		return nil, nil, err
	}

	// Build search options
	searchOpts := &jira.SearchOptionsV2{
		MaxResults:    params.MaxResults,
		NextPageToken: params.NextPageToken,
		Fields: []string{
			"key", "summary", "status", "priority", "fixVersions",
			"customfield_10855", "customfield_10886", "customfield_10878",
			"customfield_10495", "customfield_10813",
		},
	}

	// Make API request using go-jira library
	issues, searchResult, err := globalAPI.SearchIssues(ctx, jqlQuery, searchOpts)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to search JIRA issues: %w", err)
	}

	// Convert jira.Issue to our JiraIssue format
	// Keep empty search results as [] rather than null for the MCP array schema.
	jiraIssues := make([]JiraIssue, 0, len(issues))
	for _, issue := range issues {
		jiraIssue := JiraIssue{
			Key:            issue.Key,
			ViewURI:        fmt.Sprintf("%s/browse/%s", jiraBaseURL, issue.Key),
			FixVersions:    extractFixVersions(&issue),
			TargetVersions: extractTargetVersions(&issue),
		}

		if len(jiraIssue.TargetVersions) > 0 {
			jiraIssue.TargetRelease = strings.Join(jiraIssue.TargetVersions, ", ")
		}

		if issue.Fields != nil {
			jiraIssue.Summary = issue.Fields.Summary

			if issue.Fields.Status != nil {
				jiraIssue.Status = issue.Fields.Status.Name
			}

			if issue.Fields.Priority != nil {
				jiraIssue.Priority = issue.Fields.Priority.Name
			}
		}

		jiraIssues = append(jiraIssues, jiraIssue)
	}

	logging.Debugf("[search_issues] query=%q found %d issues", params.Query, len(jiraIssues))

	result := &SearchIssuesResult{
		Issues:        jiraIssues,
		NextPageToken: searchResult.NextPageToken,
		IsLast:        searchResult.IsLast,
		Count:         len(jiraIssues),
		Query:         jqlQuery,
	}

	// Build a detailed response with links
	var responseText string
	if len(jiraIssues) == 0 {
		responseText = fmt.Sprintf("No JIRA issues found for query: %s", params.Query)
	} else {
		responseText = fmt.Sprintf("Found %d JIRA issues on this page for query: %s\n\n", len(jiraIssues), params.Query)
		for i, issue := range jiraIssues {
			detailsLine := fmt.Sprintf("   Status: %s | Priority: %s", issue.Status, issue.Priority)
			if len(issue.TargetVersions) > 0 {
				detailsLine += fmt.Sprintf(" | Target: %s", strings.Join(issue.TargetVersions, ", "))
			}
			if len(issue.FixVersions) > 0 {
				detailsLine += fmt.Sprintf(" | Fix: %s", strings.Join(issue.FixVersions, ", "))
			}
			responseText += fmt.Sprintf("%d. **%s** - %s\n%s\n   Link: %s\n\n",
				i+1, issue.Key, issue.Summary, detailsLine, issue.ViewURI)
		}
	}

	return &mcp.CallToolResult{
		Content: []mcp.Content{
			&mcp.TextContent{
				Text: responseText,
			},
		},
	}, result, nil
}

// Get Issue tool parameters
type GetIssueParams struct {
	IssueKey string `json:"issue_key" jsonschema:"Jira issue key in PROJECT-NUMBER form, e.g. OCPBUGS-55179"`
}

// JIRA Issue details
type JiraIssueDetails struct {
	Key             string   `json:"key"`
	Summary         string   `json:"summary"`
	Description     string   `json:"description"`
	Status          string   `json:"status"`
	Priority        string   `json:"priority"`
	Created         string   `json:"created"`
	Updated         string   `json:"updated"`
	ViewURI         string   `json:"view_uri"`
	TargetRelease   string   `json:"target_release,omitempty"`
	TargetVersions  []string `json:"target_versions"`
	FixVersions     []string `json:"fix_versions"`
	FixVersion      string   `json:"fix_version,omitempty"`
	AffectsVersions []string `json:"affects_versions,omitempty"`
}

// Get JIRA Issue by key
func getIssue(ctx context.Context, req *mcp.CallToolRequest, params GetIssueParams) (*mcp.CallToolResult, *JiraIssueDetails, error) {
	if globalAPI == nil {
		return nil, nil, fmt.Errorf("JIRA API client not initialized")
	}

	// Make API request using go-jira library
	issue, err := globalAPI.GetIssue(ctx, params.IssueKey)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to get JIRA issue: %w", err)
	}

	// Parse response
	details := &JiraIssueDetails{
		Key:            issue.Key,
		ViewURI:        fmt.Sprintf("%s/browse/%s", jiraBaseURL, params.IssueKey),
		FixVersions:    extractFixVersions(issue),
		TargetVersions: extractTargetVersions(issue),
	}

	if len(details.TargetVersions) > 0 {
		details.TargetRelease = strings.Join(details.TargetVersions, ", ")
	}
	if len(details.FixVersions) > 0 {
		details.FixVersion = strings.Join(details.FixVersions, ", ")
	}

	affects := extractAffectsVersions(issue)
	if len(affects) > 0 {
		details.AffectsVersions = affects
	}

	if issue.Fields != nil {
		details.Summary = issue.Fields.Summary
		details.Description = jiraToMarkdown(issue.Fields.Description)

		if issue.Fields.Status != nil {
			details.Status = issue.Fields.Status.Name
		}

		if issue.Fields.Priority != nil {
			details.Priority = issue.Fields.Priority.Name
		}

		// Time is a custom type that wraps time.Time, convert it for display
		details.Created = time.Time(issue.Fields.Created).Format("2006-01-02 15:04:05")
		details.Updated = time.Time(issue.Fields.Updated).Format("2006-01-02 15:04:05")
	}

	// Build detailed response with link
	responseText := fmt.Sprintf("**%s: %s**\n\nLink: %s\n\n", details.Key, details.Summary, details.ViewURI)
	responseText += fmt.Sprintf("**Status:** %s\n**Priority:** %s\n", details.Status, details.Priority)
	if len(details.TargetVersions) > 0 {
		responseText += fmt.Sprintf("**Target Version(s):** %s\n", strings.Join(details.TargetVersions, ", "))
	}
	if len(details.FixVersions) > 0 {
		responseText += fmt.Sprintf("**Fix Version(s):** %s\n", strings.Join(details.FixVersions, ", "))
	}
	if len(details.AffectsVersions) > 0 {
		responseText += fmt.Sprintf("**Affects Version(s):** %s\n", strings.Join(details.AffectsVersions, ", "))
	}
	responseText += "\n"
	if details.Created != "" {
		responseText += fmt.Sprintf("**Created:** %s\n", details.Created)
	}
	if details.Updated != "" {
		responseText += fmt.Sprintf("**Updated:** %s\n\n", details.Updated)
	}
	if details.Description != "" {
		responseText += fmt.Sprintf("**Description:**\n%s\n", details.Description)
	}

	return &mcp.CallToolResult{
		Content: []mcp.Content{
			&mcp.TextContent{
				Text: responseText,
			},
		},
	}, details, nil
}

// NewRedHatIssuesServer creates a new MCP server with Red Hat JIRA tools
func NewRedHatIssuesServer() (*mcp.Server, error) {
	// Initialize the global API client
	if err := initGlobalAPI(); err != nil {
		return nil, fmt.Errorf("failed to initialize JIRA API client: %w", err)
	}

	// Create MCP server
	server := mcp.NewServer(&mcp.Implementation{
		Name:    "RedHat JIRA Issues API",
		Version: "1.0.0",
	}, nil)

	// Add search issues tool
	mcp.AddTool(server, &mcp.Tool{
		Name: "search_issues",
		Description: "Search Red Hat Jira Cloud issues (https://redhat.atlassian.net), covering engineering bugs, features, and escalations in projects such as OCPBUGS, OCPENG, and OCPTRL.\n\n" +
			"Input handling: query_mode defaults to auto, detecting leading JQL field/operator clauses (including project=, status IN, summary~, and custom fields) or ORDER BY; other input is wrapped in a `text ~ \"...\"` full-text search across summary, description, comments, and environment. Boolean words alone do not imply JQL. Set query_mode to jql for unchanged JQL or text for literal search terms when the input is ambiguous.\n\n" +
			"Examples: `project = OCPBUGS AND text ~ \"GPU passthrough\"` (JQL) or `ovnkube crashloop after node reboot` (plain text).\n\n" +
			"Returns one page of issues with key, summary, status, priority, target versions, fix versions, and a browse URL. Pagination is token-based: pass `next_page_token` from the response with the same query and query_mode to fetch more; `is_last` marks the end. No exact total is available from the API. Typical page sizes fit well under the default of 50.\n\n" +
			"Use get_issue to retrieve the full description of a result. For Red Hat knowledge-base articles, use search_kcs instead.",
		Annotations: &mcp.ToolAnnotations{
			Title:           "Search Red Hat Jira issues",
			ReadOnlyHint:    true,
			DestructiveHint: boolPtr(false),
			IdempotentHint:  true,
			OpenWorldHint:   boolPtr(true),
		},
	}, searchIssues)

	// Add get issue tool
	mcp.AddTool(server, &mcp.Tool{
		Name: "get_issue",
		Description: "Retrieve the full record of one Red Hat Jira Cloud issue by its key, e.g. OCPBUGS-55179 or OCPENG-1234.\n\n" +
			"Use when the key is already known — from search_issues results, a KCS article cross-reference, or the user. If you only have a topic or symptom, call search_issues first instead of guessing keys.\n\n" +
			"Returns key, summary, full description text, status, priority, target release/versions, fix versions, created/updated timestamps, and the https://redhat.atlassian.net/browse/{key} URL.",
		Annotations: &mcp.ToolAnnotations{
			Title:           "Get Red Hat Jira issue details",
			ReadOnlyHint:    true,
			DestructiveHint: boolPtr(false),
			IdempotentHint:  true,
			OpenWorldHint:   boolPtr(true),
		},
	}, getIssue)

	return server, nil
}

// boolPtr is a small helper for the pointer-valued tool annotation fields.
func boolPtr(b bool) *bool { return &b }
