package client

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func testServer(t *testing.T, handler http.HandlerFunc) *Client {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	return New("test-key", srv.URL, "test-agent")
}

func TestCreateDomain(t *testing.T) {
	c := testServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/v1/domains" {
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer test-key" {
			t.Errorf("Authorization = %q, want Bearer test-key", got)
		}
		var body map[string]string
		_ = json.NewDecoder(r.Body).Decode(&body)
		if body["domain"] != "mail.example.com" {
			t.Errorf("domain = %q", body["domain"])
		}
		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(Domain{
			ID:     "dom_1",
			Domain: "mail.example.com",
			Status: "pending",
			DNSRecords: []DNSRecord{
				{Type: "TXT", Name: "mail.example.com", Value: "v=spf1 include:smtpfa.st ~all"},
			},
		})
	})

	got, err := c.CreateDomain(context.Background(), "mail.example.com")
	if err != nil {
		t.Fatalf("CreateDomain: %v", err)
	}
	if got.ID != "dom_1" || got.Status != "pending" || len(got.DNSRecords) != 1 {
		t.Fatalf("unexpected domain: %+v", got)
	}
}

func TestSetDomainReceiving(t *testing.T) {
	c := testServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPatch || r.URL.Path != "/v1/domains/dom_1" {
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
		var body map[string]bool
		_ = json.NewDecoder(r.Body).Decode(&body)
		if !body["receiving_enabled"] {
			t.Errorf("receiving_enabled = %v, want true", body["receiving_enabled"])
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"id":        "dom_1",
			"receiving": map[string]any{"enabled": true, "status": "pending"},
		})
	})

	got, err := c.SetDomainReceiving(context.Background(), "dom_1", true)
	if err != nil {
		t.Fatalf("SetDomainReceiving: %v", err)
	}
	if !got.Enabled || got.Status != "pending" {
		t.Fatalf("unexpected receiving: %+v", got)
	}
}

func TestGetDomainNotFound(t *testing.T) {
	c := testServer(t, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"error":"Domain not found"}`))
	})

	_, err := c.GetDomain(context.Background(), "missing")
	if err == nil {
		t.Fatal("expected error")
	}
	if !IsNotFound(err) {
		t.Fatalf("IsNotFound = false, err = %v", err)
	}
}

func TestAPIErrorMessage(t *testing.T) {
	c := testServer(t, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error":"domain is required"}`))
	})

	_, err := c.CreateDomain(context.Background(), "")
	apiErr, ok := err.(*APIError)
	if !ok {
		t.Fatalf("expected *APIError, got %T", err)
	}
	if apiErr.StatusCode != http.StatusBadRequest || apiErr.Message != "domain is required" {
		t.Fatalf("unexpected APIError: %+v", apiErr)
	}
}

func TestNewDefaultsBaseURL(t *testing.T) {
	c := New("k", "", "")
	if c.BaseURL != DefaultBaseURL {
		t.Fatalf("BaseURL = %q, want %q", c.BaseURL, DefaultBaseURL)
	}
}

func TestCreateWebhook(t *testing.T) {
	c := testServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/webhooks" {
			t.Errorf("path = %s", r.URL.Path)
		}
		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(Webhook{
			ID:     "wh_1",
			URL:    "https://example.com/hook",
			Events: []string{"email.delivered"},
			Active: true,
		})
	})

	got, err := c.CreateWebhook(context.Background(), CreateWebhookRequest{
		URL:    "https://example.com/hook",
		Events: []string{"email.delivered"},
	})
	if err != nil {
		t.Fatalf("CreateWebhook: %v", err)
	}
	if got.ID != "wh_1" || !got.Active {
		t.Fatalf("unexpected webhook: %+v", got)
	}
}

func TestAPIErrorCode(t *testing.T) {
	c := testServer(t, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusConflict)
		_, _ = w.Write([]byte(`{"statusCode":409,"name":"revision_conflict","message":"changed","error":"changed"}`))
	})

	_, err := c.PublishTemplate(context.Background(), "tpl_1", "2026-10-01T00:00:00.000Z")
	if got := ErrorCode(err); got != RevisionConflictCode {
		t.Fatalf("ErrorCode = %q, want %q (err = %v)", got, RevisionConflictCode, err)
	}
	if IsNotFound(err) {
		t.Fatal("IsNotFound = true for a 409")
	}
}

