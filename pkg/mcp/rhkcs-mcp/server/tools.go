package server

import (
	"context"
	"fmt"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/cldmnky/mcp-servers/internal/logging"
)

// Global API client instance
var globalAPI *RedHatAPI

// Initialize the global API client
func initGlobalAPI() error {
	var err error
	globalAPI, err = NewRedHatAPI()
	return err
}

// Search KCS tool parameters
type SearchKCSParams struct {
	Query string `json:"query" jsonschema:"Search terms for the Red Hat knowledge base; Solr-style keywords and quoted phrases, e.g. 'ovnkube crashloop' or '\"etcd leader election\" timeout'"`
	Rows  int    `json:"rows,omitempty" jsonschema:"Page size; defaults to 10"`
	Start int    `json:"start,omitempty" jsonschema:"Offset of the first result for pagination; defaults to 0"`
	// SessionID is accepted for backwards compatibility with clients
	// configured against older versions of this server; it is ignored.
	SessionID string `json:"session_id,omitempty" jsonschema:"Deprecated: accepted for backwards compatibility and ignored"`
}

// KCS Solution response
type KCSSolution struct {
	ID      string  `json:"id"`
	Title   string  `json:"title"`
	Score   float64 `json:"score"`
	ViewURI string  `json:"view_uri"`
}

// Search KCS result wrapper
type SearchKCSResult struct {
	Solutions []KCSSolution `json:"solutions" jsonschema:"List of KCS solutions found"`
	Count     int           `json:"count" jsonschema:"Number of solutions returned"`
	Query     string        `json:"query" jsonschema:"The search query used"`
}

// Search KCS Solutions
func searchKCS(ctx context.Context, req *mcp.CallToolRequest, params SearchKCSParams) (*mcp.CallToolResult, *SearchKCSResult, error) {
	if params.Rows < 0 {
		return nil, nil, fmt.Errorf("rows must be positive")
	}
	if params.Start < 0 {
		return nil, nil, fmt.Errorf("start must be positive")
	}

	if globalAPI == nil {
		return nil, nil, fmt.Errorf("Red Hat API client not initialized")
	}

	// Set defaults
	if params.Rows == 0 {
		params.Rows = 10
	}

	// Use the Solr Search API per Case Management API documentation.
	// The 'expression' parameter is REQUIRED and specifies field list (fl),
	// filters (fq), and sorting.
	requestData := map[string]interface{}{
		"clientName": "mcp",
		"q":          params.Query,
		"rows":       params.Rows,
		"start":      params.Start,
		"expression": "fl=id,allTitle,publishedTitle,score,view_uri,documentKind,standard_product&sort=score DESC",
	}

	logging.Debugf("[search_kcs] query=%q rows=%d start=%d", params.Query, params.Rows, params.Start)

	result, err := globalAPI.MakeRequest(ctx, "POST", "/hydra/rest/search/v2/kcs", requestData)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to search KCS: %w", err)
	}

	solutions := parseKCSSolutions(result)
	logging.Debugf("[search_kcs] query=%q parsed %d solutions", params.Query, len(solutions))

	searchResult := &SearchKCSResult{
		Solutions: solutions,
		Count:     len(solutions),
		Query:     params.Query,
	}

	var responseText string
	if len(solutions) == 0 {
		responseText = fmt.Sprintf("No KCS solutions found for query: %s", params.Query)
	} else {
		responseText = fmt.Sprintf("Found %d KCS solutions for query: %s\n\n", len(solutions), params.Query)
		for i, sol := range solutions {
			responseText += fmt.Sprintf("%d. **%s** (ID: %s, Score: %.2f)\n   Link: %s\n\n",
				i+1, sol.Title, sol.ID, sol.Score, sol.ViewURI)
		}
	}

	return &mcp.CallToolResult{
		Content: []mcp.Content{
			&mcp.TextContent{
				Text: responseText,
			},
		},
	}, searchResult, nil
}

