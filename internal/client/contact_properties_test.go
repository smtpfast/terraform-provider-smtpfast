package client

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"
)

func TestCreateContactProperty(t *testing.T) {
	c := testServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/v1/contact-properties" {
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		if body["key"] != "seats" || body["type"] != "number" || body["fallback_value"] != 1.0 {
			t.Errorf("unexpected body: %v", body)
		}
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"contact_property":{"id":"cp_1","key":"seats","type":"number","fallback_value":1,"created_at":"2026-10-01T10:00:00.000Z"}}`))
	})

	got, err := c.CreateContactProperty(context.Background(), CreateContactPropertyRequest{Key: "seats", Type: "number", FallbackValue: 1.0})
	if err != nil {
		t.Fatalf("CreateContactProperty: %v", err)
	}
	if got.ID != "cp_1" || got.FallbackValue != 1.0 || got.CreatedAt == "" {
		t.Fatalf("unexpected property: %+v", got)
	}
}

func TestGetContactProperty(t *testing.T) {
	c := testServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/v1/contact-properties/cp_1" {
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
		_, _ = w.Write([]byte(`{"contact_property":{"id":"cp_1","key":"plan","type":"string","fallback_value":null,"created_at":"2026-10-01T10:00:00.000Z"}}`))
	})

	got, err := c.GetContactProperty(context.Background(), "cp_1")
	if err != nil {
		t.Fatalf("GetContactProperty: %v", err)
	}
	if got.Key != "plan" || got.FallbackValue != nil {
		t.Fatalf("unexpected property: %+v", got)
	}
}

// A nil fallback is sent as null, which clears it.
func TestUpdateContactPropertyFallbackClears(t *testing.T) {
	c := testServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPatch || r.URL.Path != "/v1/contact-properties/cp_1" {
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		if v, ok := body["fallback_value"]; !ok || v != nil {
			t.Errorf("fallback_value = %v (sent: %v), want null", v, ok)
		}
		if _, ok := body["key"]; ok {
			t.Errorf("key sent: %v", body)
		}
		_, _ = w.Write([]byte(`{"contact_property":{"id":"cp_1","key":"plan","type":"string","fallback_value":null,"created_at":"2026-10-01T10:00:00.000Z"}}`))
	})

	got, err := c.UpdateContactPropertyFallback(context.Background(), "cp_1", nil)
	if err != nil {
		t.Fatalf("UpdateContactPropertyFallback: %v", err)
	}
	if got.FallbackValue != nil {
		t.Fatalf("unexpected property: %+v", got)
	}
}

func TestDeleteContactProperty(t *testing.T) {
	c := testServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodDelete || r.URL.Path != "/v1/contact-properties/cp_1" {
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
		_, _ = w.Write([]byte(`{"success":true}`))
	})

	if err := c.DeleteContactProperty(context.Background(), "cp_1"); err != nil {
		t.Fatalf("DeleteContactProperty: %v", err)
	}
}
