package server

import (
	"context"
	"fmt"
	"regexp"
	"strings"

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
	Kind    string  `json:"kind" jsonschema:"Document kind: Solution or Article"`
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
	// filters (fq), and sorting. Restrict to Solution/Article documents:
	// other kinds (Documentation pages, Labs, Vulnerability entries,
	// container catalog rows) have non-numeric IDs that get_kcs cannot
	// fetch, and Documentation results also return one row per translation.
	requestData := map[string]interface{}{
		"clientName": "mcp",
		"q":          params.Query,
		"rows":       params.Rows,
		"start":      params.Start,
		"expression": "fl=id,allTitle,publishedTitle,score,view_uri,documentKind,standard_product&sort=score DESC&fq=documentKind:(Solution OR Article)",
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
					if kind, ok := docMap["documentKind"].(string); ok {
						solution.Kind = kind
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
	Kind        string `json:"kind,omitempty" jsonschema:"Document kind: Solution or Article"`
	Environment string `json:"environment"`
	Abstract    string `json:"abstract,omitempty" jsonschema:"Article abstract; Solutions usually have no abstract"`
	Issue       string `json:"issue"`
	Resolution  string `json:"resolution"`
	RootCause   string `json:"root_cause"`
	ViewURI     string `json:"view_uri"`
}

// numericID reports whether s is a numeric KCS solution ID (the only kind
// get_kcs can resolve; documentation URLs and catalog slugs are not valid).
var numericID = regexp.MustCompile(`^[0-9]+$`)

// Get KCS Solution by ID
func getKCS(ctx context.Context, req *mcp.CallToolRequest, params GetKCSParams) (*mcp.CallToolResult, *KCSDetails, error) {
	if !numericID.MatchString(params.SolutionID) {
		return nil, nil, fmt.Errorf("solution_id %q is not a numeric KCS solution ID (e.g. 7010411); documentation URLs and other identifiers cannot be fetched — use search_kcs to find numeric Solution or Article IDs", params.SolutionID)
	}

	if globalAPI == nil {
		return nil, nil, fmt.Errorf("Red Hat API client not initialized")
	}

	// Use the KCS search API with an ID query to get the full solution.
	// Solutions carry issue/resolution/root-cause fields; Articles carry an
	// abstract instead, so request both shapes.
	requestData := map[string]interface{}{
		"clientName": "mcp",
		"q":          fmt.Sprintf("id:%s", params.SolutionID),
		"expression": "fl=publishedTitle,allTitle,standard_product,issue,solution_resolution,solution_rootcause,view_uri,id,documentKind,abstract",
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
				// Some fields come as strings, others as arrays. Long text
				// fields arrive flattened to a single line; reflowMarkdown
				// restores the line structure of their block elements.
				if title, ok := doc["publishedTitle"].(string); ok {
					details.Title = title
				} else if title, ok := doc["allTitle"].(string); ok {
					details.Title = title
				}
				if kind, ok := doc["documentKind"].(string); ok {
					details.Kind = kind
				}
				details.Environment = firstString(doc["standard_product"])
				details.Abstract = firstString(doc["abstract"])
				details.Issue = reflowMarkdown(firstString(doc["issue"]))
				details.Resolution = reflowMarkdown(firstString(doc["solution_resolution"]))
				details.RootCause = reflowMarkdown(firstString(doc["solution_rootcause"]))
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
	if details.Abstract != "" {
		responseText += fmt.Sprintf("**Abstract:**\n%s\n\n", details.Abstract)
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

	// Some documents (e.g. draft or restricted Articles) have no body in the
	// search index. Say so explicitly instead of returning metadata only.
	if details.Abstract == "" && details.Issue == "" && details.Resolution == "" && details.RootCause == "" {
		responseText += "\n_No body content is available in the search index for this document (it may be a draft, restricted, or subscriber-only); consult the link above._\n"
	}

	// The index strips line breaks — and sometimes the spaces around them —
	// inside code fences, so fenced commands may be mangled. Warn against
	// executing them verbatim.
	if fencedCodePresent(details.Issue) || fencedCodePresent(details.Resolution) || fencedCodePresent(details.RootCause) {
		responseText += "\n_Note: the source index strips line breaks (and occasionally spaces) inside code blocks. Treat fenced commands as reference and reconstruct them before executing._\n"
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

// reflowMarkdown restores the line structure that the search API flattens
// out of stored text fields: the Solr index collapses newlines, so headings,
// list items, and code fences arrive glued into a single line.
//
// Prose is re-broken before Markdown block markers. Fenced code blocks are
// left byte-for-byte untouched — the index also eats spaces where line
// breaks used to be (e.g. "oc get nodes-o jsonpath"), so any attempt to
// re-wrap commands risks corrupting them further. Callers should warn that
// fenced commands may be mangled (see fencedCodePresent).
var (
	fenceSplit    = regexp.MustCompile("(`{3,}|~{3,})")
	reflowHeading = regexp.MustCompile(`\s*(#{1,6}\s)`)
	reflowBullet  = regexp.MustCompile(`([^\s-])- `)
	reflowOrdered = regexp.MustCompile(`([^\d\s])\s*(\d{1,2}\. )`)
	reflowSqueeze = regexp.MustCompile(`\n{3,}`)
)

func reflowMarkdown(s string) string {
	s = strings.TrimSpace(s)
	// Skip text that already contains line breaks so pre-formatted input is
	// never re-processed.
	if s == "" || strings.Contains(s, "\n") {
		return s
	}

	parts := fenceSplit.Split(s, -1)           // alternating prose / code segments
	markers := fenceSplit.FindAllString(s, -1) // fences[i] sits between parts[i] and parts[i+1]

	var b strings.Builder
	for i, part := range parts {
		if i%2 == 0 { // prose
			if p := reflowProse(part); p != "" {
				b.WriteString(p)
				b.WriteString("\n\n")
			}
		} else { // fenced code: verbatim
			b.WriteString(markers[i-1]) // opening fence
			b.WriteString("\n")
			b.WriteString(strings.TrimSpace(part))
			b.WriteString("\n")
			b.WriteString(markers[i]) // closing fence
			b.WriteString("\n\n")
		}
	}
	return strings.TrimSpace(b.String())
}

func reflowProse(s string) string {
	s = reflowHeading.ReplaceAllString(s, "\n\n$1")
	s = reflowBullet.ReplaceAllString(s, "$1\n- ")
	s = reflowOrdered.ReplaceAllString(s, "$1\n$2")
	s = reflowSqueeze.ReplaceAllString(s, "\n\n")
	return strings.TrimSpace(s)
}

// fencedCodePresent reports whether any Markdown code fence occurs in s,
// used to warn that the index may have mangled commands inside them.
func fencedCodePresent(s string) bool {
	return fenceSplit.MatchString(s)
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
			"Results include only Solution and Article documents — every numeric `id` can be fetched directly with get_kcs. Product documentation pages, labs, and catalog entries are excluded, along with their translations. Do not guess or fabricate IDs. For Red Hat Jira engineering issues, use search_issues instead.",
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
		Description: "Retrieve the full content of one Red Hat KCS solution by its numeric ID (e.g. 7010411), returning title, affected environment, issue description, resolution steps, root cause, and the access.redhat.com URL. Solutions carry issue/resolution/root-cause sections; Articles carry an abstract. Long text fields are re-formatted with restored line breaks for readability, and a warning is included when code blocks may have lost line breaks in the source index — do not execute fenced commands verbatim.\n\n" +
			"Use when the solution ID is known — from a search_kcs result, a Jira issue, or the user. `solution_id` must be purely numeric: documentation URLs and other identifiers are rejected; call search_kcs to find numeric Solution or Article IDs.",
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
