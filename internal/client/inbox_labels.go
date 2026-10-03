package client

import (
	"context"
	"net/http"
	"net/url"
)

// InboxLabel is a named, colored tag for the threads of one inbox.
type InboxLabel struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Color     string `json:"color"`
	CreatedAt string `json:"created_at"`
}

// InboxLabelRequest is the body for creating or updating a label. An empty
// Color is left out, which on create means the default (mauve).
type InboxLabelRequest struct {
	Name  string `json:"name"`
	Color string `json:"color,omitempty"`
}

func inboxLabelsPath(inboxID string) string {
	return "/v1/inboxes/" + url.PathEscape(inboxID) + "/labels"
}

// ListInboxLabels returns every label of an inbox, oldest first.
func (c *Client) ListInboxLabels(ctx context.Context, inboxID string) ([]InboxLabel, error) {
	var out struct {
		Data []InboxLabel `json:"data"`
	}
	if err := c.do(ctx, http.MethodGet, inboxLabelsPath(inboxID), nil, &out); err != nil {
		return nil, err
	}
	return out.Data, nil
}

// GetInboxLabel finds a label by ID. The API has no single-label read, so
// this searches the inbox's labels and answers a 404 APIError when the label
// is not there. A missing inbox is a 404 too.
func (c *Client) GetInboxLabel(ctx context.Context, inboxID, labelID string) (*InboxLabel, error) {
	labels, err := c.ListInboxLabels(ctx, inboxID)
	if err != nil {
		return nil, err
	}
	for i := range labels {
		if labels[i].ID == labelID {
			return &labels[i], nil
		}
	}
	return nil, &APIError{StatusCode: http.StatusNotFound, Message: "Label not found"}
}

// CreateInboxLabel adds a label to an inbox. Names are unique in the inbox,
// ignoring case.
func (c *Client) CreateInboxLabel(ctx context.Context, inboxID string, req InboxLabelRequest) (*InboxLabel, error) {
	var out InboxLabel
	if err := c.do(ctx, http.MethodPost, inboxLabelsPath(inboxID), req, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// UpdateInboxLabel renames or recolors a label. The API answers with the id
// only, so read the label back afterwards.
func (c *Client) UpdateInboxLabel(ctx context.Context, inboxID, labelID string, req InboxLabelRequest) error {
	return c.do(ctx, http.MethodPatch, inboxLabelsPath(inboxID)+"/"+url.PathEscape(labelID), req, nil)
}

// DeleteInboxLabel removes a label from the inbox and from every thread that
// carries it. No message is removed.
func (c *Client) DeleteInboxLabel(ctx context.Context, inboxID, labelID string) error {
	return c.do(ctx, http.MethodDelete, inboxLabelsPath(inboxID)+"/"+url.PathEscape(labelID), nil, nil)
}