// parseKCSSolutions extracts solutions from either of the two response
// shapes returned by the search API: {"response": {"docs": [...]}} or
// {"results": [...]}.
func parseKCSSolutions(result map[string]interface{}) []KCSSolution {
	var solutions []KCSSolution

	if response, ok := result["response"].(map[string]interface{}); ok {
		if docs, ok := response["docs"].([]interface{}); ok {
			for _, doc := range docs {
				if docMap, ok := doc.(map[string]interface{}); ok {
					solution := KCSSolution{}
					if id, ok := docMap["id"].(string); ok {
						solution.ID = id
					}
					if title, ok := docMap["allTitle"].(string); ok {
						solution.Title = title
					} else if title, ok := docMap["title"].(string); ok {
						solution.Title = title
					}
					if score, ok := docMap["score"].(float64); ok {
						solution.Score = score
					}
					if viewURI, ok := docMap["view_uri"].(string); ok {
						solution.ViewURI = viewURI
					} else if uri, ok := docMap["uri"].(string); ok {
						solution.ViewURI = "https://access.redhat.com" + uri
					} else if solution.ID != "" {
						solution.ViewURI = fmt.Sprintf("https://access.redhat.com/solutions/%s", solution.ID)
					}

					if solution.ID != "" && solution.Title != "" {
						solutions = append(solutions, solution)
					}
				}
			}
		}
	}

	if len(solutions) == 0 {
		if results, ok := result["results"].([]interface{}); ok {
			for _, doc := range results {
				if docMap, ok := doc.(map[string]interface{}); ok {
					solution := KCSSolution{}
					if id, ok := docMap["id"].(string); ok {
						solution.ID = id
					} else if id, ok := docMap["documentId"].(string); ok {
						solution.ID = id
					}
					if title, ok := docMap["title"].(string); ok {
						solution.Title = title
					}
					if score, ok := docMap["score"].(float64); ok {
						solution.Score = score
					}
					if uri, ok := docMap["uri"].(string); ok {
						solution.ViewURI = "https://access.redhat.com" + uri
					} else if solution.ID != "" {
						solution.ViewURI = fmt.Sprintf("https://access.redhat.com/solutions/%s", solution.ID)
					}

					if solution.ID != "" && solution.Title != "" {
						solutions = append(solutions, solution)
					}
				}
			}
		}
	}

	return solutions
}

// Get KCS tool parameters
type GetKCSParams struct {
	SolutionID string `json:"solution_id" jsonschema:"Numeric KCS solution ID from a search_kcs result, e.g. 7010411"`
	// SessionID is accepted for backwards compatibility with clients
	// configured against older versions of this server; it is ignored.
	SessionID string `json:"session_id,omitempty" jsonschema:"Deprecated: accepted for backwards compatibility and ignored"`
}

// KCS Solution details
type KCSDetails struct {
	Title       string `json:"title"`
	Environment string `json:"environment"`
	Issue       string `json:"issue"`
	Resolution  string `json:"resolution"`
	RootCause   string `json:"root_cause"`
	ViewURI     string `json:"view_uri"`
}

