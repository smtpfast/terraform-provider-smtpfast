package provider

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"reflect"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

// fakeAPI is an in-memory stand-in for the parts of the SMTPfast API the
// provider uses. It copies the response shapes and normalisation of the real
// handlers (camelCase list rows, trimmed strings, default scopes, revision
// checks), so the plan and apply tests below catch provider/API mismatches
// without credentials or network.
type fakeAPI struct {
	mu    sync.Mutex
	seq   int
	clock time.Time

	keys       map[string]*fakeKey
	webhooks   map[string]*fakeWebhook
	templates  map[string]*fakeTemplate
	inboxes    map[string]*fakeInbox
	properties map[string]*fakeProperty
	domains    map[string]*fakeDomain

	// conflictNextTemplateWrite simulates an edit made in the dashboard right
	// before the next template update or publish arrives.
	conflictNextTemplateWrite bool
}

type fakeKey struct {
	ID, Name, Prefix, CreatedAt, RevokedAt string
	Scopes                                 []string
}

type fakeWebhook struct {
	ID, URL, Format, Secret, CreatedAt string
	Events                             []string
	Active                             bool
}

type fakeTemplate struct {
	ID, Name, CreatedAt, UpdatedAt string
	Content                        map[string]any
	Published                      map[string]any
	PublishedAt, VersionID         *string
}

type fakeInbox struct {
	ID, Name, EmailAddress, DomainID, CreatedAt string
	FromName                                    *string
}

type fakeProperty struct {
	ID, Key, Type, CreatedAt string
	Fallback                 any
}

type fakeDomain struct {
	ID, Domain, Status string
	Receiving          bool
}

// newFakeAPI starts the fake API and returns it with its base URL.
func newFakeAPI(t *testing.T) (*fakeAPI, string) {
	t.Helper()
	f := &fakeAPI{
		clock:      time.Date(2026, 10, 1, 10, 0, 0, 0, time.UTC),
		keys:       map[string]*fakeKey{},
		webhooks:   map[string]*fakeWebhook{},
		templates:  map[string]*fakeTemplate{},
		inboxes:    map[string]*fakeInbox{},
		properties: map[string]*fakeProperty{},
		domains:    map[string]*fakeDomain{},
	}
	srv := httptest.NewServer(http.HandlerFunc(f.serve))
	t.Cleanup(srv.Close)
	return f, srv.URL
}

// skipWithoutTerraform skips plan and apply tests when no Terraform CLI is
// available, so `go test` stays offline: the test framework would otherwise
// download one.
func skipWithoutTerraform(t *testing.T) {
	t.Helper()
	if os.Getenv("TF_ACC_TERRAFORM_PATH") != "" {
		return
	}
	if _, err := exec.LookPath("terraform"); err != nil {
		t.Skip("terraform CLI not found on PATH; skipping plan/apply test against the fake API")
	}
	t.Setenv("CHECKPOINT_DISABLE", "1")
}

func fakeProviderConfig(baseURL string) string {
	return fmt.Sprintf("provider \"smtpfast\" {\n  api_key  = \"test-key\"\n  base_url = %q\n}\n", baseURL)
}

func (f *fakeAPI) nextID(prefix string) string {
	f.seq++
	return fmt.Sprintf("%s_%d", prefix, f.seq)
}

// now advances the clock by a millisecond per call, so every write gets a
// distinct revision, as nextRevision guarantees in the real API.
func (f *fakeAPI) now() string {
	f.clock = f.clock.Add(time.Millisecond)
	return f.clock.Format("2006-01-02T15:04:05.000Z")
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}