// The list object is the current shape; the bare array is what the API
// answered before October 2026, and both have to keep working.
func TestListDomains(t *testing.T) {
	for name, body := range map[string]string{
		"list object": `{"object":"list","has_more":false,"data":[{"id":"dom_1","domain":"mail.example.com","status":"verified","receiving_enabled":false}]}`,
		"bare array":  `[{"id":"dom_1","domain":"mail.example.com","status":"verified","receivingEnabled":false}]`,
	} {
		t.Run(name, func(t *testing.T) {
			c := testServer(t, func(w http.ResponseWriter, r *http.Request) {
				if r.Method != http.MethodGet || r.URL.Path != "/v1/domains" {
					t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
				}
				_, _ = w.Write([]byte(body))
			})

			got, err := c.ListDomains(context.Background())
			if err != nil {
				t.Fatalf("ListDomains: %v", err)
			}
			if len(got) != 1 || got[0].ID != "dom_1" || got[0].Domain != "mail.example.com" {
				t.Fatalf("unexpected domains: %+v", got)
			}
		})
	}
}

func TestCreateAPIKey(t *testing.T) {
	c := testServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/v1/api-keys" {
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
		var body CreateAPIKeyRequest
		_ = json.NewDecoder(r.Body).Decode(&body)
		if body.Name != "ci" || len(body.Scopes) != 1 || body.Scopes[0] != "email:send" {
			t.Errorf("unexpected body: %+v", body)
		}
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"id":"key_1","name":"ci","key":"sf_live_secret","prefix":"sf_live_a","scopes":["email:send"],"created_at":"2026-10-01T10:00:00.000Z"}`))
	})

	got, err := c.CreateAPIKey(context.Background(), CreateAPIKeyRequest{Name: "ci", Scopes: []string{"email:send"}})
	if err != nil {
		t.Fatalf("CreateAPIKey: %v", err)
	}
	if got.Key != "sf_live_secret" || got.Prefix != "sf_live_a" || got.CreatedAt != "2026-10-01T10:00:00.000Z" {
		t.Fatalf("unexpected key: %+v", got)
	}
}

// The list endpoint answers with database field names (keyPrefix, createdAt,
// revokedAt), not the snake_case the create call uses.
func TestGetAPIKeySearchesTheList(t *testing.T) {
	c := testServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/v1/api-keys" {
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
		_, _ = w.Write([]byte(`{"object":"list","has_more":false,"data":[
			{"id":"key_2","name":"old","keyPrefix":"sf_live_b","scopes":["email:send"],"createdAt":"2026-09-01T10:00:00.000Z","revokedAt":"2026-09-02T10:00:00.000Z","lastUsedAt":null},
			{"id":"key_1","name":"ci","keyPrefix":"sf_live_a","scopes":["email:send","domain:read"],"createdAt":"2026-10-01T10:00:00.000Z","revokedAt":null,"lastUsedAt":null}
		]}`))
	})

	got, err := c.GetAPIKey(context.Background(), "key_1")
	if err != nil {
		t.Fatalf("GetAPIKey: %v", err)
	}
	if got.Name != "ci" || got.Prefix != "sf_live_a" || got.CreatedAt != "2026-10-01T10:00:00.000Z" || got.RevokedAt != "" || len(got.Scopes) != 2 {
		t.Fatalf("unexpected key: %+v", got)
	}

	revoked, err := c.GetAPIKey(context.Background(), "key_2")
	if err != nil {
		t.Fatalf("GetAPIKey(revoked): %v", err)
	}
	if revoked.RevokedAt == "" {
		t.Fatalf("RevokedAt not decoded: %+v", revoked)
	}

	if _, err := c.GetAPIKey(context.Background(), "key_missing"); !IsNotFound(err) {
		t.Fatalf("missing key: IsNotFound = false, err = %v", err)
	}
}

func TestUpdateAPIKeySendsOnlyChangedFields(t *testing.T) {
	c := testServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPatch || r.URL.Path != "/v1/api-keys/key_1" {
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		if _, ok := body["name"]; ok {
			t.Errorf("name sent although unchanged: %v", body)
		}
		if scopes, ok := body["scopes"].([]any); !ok || len(scopes) != 2 {
			t.Errorf("scopes = %v", body["scopes"])
		}
		_, _ = w.Write([]byte(`{"id":"key_1","name":"ci","keyPrefix":"sf_live_a","scopes":["email:send","logs:read"],"createdAt":"2026-10-01T10:00:00.000Z","revokedAt":null}`))
	})

	got, err := c.UpdateAPIKey(context.Background(), "key_1", UpdateAPIKeyRequest{Scopes: []string{"email:send", "logs:read"}})
	if err != nil {
		t.Fatalf("UpdateAPIKey: %v", err)
	}
	if got.Prefix != "sf_live_a" || len(got.Scopes) != 2 || got.Scopes[1] != "logs:read" {
		t.Fatalf("unexpected key: %+v", got)
	}
}

func TestCreateWebhookReturnsSigningSecret(t *testing.T) {
	c := testServer(t, func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		if body["format"] != "slack" {
			t.Errorf("format = %v", body["format"])
		}
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"id":"wh_1","url":"https://hooks.slack.com/x","events":["email.bounced"],"format":"slack","secret":"whsec","active":true,"created_at":"2026-10-01T10:00:00.000Z"}`))
	})

	got, err := c.CreateWebhook(context.Background(), CreateWebhookRequest{URL: "https://hooks.slack.com/x", Events: []string{"email.bounced"}, Format: "slack"})
	if err != nil {
		t.Fatalf("CreateWebhook: %v", err)
	}
	if got.SigningSecret != "whsec" || got.Format != "slack" || got.CreatedAt == "" {
		t.Fatalf("unexpected webhook: %+v", got)
	}
}

