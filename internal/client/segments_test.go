package client

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"
)

// Description and color are always sent, so nil clears them.
func TestUpdateSegmentSendsNulls(t *testing.T) {
	c := testServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPatch || r.URL.Path != "/v1/segments/seg_1" {
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		for _, k := range []string{"description", "color"} {
			if v, ok := body[k]; !ok || v != nil {
				t.Errorf("%s = %v (sent: %v), want null", k, v, ok)
			}
		}
		_, _ = w.Write([]byte(`{"object":"segment","id":"seg_1","name":"VIP","description":null,"color":null,"contact_count":3,"created_at":"2026-10-01T10:00:00.000Z","updated_at":"2026-10-01T11:00:00.000Z"}`))
	})

	got, err := c.UpdateSegment(context.Background(), "seg_1", SegmentRequest{Name: "VIP"})
	if err != nil {
		t.Fatalf("UpdateSegment: %v", err)
	}
	if got.Name != "VIP" || got.Description != nil || got.CreatedAt == "" {
		t.Fatalf("unexpected segment: %+v", got)
	}
}

func TestListSegments(t *testing.T) {
	c := testServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/v1/segments" {
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
		_, _ = w.Write([]byte(`{"object":"list","data":[{"id":"seg_1","name":"VIP","description":"Top customers","color":"#10b981","contact_count":3,"created_at":"2026-10-01T10:00:00.000Z","updated_at":"2026-10-01T10:00:00.000Z"}],"total":1,"segment_limit":10,"tier":"free"}`))
	})

	got, err := c.ListSegments(context.Background())
	if err != nil {
		t.Fatalf("ListSegments: %v", err)
	}
	if len(got) != 1 || got[0].ID != "seg_1" || got[0].Color == nil || *got[0].Color != "#10b981" {
		t.Fatalf("unexpected segments: %+v", got)
	}
}