func (f *fakeAPI) serve(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()

	if r.Header.Get("Authorization") != "Bearer test-key" {
		writeError(w, http.StatusUnauthorized, "Invalid API key")
		return
	}
	var body map[string]any
	if r.Body != nil {
		_ = json.NewDecoder(r.Body).Decode(&body)
	}
	parts := strings.Split(strings.TrimPrefix(r.URL.Path, "/v1/"), "/")
	id := ""
	if len(parts) > 1 {
		id = parts[1]
	}
	switch parts[0] {
	case "api-keys":
		f.serveAPIKeys(w, r.Method, id, body)
	case "webhooks":
		f.serveWebhooks(w, r.Method, id, body)
	case "templates":
		action := ""
		if len(parts) > 2 {
			action = parts[2]
		}
		f.serveTemplates(w, r.Method, id, action, body)
	case "inboxes":
		f.serveInboxes(w, r.Method, id, body)
	case "contact-properties":
		f.serveProperties(w, r.Method, id, body)
	case "domains":
		f.serveDomains(w, r.Method, id, body)
	default:
		writeError(w, http.StatusNotFound, "Not found")
	}
}

func stringsOf(v any) []string {
	items, _ := v.([]any)
	out := make([]string, 0, len(items))
	seen := map[string]bool{}
	for _, item := range items {
		if s, ok := item.(string); ok && !seen[s] {
			seen[s] = true
			out = append(out, s)
		}
	}
	return out
}

func (f *fakeAPI) serveAPIKeys(w http.ResponseWriter, method, id string, body map[string]any) {
	row := func(k *fakeKey) map[string]any {
		var revoked any
		if k.RevokedAt != "" {
			revoked = k.RevokedAt
		}
		return map[string]any{"id": k.ID, "name": k.Name, "keyPrefix": k.Prefix, "scopes": k.Scopes, "createdAt": k.CreatedAt, "revokedAt": revoked, "lastUsedAt": nil}
	}
	switch {
	case method == http.MethodGet && id == "":
		rows := []map[string]any{}
		for _, k := range f.keys {
			rows = append(rows, row(k))
		}
		writeJSON(w, http.StatusOK, rows)
	case method == http.MethodPost && id == "":
		scopes := append([]string(nil), defaultAPIKeyScopes...)
		if raw, ok := body["scopes"]; ok {
			scopes = stringsOf(raw)
			if len(scopes) == 0 {
				writeError(w, http.StatusBadRequest, "scopes must be a non-empty array")
				return
			}
		}
		k := &fakeKey{ID: f.nextID("key"), Name: body["name"].(string), Prefix: "sf_live_x", CreatedAt: f.now(), Scopes: scopes}
		f.keys[k.ID] = k
		writeJSON(w, http.StatusCreated, map[string]any{"id": k.ID, "name": k.Name, "key": "sf_live_secret_" + k.ID, "prefix": k.Prefix, "scopes": k.Scopes, "created_at": k.CreatedAt})
	case method == http.MethodPatch:
		k, ok := f.keys[id]
		if !ok {
			writeError(w, http.StatusNotFound, "API key not found")
			return
		}
		if name, ok := body["name"].(string); ok {
			k.Name = strings.TrimSpace(name)
		}
		if raw, ok := body["scopes"]; ok {
			k.Scopes = stringsOf(raw)
		}
		writeJSON(w, http.StatusOK, row(k))
	case method == http.MethodDelete:
		k, ok := f.keys[id]
		if !ok {
			writeError(w, http.StatusNotFound, "API key not found")
			return
		}
		if k.RevokedAt != "" {
			writeError(w, http.StatusBadRequest, "API key already revoked")
			return
		}
		k.RevokedAt = f.now()
		writeJSON(w, http.StatusOK, map[string]any{"success": true})
	default:
		// The real API has no GET /v1/api-keys/{id}.
		writeError(w, http.StatusMethodNotAllowed, "Method not allowed")
	}
}

