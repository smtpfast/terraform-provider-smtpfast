package client

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
)

// Webhook is an event subscription that POSTs delivery events to a URL.
type Webhook struct {
	ID     string
	URL    string
	Events []string
	// Format is the delivery format: standard, discord or slack.
	Format string
	Active bool
	// SigningSecret is only returned by the create call.
	SigningSecret string
	CreatedAt     string
}

// UnmarshalJSON accepts both response shapes: create answers in snake_case
// (created_at, secret), while get and list answer with createdAt. Update
// answers without a timestamp at all.
func (w *Webhook) UnmarshalJSON(data []byte) error {
	var raw struct {
		ID            string   `json:"id"`
		URL           string   `json:"url"`
		Events        []string `json:"events"`
		Format        string   `json:"format"`
		Active        *bool    `json:"active"`
		Secret        string   `json:"secret"`
		SigningSecret string   `json:"signing_secret"`
		CreatedAt     string   `json:"created_at"`
		CreatedAtAlt  string   `json:"createdAt"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	*w = Webhook{
		ID:            raw.ID,
		URL:           raw.URL,
		Events:        raw.Events,
		Format:        raw.Format,
		Active:        raw.Active == nil || *raw.Active,
		SigningSecret: firstNonEmpty(raw.SigningSecret, raw.Secret),
		CreatedAt:     firstNonEmpty(raw.CreatedAt, raw.CreatedAtAlt),
	}
	return nil
}

// CreateWebhookRequest is the body for creating a webhook.
type CreateWebhookRequest struct {
	URL    string   `json:"url"`
	Events []string `json:"events"`
	// Format is optional; the API detects Discord and Slack URLs when it is empty.
	Format string `json:"format,omitempty"`
}

// UpdateWebhookRequest is the body for updating a webhook. Nil fields are left
// unchanged.
type UpdateWebhookRequest struct {
	URL    *string  `json:"url,omitempty"`
	Events []string `json:"events,omitempty"`
	Active *bool    `json:"active,omitempty"`
	Format *string  `json:"format,omitempty"`
}

// CreateWebhook creates a webhook subscription.
func (c *Client) CreateWebhook(ctx context.Context, req CreateWebhookRequest) (*Webhook, error) {
	var out Webhook
	err := c.do(ctx, http.MethodPost, "/v1/webhooks", req, &out)
	if err != nil {
		return nil, err
	}
	return &out, nil
}

// GetWebhook fetches a webhook by ID.
func (c *Client) GetWebhook(ctx context.Context, id string) (*Webhook, error) {
	var out Webhook
	err := c.do(ctx, http.MethodGet, "/v1/webhooks/"+url.PathEscape(id), nil, &out)
	if err != nil {
		return nil, err
	}
	return &out, nil
}

// UpdateWebhook updates a webhook's URL, events, format or active flag. The
// response carries no created_at.
func (c *Client) UpdateWebhook(ctx context.Context, id string, req UpdateWebhookRequest) (*Webhook, error) {
	var out Webhook
	err := c.do(ctx, http.MethodPatch, "/v1/webhooks/"+url.PathEscape(id), req, &out)
	if err != nil {
		return nil, err
	}
	return &out, nil
}

// DeleteWebhook removes a webhook.
func (c *Client) DeleteWebhook(ctx context.Context, id string) error {
	return c.do(ctx, http.MethodDelete, "/v1/webhooks/"+url.PathEscape(id), nil, nil)
}
