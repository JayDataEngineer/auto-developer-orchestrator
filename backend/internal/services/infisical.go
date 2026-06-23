package services

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"time"
)

// InfisicalClient resolves secrets from the cluster Infisical instance.
// If INFISICAL_URL is not set, all methods return empty strings.
type InfisicalClient struct {
	baseURL    string
	authToken  string
	httpClient *http.Client
}

// NewInfisicalClient creates a client using env vars.
// Set INFISICAL_URL to enable (defaults to cluster instance).
func NewInfisicalClient() *InfisicalClient {
	url := os.Getenv("INFISICAL_URL")
	if url == "" {
		// Check if the hub is reachable — if so, default to cluster Infisical
		hub := os.Getenv("MCP_HUB_ENDPOINT")
		if hub == "" {
			return nil
		}
		url = hub + "/infisical"
	}
	token := os.Getenv("INFISICAL_TOKEN")
	if token == "" {
		// Try service account credentials
		clientID := os.Getenv("INFISICAL_CLIENT_ID")
		clientSecret := os.Getenv("INFISICAL_CLIENT_SECRET")
		if clientID == "" || clientSecret == "" {
			return nil // not configured
		}
		// Authenticate to get a token
		c := &InfisicalClient{
			baseURL:    url,
			httpClient: &http.Client{Timeout: 10 * time.Second},
		}
		if tok, err := c.authenticate(clientID, clientSecret); err == nil {
			c.authToken = tok
		}
		return c
	}
	return &InfisicalClient{
		baseURL:    url,
		authToken:  token,
		httpClient: &http.Client{Timeout: 10 * time.Second},
	}
}

// Enabled returns true if Infisical is configured.
func (c *InfisicalClient) Enabled() bool { return c != nil }

type infisicalTokenResponse struct {
	AccessToken string `json:"accessToken"`
}

// authenticate exchanges client credentials for an access token.
func (c *InfisicalClient) authenticate(clientID, clientSecret string) (string, error) {
	body := map[string]string{
		"clientId":     clientID,
		"clientSecret": clientSecret,
	}
	data, _ := json.Marshal(body)
	resp, err := c.httpClient.Post(c.baseURL+"/api/v1/auth/universal-auth/token", "application/json", mustReader(data))
	if err != nil {
		return "", fmt.Errorf("infisical auth: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("infisical auth returned %d", resp.StatusCode)
	}
	var tokResp infisicalTokenResponse
	if err := json.NewDecoder(resp.Body).Decode(&tokResp); err != nil {
		return "", fmt.Errorf("infisical auth decode: %w", err)
	}
	return tokResp.AccessToken, nil
}

type infisicalSecret struct {
	SecretKey   string `json:"secretKey"`
	SecretValue string `json:"secretValue"`
}

type infisicalSecretsResponse struct {
	Secrets []infisicalSecret `json:"secrets"`
}

// GetSecret resolves a single secret by name from a given project and environment.
func (c *InfisicalClient) GetSecret(project, environment, secretName string) (string, error) {
	if c == nil {
		return "", fmt.Errorf("infisical not configured")
	}
	url := fmt.Sprintf("%s/api/v3/secrets?secretPath=/&workspaceId=%s&environment=%s&type=shared",
		c.baseURL, project, environment)
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("Authorization", "Bearer "+c.authToken)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("infisical get secret: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("infisical returned %d", resp.StatusCode)
	}
	var secretsResp infisicalSecretsResponse
	if err := json.NewDecoder(resp.Body).Decode(&secretsResp); err != nil {
		return "", fmt.Errorf("infisical decode: %w", err)
	}
	for _, s := range secretsResp.Secrets {
		if s.SecretKey == secretName {
			return s.SecretValue, nil
		}
	}
	return "", fmt.Errorf("secret %q not found", secretName)
}

// ResolveEnvVars fetches secrets from Infisical and sets them as env vars.
// The mapping is: env var name → Infisical secret key.
func (c *InfisicalClient) ResolveEnvVars(project, environment string, mapping map[string]string) error {
	if c == nil {
		return nil
	}
	for envVar, secretKey := range mapping {
		// Skip if already set in the environment
		if os.Getenv(envVar) != "" {
			continue
		}
		val, err := c.GetSecret(project, environment, secretKey)
		if err != nil {
			continue // graceful — skip secrets that aren't found
		}
		os.Setenv(envVar, val)
	}
	return nil
}