func (f *fakeAPI) serveWebhooks(w http.ResponseWriter, method, id string, body map[string]any) {
	if method == http.MethodPost && id == "" {
		url, _ := body["url"].(string)
		format, _ := body["format"].(string)
		if format == "" {
			format = "standard"
			if strings.Contains(url, "discord.com") {
				format = "discord"
			}
		}
		wh := &fakeWebhook{ID: f.nextID("wh"), URL: url, Events: stringsOf(body["events"]), Format: format, Secret: "whsec_" + strconv.Itoa(f.seq), CreatedAt: f.now(), Active: true}
		f.webhooks[wh.ID] = wh
		writeJSON(w, http.StatusCreated, map[string]any{"id": wh.ID, "url": wh.URL, "events": wh.Events, "format": wh.Format, "secret": wh.Secret, "active": wh.Active, "created_at": wh.CreatedAt})
		return
	}
	wh, ok := f.webhooks[id]
	if !ok {
		writeError(w, http.StatusNotFound, "Webhook not found")
		return
	}
	switch method {
	case http.MethodGet:
		writeJSON(w, http.StatusOK, map[string]any{"id": wh.ID, "url": wh.URL, "events": wh.Events, "format": wh.Format, "active": wh.Active, "createdAt": wh.CreatedAt, "updatedAt": wh.CreatedAt})
	case http.MethodPatch, http.MethodPut:
		if url, ok := body["url"].(string); ok {
			wh.URL = url
		}
		if _, ok := body["events"]; ok {
			wh.Events = stringsOf(body["events"])
		}
		if active, ok := body["active"].(bool); ok {
			wh.Active = active
		}
		if format, ok := body["format"].(string); ok {
			wh.Format = format
		}
		writeJSON(w, http.StatusOK, map[string]any{"id": wh.ID, "url": wh.URL, "events": wh.Events, "format": wh.Format, "active": wh.Active})
	case http.MethodDelete:
		delete(f.webhooks, id)
		writeJSON(w, http.StatusOK, map[string]any{"success": true})
	}
}

var fakeTemplateFields = []string{"alias", "from", "subject", "reply_to", "preview_text", "html", "text", "markdown", "variables"}

func templateError(w http.ResponseWriter, status int, name, msg string) {
	writeJSON(w, status, map[string]any{"statusCode": status, "name": name, "message": msg, "error": msg})
}

func (f *fakeAPI) serveTemplates(w http.ResponseWriter, method, id, action string, body map[string]any) {
	ref := func(t *fakeTemplate) map[string]any {
		status := "draft"
		if t.PublishedAt != nil {
			status = "published"
		}
		return map[string]any{
			"object": "template", "id": t.ID, "updated_at": t.UpdatedAt, "status": status,
			"published_at": t.PublishedAt, "has_unpublished_versions": !reflect.DeepEqual(t.Content, t.Published),
		}
	}
	apply := func(t *fakeTemplate, body map[string]any) {
		if name, ok := body["name"].(string); ok {
			t.Name = strings.TrimSpace(name)
		}
		for _, k := range fakeTemplateFields {
			v, ok := body[k]
			if !ok {
				continue
			}
			switch k {
			case "reply_to":
				if v == nil {
					v = []any{}
				}
			case "variables":
				if v == nil {
					v = []any{}
				}
			case "html", "text", "markdown":
			default:
				if s, ok := v.(string); ok {
					v = strings.TrimSpace(s)
				}
			}
			t.Content[k] = v
		}
	}
	checkRevision := func(t *fakeTemplate) bool {
		if f.conflictNextTemplateWrite {
			f.conflictNextTemplateWrite = false
			t.Content["subject"] = "Edited in the dashboard"
			t.UpdatedAt = f.now()
		}
		if expected, ok := body["expected_updated_at"].(string); ok && expected != t.UpdatedAt {
			templateError(w, http.StatusConflict, "revision_conflict", "This template was changed after you loaded it, so your request was not applied.")
			return false
		}
		return true
	}

	if method == http.MethodPost && id == "" {
		t := &fakeTemplate{ID: f.nextID("tpl"), Content: map[string]any{"reply_to": []any{}, "variables": []any{}}}
		apply(t, body)
		t.CreatedAt = f.now()
		t.UpdatedAt = t.CreatedAt
		f.templates[t.ID] = t
		writeJSON(w, http.StatusCreated, ref(t))
		return
	}
	var t *fakeTemplate
	for _, candidate := range f.templates {
		if candidate.ID == id || candidate.Content["alias"] == id {
			t = candidate
		}
	}
	if t == nil {
		templateError(w, http.StatusNotFound, "not_found", "Template not found")
		return
	}
	switch {
	case method == http.MethodGet:
		out := ref(t)
		out["name"] = t.Name
		out["created_at"] = t.CreatedAt
		out["current_version_id"] = t.VersionID
		for _, k := range fakeTemplateFields {
			out[k] = t.Content[k]
		}
		if rt, _ := t.Content["reply_to"].([]any); len(rt) == 0 {
			out["reply_to"] = nil
		}
		writeJSON(w, http.StatusOK, out)
	case method == http.MethodPatch:
		if !checkRevision(t) {
			return
		}
		apply(t, body)
		t.UpdatedAt = f.now()
		writeJSON(w, http.StatusOK, ref(t))
	case method == http.MethodPost && action == "publish":
		if !checkRevision(t) {
			return
		}
		snapshot := map[string]any{}
		for k, v := range t.Content {
			snapshot[k] = v
		}
		t.Published = snapshot
		t.UpdatedAt = f.now()
		at, version := t.UpdatedAt, f.nextID("ver")
		t.PublishedAt, t.VersionID = &at, &version
		writeJSON(w, http.StatusOK, ref(t))
	case method == http.MethodDelete:
		delete(f.templates, t.ID)
		writeJSON(w, http.StatusOK, map[string]any{"object": "template", "id": t.ID, "deleted": true})
	}
}

