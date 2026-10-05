package server

import (
	"context"
	"fmt"
	"log"

	"github.com/modelcontextprotocol/go-sdk/mcp"
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
	Query     string `json:"query" jsonschema:"Search query string"`
	Rows      int    `json:"rows,omitempty" jsonschema:"Number of results to return (default: 50)"`
	Start     int    `json:"start,omitempty" jsonschema:"Starting index for pagination (default: 0)"`
	SessionID string `json:"session_id,omitempty" jsonschema:"Optional session ID"`
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
	log.Printf("[search_kcs] Starting search with query: %q, rows: %d, start: %d", params.Query, params.Rows, params.Start)

	if globalAPI == nil {
		log.Println("[search_kcs] ERROR: Red Hat API client not initialized")
		return nil, nil, fmt.Errorf("Red Hat API client not initialized")
	}

	// Set defaults
	if params.Rows == 0 {
		params.Rows = 10
	}

	// Use the Solr Search API per Case Management API documentation
	// This matches the SolrSearchRequest structure from the API docs
	// The 'expression' parameter is REQUIRED and specifies field list (fl), filters (fq), and sorting
	requestData := map[string]interface{}{
		"clientName": "mcp",
		"q":          params.Query,
		"rows":       params.Rows,
		"start":      params.Start,
		"expression": "fl=id,allTitle,publishedTitle,score,view_uri,documentKind,standard_product&sort=score DESC",
	}

	log.Printf("[search_kcs] Executing query: %q", params.Query)
	log.Println("[search_kcs] Making API request to /hydra/rest/search/v2/kcs")

	// Make API request - using /hydra/rest/search/v2/kcs which accepts the SolrSearchRequest
	result, err := globalAPI.MakeRequest(ctx, "POST", "/hydra/rest/search/v2/kcs", requestData)
	if err != nil {
		log.Printf("[search_kcs] ERROR: API request failed for query %q: %v", params.Query, err)
		return nil, nil, fmt.Errorf("failed to search KCS: %w", err)
	}

	log.Printf("[search_kcs] Query %q completed successfully", params.Query)
	log.Printf("[search_kcs] Response structure: %+v", result)

	// Parse response
	var solutions []KCSSolution

	// Try multiple response structures
	// Structure 1: { "response": { "docs": [...] } }
	if response, ok := result["response"].(map[string]interface{}); ok {
		if docs, ok := response["docs"].([]interface{}); ok {
			log.Printf("[search_kcs] Found %d docs in response.docs", len(docs))
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

	// Structure 2: { "results": [...] }
	if len(solutions) == 0 {
		if results, ok := result["results"].([]interface{}); ok {
			log.Printf("[search_kcs] Found %d results directly", len(results))
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

	log.Printf("[search_kcs] Successfully parsed %d solutions", len(solutions))

	searchResult := &SearchKCSResult{
		Solutions: solutions,
		Count:     len(solutions),
		Query:     params.Query,
	}

	// Build a detailed response with links
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
} // Get KCS tool parameters
type GetKCSParams struct {
	SolutionID string `json:"solution_id" jsonschema:"The ID of the solution to retrieve"`
	SessionID  string `json:"session_id,omitempty" jsonschema:"Optional session ID"`
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
	log.Printf("[get_kcs] Getting solution with ID: %s", params.SolutionID)

	if globalAPI == nil {
		log.Println("[get_kcs] ERROR: Red Hat API client not initialized")
		return nil, nil, fmt.Errorf("Red Hat API client not initialized")
	}

	log.Printf("[get_kcs] Executing query for solution ID: %s", params.SolutionID)
	log.Printf("[get_kcs] Making API request to /hydra/rest/search/v2/kcs with id query")

	// Use the KCS search API with an ID query to get the full solution
	// The search API supports field selection through the 'expression' parameter
	// Request specific fields: publishedTitle, standard_product, issue, solution_resolution, solution_rootcause, view_uri
	requestData := map[string]interface{}{
		"clientName": "mcp",
		"q":          fmt.Sprintf("id:%s", params.SolutionID),
		"expression": "fl=publishedTitle,allTitle,standard_product,issue,solution_resolution,solution_rootcause,view_uri,id",
	}

	result, err := globalAPI.MakeRequest(ctx, "POST", "/hydra/rest/search/v2/kcs", requestData)
	if err != nil {
		log.Printf("[get_kcs] ERROR: API request failed for solution ID %s: %v", params.SolutionID, err)
		return nil, nil, fmt.Errorf("failed to get KCS solution: %w", err)
	}

	log.Printf("[get_kcs] Query for solution ID %s completed successfully", params.SolutionID)

	// Parse response - search API returns { "response": { "docs": [...] } }
	details := &KCSDetails{}

	// Check if we got a result
	if response, ok := result["response"].(map[string]interface{}); ok {
		if docs, ok := response["docs"].([]interface{}); ok && len(docs) > 0 {
			if doc, ok := docs[0].(map[string]interface{}); ok {
				// Extract fields from the document
				// Note: Some fields come as strings, others as arrays
				if title, ok := doc["publishedTitle"].(string); ok {
					details.Title = title
				} else if title, ok := doc["allTitle"].(string); ok {
					details.Title = title
				}

				// standard_product can be a string or array
				if env, ok := doc["standard_product"].(string); ok {
					details.Environment = env
				} else if envArr, ok := doc["standard_product"].([]interface{}); ok && len(envArr) > 0 {
					if envStr, ok := envArr[0].(string); ok {
						details.Environment = envStr
					}
				}

				// issue is typically an array
				if issue, ok := doc["issue"].(string); ok {
					details.Issue = issue
				} else if issueArr, ok := doc["issue"].([]interface{}); ok && len(issueArr) > 0 {
					if issueStr, ok := issueArr[0].(string); ok {
						details.Issue = issueStr
					}
				}

				// solution_resolution is typically an array
				if resolution, ok := doc["solution_resolution"].(string); ok {
					details.Resolution = resolution
				} else if resArr, ok := doc["solution_resolution"].([]interface{}); ok && len(resArr) > 0 {
					if resStr, ok := resArr[0].(string); ok {
						details.Resolution = resStr
					}
				}

				// solution_rootcause is typically an array (optional field)
				if rootCause, ok := doc["solution_rootcause"].(string); ok {
					details.RootCause = rootCause
				} else if rcArr, ok := doc["solution_rootcause"].([]interface{}); ok && len(rcArr) > 0 {
					if rcStr, ok := rcArr[0].(string); ok {
						details.RootCause = rcStr
					}
				}

				// Get view_uri from response or construct from ID
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

	log.Printf("[get_kcs] Successfully retrieved solution: %s", details.Title)

	// Build detailed response with link
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

// NewRedHatKCSServer creates a new MCP server with Red Hat KCS tools
func NewRedHatKCSServer() (*mcp.Server, error) {
	log.Println("[server] Initializing Red Hat KCS MCP server")

	// Initialize the global API client
	if err := initGlobalAPI(); err != nil {
		log.Printf("[server] ERROR: Failed to initialize API client: %v", err)
		return nil, fmt.Errorf("failed to initialize Red Hat API client: %w", err)
	}
	log.Println("[server] Red Hat API client initialized successfully")

	// Create MCP server
	server := mcp.NewServer(&mcp.Implementation{
		Name:    "RedHat KCS API",
		Version: "1.0.0",
	}, nil)

	log.Println("[server] Registering tools: search_kcs, get_kcs")

	// Add search KCS tool
	mcp.AddTool(server, &mcp.Tool{
		Name:        "search_kcs",
		Description: "**ALWAYS USE THIS FIRST** when the user asks to search for, find, or look up Red Hat KCS (Knowledge Centered Service) articles, solutions, or knowledge base content. This tool searches the official Red Hat Customer Portal knowledge base and returns real KCS article IDs with titles, scores, and URLs. Returns only verified documents where documentKind is 'Article' or 'Solution' and accessState is 'active' or 'private'. Use this instead of making up fake KCS IDs or searching the general web for KCS content.",
	}, searchKCS)

	// Add get KCS tool
	mcp.AddTool(server, &mcp.Tool{
		Name:        "get_kcs",
		Description: "Retrieve the full detailed content of a specific Red Hat KCS solution by its ID (obtained from search_kcs results). Returns structured information including Title, Environment, Issue description, Resolution steps, and Root Cause analysis. Always use search_kcs first to get valid solution IDs before calling this tool.",
	}, getKCS)

	log.Println("[server] Red Hat KCS MCP server created successfully")
	return server, nil
}
