package client

import (
	"context"
	"net/http"
)

// Webhook is an event subscription that POSTs delivery events to a URL.
type Webhook struct {
	ID        string   `json:"id"`
	URL       string   `json:"url"`
	Events    []string `json:"events"`
	Active    bool     `json:"active"`
	CreatedAt string   `json:"created_at"`
}

// CreateWebhookRequest is the body for creating a webhook.
type CreateWebhookRequest struct {
	URL    string   `json:"url"`
	Events []string `json:"events"`
}

// UpdateWebhookRequest is the body for updating a webhook. Nil fields are left
// unchanged.
type UpdateWebhookRequest struct {
	URL    *string  `json:"url,omitempty"`
	Events []string `json:"events,omitempty"`
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
	err := c.do(ctx, http.MethodGet, "/v1/webhooks/"+id, nil, &out)
	if err != nil {
		return nil, err
	}
	return &out, nil
}

// UpdateWebhook updates a webhook's URL and/or events.
func (c *Client) UpdateWebhook(ctx context.Context, id string, req UpdateWebhookRequest) (*Webhook, error) {
	var out Webhook
	err := c.do(ctx, http.MethodPatch, "/v1/webhooks/"+id, req, &out)
	if err != nil {
		return nil, err
	}
	return &out, nil
}

// DeleteWebhook removes a webhook.
func (c *Client) DeleteWebhook(ctx context.Context, id string) error {
	return c.do(ctx, http.MethodDelete, "/v1/webhooks/"+id, nil, nil)
}
