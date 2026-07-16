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