func (f *fakeAPI) serveInboxes(w http.ResponseWriter, method, id string, body map[string]any) {
	format := func(in *fakeInbox) map[string]any {
		return map[string]any{"object": "inbox", "id": in.ID, "name": in.Name, "email_address": in.EmailAddress, "domain_id": in.DomainID, "receiving_address": nil, "from_name": in.FromName, "unread": 0, "drafts": 0, "last_received": nil, "created_at": in.CreatedAt}
	}
	if method == http.MethodPost && id == "" {
		address := strings.ToLower(body["email_address"].(string))
		in := &fakeInbox{ID: f.nextID("inb"), EmailAddress: address, Name: address, DomainID: "dom_inbound", CreatedAt: f.now()}
		if name, ok := body["name"].(string); ok {
			in.Name = name
		}
		if fromName, ok := body["from_name"].(string); ok {
			in.FromName = &fromName
		}
		f.inboxes[in.ID] = in
		writeJSON(w, http.StatusCreated, format(in))
		return
	}
	var in *fakeInbox
	for _, candidate := range f.inboxes {
		if candidate.ID == id || candidate.EmailAddress == id {
			in = candidate
		}
	}
	if in == nil {
		writeError(w, http.StatusNotFound, "Inbox not found")
		return
	}
	switch method {
	case http.MethodGet:
		writeJSON(w, http.StatusOK, format(in))
	case http.MethodPatch:
		if name, ok := body["name"].(string); ok {
			in.Name = name
		}
		if v, ok := body["from_name"]; ok {
			if s, isString := v.(string); isString {
				in.FromName = &s
			} else {
				in.FromName = nil
			}
		}
		writeJSON(w, http.StatusOK, map[string]any{"object": "inbox", "id": in.ID})
	case http.MethodDelete:
		delete(f.inboxes, in.ID)
		writeJSON(w, http.StatusOK, map[string]any{"object": "inbox", "id": in.ID, "deleted": true})
	}
}

func (f *fakeAPI) serveProperties(w http.ResponseWriter, method, id string, body map[string]any) {
	envelope := func(p *fakeProperty) map[string]any {
		return map[string]any{"contact_property": map[string]any{"id": p.ID, "key": p.Key, "type": p.Type, "fallback_value": p.Fallback, "created_at": p.CreatedAt}}
	}
	if method == http.MethodPost && id == "" {
		for _, p := range f.properties {
			if p.Key == body["key"] {
				writeError(w, http.StatusConflict, p.Key+" is already declared")
				return
			}
		}
		p := &fakeProperty{ID: f.nextID("cp"), Key: body["key"].(string), Type: body["type"].(string), Fallback: body["fallback_value"], CreatedAt: f.now()}
		f.properties[p.ID] = p
		writeJSON(w, http.StatusCreated, envelope(p))
		return
	}
	p, ok := f.properties[id]
	if !ok {
		writeError(w, http.StatusNotFound, "Contact property not found")
		return
	}
	switch method {
	case http.MethodGet:
		writeJSON(w, http.StatusOK, envelope(p))
	case http.MethodPatch:
		if _, ok := body["key"]; ok {
			writeError(w, http.StatusBadRequest, "key and type cannot be changed")
			return
		}
		p.Fallback = body["fallback_value"]
		writeJSON(w, http.StatusOK, envelope(p))
	case http.MethodDelete:
		delete(f.properties, id)
		writeJSON(w, http.StatusOK, map[string]any{"success": true})
	}
}

