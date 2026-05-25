// Package auth provides authentication utilities including OAuth2 device code flow.
package auth

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// OAuthProvider defines the configuration for an OAuth2 device code flow.
type OAuthProvider struct {
	Name         string // e.g. "google"
	TokenURL     string // e.g. "https://oauth2.googleapis.com/token"
	DeviceCodeURL string // e.g. "https://oauth2.googleapis.com/device/code"
	ClientID     string
	ClientSecret string // can be empty for public clients
	Scope        string // space-separated scopes
}

// GoogleOAuth is the pre-configured Google OAuth provider for Gemini.
var GoogleOAuth = OAuthProvider{
	Name:         "google",
	TokenURL:     "https://oauth2.googleapis.com/token",
	DeviceCodeURL: "https://oauth2.googleapis.com/device/code",
	ClientID:     "", // Must be set via GOOGLE_OAUTH_CLIENT_ID env var
	ClientSecret: "", // Optional — public clients don't need it
	Scope:        "https://www.googleapis.com/auth/cloud-platform",
}

// Token holds an OAuth2 access token and refresh token.
type Token struct {
	AccessToken  string    `json:"access_token"`
	RefreshToken string    `json:"refresh_token"`
	TokenType    string    `json:"token_type"`
	Expiry       time.Time `json:"expiry"`
	Scope        string    `json:"scope,omitempty"`
}

// IsExpired returns true if the token has expired or will expire within the given buffer.
func (t *Token) IsExpired(buffer time.Duration) bool {
	if t.AccessToken == "" {
		return true
	}
	return time.Now().Add(buffer).After(t.Expiry)
}

// TokenStore manages OAuth tokens with file-based persistence.
type TokenStore struct {
	mu       sync.RWMutex
	tokens   map[string]*Token
	filePath string
}

// NewTokenStore creates a token store that persists to the given file path.
func NewTokenStore(filePath string) *TokenStore {
	return &TokenStore{
		tokens:   make(map[string]*Token),
		filePath: filePath,
	}
}

// Load reads tokens from the persistence file.
func (s *TokenStore) Load() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	data, err := os.ReadFile(s.filePath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("read token file: %w", err)
	}

	var tokens map[string]*Token
	if err := json.Unmarshal(data, &tokens); err != nil {
		return fmt.Errorf("parse token file: %w", err)
	}

	s.tokens = tokens
	return nil
}

// Save writes tokens to the persistence file.
func (s *TokenStore) Save() error {
	s.mu.RLock()
	defer s.mu.RUnlock()

	dir := filepath.Dir(s.filePath)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return err
	}

	data, err := json.MarshalIndent(s.tokens, "", "  ")
	if err != nil {
		return err
	}

	return os.WriteFile(s.filePath, data, 0600) // sensitive — owner only
}

// Get retrieves a token for the given provider.
func (s *TokenStore) Get(provider string) *Token {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.tokens[provider]
}

// Set stores a token for the given provider and persists to disk.
func (s *TokenStore) Set(provider string, token *Token) error {
	s.mu.Lock()
	s.tokens[provider] = token
	s.mu.Unlock()
	return s.Save()
}

// DeviceCodeResponse represents the response from the device code authorization request.
type DeviceCodeResponse struct {
	DeviceCode      string `json:"device_code"`
	UserCode        string `json:"user_code"`
	VerificationURI string `json:"verification_uri"`
	ExpiresIn       int    `json:"expires_in"`
	Interval        int    `json:"interval"`
}

// StartDeviceAuth initiates the OAuth2 device code flow.
// Returns the device code response with the user code and verification URL.
func StartDeviceAuth(ctx context.Context, provider OAuthProvider) (*DeviceCodeResponse, error) {
	if provider.ClientID == "" {
		return nil, fmt.Errorf("OAuth client ID not configured for %s", provider.Name)
	}

	data := url.Values{
		"client_id": {provider.ClientID},
		"scope":     {provider.Scope},
	}
	if provider.ClientSecret != "" {
		data.Set("client_secret", provider.ClientSecret)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, provider.DeviceCodeURL, bytes.NewBufferString(data.Encode()))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("device code request failed: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("device code request returned %d: %s", resp.StatusCode, string(body))
	}

	var dcr DeviceCodeResponse
	if err := json.Unmarshal(body, &dcr); err != nil {
		return nil, fmt.Errorf("parse device code response: %w", err)
	}

	return &dcr, nil
}

