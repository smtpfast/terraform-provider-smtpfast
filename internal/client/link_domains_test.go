package client

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestCreateLinkDomain(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/v1/link-domains" {
			t.Fatalf("unexpected %s %s", r.Method, r.URL.Path)
		}
		var body map[string]string
		_ = json.NewDecoder(r.Body).Decode(&body)
		if body["hostname"] != "links.example.com" {
			t.Fatalf("hostname = %q", body["hostname"])
		}
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"object":"link_domain","id":"ld_1","hostname":"links.example.com","domain_id":"dom_1","domain":"example.com","status":"pending","ssl_status":null,"last_error":null,"dns_records":[{"type":"CNAME","name":"links.example.com","value":"links.smtpfa.st","ttl":"Auto","purpose":"Points the link domain at SMTPfast","required":true,"status":"pending"}],"verified_at":null,"last_checked_at":null,"created_at":"2026-10-11T09:00:00.000Z"}`))
	}))
	defer srv.Close()

	got, err := New("k", srv.URL, "").CreateLinkDomain(context.Background(), "links.example.com")
	if err != nil {
		t.Fatalf("CreateLinkDomain: %v", err)
	}
	if got.ID != "ld_1" || got.DomainID == nil || *got.DomainID != "dom_1" || len(got.DNSRecords) != 1 || got.DNSRecords[0].Value != "links.smtpfa.st" || !got.DNSRecords[0].Required {
		t.Fatalf("unexpected link domain: %+v", got)
	}
}

func TestCreateLinkDomainPlanError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte(`{"error":"Custom link domains are on paid plans.","code":"plan_upgrade_required"}`))
	}))
	defer srv.Close()

	_, err := New("k", srv.URL, "").CreateLinkDomain(context.Background(), "links.example.com")
	if ErrorCode(err) != "plan_upgrade_required" {
		t.Fatalf("code = %q, err = %v", ErrorCode(err), err)
	}
}
