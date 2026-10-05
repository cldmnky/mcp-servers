package server

import (
	"context"
	"fmt"
	"strings"
	"time"

	jira "github.com/andygrunwald/go-jira/v2/cloud"
	"github.com/modelcontextprotocol/go-sdk/mcp"

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
	Query         string `json:"query" jsonschema:"Search terms or a JQL expression. JQL is detected and passed through when it contains operators like 'project =' or '~'; free text is wrapped in a full-text search. Examples: 'project = OCPBUGS AND text ~ \"GPU passthrough\"' or 'ovnkube crashloop'"`
	MaxResults    int    `json:"max_results,omitempty" jsonschema:"Page size, 1-100; defaults to 50. Keep modest to limit response size"`
	NextPageToken string `json:"next_page_token,omitempty" jsonschema:"Continuation token returned by a previous search_issues call. Omit on the first page and reuse the exact same query when paginating"`
}

// JIRA Issue response
type JiraIssue struct {
	Key      string `json:"key"`
	Summary  string `json:"summary"`
	Status   string `json:"status"`
	Priority string `json:"priority"`
	ViewURI  string `json:"view_uri"`
}

// Search Issues result wrapper
type SearchIssuesResult struct {
	Issues        []JiraIssue `json:"issues" jsonschema:"List of JIRA issues found"`
	NextPageToken string      `json:"next_page_token,omitempty" jsonschema:"Continuation token for the next page"`
	IsLast        bool        `json:"is_last" jsonschema:"Whether this is the last page of results"`
	Count         int         `json:"count" jsonschema:"Number of issues returned in this response"`
	Query         string      `json:"query" jsonschema:"The JQL query used"`
}

// isJQLQuery attempts to detect if a query is already in JQL format
func isJQLQuery(query string) bool {
	// Check for common JQL operators and syntax
	jqlIndicators := []string{
		" AND ", " OR ", " NOT ",
		"project =", "project IN",
		"summary ~", "description ~", "text ~",
		"status =", "status IN",
		"priority =", "priority IN",
		"assignee =", "reporter =",
		"created >=", "updated >=",
		"key =", "key IN",
	}

	queryUpper := strings.ToUpper(query)
	for _, indicator := range jqlIndicators {
		if strings.Contains(queryUpper, strings.ToUpper(indicator)) {
			return true
		}
	}

	return false
}

// buildJQLQuery converts a natural language query to JQL
func buildJQLQuery(query string) string {
	// If it already looks like JQL, return as-is
	if isJQLQuery(query) {
		return query
	}

	// Otherwise, build a text search query that searches across multiple fields
	// Using text ~ "query" searches summary, description, comments, and environment
	escapedQuery := strings.ReplaceAll(query, `"`, `\"`)

	jql := fmt.Sprintf(`text ~ "%s"`, escapedQuery)

	logging.Debugf("[buildJQLQuery] converted natural language query %q to JQL: %q", query, jql)
	return jql
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

	// Convert query to JQL if needed
	jqlQuery := buildJQLQuery(params.Query)

	// Build search options
	searchOpts := &jira.SearchOptionsV2{
		MaxResults:    params.MaxResults,
		NextPageToken: params.NextPageToken,
		Fields:        []string{"key", "summary", "status", "priority"},
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
			Key:     issue.Key,
			ViewURI: fmt.Sprintf("%s/browse/%s", jiraBaseURL, issue.Key),
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
			responseText += fmt.Sprintf("%d. **%s** - %s\n   Status: %s | Priority: %s\n   Link: %s\n\n",
				i+1, issue.Key, issue.Summary, issue.Status, issue.Priority, issue.ViewURI)
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
	Key         string `json:"key"`
	Summary     string `json:"summary"`
	Description string `json:"description"`
	Status      string `json:"status"`
	Priority    string `json:"priority"`
	Created     string `json:"created"`
	Updated     string `json:"updated"`
	ViewURI     string `json:"view_uri"`
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
		Key:     issue.Key,
		ViewURI: fmt.Sprintf("%s/browse/%s", jiraBaseURL, params.IssueKey),
	}

	if issue.Fields != nil {
		details.Summary = issue.Fields.Summary
		details.Description = issue.Fields.Description

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
	responseText += fmt.Sprintf("**Status:** %s\n**Priority:** %s\n\n", details.Status, details.Priority)
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
			"Input handling: if the query contains JQL operators (project =, status IN, ~, AND, OR), it is sent as-is; anything else is wrapped in a `text ~ \"...\"` full-text search across summary, description, comments, and environment. Prefer explicit JQL to filter precisely; use plain text only for broad topical searches.\n\n" +
			"Examples: `project = OCPBUGS AND text ~ \"GPU passthrough\"` (JQL) or `ovnkube crashloop after node reboot` (plain text).\n\n" +
			"Returns one page of issues with key, summary, status, priority, and a browse URL. Pagination is token-based: pass `next_page_token` from the response with the same query to fetch more; `is_last` marks the end. No exact total is available from the API. Typical page sizes fit well under the default of 50.\n\n" +
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
			"Returns key, summary, full description text, status, priority, created/updated timestamps, and the https://redhat.atlassian.net/browse/{key} URL.",
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
