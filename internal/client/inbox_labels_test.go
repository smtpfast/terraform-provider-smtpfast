package client

import (
	"context"
	"net/http"
	"testing"
)

func TestGetInboxLabelSearchesTheList(t *testing.T) {
	c := testServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/v1/inboxes/inb_1/labels" {
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
		_, _ = w.Write([]byte(`{"object":"list","data":[{"object":"inbox_label","id":"lbl_1","name":"Urgent","color":"crimson","created_at":"2026-10-01T10:00:00.000Z"}]}`))
	})

	got, err := c.GetInboxLabel(context.Background(), "inb_1", "lbl_1")
	if err != nil {
		t.Fatalf("GetInboxLabel: %v", err)
	}
	if got.Name != "Urgent" || got.Color != "crimson" {
		t.Fatalf("unexpected label: %+v", got)
	}
	if _, err := c.GetInboxLabel(context.Background(), "inb_1", "lbl_2"); !IsNotFound(err) {
		t.Fatalf("missing label: got %v, want a 404", err)
	}
}
