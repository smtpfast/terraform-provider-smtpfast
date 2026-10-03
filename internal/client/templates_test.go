package client

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"
)

func strPtr(s string) *string { return &s }

func TestCreateTemplateLeavesOutUnsetFields(t *testing.T) {
	c := testServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/v1/templates" {
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		if body["name"] != "welcome" || body["html"] != "<p>Hi {{{NAME}}}</p>" {
			t.Errorf("unexpected body: %v", body)
		}
		for _, k := range []string{"alias", "subject", "text", "markdown", "reply_to"} {
			if _, ok := body[k]; ok {
				t.Errorf("%s sent although unset", k)
			}
		}
		vars, _ := body["variables"].([]any)
		if len(vars) != 1 || vars[0].(map[string]any)["fallback_value"] != 25.0 {
			t.Errorf("variables = %v", body["variables"])
		}
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"id":"tpl_1","object":"template","updated_at":"2026-10-01T10:00:00.000Z","status":"draft","published_at":null,"has_unpublished_versions":true}`))
	})

	got, err := c.CreateTemplate(context.Background(), TemplateFields{
		Name:      "welcome",
		HTML:      strPtr("<p>Hi {{{NAME}}}</p>"),
		Variables: []TemplateVariable{{Key: "PRICE", Type: "number", FallbackValue: 25.0}},
	})
	if err != nil {
		t.Fatalf("CreateTemplate: %v", err)
	}
	if got.ID != "tpl_1" || got.UpdatedAt != "2026-10-01T10:00:00.000Z" || !got.HasUnpublishedVersions {
		t.Fatalf("unexpected result: %+v", got)
	}
}

// An update sends every field, null for the unset ones, so a field removed
// from the configuration is cleared, plus the revision it expects.
func TestUpdateTemplateClearsUnsetFields(t *testing.T) {
	c := testServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPatch || r.URL.Path != "/v1/templates/tpl_1" {
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		if body["expected_updated_at"] != "2026-10-01T10:00:00.000Z" {
			t.Errorf("expected_updated_at = %v", body["expected_updated_at"])
		}
		for _, k := range []string{"alias", "subject", "html", "reply_to", "variables", "text"} {
			v, ok := body[k]
			if !ok || v != nil {
				t.Errorf("%s = %v (sent: %v), want null", k, v, ok)
			}
		}
		if body["markdown"] != "# Hi" {
			t.Errorf("markdown = %v", body["markdown"])
		}
		_, _ = w.Write([]byte(`{"id":"tpl_1","object":"template","updated_at":"2026-10-01T10:00:01.000Z","status":"published","published_at":"2026-09-30T10:00:00.000Z","has_unpublished_versions":true}`))
	})

	got, err := c.UpdateTemplate(context.Background(), "tpl_1", TemplateFields{Name: "welcome", Markdown: strPtr("# Hi")}, "2026-10-01T10:00:00.000Z")
	if err != nil {
		t.Fatalf("UpdateTemplate: %v", err)
	}
	if got.UpdatedAt != "2026-10-01T10:00:01.000Z" || !got.HasUnpublishedVersions {
		t.Fatalf("unexpected result: %+v", got)
	}
}

func TestPublishTemplateSendsExpectedRevision(t *testing.T) {
	c := testServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/v1/templates/tpl_1/publish" {
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
		var body map[string]string
		_ = json.NewDecoder(r.Body).Decode(&body)
		if body["expected_updated_at"] != "2026-10-01T10:00:01.000Z" {
			t.Errorf("expected_updated_at = %q", body["expected_updated_at"])
		}
		_, _ = w.Write([]byte(`{"id":"tpl_1","object":"template","updated_at":"2026-10-01T10:00:02.000Z","status":"published","published_at":"2026-10-01T10:00:02.000Z","has_unpublished_versions":false}`))
	})

	got, err := c.PublishTemplate(context.Background(), "tpl_1", "2026-10-01T10:00:01.000Z")
	if err != nil {
		t.Fatalf("PublishTemplate: %v", err)
	}
	if got.Status != "published" || got.HasUnpublishedVersions {
		t.Fatalf("unexpected result: %+v", got)
	}
}

func TestGetTemplate(t *testing.T) {
	c := testServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/v1/templates/order-confirmation" {
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
		_, _ = w.Write([]byte(`{
			"object":"template","id":"tpl_1","current_version_id":"ver_1","alias":"order-confirmation","name":"Order",
			"created_at":"2026-09-01T10:00:00.000Z","updated_at":"2026-10-01T10:00:00.000Z","status":"published",
			"published_at":"2026-10-01T10:00:00.000Z","from":"Acme <orders@acme.com>","subject":"Order {{{ID}}}",
			"reply_to":["help@acme.com"],"html":"<p>{{{PRICE}}}</p>","text":null,"markdown":null,"preview_text":null,
			"variables":[{"id":"v1","key":"PRICE","type":"number","fallback_value":25,"created_at":"x","updated_at":"x"},
			             {"id":"v2","key":"ID","type":"string","fallback_value":null}],
			"has_unpublished_versions":false,"category":null}`))
	})

	got, err := c.GetTemplate(context.Background(), "order-confirmation")
	if err != nil {
		t.Fatalf("GetTemplate: %v", err)
	}
	if got.ID != "tpl_1" || *got.CurrentVersionID != "ver_1" || got.Text != nil || len(got.ReplyTo) != 1 {
		t.Fatalf("unexpected template: %+v", got)
	}
	if len(got.Variables) != 2 || got.Variables[0].FallbackValue != 25.0 || got.Variables[1].FallbackValue != nil {
		t.Fatalf("unexpected variables: %+v", got.Variables)
	}
}

func TestDeleteTemplateNotFound(t *testing.T) {
	c := testServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodDelete || r.URL.Path != "/v1/templates/tpl_gone" {
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"statusCode":404,"name":"not_found","message":"Template not found","error":"Template not found"}`))
	})

	err := c.DeleteTemplate(context.Background(), "tpl_gone")
	if !IsNotFound(err) {
		t.Fatalf("IsNotFound = false, err = %v", err)
	}
}
