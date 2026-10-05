package server

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/cldmnky/mcp-servers/internal/logging"
)

// maxErrorSnippet bounds how much of an error response body is surfaced
// to the MCP client, so failures stay diagnosable without dumping pages.
const maxErrorSnippet = 200

// RedHatAPI represents the Red Hat API client
type RedHatAPI struct {
	BaseURL      string
	SSOURL       string
	OfflineToken string

	// mu guards the cached access token below; the Streamable HTTP
	// transport serves concurrent requests, so token refresh must be
	// serialized.
	mu          sync.Mutex
	accessToken string
	tokenExpiry time.Time

	client *http.Client
}

// NewRedHatAPI creates a new Red Hat API client
func NewRedHatAPI() (*RedHatAPI, error) {
	offlineToken := os.Getenv("RH_API_OFFLINE_TOKEN")
	if offlineToken == "" {
		return nil, fmt.Errorf("RH_API_OFFLINE_TOKEN environment variable is required")
	}

	return &RedHatAPI{
		BaseURL:      "https://access.redhat.com",
		SSOURL:       "https://sso.redhat.com/auth/realms/redhat-external/protocol/openid-connect/token",
		OfflineToken: offlineToken,
		client:       &http.Client{Timeout: 30 * time.Second},
	}, nil
}

// TokenResponse represents the OAuth token response
type TokenResponse struct {
	AccessToken string `json:"access_token"`
	ExpiresIn   int    `json:"expires_in"`
}

// GetAccessToken retrieves a valid access token, refreshing if necessary.
func (r *RedHatAPI) GetAccessToken(ctx context.Context) (string, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.accessToken != "" && time.Now().Before(r.tokenExpiry) {
		return r.accessToken, nil
	}

	data := fmt.Sprintf("grant_type=refresh_token&client_id=rhsm-api&refresh_token=%s", r.OfflineToken)

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, r.SSOURL, strings.NewReader(data))
	if err != nil {
		return "", fmt.Errorf("failed to create token request: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := r.client.Do(req)
	if err != nil {
		logging.Errorf("[api] token refresh failed: %v", err)
		return "", fmt.Errorf("failed to refresh token: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		// Read one byte past the snippet limit so snippet() can tell that the
		// body was truncated.
		body, _ := io.ReadAll(io.LimitReader(resp.Body, maxErrorSnippet+1))
		logging.Errorf("[api] token refresh failed with status %d: %s", resp.StatusCode, snippet(string(body)))
		return "", fmt.Errorf("token refresh failed with status %d: %s", resp.StatusCode, snippet(string(body)))
	}

	var tokenResp TokenResponse
	if err := json.NewDecoder(resp.Body).Decode(&tokenResp); err != nil {
		logging.Errorf("[api] failed to decode token response: %v", err)
		return "", fmt.Errorf("failed to decode token response: %w", err)
	}

	r.accessToken = tokenResp.AccessToken
	// Expire slightly early to avoid using a token on the edge of validity.
	r.tokenExpiry = time.Now().Add(time.Duration(tokenResp.ExpiresIn-60) * time.Second)
	logging.Debugf("[api] access token refreshed (expires in %d seconds)", tokenResp.ExpiresIn)
	return r.accessToken, nil
}

// snippet condenses an error response body into a single-line, length-bounded
// string suitable for both logs and tool errors.
func snippet(body string) string {
	body = strings.TrimSpace(strings.ReplaceAll(body, "\n", " "))
	if len(body) > maxErrorSnippet {
		body = body[:maxErrorSnippet] + "..."
	}
	return body
}

// MakeRequest makes an authenticated request to the Red Hat API and decodes
// the JSON response. Non-2xx responses return an error that includes the
// status and a bounded excerpt of the body.
func (r *RedHatAPI) MakeRequest(ctx context.Context, method, path string, body interface{}) (map[string]interface{}, error) {
	token, err := r.GetAccessToken(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get access token: %w", err)
	}

	var reqBody io.Reader = http.NoBody
	if body != nil {
		jsonData, err := json.Marshal(body)
		if err != nil {
			return nil, fmt.Errorf("failed to marshal request body: %w", err)
		}
		reqBody = strings.NewReader(string(jsonData))
	}

	req, err := http.NewRequestWithContext(ctx, method, r.BaseURL+path, reqBody)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")

	start := time.Now()
	resp, err := r.client.Do(req)
	if err != nil {
		logging.Errorf("[api] %s %s failed after %v: %v", method, path, time.Since(start), err)
		return nil, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	logging.Debugf("[api] %s %s -> %d in %v", method, path, resp.StatusCode, time.Since(start))

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		// Read one byte past the snippet limit so snippet() can tell that the
		// body was truncated.
		bodyBytes, _ := io.ReadAll(io.LimitReader(resp.Body, maxErrorSnippet+1))
		detail := snippet(string(bodyBytes))
		logging.Errorf("[api] %s %s failed with status %d: %s", method, path, resp.StatusCode, detail)
		return nil, fmt.Errorf("red hat API %s %s failed with status %d: %s", method, path, resp.StatusCode, detail)
	}

	// Handle both JSON and plain-text responses.
	if strings.Contains(resp.Header.Get("Content-Type"), "application/json") {
		var result map[string]interface{}
		if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
			return nil, fmt.Errorf("failed to decode JSON response: %w", err)
		}
		return result, nil
	}

	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response body: %w", err)
	}
	return map[string]interface{}{
		"content": string(bodyBytes),
	}, nil
}