// GET answers with createdAt; PATCH answers without any timestamp.
func TestWebhookReadAndUpdateShapes(t *testing.T) {
	c := testServer(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			_, _ = w.Write([]byte(`{"id":"wh_1","url":"https://example.com/h","events":["email.sent"],"format":"standard","active":false,"createdAt":"2026-10-01T10:00:00.000Z","updatedAt":"2026-10-02T10:00:00.000Z"}`))
		case http.MethodPatch:
			var body map[string]any
			_ = json.NewDecoder(r.Body).Decode(&body)
			if body["active"] != false {
				t.Errorf("active = %v, want false", body["active"])
			}
			if _, ok := body["url"]; ok {
				t.Errorf("url sent although nil: %v", body)
			}
			_, _ = w.Write([]byte(`{"id":"wh_1","url":"https://example.com/h","events":["email.sent"],"format":"standard","active":false}`))
		default:
			t.Errorf("unexpected method %s", r.Method)
		}
	})

	got, err := c.GetWebhook(context.Background(), "wh_1")
	if err != nil {
		t.Fatalf("GetWebhook: %v", err)
	}
	if got.CreatedAt != "2026-10-01T10:00:00.000Z" || got.Active {
		t.Fatalf("unexpected webhook: %+v", got)
	}

	active := false
	updated, err := c.UpdateWebhook(context.Background(), "wh_1", UpdateWebhookRequest{Active: &active})
	if err != nil {
		t.Fatalf("UpdateWebhook: %v", err)
	}
	if updated.Active || updated.CreatedAt != "" {
		t.Fatalf("unexpected webhook: %+v", updated)
	}
}

func TestListAPIKeysReadsTheOldBareArray(t *testing.T) {
	c := testServer(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`[{"id":"key_1","name":"ci","keyPrefix":"sf_live_a","scopes":["email:send"],"createdAt":"2026-10-01T10:00:00.000Z","revokedAt":null}]`))
	})

	got, err := c.ListAPIKeys(context.Background())
	if err != nil {
		t.Fatalf("ListAPIKeys: %v", err)
	}
	if len(got) != 1 || got[0].ID != "key_1" || got[0].Prefix != "sf_live_a" {
		t.Fatalf("unexpected keys: %+v", got)
	}
}
