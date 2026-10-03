package client

import (
	"context"
	"net/http"
	"net/url"
)

// Segment is a manual contact segment.
type Segment struct {
	ID          string  `json:"id"`
	Name        string  `json:"name"`
	Description *string `json:"description"`
	Color       *string `json:"color"`
	CreatedAt   string  `json:"created_at"`
}

// SegmentRequest is the body for creating or updating a segment. Name is
// always sent, and so are Description and Color: nil clears them.
type SegmentRequest struct {
	Name        string  `json:"name"`
	Description *string `json:"description"`
	Color       *string `json:"color"`
}

// CreateSegment creates a segment. Names are unique in the team.
func (c *Client) CreateSegment(ctx context.Context, req SegmentRequest) (*Segment, error) {
	var out Segment
	if err := c.do(ctx, http.MethodPost, "/v1/segments", req, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// GetSegment fetches a segment by ID.
func (c *Client) GetSegment(ctx context.Context, id string) (*Segment, error) {
	var out Segment
	if err := c.do(ctx, http.MethodGet, "/v1/segments/"+url.PathEscape(id), nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// ListSegments returns the team's segments, newest first.
func (c *Client) ListSegments(ctx context.Context) ([]Segment, error) {
	var out struct {
		Data []Segment `json:"data"`
	}
	if err := c.do(ctx, http.MethodGet, "/v1/segments", nil, &out); err != nil {
		return nil, err
	}
	return out.Data, nil
}

// UpdateSegment renames a segment or changes its description and color.
func (c *Client) UpdateSegment(ctx context.Context, id string, req SegmentRequest) (*Segment, error) {
	var out Segment
	if err := c.do(ctx, http.MethodPatch, "/v1/segments/"+url.PathEscape(id), req, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// DeleteSegment deletes a segment. The contacts in it stay.
func (c *Client) DeleteSegment(ctx context.Context, id string) error {
	return c.do(ctx, http.MethodDelete, "/v1/segments/"+url.PathEscape(id), nil, nil)
}
