package client

import (
	"context"
	"net/http"
	"net/url"
)

// Inbox organises one address on a receiving domain into threads, labels,
// folders and drafts.
type Inbox struct {
	ID           string  `json:"id"`
	Name         string  `json:"name"`
	EmailAddress string  `json:"email_address"`
	DomainID     string  `json:"domain_id"`
	FromName     *string `json:"from_name"`
	CreatedAt    string  `json:"created_at"`
}

// CreateInboxRequest is the body for creating an inbox.
type CreateInboxRequest struct {
	EmailAddress string  `json:"email_address"`
	Name         *string `json:"name,omitempty"`
	FromName     *string `json:"from_name,omitempty"`
}

// UpdateInboxRequest changes an inbox's names. FromName is always sent, so
// nil clears it.
type UpdateInboxRequest struct {
	Name     *string `json:"name,omitempty"`
	FromName *string `json:"from_name"`
}

// CreateInbox creates an inbox. The address's domain must have receiving on
// and be able to send.
func (c *Client) CreateInbox(ctx context.Context, req CreateInboxRequest) (*Inbox, error) {
	var out Inbox
	if err := c.do(ctx, http.MethodPost, "/v1/inboxes", req, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// GetInbox fetches an inbox by id or by its email address.
func (c *Client) GetInbox(ctx context.Context, idOrAddress string) (*Inbox, error) {
	var out Inbox
	if err := c.do(ctx, http.MethodGet, "/v1/inboxes/"+url.PathEscape(idOrAddress), nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// UpdateInbox changes an inbox's internal name or the name recipients see.
// The API answers with the id only, so read the inbox back afterwards.
func (c *Client) UpdateInbox(ctx context.Context, id string, req UpdateInboxRequest) error {
	return c.do(ctx, http.MethodPatch, "/v1/inboxes/"+url.PathEscape(id), req, nil)
}

// DeleteInbox deletes an inbox with its threads, labels and drafts. The
// address keeps receiving, because receiving belongs to the domain.
func (c *Client) DeleteInbox(ctx context.Context, id string) error {
	return c.do(ctx, http.MethodDelete, "/v1/inboxes/"+url.PathEscape(id), nil, nil)
}
