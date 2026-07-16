package client

import (
	"context"
	"net/http"
)

// APIKey is an API key. Key holds the full secret and is only populated by the
// create call; reads never return it.
type APIKey struct {
	ID        string   `json:"id"`
	Name      string   `json:"name"`
	Key       string   `json:"key"`
	Prefix    string   `json:"prefix"`
	Scopes    []string `json:"scopes"`
	CreatedAt string   `json:"created_at"`
}

// CreateAPIKeyRequest is the body for creating an API key.
type CreateAPIKeyRequest struct {
	Name   string   `json:"name"`
	Scopes []string `json:"scopes,omitempty"`
}

// CreateAPIKey creates a new API key. The returned APIKey.Key is the only time
// the secret is exposed.
func (c *Client) CreateAPIKey(ctx context.Context, req CreateAPIKeyRequest) (*APIKey, error) {
	var out APIKey
	err := c.do(ctx, http.MethodPost, "/v1/api-keys", req, &out)
	if err != nil {
		return nil, err
	}
	return &out, nil
}

// GetAPIKey fetches an API key's metadata by ID (without the secret).
func (c *Client) GetAPIKey(ctx context.Context, id string) (*APIKey, error) {
	var out APIKey
	err := c.do(ctx, http.MethodGet, "/v1/api-keys/"+id, nil, &out)
	if err != nil {
		return nil, err
	}
	return &out, nil
}

// DeleteAPIKey revokes an API key.
func (c *Client) DeleteAPIKey(ctx context.Context, id string) error {
	return c.do(ctx, http.MethodDelete, "/v1/api-keys/"+id, nil, nil)
}
