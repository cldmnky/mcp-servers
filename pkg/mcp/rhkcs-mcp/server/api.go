package server

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"strings"
	"time"
)

// RedHatAPI represents the Red Hat API client
type RedHatAPI struct {
	BaseURL      string
	SSOURL       string
	OfflineToken string
	AccessToken  string
	TokenExpiry  time.Time
	client       *http.Client
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

// GetAccessToken retrieves a valid access token, refreshing if necessary
func (r *RedHatAPI) GetAccessToken(ctx context.Context) (string, error) {
	// Check if current token is still valid
	if r.AccessToken != "" && time.Now().Before(r.TokenExpiry) {
		log.Println("[api] Using cached access token")
		return r.AccessToken, nil
	}

	log.Println("[api] Refreshing access token...")

	// Refresh token
	data := fmt.Sprintf("grant_type=refresh_token&client_id=rhsm-api&refresh_token=%s", r.OfflineToken)

	req, err := http.NewRequestWithContext(ctx, "POST", r.SSOURL,
		strings.NewReader(data))
	if err != nil {
		log.Printf("[api] ERROR: Failed to create token request: %v", err)
		return "", fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := r.client.Do(req)
	if err != nil {
		log.Printf("[api] ERROR: Failed to refresh token: %v", err)
		return "", fmt.Errorf("failed to refresh token: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		log.Printf("[api] ERROR: Token refresh failed with status %d", resp.StatusCode)
		return "", fmt.Errorf("token refresh failed with status %d", resp.StatusCode)
	}

	var tokenResp TokenResponse
	if err := json.NewDecoder(resp.Body).Decode(&tokenResp); err != nil {
		log.Printf("[api] ERROR: Failed to decode token response: %v", err)
		return "", fmt.Errorf("failed to decode token response: %w", err)
	}

	r.AccessToken = tokenResp.AccessToken
	// Set expiry time slightly before actual expiry to be safe
	r.TokenExpiry = time.Now().Add(time.Duration(tokenResp.ExpiresIn-60) * time.Second)

	log.Printf("[api] Access token refreshed successfully (expires in %d seconds)", tokenResp.ExpiresIn)
	return r.AccessToken, nil
}

// MakeRequest makes an authenticated request to the Red Hat API
func (r *RedHatAPI) MakeRequest(ctx context.Context, method, path string, body interface{}) (map[string]interface{}, error) {
	log.Printf("[api] Making %s request to %s", method, path)

	token, err := r.GetAccessToken(ctx)
	if err != nil {
		log.Printf("[api] ERROR: Failed to get access token: %v", err)
		return nil, fmt.Errorf("failed to get access token: %w", err)
	}

	url := r.BaseURL + path

	var reqBody io.Reader = http.NoBody
	if body != nil {
		jsonData, err := json.Marshal(body)
		if err != nil {
			log.Printf("[api] ERROR: Failed to marshal request body: %v", err)
			return nil, fmt.Errorf("failed to marshal request body: %w", err)
		}
		reqBody = strings.NewReader(string(jsonData))
		log.Printf("[api] Request body size: %d bytes", len(jsonData))
	}

	req, err := http.NewRequestWithContext(ctx, method, url, reqBody)
	if err != nil {
		log.Printf("[api] ERROR: Failed to create request: %v", err)
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")

	startTime := time.Now()
	resp, err := r.client.Do(req)
	duration := time.Since(startTime)

	if err != nil {
		log.Printf("[api] ERROR: Request failed after %v: %v", duration, err)
		return nil, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	log.Printf("[api] Response received: status=%d, duration=%v", resp.StatusCode, duration)

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		bodyBytes, _ := io.ReadAll(resp.Body)
		log.Printf("[api] ERROR: API request failed with status %d, body: %s", resp.StatusCode, string(bodyBytes))
		return nil, fmt.Errorf("API request failed with status %d", resp.StatusCode)
	}

	// Handle both JSON responses and text responses
	contentType := resp.Header.Get("Content-Type")
	if strings.Contains(contentType, "application/json") {
		var result map[string]interface{}
		if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
			log.Printf("[api] ERROR: Failed to decode JSON response: %v", err)
			return nil, fmt.Errorf("failed to decode JSON response: %w", err)
		}
		log.Println("[api] Successfully decoded JSON response")
		return result, nil
	}

	// For non-JSON responses, read as text
	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		log.Printf("[api] ERROR: Failed to read response body: %v", err)
		return nil, fmt.Errorf("failed to read response body: %w", err)
	}

	log.Printf("[api] Received text response: %d bytes", len(bodyBytes))
	return map[string]interface{}{
		"content": string(bodyBytes),
	}, nil
}

// MakeGetRequest makes an authenticated GET request with query parameters
func (r *RedHatAPI) MakeGetRequest(ctx context.Context, path string, queryParams map[string]string) (map[string]interface{}, error) {
	log.Printf("[api] Making GET request to %s with params: %v", path, queryParams)

	token, err := r.GetAccessToken(ctx)
	if err != nil {
		log.Printf("[api] ERROR: Failed to get access token: %v", err)
		return nil, fmt.Errorf("failed to get access token: %w", err)
	}

	url := r.BaseURL + path
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		log.Printf("[api] ERROR: Failed to create request: %v", err)
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	// Add query parameters
	q := req.URL.Query()
	for key, value := range queryParams {
		q.Add(key, value)
	}
	req.URL.RawQuery = q.Encode()

	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")

	log.Printf("[api] Full URL: %s", req.URL.String())

	startTime := time.Now()
	resp, err := r.client.Do(req)
	duration := time.Since(startTime)

	if err != nil {
		log.Printf("[api] ERROR: Request failed after %v: %v", duration, err)
		return nil, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	log.Printf("[api] Response received: status=%d, duration=%v", resp.StatusCode, duration)

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		bodyBytes, _ := io.ReadAll(resp.Body)
		log.Printf("[api] ERROR: API request failed with status %d, body: %s", resp.StatusCode, string(bodyBytes))
		return nil, fmt.Errorf("API request failed with status %d", resp.StatusCode)
	}

	var result map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		log.Printf("[api] ERROR: Failed to decode JSON response: %v", err)
		return nil, fmt.Errorf("failed to decode JSON response: %w", err)
	}

	log.Println("[api] Successfully decoded JSON response")
	return result, nil
}
