package client

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"
)

func TestCreateTeamInvite(t *testing.T) {
	c := testServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/v1/team/invites" {
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
		var body map[string]string
		_ = json.NewDecoder(r.Body).Decode(&body)
		if body["email"] != "ada@example.com" || body["role"] != "admin" {
			t.Errorf("unexpected body: %v", body)
		}
		w.WriteHeader(http.StatusCreated)
		// The create response has no created_at.
		_, _ = w.Write([]byte(`{"object":"team_invite","id":"inv_1","email":"ada@example.com","role":"admin","expires_at":"2026-10-08T10:00:00.000Z"}`))
	})

	got, err := c.CreateTeamInvite(context.Background(), "ada@example.com", "admin")
	if err != nil {
		t.Fatalf("CreateTeamInvite: %v", err)
	}
	if got.ID != "inv_1" || got.Role != "admin" || got.ExpiresAt == "" || got.CreatedAt != "" {
		t.Fatalf("unexpected invite: %+v", got)
	}
}

// Only the fields that are set go out: sending can_manage_billing at all
// needs an owner, even when the value would not change.
func TestUpdateTeamMemberLeavesOutUnsetFields(t *testing.T) {
	c := testServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPatch || r.URL.Path != "/v1/team/members/mem_1" {
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		if body["role"] != "admin" {
			t.Errorf("role = %v", body["role"])
		}
		if _, ok := body["can_manage_billing"]; ok {
			t.Errorf("can_manage_billing sent although unset: %v", body)
		}
		_, _ = w.Write([]byte(`{"object":"team_member","id":"mem_1","role":"admin","can_manage_billing":false}`))
	})

	if err := c.UpdateTeamMember(context.Background(), "mem_1", UpdateTeamMemberRequest{Role: strPtr("admin")}); err != nil {
		t.Fatalf("UpdateTeamMember: %v", err)
	}
}

func TestListTeamMembers(t *testing.T) {
	c := testServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/v1/team/members" {
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
		_, _ = w.Write([]byte(`{"object":"list","has_more":false,"data":[{"object":"team_member","id":"mem_1","user_id":"usr_1","email":"ada@example.com","name":null,"role":"owner","can_manage_billing":true,"created_at":"2026-10-01T10:00:00.000Z"}]}`))
	})

	got, err := c.ListTeamMembers(context.Background())
	if err != nil {
		t.Fatalf("ListTeamMembers: %v", err)
	}
	if len(got) != 1 || got[0].UserID != "usr_1" || got[0].Name != nil || !got[0].CanManageBilling {
		t.Fatalf("unexpected members: %+v", got)
	}
}
