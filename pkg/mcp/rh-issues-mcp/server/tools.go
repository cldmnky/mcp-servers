package server

import (
	"context"
	"fmt"
	"log"
	"strings"
	"time"

	jira "github.com/andygrunwald/go-jira/v2/cloud"
	"github.com/modelcontextprotocol/go-sdk/mcp"
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
	Query         string `json:"query" jsonschema:"Search query - can be either JQL (JIRA Query Language) or natural language text. For natural language, the tool will automatically construct an appropriate JQL query."`
	MaxResults    int    `json:"max_results,omitempty" jsonschema:"Maximum number of results to return (default: 50)"`
	NextPageToken string `json:"next_page_token,omitempty" jsonschema:"Continuation token from a previous search response; omit for the first page"`
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

	// Build a comprehensive search that looks in key fields
	jql := fmt.Sprintf(`text ~ "%s"`, escapedQuery)

	log.Printf("[buildJQLQuery] Converted natural language query %q to JQL: %q", query, jql)
	return jql
}

// Search JIRA Issues
func searchIssues(ctx context.Context, req *mcp.CallToolRequest, params SearchIssuesParams) (*mcp.CallToolResult, *SearchIssuesResult, error) {
	log.Printf("[search_issues] Starting search with query: %q, maxResults: %d", params.Query, params.MaxResults)
	if params.MaxResults < 0 {
		return nil, nil, fmt.Errorf("max_results must be positive")
	}

	if globalAPI == nil {
		log.Println("[search_issues] ERROR: JIRA API client not initialized")
		return nil, nil, fmt.Errorf("JIRA API client not initialized")
	}

	// Set defaults
	if params.MaxResults == 0 {
		params.MaxResults = 50
	}

	// Convert query to JQL if needed
	jqlQuery := buildJQLQuery(params.Query)
	log.Printf("[search_issues] Using JQL query: %q", jqlQuery)

	// Build search options
	searchOpts := &jira.SearchOptionsV2{
		MaxResults:    params.MaxResults,
		NextPageToken: params.NextPageToken,
		Fields:        []string{"key", "summary", "status", "priority"},
	}

	// Make API request using go-jira library
	issues, searchResult, err := globalAPI.SearchIssues(ctx, jqlQuery, searchOpts)
	if err != nil {
		log.Printf("[search_issues] ERROR: API request failed for query %q: %v", params.Query, err)
		return nil, nil, fmt.Errorf("failed to search JIRA issues: %w", err)
	}

	log.Printf("[search_issues] Query %q completed successfully, found %d issues", params.Query, len(issues))

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

	log.Printf("[search_issues] Successfully parsed %d issues", len(jiraIssues))

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
	IssueKey string `json:"issue_key" jsonschema:"The JIRA issue key (e.g., OCPBUGS-12345)"`
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
	log.Printf("[get_issue] Getting issue with key: %s", params.IssueKey)

	if globalAPI == nil {
		log.Println("[get_issue] ERROR: JIRA API client not initialized")
		return nil, nil, fmt.Errorf("JIRA API client not initialized")
	}

	log.Printf("[get_issue] Executing query for issue key: %s", params.IssueKey)

	// Make API request using go-jira library
	issue, err := globalAPI.GetIssue(ctx, params.IssueKey)
	if err != nil {
		log.Printf("[get_issue] ERROR: API request failed for issue %s: %v", params.IssueKey, err)
		return nil, nil, fmt.Errorf("failed to get JIRA issue: %w", err)
	}

	log.Printf("[get_issue] Query for issue %s completed successfully", params.IssueKey)

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

	log.Printf("[get_issue] Successfully retrieved issue: %s", details.Key) // Build detailed response with link
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
	log.Println("[server] Initializing Red Hat JIRA Issues MCP server")

	// Initialize the global API client
	if err := initGlobalAPI(); err != nil {
		log.Printf("[server] ERROR: Failed to initialize API client: %v", err)
		return nil, fmt.Errorf("failed to initialize JIRA API client: %w", err)
	}
	log.Println("[server] JIRA API client initialized successfully")

	// Create MCP server
	server := mcp.NewServer(&mcp.Implementation{
		Name:    "RedHat JIRA Issues API",
		Version: "1.0.0",
	}, nil)

	log.Println("[server] Registering tools: search_issues, get_issue")

	// Add search issues tool
	mcp.AddTool(server, &mcp.Tool{
		Name:        "search_issues",
		Description: "Search Red Hat JIRA issues using either natural language or JQL (JIRA Query Language). You can provide simple text like 'GPU passthrough errors' or 'KubeVirt networking issues', and the tool will automatically convert it to proper JQL. Alternatively, you can use explicit JQL queries like 'project = OCPBUGS AND summary ~ \"DNS\"' or 'key = OCPBUGS-55179'. Returns issue keys, summaries, status, and priority. Requires RH_JIRA_EMAIL and RH_JIRA_TOKEN to be set. Use next_page_token from the response to fetch the next page.",
	}, searchIssues)

	// Add get issue tool
	mcp.AddTool(server, &mcp.Tool{
		Name:        "get_issue",
		Description: "Retrieve the full details of a specific Red Hat JIRA issue by its key (e.g., OCPBUGS-55179). Returns structured information including Key, Summary, Description, Status, Priority, Created date, and Updated date. Use search_issues first if you need to find issue keys, or use this directly if you already know the issue key from a KCS article or other source.",
	}, getIssue)

	log.Println("[server] Red Hat JIRA Issues MCP server created successfully")
	return server, nil
}
