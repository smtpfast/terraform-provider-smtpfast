// Package client is a small HTTP client for the SMTPfast (smtpfa.st) API.
// It wraps the v1 endpoints the Terraform provider needs: sending domains,
// API keys, webhooks, templates, inboxes and contact properties.
package client

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// DefaultBaseURL is the production SMTPfast API base. The version prefix (/v1)
// is added per request.
const DefaultBaseURL = "https://smtpfa.st/api"

// Client talks to the SMTPfast API with a Bearer token.
type Client struct {
	APIKey     string
	BaseURL    string
	HTTPClient *http.Client
	UserAgent  string
}

// New builds a Client. An empty baseURL falls back to the production API.
func New(apiKey, baseURL, userAgent string) *Client {
	if baseURL == "" {
		baseURL = DefaultBaseURL
	}
	return &Client{
		APIKey:     apiKey,
		BaseURL:    strings.TrimRight(baseURL, "/"),
		HTTPClient: &http.Client{Timeout: 30 * time.Second},
		UserAgent:  userAgent,
	}
}

// APIError is returned for non-2xx responses.
type APIError struct {
	StatusCode int
	Message    string
	// Code is the machine-readable error code when the API sends one, such as
	// "revision_conflict" from the templates API or "domain_claimable".
	Code string
}

func (e *APIError) Error() string {
	return fmt.Sprintf("smtpfast API error %d: %s", e.StatusCode, e.Message)
}

// NotFound reports whether the error is a 404, which lets resources drop
// themselves from state when they have been deleted out of band.
func (e *APIError) NotFound() bool { return e.StatusCode == http.StatusNotFound }

// IsNotFound is a convenience for callers that have an error value.
func IsNotFound(err error) bool {
	var apiErr *APIError
	return errors.As(err, &apiErr) && apiErr.NotFound()
}

// ErrorCode returns the API error code of err, or "" when it has none.
func ErrorCode(err error) string {
	var apiErr *APIError
	if errors.As(err, &apiErr) {
		return apiErr.Code
	}
	return ""
}

func (c *Client) do(ctx context.Context, method, path string, body, out any) error {
	var reqBody io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return fmt.Errorf("encoding request body: %w", err)
		}
		reqBody = bytes.NewReader(b)
	}

	req, err := http.NewRequestWithContext(ctx, method, c.BaseURL+path, reqBody)
	if err != nil {
		return fmt.Errorf("building request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+c.APIKey)
	req.Header.Set("Accept", "application/json")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if c.UserAgent != "" {
		req.Header.Set("User-Agent", c.UserAgent)
	}

	res, err := c.HTTPClient.Do(req)
	if err != nil {
		return fmt.Errorf("performing request: %w", err)
	}
	defer func() { _ = res.Body.Close() }()

	data, _ := io.ReadAll(res.Body)

	if res.StatusCode >= http.StatusBadRequest {
		msg := strings.TrimSpace(string(data))
		var body struct {
			Error   string `json:"error"`
			Message string `json:"message"`
			Code    string `json:"code"`
			// Name carries the code on the Resend-compatible endpoints.
			Name string `json:"name"`
		}
		apiErr := &APIError{StatusCode: res.StatusCode, Message: msg}
		if json.Unmarshal(data, &body) == nil {
			if body.Error != "" {
				apiErr.Message = body.Error
			} else if body.Message != "" {
				apiErr.Message = body.Message
			}
			apiErr.Code = body.Code
			if apiErr.Code == "" {
				apiErr.Code = body.Name
			}
		}
		return apiErr
	}

	if out != nil && len(data) > 0 {
		if err := json.Unmarshal(data, out); err != nil {
			return fmt.Errorf("decoding response: %w", err)
		}
	}
	return nil
}
