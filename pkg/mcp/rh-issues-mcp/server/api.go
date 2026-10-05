package server

import (
	"context"
	"fmt"
	"log"
	"os"
	"time"

	jira "github.com/andygrunwald/go-jira/v2/cloud"
)

const jiraBaseURL = "https://redhat.atlassian.net"

// JiraAPI represents the Red Hat JIRA API client
type JiraAPI struct {
	client *jira.Client
}

// NewJiraAPI creates a new Red Hat JIRA API client using go-jira library
func NewJiraAPI() (*JiraAPI, error) {
	accessToken := os.Getenv("RH_JIRA_TOKEN")
	if accessToken == "" {
		return nil, fmt.Errorf("RH_JIRA_TOKEN environment variable is required")
	}

	email := os.Getenv("RH_JIRA_EMAIL")
	if email == "" {
		return nil, fmt.Errorf("RH_JIRA_EMAIL environment variable is required")
	}

	// Jira Cloud uses the Atlassian account email and API token, not a legacy PAT.
	tp := jira.BasicAuthTransport{Username: email, APIToken: accessToken}
	httpClient := tp.Client()
	httpClient.Timeout = 30 * time.Second
	client, err := jira.NewClient(jiraBaseURL, httpClient)
	if err != nil {
		return nil, fmt.Errorf("failed to create JIRA client: %w", err)
	}

	return &JiraAPI{
		client: client,
	}, nil
}

// SearchIssues searches for JIRA issues using JQL
func (j *JiraAPI) SearchIssues(ctx context.Context, jql string, opts *jira.SearchOptionsV2) ([]jira.Issue, *jira.Response, error) {
	log.Printf("[api] Searching issues with JQL: %q", jql)

	issues, resp, err := j.client.Issue.SearchV2JQL(ctx, jql, opts)
	if err != nil {
		log.Printf("[api] ERROR: Search failed: %v", err)
		return nil, nil, fmt.Errorf("JIRA search failed: %w", err)
	}

	log.Printf("[api] Search completed successfully, found %d issues (last page: %t)", len(issues), resp.IsLast)
	return issues, resp, nil
}

// GetIssue retrieves a single JIRA issue by key
func (j *JiraAPI) GetIssue(ctx context.Context, issueKey string) (*jira.Issue, error) {
	log.Printf("[api] Getting issue: %s", issueKey)

	issue, _, err := j.client.Issue.Get(ctx, issueKey, nil)
	if err != nil {
		log.Printf("[api] ERROR: Failed to get issue %s: %v", issueKey, err)
		return nil, fmt.Errorf("failed to get JIRA issue: %w", err)
	}

	log.Printf("[api] Successfully retrieved issue: %s", issueKey)
	return issue, nil
}
