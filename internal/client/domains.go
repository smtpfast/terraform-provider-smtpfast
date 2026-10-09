package client

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
)

// DNSRecord is a DNS entry the user must publish to verify a sending domain.
type DNSRecord struct {
	Type  string `json:"type"`
	Name  string `json:"name"`
	Value string `json:"value"`
	// Priority is set on MX records (inbound receiving) and nil otherwise.
	Priority *int64 `json:"priority,omitempty"`
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
	// MXRecord is the record receiving needs. GET returns it whether or not
	// receiving is on, which lets a plan predict dns_records.
	MXRecord *DNSRecord `json:"mx_record,omitempty"`
}

// SetDomainReceiving turns inbound email on or off for a domain. Enabling
// needs a paid plan and a domain that is verified for sending.
func (c *Client) SetDomainReceiving(ctx context.Context, id string, enabled bool) (*DomainReceiving, error) {
	var out struct {
		Receiving DomainReceiving `json:"receiving"`
	}
	err := c.do(ctx, http.MethodPatch, "/v1/domains/"+url.PathEscape(id), map[string]bool{"receiving_enabled": enabled}, &out)
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
	err := c.do(ctx, http.MethodGet, "/v1/domains/"+url.PathEscape(id), nil, &out)
	if err != nil {
		return nil, err
	}
	return &out, nil
}

// DomainSummary is one row of the domain list.
type DomainSummary struct {
	ID     string `json:"id"`
	Domain string `json:"domain"`
	Status string `json:"status"`
}

// ListDomains returns the team's sending domains, newest first.
func (c *Client) ListDomains(ctx context.Context) ([]DomainSummary, error) {
	var raw json.RawMessage
	if err := c.do(ctx, http.MethodGet, "/v1/domains", nil, &raw); err != nil {
		return nil, err
	}
	var out []DomainSummary
	if err := listData(raw, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// DeleteDomain removes a sending domain.
func (c *Client) DeleteDomain(ctx context.Context, id string) error {
	return c.do(ctx, http.MethodDelete, "/v1/domains/"+url.PathEscape(id), nil, nil)
}
