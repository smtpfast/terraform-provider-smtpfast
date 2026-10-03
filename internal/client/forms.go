package client

import (
	"context"
	"net/http"
	"net/url"
)

// SignupForm is a hosted signup form that adds contacts. Responses are in
// snake_case. Create answers without the confirmation and welcome email
// fields, so those are only filled by get and update.
type SignupForm struct {
	ID                        string   `json:"id"`
	Name                      string   `json:"name"`
	Fields                    []string `json:"fields"`
	ButtonText                string   `json:"button_text"`
	ButtonColor               string   `json:"button_color"`
	SuccessMessage            string   `json:"success_message"`
	DoubleOptIn               bool     `json:"double_opt_in"`
	RedirectURL               *string  `json:"redirect_url"`
	CaptchaEnabled            bool     `json:"captcha_enabled"`
	TurnstileSiteKey          *string  `json:"turnstile_site_key"`
	TurnstileSecretConfigured bool     `json:"turnstile_secret_configured"`
	BlockDisposableEmails     bool     `json:"block_disposable_emails"`
	Active                    bool     `json:"active"`
	ConfirmationEmailFrom     *string  `json:"confirmation_email_from"`
	WelcomeEmailEnabled       bool     `json:"welcome_email_enabled"`
	WelcomeEmailFrom          *string  `json:"welcome_email_from"`
	WelcomeEmailSubject       *string  `json:"welcome_email_subject"`
	WelcomeEmailMarkdown      *string  `json:"welcome_email_markdown"`
	CreatedAt                 string   `json:"created_at"`
}

// SignupFormRequest is the body for creating or updating a form. Unlike the
// responses, the API reads these fields in camelCase. Nil fields are left
// out, and an empty string clears a nullable field (redirect URL, Turnstile
// keys, senders, welcome subject and Markdown).
//
// Create ignores ConfirmationEmailFrom and the WelcomeEmail fields: set them
// with an update afterwards.
type SignupFormRequest struct {
	Name                  *string  `json:"name,omitempty"`
	Fields                []string `json:"fields,omitempty"`
	ButtonText            *string  `json:"buttonText,omitempty"`
	ButtonColor           *string  `json:"buttonColor,omitempty"`
	SuccessMessage        *string  `json:"successMessage,omitempty"`
	DoubleOptIn           *bool    `json:"doubleOptIn,omitempty"`
	RedirectURL           *string  `json:"redirectUrl,omitempty"`
	CaptchaEnabled        *bool    `json:"captchaEnabled,omitempty"`
	TurnstileSiteKey      *string  `json:"turnstileSiteKey,omitempty"`
	TurnstileSecretKey    *string  `json:"turnstileSecretKey,omitempty"`
	BlockDisposableEmails *bool    `json:"blockDisposableEmails,omitempty"`
	Active                *bool    `json:"active,omitempty"`
	ConfirmationEmailFrom *string  `json:"confirmationEmailFrom,omitempty"`
	WelcomeEmailEnabled   *bool    `json:"welcomeEmailEnabled,omitempty"`
	WelcomeEmailFrom      *string  `json:"welcomeEmailFrom,omitempty"`
	WelcomeEmailSubject   *string  `json:"welcomeEmailSubject,omitempty"`
	WelcomeEmailMarkdown  *string  `json:"welcomeEmailMarkdown,omitempty"`
}

// CreateSignupForm creates a signup form.
func (c *Client) CreateSignupForm(ctx context.Context, req SignupFormRequest) (*SignupForm, error) {
	var out SignupForm
	if err := c.do(ctx, http.MethodPost, "/v1/forms", req, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// GetSignupForm fetches a signup form by ID.
func (c *Client) GetSignupForm(ctx context.Context, id string) (*SignupForm, error) {
	var out SignupForm
	if err := c.do(ctx, http.MethodGet, "/v1/forms/"+url.PathEscape(id), nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// UpdateSignupForm changes the fields that are set in req.
func (c *Client) UpdateSignupForm(ctx context.Context, id string, req SignupFormRequest) (*SignupForm, error) {
	var out SignupForm
	if err := c.do(ctx, http.MethodPatch, "/v1/forms/"+url.PathEscape(id), req, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// DeleteSignupForm deletes a form. Embedded copies of it stop working.
func (c *Client) DeleteSignupForm(ctx context.Context, id string) error {
	return c.do(ctx, http.MethodDelete, "/v1/forms/"+url.PathEscape(id), nil, nil)
}
