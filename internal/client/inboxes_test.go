package client

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"
)

const inboxJSON = `{"object":"inbox","id":"inb_1","name":"Support","email_address":"support@example.com","domain_id":"dom_1","receiving_address":null,"from_name":"Ada from Support","unread":0,"drafts":0,"last_received":null,"created_at":"2026-10-01T10:00:00.000Z"}`

func TestCreateInbox(t *testing.T) {
	c := testServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/v1/inboxes" {
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		if body["email_address"] != "support@example.com" || body["from_name"] != "Ada from Support" {
			t.Errorf("unexpected body: %v", body)
		}
		if _, ok := body["name"]; ok {
			t.Errorf("name sent although unset: %v", body)
		}
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(inboxJSON))
	})

	got, err := c.CreateInbox(context.Background(), CreateInboxRequest{EmailAddress: "support@example.com", FromName: strPtr("Ada from Support")})
	if err != nil {
		t.Fatalf("CreateInbox: %v", err)
	}
	if got.ID != "inb_1" || got.DomainID != "dom_1" || got.FromName == nil || *got.FromName != "Ada from Support" {
		t.Fatalf("unexpected inbox: %+v", got)
	}
}

func TestGetInboxByAddress(t *testing.T) {
	c := testServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/v1/inboxes/support@example.com" {
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
		_, _ = w.Write([]byte(inboxJSON))
	})

	got, err := c.GetInbox(context.Background(), "support@example.com")
	if err != nil {
		t.Fatalf("GetInbox: %v", err)
	}
	if got.ID != "inb_1" || got.CreatedAt == "" {
		t.Fatalf("unexpected inbox: %+v", got)
	}
}

// from_name is always sent, so a nil value clears it.
func TestUpdateInboxClearsFromName(t *testing.T) {
	c := testServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPatch || r.URL.Path != "/v1/inboxes/inb_1" {
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		if v, ok := body["from_name"]; !ok || v != nil {
			t.Errorf("from_name = %v (sent: %v), want null", v, ok)
		}
		if body["name"] != "Help desk" {
			t.Errorf("name = %v", body["name"])
		}
		_, _ = w.Write([]byte(`{"object":"inbox","id":"inb_1"}`))
	})

	if err := c.UpdateInbox(context.Background(), "inb_1", UpdateInboxRequest{Name: strPtr("Help desk")}); err != nil {
		t.Fatalf("UpdateInbox: %v", err)
	}
}

func TestDeleteInbox(t *testing.T) {
	c := testServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodDelete || r.URL.Path != "/v1/inboxes/inb_1" {
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
		_, _ = w.Write([]byte(`{"object":"inbox","id":"inb_1","deleted":true}`))
	})

	if err := c.DeleteInbox(context.Background(), "inb_1"); err != nil {
		t.Fatalf("DeleteInbox: %v", err)
	}
}
