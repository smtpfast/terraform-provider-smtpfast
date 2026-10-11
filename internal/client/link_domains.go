package client

import (
	"context"
	"net/http"
	"net/url"
)

// LinkDomainDNSRecord is a record to publish for a link domain: the CNAME
// pointing the hostname at SMTPfast, and any optional TXT record Cloudflare
// offers to activate the hostname before the CNAME is live.
type LinkDomainDNSRecord struct {
	Type     string `json:"type"`
	Name     string `json:"name"`
	Value    string `json:"value"`
	Purpose  string `json:"purpose"`
	Required bool   `json:"required"`
	// Status is pending, verified (the CNAME of an active link domain) or
	// not_needed (a TXT record once the link domain is active).
	Status string `json:"status"`
}

// LinkDomain is a custom hostname for open tracking, click tracking and
// unsubscribe links.
type LinkDomain struct {
	ID         string                `json:"id"`
	Hostname   string                `json:"hostname"`
	DomainID   *string               `json:"domain_id"`
	Domain     *string               `json:"domain"`
	Status     string                `json:"status"`
	SSLStatus  *string               `json:"ssl_status"`
	LastError  *string               `json:"last_error"`
	DNSRecords []LinkDomainDNSRecord `json:"dns_records"`
	VerifiedAt *string               `json:"verified_at"`
	CreatedAt  string                `json:"created_at"`
}

// CreateLinkDomain adds a link domain. The hostname must be a subdomain of a
// verified sending domain on the team, and the team's plan must include link
// domains.
func (c *Client) CreateLinkDomain(ctx context.Context, hostname string) (*LinkDomain, error) {
	var out LinkDomain
	if err := c.do(ctx, http.MethodPost, "/v1/link-domains", map[string]string{"hostname": hostname}, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// GetLinkDomain fetches a link domain by ID, with the state from its last check.
func (c *Client) GetLinkDomain(ctx context.Context, id string) (*LinkDomain, error) {
	var out LinkDomain
	if err := c.do(ctx, http.MethodGet, "/v1/link-domains/"+url.PathEscape(id), nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// DeleteLinkDomain removes a link domain. Mail stops using it at once.
func (c *Client) DeleteLinkDomain(ctx context.Context, id string) error {
	return c.do(ctx, http.MethodDelete, "/v1/link-domains/"+url.PathEscape(id), nil, nil)
}
