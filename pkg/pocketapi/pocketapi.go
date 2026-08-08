package pocketapi

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"strings"
	"time"
)

type UserCreateData struct {
	FirstName     string `json:"firstName"`
	LastName      string `json:"lastName"`
	DisplayName   string `json:"displayName"`
	Email         string `json:"email"`
	EmailVerified bool   `json:"emailVerified"`
	Username      string `json:"username"`
	IsAdmin       bool   `json:"isAdmin"`
	Disabled      bool   `json:"disabled"`
}

type PocketUser struct {
	ID string `json:"id"`
	UserCreateData
}

type pocketUserSearchResponse struct {
	Data []PocketUser `json:"data"`
}

// Client is a lightweight wrapper for PocketId API interactions.
type Client struct {
	baseURL    string
	apiKey     string
	httpClient *http.Client
}

func NewClient(baseURL, apiKey string, httpClient *http.Client) (*Client, error) {
	if baseURL == "" {
		return nil, fmt.Errorf("baseURL is required")
	}
	if apiKey == "" {
		return nil, fmt.Errorf("apiKey is required")
	}

	// validate URL
	u, err := url.Parse(baseURL)
	if err != nil {
		return nil, fmt.Errorf("invalid baseURL: %w", err)
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return nil, fmt.Errorf("baseURL must be http or https")
	}

	if httpClient == nil {
		httpClient = &http.Client{Timeout: 10 * time.Second}
	}

	return &Client{baseURL: strings.TrimRight(baseURL, "/"), apiKey: apiKey, httpClient: httpClient}, nil
}

func (c *Client) CreateUser(ctx context.Context, data UserCreateData) error {
	endpoint := c.baseURL + "/api/users"
	body, err := json.Marshal(data)
	if err != nil {
		return err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-API-KEY", c.apiKey)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		respBody, _ := io.ReadAll(resp.Body)
		log.Printf("pocket API error response: status=%d headers=%v body=%s", resp.StatusCode, resp.Header, strings.TrimSpace(string(respBody)))
		if resp.StatusCode == http.StatusConflict {
			log.Printf("pocket user already exists (409), ignoring")
			return nil
		}
		return fmt.Errorf("non-2xx response: %d: %s", resp.StatusCode, strings.TrimSpace(string(respBody)))
	}

	return nil
}

func (c *Client) SearchUser(ctx context.Context, query string) (*PocketUser, error) {
	endpoint := c.baseURL + "/api/users"
	u, err := url.Parse(endpoint)
	if err != nil {
		return nil, err
	}
	q := u.Query()
	q.Set("pagination[limit]", "20")
	q.Set("pagination[page]", "1")
	q.Set("search", query)
	u.RawQuery = q.Encode()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("X-API-KEY", c.apiKey)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		respBody, _ := io.ReadAll(resp.Body)
		log.Printf("pocket user search error: status=%d headers=%v body=%s", resp.StatusCode, resp.Header, strings.TrimSpace(string(respBody)))
		return nil, fmt.Errorf("non-2xx response: %d", resp.StatusCode)
	}

	var out pocketUserSearchResponse
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, err
	}
	if len(out.Data) == 0 {
		return nil, nil
	}
	return &out.Data[0], nil
}

func (c *Client) CreateOneTimeToken(ctx context.Context, userID string, ttl int) (string, error) {
	endpoint := fmt.Sprintf("%s/api/users/%s/one-time-access-token", c.baseURL, userID)
	payload := map[string]int{"ttl": ttl}
	body, err := json.Marshal(payload)
	if err != nil {
		return "", err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-API-KEY", c.apiKey)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(resp.Body)
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		log.Printf("one-time-token error: status=%d headers=%v body=%s", resp.StatusCode, resp.Header, strings.TrimSpace(string(respBody)))
		return "", fmt.Errorf("non-2xx response: %d: %s", resp.StatusCode, strings.TrimSpace(string(respBody)))
	}
	var tokenResp struct {
		Token string `json:"token"`
	}
	if err := json.Unmarshal(respBody, &tokenResp); err != nil {
		return "", err
	}
	return tokenResp.Token, nil
}
