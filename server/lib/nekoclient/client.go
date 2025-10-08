package nekoclient

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sync"

	nekooapi "github.com/m1k1o/neko/server/lib/oapi"
)

// AuthClient wraps the Neko OpenAPI client and handles authentication automatically.
// It manages token caching and refresh on 401 responses.
type AuthClient struct {
	client   *nekooapi.Client
	tokenMu  sync.Mutex
	token    string
	username string
	password string
}

// NewAuthClient creates a new authenticated Neko client.
func NewAuthClient(baseURL, username, password string) (*AuthClient, error) {
	client, err := nekooapi.NewClient(baseURL)
	if err != nil {
		return nil, fmt.Errorf("failed to create neko client: %w", err)
	}

	return &AuthClient{
		client:   client,
		username: username,
		password: password,
	}, nil
}

// ensureToken ensures we have a valid token, logging in if necessary.
// Must be called with tokenMu held.
func (c *AuthClient) ensureToken(ctx context.Context) error {
	// Check if we already have a token
	if c.token != "" {
		return nil
	}

	// Login to get a new token
	loginReq := nekooapi.SessionLoginRequest{
		Username: &c.username,
		Password: &c.password,
	}

	resp, err := c.client.Login(ctx, loginReq)
	if err != nil {
		return fmt.Errorf("failed to call login API: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("login API returned status %d: %s", resp.StatusCode, string(respBody))
	}

	var loginResp nekooapi.SessionLoginResponse
	if err := json.NewDecoder(resp.Body).Decode(&loginResp); err != nil {
		return fmt.Errorf("failed to parse login response: %w", err)
	}

	if loginResp.Token == nil || *loginResp.Token == "" {
		return fmt.Errorf("login response did not contain a token")
	}

	c.token = *loginResp.Token
	return nil
}

// clearToken clears the cached token, forcing a new login on next request.
// Must be called with tokenMu held.
func (c *AuthClient) clearToken() {
	c.token = ""
}

// SessionsGet retrieves all active sessions from Neko API.
func (c *AuthClient) SessionsGet(ctx context.Context) ([]nekooapi.SessionData, error) {
	c.tokenMu.Lock()
	defer c.tokenMu.Unlock()

	// Ensure we have a token
	if err := c.ensureToken(ctx); err != nil {
		return nil, err
	}

	// Create request editor to add Bearer token
	addAuth := func(ctx context.Context, req *http.Request) error {
		req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", c.token))
		return nil
	}

	// Make the request
	resp, err := c.client.SessionsGet(ctx, addAuth)
	if err != nil {
		return nil, fmt.Errorf("failed to query sessions: %w", err)
	}
	defer resp.Body.Close()

	// Handle 401 by clearing token and retrying once
	if resp.StatusCode == http.StatusUnauthorized {
		c.clearToken()
		if err := c.ensureToken(ctx); err != nil {
			return nil, err
		}

		// Retry with fresh token
		addAuthRetry := func(ctx context.Context, req *http.Request) error {
			req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", c.token))
			return nil
		}

		resp, err = c.client.SessionsGet(ctx, addAuthRetry)
		if err != nil {
			return nil, fmt.Errorf("failed to retry sessions query: %w", err)
		}
		defer resp.Body.Close()
	}

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("sessions API returned status %d: %s", resp.StatusCode, string(respBody))
	}

	var sessions []nekooapi.SessionData
	if err := json.NewDecoder(resp.Body).Decode(&sessions); err != nil {
		return nil, fmt.Errorf("failed to parse sessions response: %w", err)
	}

	return sessions, nil
}

// ScreenConfigurationChange changes the screen resolution via Neko API.
func (c *AuthClient) ScreenConfigurationChange(ctx context.Context, config nekooapi.ScreenConfiguration) error {
	c.tokenMu.Lock()
	defer c.tokenMu.Unlock()

	// Ensure we have a token
	if err := c.ensureToken(ctx); err != nil {
		return err
	}

	// Create request editor to add Bearer token
	addAuth := func(ctx context.Context, req *http.Request) error {
		req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", c.token))
		return nil
	}

	// Make the request
	resp, err := c.client.ScreenConfigurationChange(ctx, config, addAuth)
	if err != nil {
		return fmt.Errorf("failed to change screen configuration: %w", err)
	}
	defer resp.Body.Close()

	// Handle 401 by clearing token and retrying once
	if resp.StatusCode == http.StatusUnauthorized {
		c.clearToken()
		if err := c.ensureToken(ctx); err != nil {
			return err
		}

		// Retry with fresh token
		addAuthRetry := func(ctx context.Context, req *http.Request) error {
			req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", c.token))
			return nil
		}

		resp, err = c.client.ScreenConfigurationChange(ctx, config, addAuthRetry)
		if err != nil {
			return fmt.Errorf("failed to retry screen configuration change: %w", err)
		}
		defer resp.Body.Close()
	}

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusNoContent {
		respBody, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("screen configuration API returned status %d: %s", resp.StatusCode, string(respBody))
	}

	return nil
}
