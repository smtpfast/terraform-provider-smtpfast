package client

import (
	"context"
	"net/http"
	"net/url"
)

// TeamInvite is a pending invitation to the team. Roles are lowercase.
type TeamInvite struct {
	ID    string `json:"id"`
	Email string `json:"email"`
	Role  string `json:"role"`
	// CreatedAt is only in list responses, not in the create response.
	CreatedAt string `json:"created_at"`
	ExpiresAt string `json:"expires_at"`
}

// TeamMember is one membership of the team. ID is the membership ID, which
// the update and remove calls take; UserID is the person's account.
type TeamMember struct {
	ID     string  `json:"id"`
	UserID string  `json:"user_id"`
	Email  string  `json:"email"`
	Name   *string `json:"name"`
	Role   string  `json:"role"`
	// CanManageBilling is always true for owners.
	CanManageBilling bool   `json:"can_manage_billing"`
	CreatedAt        string `json:"created_at"`
}

// UpdateTeamMemberRequest changes a member's role, billing access or both.
// Nil fields are left out. Only an owner can send CanManageBilling, even
// when it would not change anything.
type UpdateTeamMemberRequest struct {
	Role             *string `json:"role,omitempty"`
	CanManageBilling *bool   `json:"can_manage_billing,omitempty"`
}

// ListTeamInvites returns the invitations that are neither accepted nor
// expired, newest first. Owner or admin only.
func (c *Client) ListTeamInvites(ctx context.Context) ([]TeamInvite, error) {
	var out struct {
		Data []TeamInvite `json:"data"`
	}
	if err := c.do(ctx, http.MethodGet, "/v1/team/invites", nil, &out); err != nil {
		return nil, err
	}
	return out.Data, nil
}

// GetTeamInvite reads one pending invitation. An accepted or expired
// invitation, or another team's, is a 404 APIError. Owner or admin only.
func (c *Client) GetTeamInvite(ctx context.Context, id string) (*TeamInvite, error) {
	var out TeamInvite
	if err := c.do(ctx, http.MethodGet, "/v1/team/invites/"+url.PathEscape(id), nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// CreateTeamInvite emails an invitation to join the team. Inviting an
// address that already has a pending invitation refreshes that invitation
// (same ID, new link, new role).
func (c *Client) CreateTeamInvite(ctx context.Context, email, role string) (*TeamInvite, error) {
	var out TeamInvite
	body := map[string]string{"email": email, "role": role}
	if err := c.do(ctx, http.MethodPost, "/v1/team/invites", body, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// RevokeTeamInvite deletes an invitation, so its link stops working.
func (c *Client) RevokeTeamInvite(ctx context.Context, id string) error {
	return c.do(ctx, http.MethodDelete, "/v1/team/invites/"+url.PathEscape(id), nil, nil)
}

// ListTeamMembers returns everyone on the team, oldest first.
func (c *Client) ListTeamMembers(ctx context.Context) ([]TeamMember, error) {
	var out struct {
		Data []TeamMember `json:"data"`
	}
	if err := c.do(ctx, http.MethodGet, "/v1/team/members", nil, &out); err != nil {
		return nil, err
	}
	return out.Data, nil
}

// UpdateTeamMember changes a member's role or billing access. The response
// carries only the id, role and billing flag, so list the members to read
// the rest.
func (c *Client) UpdateTeamMember(ctx context.Context, id string, req UpdateTeamMemberRequest) error {
	return c.do(ctx, http.MethodPatch, "/v1/team/members/"+url.PathEscape(id), req, nil)
}

// RemoveTeamMember removes someone from the team and revokes their API keys
// for it.
func (c *Client) RemoveTeamMember(ctx context.Context, id string) error {
	return c.do(ctx, http.MethodDelete, "/v1/team/members/"+url.PathEscape(id), nil, nil)
}
