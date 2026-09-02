package client

import (
	"context"
	"net/http"
)

// DNSRecord is a DNS entry the user must publish to verify a sending domain.
type DNSRecord struct {
	Type  string `json:"type"`
	Name  string `json:"name"`
	Value string `json:"value"`
}

// Domain is a sending domain.
type Domain struct {
	ID         string           `json:"id"`
	Domain     string           `json:"domain"`
	Status     string           `json:"status"`
	DNSRecords []DNSRecord      `json:"dns_records"`
	Receiving  *DomainReceiving `json:"receiving,omitempty"`
}

// DomainReceiving is the inbound email state of a domain.
type DomainReceiving struct {
	Enabled bool   `json:"enabled"`
	Status  string `json:"status"`
}

// SetDomainReceiving turns inbound email on or off for a domain. Enabling
// needs a paid plan and a domain that is verified for sending.
func (c *Client) SetDomainReceiving(ctx context.Context, id string, enabled bool) (*DomainReceiving, error) {
	var out struct {
		Receiving DomainReceiving `json:"receiving"`
	}
	err := c.do(ctx, http.MethodPatch, "/v1/domains/"+id, map[string]bool{"receiving_enabled": enabled}, &out)
	if err != nil {
		return nil, err
	}
	return &out.Receiving, nil
}

// CreateDomain registers a new sending domain.
func (c *Client) CreateDomain(ctx context.Context, domain string) (*Domain, error) {
	var out Domain
	err := c.do(ctx, http.MethodPost, "/v1/domains", map[string]string{"domain": domain}, &out)
	if err != nil {
		return nil, err
	}
	return &out, nil
}

// GetDomain fetches a sending domain by ID.
func (c *Client) GetDomain(ctx context.Context, id string) (*Domain, error) {
	var out Domain
	err := c.do(ctx, http.MethodGet, "/v1/domains/"+id, nil, &out)
	if err != nil {
		return nil, err
	}
	return &out, nil
}

// DeleteDomain removes a sending domain.
func (c *Client) DeleteDomain(ctx context.Context, id string) error {
	return c.do(ctx, http.MethodDelete, "/v1/domains/"+id, nil, nil)
}