// Get KCS Solution by ID
func getKCS(ctx context.Context, req *mcp.CallToolRequest, params GetKCSParams) (*mcp.CallToolResult, *KCSDetails, error) {
	if globalAPI == nil {
		return nil, nil, fmt.Errorf("Red Hat API client not initialized")
	}

	// Use the KCS search API with an ID query to get the full solution.
	// The search API supports field selection through the 'expression' parameter.
	requestData := map[string]interface{}{
		"clientName": "mcp",
		"q":          fmt.Sprintf("id:%s", params.SolutionID),
		"expression": "fl=publishedTitle,allTitle,standard_product,issue,solution_resolution,solution_rootcause,view_uri,id",
	}

	logging.Debugf("[get_kcs] solution ID: %s", params.SolutionID)

	result, err := globalAPI.MakeRequest(ctx, "POST", "/hydra/rest/search/v2/kcs", requestData)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to get KCS solution: %w", err)
	}

	details := &KCSDetails{}

	// The search API returns { "response": { "docs": [...] } }
	if response, ok := result["response"].(map[string]interface{}); ok {
		if docs, ok := response["docs"].([]interface{}); ok && len(docs) > 0 {
			if doc, ok := docs[0].(map[string]interface{}); ok {
				// Some fields come as strings, others as arrays.
				if title, ok := doc["publishedTitle"].(string); ok {
					details.Title = title
				} else if title, ok := doc["allTitle"].(string); ok {
					details.Title = title
				}
				details.Environment = firstString(doc["standard_product"])
				details.Issue = firstString(doc["issue"])
				details.Resolution = firstString(doc["solution_resolution"])
				details.RootCause = firstString(doc["solution_rootcause"])
				if viewURI, ok := doc["view_uri"].(string); ok {
					details.ViewURI = viewURI
				}
			}
		}
	}

	// Fallback: construct ViewURI if not found in response
	if details.ViewURI == "" {
		details.ViewURI = fmt.Sprintf("https://access.redhat.com/solutions/%s", params.SolutionID)
	}

	responseText := fmt.Sprintf("**%s**\n\nLink: %s\n\n", details.Title, details.ViewURI)
	if details.Environment != "" {
		responseText += fmt.Sprintf("**Environment:** %s\n\n", details.Environment)
	}
	if details.Issue != "" {
		responseText += fmt.Sprintf("**Issue:**\n%s\n\n", details.Issue)
	}
	if details.Resolution != "" {
		responseText += fmt.Sprintf("**Resolution:**\n%s\n\n", details.Resolution)
	}
	if details.RootCause != "" {
		responseText += fmt.Sprintf("**Root Cause:**\n%s\n", details.RootCause)
	}

	return &mcp.CallToolResult{
		Content: []mcp.Content{
			&mcp.TextContent{
				Text: responseText,
			},
		},
	}, details, nil
}

// firstString extracts the first value of a field that may arrive either as
// a plain string or as a list of strings.
func firstString(v interface{}) string {
	switch val := v.(type) {
	case string:
		return val
	case []interface{}:
		if len(val) > 0 {
			if s, ok := val[0].(string); ok {
				return s
			}
		}
	}
	return ""
}

// NewRedHatKCSServer creates a new MCP server with Red Hat KCS tools
func NewRedHatKCSServer() (*mcp.Server, error) {
	// Initialize the global API client
	if err := initGlobalAPI(); err != nil {
		return nil, fmt.Errorf("failed to initialize Red Hat API client: %w", err)
	}

	// Create MCP server
	server := mcp.NewServer(&mcp.Implementation{
		Name:    "RedHat KCS API",
		Version: "1.0.0",
	}, nil)

	// Add search KCS tool
	mcp.AddTool(server, &mcp.Tool{
		Name: "search_kcs",
		Description: "Search the official Red Hat Customer Portal knowledge base (KCS) for solutions and articles, and get their real numeric IDs, titles, relevance scores, and access.redhat.com URLs.\n\n" +
			"Use for Red Hat product troubleshooting, configuration how-tos, and error lookups (OpenShift, RHEL, Ansible, etc.). Use plain keywords or quoted phrases; simple topical queries work better than long natural-language sentences.\n\n" +
			"Results are the authoritative solution IDs — do not guess or fabricate IDs; pass them to get_kcs for the full article. For Red Hat Jira engineering issues, use search_issues instead.",
		Annotations: &mcp.ToolAnnotations{
			Title:           "Search Red Hat KCS solutions",
			ReadOnlyHint:    true,
			DestructiveHint: boolPtr(false),
			IdempotentHint:  true,
			OpenWorldHint:   boolPtr(true),
		},
	}, searchKCS)

	// Add get KCS tool
	mcp.AddTool(server, &mcp.Tool{
		Name: "get_kcs",
		Description: "Retrieve the full content of one Red Hat KCS solution by its numeric ID (e.g. 7010411), returning title, affected environment, issue description, resolution steps, root cause, and the access.redhat.com URL.\n\n" +
			"Use when the solution ID is known — from a search_kcs result, a Jira issue, or the user. Without an ID, call search_kcs first; arbitrary numbers will not resolve.",
		Annotations: &mcp.ToolAnnotations{
			Title:           "Get Red Hat KCS solution details",
			ReadOnlyHint:    true,
			DestructiveHint: boolPtr(false),
			IdempotentHint:  true,
			OpenWorldHint:   boolPtr(true),
		},
	}, getKCS)

	return server, nil
}

// boolPtr is a small helper for the pointer-valued tool annotation fields.
func boolPtr(b bool) *bool { return &b }
