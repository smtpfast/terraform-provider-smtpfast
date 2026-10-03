package client

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"
)

func boolPtr(b bool) *bool { return &b }

// The forms API reads camelCase keys and answers in snake_case.
func TestCreateSignupFormSendsCamelCase(t *testing.T) {
	c := testServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/v1/forms" {
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		if body["name"] != "Newsletter" || body["buttonText"] != "Join" || body["doubleOptIn"] != false {
			t.Errorf("unexpected body: %v", body)
		}
		for _, k := range []string{"redirectUrl", "turnstileSecretKey", "welcomeEmailEnabled"} {
			if _, ok := body[k]; ok {
				t.Errorf("%s sent although unset: %v", k, body)
			}
		}
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"object":"signup_form","id":"form_1","name":"Newsletter","fields":["email"],"button_text":"Join","button_color":"#10b981","success_message":"Thanks","double_opt_in":false,"redirect_url":null,"captcha_enabled":false,"captcha_provider":"turnstile","turnstile_site_key":null,"turnstile_secret_configured":false,"block_disposable_emails":true,"active":true,"created_at":"2026-10-01T10:00:00.000Z","updated_at":"2026-10-01T10:00:00.000Z"}`))
	})

	got, err := c.CreateSignupForm(context.Background(), SignupFormRequest{
		Name:        strPtr("Newsletter"),
		ButtonText:  strPtr("Join"),
		DoubleOptIn: boolPtr(false),
	})
	if err != nil {
		t.Fatalf("CreateSignupForm: %v", err)
	}
	if got.ID != "form_1" || got.ButtonText != "Join" || got.DoubleOptIn || !got.Active || got.RedirectURL != nil {
		t.Fatalf("unexpected form: %+v", got)
	}
}

// An empty string is how a nullable field is cleared.
func TestUpdateSignupFormClearsWithEmptyString(t *testing.T) {
	c := testServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPatch || r.URL.Path != "/v1/forms/form_1" {
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		if v, ok := body["redirectUrl"]; !ok || v != "" {
			t.Errorf("redirectUrl = %v (sent: %v), want \"\"", v, ok)
		}
		if body["welcomeEmailEnabled"] != true || body["welcomeEmailSubject"] != "Welcome" {
			t.Errorf("unexpected body: %v", body)
		}
		_, _ = w.Write([]byte(`{"object":"signup_form","id":"form_1","name":"Newsletter","fields":["email"],"welcome_email_enabled":true,"welcome_email_subject":"Welcome","welcome_stats":{"sent":0}}`))
	})

	got, err := c.UpdateSignupForm(context.Background(), "form_1", SignupFormRequest{
		RedirectURL:         strPtr(""),
		WelcomeEmailEnabled: boolPtr(true),
		WelcomeEmailSubject: strPtr("Welcome"),
	})
	if err != nil {
		t.Fatalf("UpdateSignupForm: %v", err)
	}
	if !got.WelcomeEmailEnabled || got.WelcomeEmailSubject == nil || *got.WelcomeEmailSubject != "Welcome" {
		t.Fatalf("unexpected form: %+v", got)
	}
}
