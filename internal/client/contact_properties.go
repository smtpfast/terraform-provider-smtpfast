package client

import (
	"context"
	"net/http"
	"net/url"
)

// ContactProperty is a declared custom contact field.
type ContactProperty struct {
	ID   string `json:"id"`
	Key  string `json:"key"`
	Type string `json:"type"`
	// FallbackValue is a string or a float64, matching Type, or nil.
	FallbackValue any    `json:"fallback_value"`
	CreatedAt     string `json:"created_at"`
}

// CreateContactPropertyRequest declares a contact property.
type CreateContactPropertyRequest struct {
	Key           string `json:"key"`
	Type          string `json:"type"`
	FallbackValue any    `json:"fallback_value"`
}

type contactPropertyEnvelope struct {
	ContactProperty ContactProperty `json:"contact_property"`
}

// CreateContactProperty declares a contact property.
func (c *Client) CreateContactProperty(ctx context.Context, req CreateContactPropertyRequest) (*ContactProperty, error) {
	var out contactPropertyEnvelope
	if err := c.do(ctx, http.MethodPost, "/v1/contact-properties", req, &out); err != nil {
		return nil, err
	}
	return &out.ContactProperty, nil
}

// GetContactProperty fetches a contact property by ID.
func (c *Client) GetContactProperty(ctx context.Context, id string) (*ContactProperty, error) {
	var out contactPropertyEnvelope
	if err := c.do(ctx, http.MethodGet, "/v1/contact-properties/"+url.PathEscape(id), nil, &out); err != nil {
		return nil, err
	}
	return &out.ContactProperty, nil
}

// UpdateContactPropertyFallback sets a property's fallback value. Key and type
// cannot change; a nil fallback clears it.
func (c *Client) UpdateContactPropertyFallback(ctx context.Context, id string, fallback any) (*ContactProperty, error) {
	var out contactPropertyEnvelope
	body := map[string]any{"fallback_value": fallback}
	if err := c.do(ctx, http.MethodPatch, "/v1/contact-properties/"+url.PathEscape(id), body, &out); err != nil {
		return nil, err
	}
	return &out.ContactProperty, nil
}

// DeleteContactProperty removes the declaration. Values already stored on
// contacts stay.
func (c *Client) DeleteContactProperty(ctx context.Context, id string) error {
	return c.do(ctx, http.MethodDelete, "/v1/contact-properties/"+url.PathEscape(id), nil, nil)
}
