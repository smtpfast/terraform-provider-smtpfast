package client

import (
	"context"
	"net/http"
	"net/url"
)

// RevisionConflictCode is the error code the templates API answers with when
// expected_updated_at no longer matches the template.
const RevisionConflictCode = "revision_conflict"

// TemplateVariable is a variable a template declares, used in the content as
// {{{KEY}}}.
type TemplateVariable struct {
	Key  string `json:"key"`
	Type string `json:"type"`
	// FallbackValue is a string or a float64, matching Type, or nil.
	FallbackValue any `json:"fallback_value"`
}

// Template is a hosted template as GET /v1/templates/{id} returns it. The
// content fields are the current draft.
type Template struct {
	ID                     string             `json:"id"`
	CurrentVersionID       *string            `json:"current_version_id"`
	Alias                  *string            `json:"alias"`
	Name                   string             `json:"name"`
	Status                 string             `json:"status"`
	PublishedAt            *string            `json:"published_at"`
	CreatedAt              string             `json:"created_at"`
	UpdatedAt              string             `json:"updated_at"`
	From                   *string            `json:"from"`
	Subject                *string            `json:"subject"`
	ReplyTo                []string           `json:"reply_to"`
	HTML                   *string            `json:"html"`
	Text                   *string            `json:"text"`
	Markdown               *string            `json:"markdown"`
	PreviewText            *string            `json:"preview_text"`
	Variables              []TemplateVariable `json:"variables"`
	HasUnpublishedVersions bool               `json:"has_unpublished_versions"`
}

// TemplateWriteResult is what create, update and publish answer. UpdatedAt is
// the revision the write created, to pass as expected_updated_at next time.
type TemplateWriteResult struct {
	ID                     string  `json:"id"`
	UpdatedAt              string  `json:"updated_at"`
	Status                 string  `json:"status"`
	PublishedAt            *string `json:"published_at"`
	HasUnpublishedVersions bool    `json:"has_unpublished_versions"`
}

// TemplateFields is the editable content of a template.
type TemplateFields struct {
	Name        string
	Alias       *string
	From        *string
	Subject     *string
	ReplyTo     []string
	PreviewText *string
	HTML        *string
	Text        *string
	Markdown    *string
	Variables   []TemplateVariable
}

// body builds the request body. With clear unset, nil fields are left out (a
// create). With clear set, every field is sent and nil ones as null, so a
// field removed from the configuration is cleared on update.
func (f TemplateFields) body(clear bool) map[string]any {
	b := map[string]any{"name": f.Name}
	optional := map[string]*string{
		"alias":        f.Alias,
		"from":         f.From,
		"subject":      f.Subject,
		"preview_text": f.PreviewText,
		"html":         f.HTML,
		"text":         f.Text,
		"markdown":     f.Markdown,
	}
	for k, v := range optional {
		switch {
		case v != nil:
			b[k] = *v
		case clear:
			b[k] = nil
		}
	}
	switch {
	case f.ReplyTo != nil:
		b["reply_to"] = f.ReplyTo
	case clear:
		b["reply_to"] = nil
	}
	switch {
	case f.Variables != nil:
		b["variables"] = f.Variables
	case clear:
		b["variables"] = nil
	}
	return b
}

// CreateTemplate creates a template as a draft.
func (c *Client) CreateTemplate(ctx context.Context, fields TemplateFields) (*TemplateWriteResult, error) {
	var out TemplateWriteResult
	if err := c.do(ctx, http.MethodPost, "/v1/templates", fields.body(false), &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// GetTemplate fetches a template by id or alias.
func (c *Client) GetTemplate(ctx context.Context, idOrAlias string) (*Template, error) {
	var out Template
	if err := c.do(ctx, http.MethodGet, "/v1/templates/"+url.PathEscape(idOrAlias), nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// UpdateTemplate replaces the draft with fields. A non-empty expectedUpdatedAt
// makes the API refuse the write with a 409 revision_conflict if the template
// changed since that revision.
func (c *Client) UpdateTemplate(ctx context.Context, id string, fields TemplateFields, expectedUpdatedAt string) (*TemplateWriteResult, error) {
	body := fields.body(true)
	if expectedUpdatedAt != "" {
		body["expected_updated_at"] = expectedUpdatedAt
	}
	var out TemplateWriteResult
	if err := c.do(ctx, http.MethodPatch, "/v1/templates/"+url.PathEscape(id), body, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// PublishTemplate publishes the current draft, so sends use it. A non-empty
// expectedUpdatedAt publishes only that revision.
func (c *Client) PublishTemplate(ctx context.Context, id, expectedUpdatedAt string) (*TemplateWriteResult, error) {
	var body any
	if expectedUpdatedAt != "" {
		body = map[string]string{"expected_updated_at": expectedUpdatedAt}
	}
	var out TemplateWriteResult
	if err := c.do(ctx, http.MethodPost, "/v1/templates/"+url.PathEscape(id)+"/publish", body, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// DeleteTemplate deletes a template. Emails already sent with it are not
// affected.
func (c *Client) DeleteTemplate(ctx context.Context, id string) error {
	return c.do(ctx, http.MethodDelete, "/v1/templates/"+url.PathEscape(id), nil, nil)
}
