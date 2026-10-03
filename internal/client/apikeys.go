package client

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
)

// APIKey is an API key. Key holds the full secret and is only populated by the
// create call; reads never return it.
type APIKey struct {
	ID        string
	Name      string
	Key       string
	Prefix    string
	Scopes    []string
	CreatedAt string
	// RevokedAt is set on revoked keys, which the list endpoint keeps returning.
	RevokedAt string
}

// UnmarshalJSON accepts both response shapes: create answers in snake_case
// (prefix, created_at), while list and update answer with the database field
// names (keyPrefix, createdAt, revokedAt).
func (k *APIKey) UnmarshalJSON(data []byte) error {
	var raw struct {
		ID           string   `json:"id"`
		Name         string   `json:"name"`
		Key          string   `json:"key"`
		Prefix       string   `json:"prefix"`
		KeyPrefix    string   `json:"keyPrefix"`
		Scopes       []string `json:"scopes"`
		CreatedAt    string   `json:"created_at"`
		CreatedAtAlt string   `json:"createdAt"`
		RevokedAt    *string  `json:"revokedAt"`
		RevokedAtAlt *string  `json:"revoked_at"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	*k = APIKey{
		ID:        raw.ID,
		Name:      raw.Name,
		Key:       raw.Key,
		Prefix:    firstNonEmpty(raw.Prefix, raw.KeyPrefix),
		Scopes:    raw.Scopes,
		CreatedAt: firstNonEmpty(raw.CreatedAt, raw.CreatedAtAlt),
	}
	if raw.RevokedAt != nil {
		k.RevokedAt = *raw.RevokedAt
	} else if raw.RevokedAtAlt != nil {
		k.RevokedAt = *raw.RevokedAtAlt
	}
	return nil
}

// CreateAPIKeyRequest is the body for creating an API key.
type CreateAPIKeyRequest struct {
	Name   string   `json:"name"`
	Scopes []string `json:"scopes,omitempty"`
}

// UpdateAPIKeyRequest renames a key or replaces its scopes. Nil fields are
// left unchanged. The secret never changes.
type UpdateAPIKeyRequest struct {
	Name   *string  `json:"name,omitempty"`
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

// ListAPIKeys returns the team's API keys (without secrets), revoked ones
// included.
func (c *Client) ListAPIKeys(ctx context.Context) ([]APIKey, error) {
	var out []APIKey
	if err := c.do(ctx, http.MethodGet, "/v1/api-keys", nil, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// GetAPIKey finds an API key's metadata by ID (without the secret). The API
// has no single-key read, so this searches the list and answers a 404
// APIError when the key is not in it. A revoked key is returned with
// RevokedAt set.
func (c *Client) GetAPIKey(ctx context.Context, id string) (*APIKey, error) {
	keys, err := c.ListAPIKeys(ctx)
	if err != nil {
		return nil, err
	}
	for i := range keys {
		if keys[i].ID == id {
			return &keys[i], nil
		}
	}
	return nil, &APIError{StatusCode: http.StatusNotFound, Message: "API key not found"}
}

// UpdateAPIKey renames a key or changes its scopes.
func (c *Client) UpdateAPIKey(ctx context.Context, id string, req UpdateAPIKeyRequest) (*APIKey, error) {
	var out APIKey
	err := c.do(ctx, http.MethodPatch, "/v1/api-keys/"+url.PathEscape(id), req, &out)
	if err != nil {
		return nil, err
	}
	return &out, nil
}

// DeleteAPIKey revokes an API key.
func (c *Client) DeleteAPIKey(ctx context.Context, id string) error {
	return c.do(ctx, http.MethodDelete, "/v1/api-keys/"+url.PathEscape(id), nil, nil)
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if v != "" {
			return v
		}
	}
	return ""
}