func (f *fakeAPI) serveDomains(w http.ResponseWriter, method, id string, body map[string]any) {
	records := func(d *fakeDomain) []map[string]any {
		out := []map[string]any{
			{"type": "CNAME", "name": "tok1._domainkey." + d.Domain, "value": "tok1.dkim.amazonses.com", "priority": nil, "ttl": "Auto"},
			{"type": "TXT", "name": d.Domain, "value": "v=spf1 include:amazonses.com ~all", "priority": nil, "ttl": "Auto"},
			{"type": "TXT", "name": "_dmarc." + d.Domain, "value": "v=DMARC1; p=none;", "priority": nil, "ttl": "Auto"},
			{"type": "MX", "name": "bounce." + d.Domain, "value": "feedback-smtp.us-east-1.amazonses.com", "priority": 10, "ttl": "Auto"},
			{"type": "TXT", "name": "bounce." + d.Domain, "value": "v=spf1 include:amazonses.com ~all", "priority": nil, "ttl": "Auto"},
		}
		if d.Receiving {
			out = append(out, map[string]any{"type": "MX", "name": d.Domain, "value": "inbound-smtp.us-east-1.amazonaws.com", "priority": 10, "purpose": "Inbound mail"})
		}
		return out
	}
	switch {
	case method == http.MethodGet && id == "":
		rows := []map[string]any{}
		for _, d := range f.domains {
			rows = append(rows, map[string]any{"id": d.ID, "domain": d.Domain, "status": d.Status, "receivingEnabled": d.Receiving, "receivingStatus": "disabled"})
		}
		writeJSON(w, http.StatusOK, rows)
		return
	case method == http.MethodPost && id == "":
		d := &fakeDomain{ID: f.nextID("dom"), Domain: body["domain"].(string), Status: "pending"}
		f.domains[d.ID] = d
		// Create answers without a receiving block.
		writeJSON(w, http.StatusCreated, map[string]any{"id": d.ID, "domain": d.Domain, "status": d.Status, "dns_records": records(d), "created_at": f.now()})
		return
	}
	d, ok := f.domains[id]
	if !ok {
		writeError(w, http.StatusNotFound, "Domain not found")
		return
	}
	receiving := func() map[string]any {
		status := "disabled"
		if d.Receiving {
			status = "pending"
		}
		return map[string]any{
			"enabled": d.Receiving, "status": status,
			"mx_record": map[string]any{"name": d.Domain, "type": "MX", "priority": 10, "value": "inbound-smtp.us-east-1.amazonaws.com"},
		}
	}
	switch method {
	case http.MethodGet:
		writeJSON(w, http.StatusOK, map[string]any{"id": d.ID, "domain": d.Domain, "status": d.Status, "receiving": receiving(), "dns_records": records(d)})
	case http.MethodPatch:
		enabled, _ := body["receiving_enabled"].(bool)
		if enabled && d.Status != "verified" {
			writeError(w, http.StatusBadRequest, "Verify this domain, or add it as a subdomain of a domain that is already verified, before enabling receiving.")
			return
		}
		d.Receiving = enabled
		writeJSON(w, http.StatusOK, map[string]any{"id": d.ID, "receiving": receiving()})
	case http.MethodDelete:
		delete(f.domains, id)
		writeJSON(w, http.StatusOK, map[string]any{"success": true, "domain": d.Domain})
	}
}