// PollToken polls the token endpoint until the user authorizes the device code.
// Blocks until the token is obtained, the context is cancelled, or the code expires.
func PollToken(ctx context.Context, provider OAuthProvider, deviceCode string, interval time.Duration) (*Token, error) {
	data := url.Values{
		"client_id":   {provider.ClientID},
		"grant_type":  {"urn:ietf:params:oauth:grant-type:device_code"},
		"device_code": {deviceCode},
	}
	if provider.ClientSecret != "" {
		data.Set("client_secret", provider.ClientSecret)
	}

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-ticker.C:
			req, err := http.NewRequestWithContext(ctx, http.MethodPost, provider.TokenURL, bytes.NewBufferString(data.Encode()))
			if err != nil {
				return nil, err
			}
			req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

			resp, err := http.DefaultClient.Do(req)
			if err != nil {
				continue // retry on transient errors
			}

			body, _ := io.ReadAll(resp.Body)
			resp.Body.Close()

			var result map[string]interface{}
			json.Unmarshal(body, &result)

			if errMsg, ok := result["error"].(string); ok {
				switch errMsg {
				case "authorization_pending":
					continue // user hasn't authorized yet
				case "slow_down":
					ticker.Reset(interval * 2)
					continue
				case "expired_token":
					return nil, fmt.Errorf("device code expired")
				case "access_denied":
					return nil, fmt.Errorf("user denied authorization")
				default:
					return nil, fmt.Errorf("OAuth error: %s", errMsg)
				}
			}

			// Success — parse token
			var token Token
			if err := json.Unmarshal(body, &token); err != nil {
				return nil, fmt.Errorf("parse token response: %w", err)
			}

			expiresIn, _ := result["expires_in"].(float64)
			token.Expiry = time.Now().Add(time.Duration(expiresIn) * time.Second)

			return &token, nil
		}
	}
}

// RefreshToken refreshes an expired access token using the refresh token.
func RefreshToken(ctx context.Context, provider OAuthProvider, refreshToken string) (*Token, error) {
	data := url.Values{
		"client_id":     {provider.ClientID},
		"grant_type":    {"refresh_token"},
		"refresh_token": {refreshToken},
	}
	if provider.ClientSecret != "" {
		data.Set("client_secret", provider.ClientSecret)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, provider.TokenURL, bytes.NewBufferString(data.Encode()))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("token refresh request failed: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("token refresh returned %d: %s", resp.StatusCode, string(body))
	}

	var result map[string]interface{}
	json.Unmarshal(body, &result)

	var token Token
	if err := json.Unmarshal(body, &token); err != nil {
		return nil, fmt.Errorf("parse token response: %w", err)
	}

	// Preserve the refresh token if not returned
	if token.RefreshToken == "" {
		token.RefreshToken = refreshToken
	}

	expiresIn, _ := result["expires_in"].(float64)
	token.Expiry = time.Now().Add(time.Duration(expiresIn) * time.Second)

	return &token, nil
}

// GetOrRefreshToken gets a valid token, refreshing if necessary.
// If no token exists, it returns nil (the caller should start device auth).
func GetOrRefreshToken(ctx context.Context, provider OAuthProvider, store *TokenStore) (*Token, error) {
	token := store.Get(provider.Name)
	if token == nil {
		return nil, nil // no token — caller should start device auth
	}

	// Still valid?
	if !token.IsExpired(5 * time.Minute) {
		return token, nil
	}

	// Has refresh token?
	if token.RefreshToken == "" {
		return nil, fmt.Errorf("token expired and no refresh token available")
	}

	// Refresh
	newToken, err := RefreshToken(ctx, provider, token.RefreshToken)
	if err != nil {
		return nil, fmt.Errorf("refresh token: %w", err)
	}

	if err := store.Set(provider.Name, newToken); err != nil {
		return nil, fmt.Errorf("persist refreshed token: %w", err)
	}

	return newToken, nil
}

// DefaultTokenPath returns the default path for token storage.
func DefaultTokenPath() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".pi", "agent", "oauth-tokens.json")
}
