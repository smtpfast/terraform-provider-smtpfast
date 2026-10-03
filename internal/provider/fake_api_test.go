package provider

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"reflect"
	"slices"
	"sort"
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
	forms      map[string]*fakeForm
	segments   map[string]*fakeSegment
	labels     map[string]*fakeLabel
	members    map[string]*fakeMember
	invites    map[string]*fakeInvite

	// callerRole is the team role of the API key's user ("owner", "admin" or
	// "member"), and callerMemberID their membership. freeTeam turns off
	// inviting, which needs a paid plan.
	callerRole, callerMemberID string
	freeTeam                   bool

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

type fakeForm struct {
	ID, Name, ButtonText, ButtonColor, SuccessMessage, CreatedAt, UpdatedAt string
	Fields                                                                  []string
	DoubleOptIn, CaptchaEnabled, BlockDisposable, Active, WelcomeEnabled    bool
	RedirectURL, SiteKey, SecretKey                                         *string
	ConfirmationFrom, WelcomeFrom, WelcomeSubject, WelcomeMarkdown          *string
}

type fakeSegment struct {
	ID, Name, CreatedAt, UpdatedAt string
	Description, Color             *string
}

type fakeLabel struct {
	ID, InboxID, Name, Color, CreatedAt string
}

type fakeMember struct {
	ID, UserID, Email, Role, CreatedAt string
	Name                               *string
	Billing                            bool
}

type fakeInvite struct {
	ID, Email, Role, CreatedAt, ExpiresAt string
	Accepted, Expired                     bool
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
		forms:      map[string]*fakeForm{},
		segments:   map[string]*fakeSegment{},
		labels:     map[string]*fakeLabel{},
		members:    map[string]*fakeMember{},
		invites:    map[string]*fakeInvite{},
		callerRole: "owner",
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
		if len(parts) > 2 && parts[2] == "labels" {
			labelID := ""
			if len(parts) > 3 {
				labelID = parts[3]
			}
			f.serveInboxLabels(w, r.Method, id, labelID, body)
			return
		}
		f.serveInboxes(w, r.Method, id, body)
	case "contact-properties":
		f.serveProperties(w, r.Method, id, body)
	case "domains":
		f.serveDomains(w, r.Method, id, body)
	case "forms":
		f.serveForms(w, r.Method, id, body)
	case "segments":
		f.serveSegments(w, r.Method, id, body)
	case "team":
		sub := ""
		if len(parts) > 2 {
			sub = parts[2]
		}
		switch id {
		case "invites":
			f.serveInvites(w, r.Method, sub, body)
		case "members":
			f.serveMembers(w, r.Method, sub, body)
		default:
			writeError(w, http.StatusNotFound, "Not found")
		}
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

// fakeNullable applies a nullable text field the way the forms and segments
// APIs do: absent leaves it, null or "" clears it, anything else is stored
// (trimmed when trim is set, and cleared when that leaves nothing).
func fakeNullable(body map[string]any, key string, dst **string, trim bool) {
	v, ok := body[key]
	if !ok {
		return
	}
	str, _ := v.(string)
	if trim {
		str = strings.TrimSpace(str)
	}
	if str == "" {
		*dst = nil
		return
	}
	*dst = &str
}

func (f *fakeAPI) serveForms(w http.ResponseWriter, method, id string, body map[string]any) {
	// The create and list shape: no confirmation or welcome email fields.
	base := func(fm *fakeForm) map[string]any {
		return map[string]any{
			"object": "signup_form", "id": fm.ID, "name": fm.Name, "fields": fm.Fields,
			"button_text": fm.ButtonText, "button_color": fm.ButtonColor, "success_message": fm.SuccessMessage,
			"double_opt_in": fm.DoubleOptIn, "redirect_url": fm.RedirectURL, "captcha_enabled": fm.CaptchaEnabled,
			"captcha_provider": "turnstile", "turnstile_site_key": fm.SiteKey, "turnstile_secret_configured": fm.SecretKey != nil,
			"block_disposable_emails": fm.BlockDisposable, "active": fm.Active, "created_at": fm.CreatedAt, "updated_at": fm.UpdatedAt,
		}
	}
	full := func(fm *fakeForm) map[string]any {
		out := base(fm)
		out["confirmation_email_from"] = fm.ConfirmationFrom
		out["welcome_email_enabled"] = fm.WelcomeEnabled
		out["welcome_email_from"] = fm.WelcomeFrom
		out["welcome_email_subject"] = fm.WelcomeSubject
		out["welcome_email_markdown"] = fm.WelcomeMarkdown
		out["welcome_stats"] = map[string]any{"sent": 0, "skipped": 0, "delivered": 0, "opened": 0, "clicked": 0}
		out["pending_confirmations_count"] = 0
		out["recent_pending_confirmations"] = []any{}
		return out
	}
	// apply mirrors parseFormInput. Create ignores the email settings.
	apply := func(fm *fakeForm, create bool) {
		if v, ok := body["name"].(string); ok {
			fm.Name = strings.TrimSpace(v)
		}
		if raw, ok := body["fields"]; ok {
			fields := stringsOf(raw)
			if !slices.Contains(fields, "email") {
				fields = append([]string{"email"}, fields...)
			}
			fm.Fields = fields
		}
		if v, ok := body["buttonText"].(string); ok {
			fm.ButtonText = strings.TrimSpace(v)
		}
		if v, ok := body["buttonColor"].(string); ok {
			fm.ButtonColor = v
		}
		if v, ok := body["successMessage"].(string); ok {
			fm.SuccessMessage = strings.TrimSpace(v)
		}
		for key, dst := range map[string]*bool{"doubleOptIn": &fm.DoubleOptIn, "captchaEnabled": &fm.CaptchaEnabled, "blockDisposableEmails": &fm.BlockDisposable, "active": &fm.Active} {
			if v, ok := body[key].(bool); ok {
				*dst = v
			}
		}
		fakeNullable(body, "redirectUrl", &fm.RedirectURL, true)
		fakeNullable(body, "turnstileSiteKey", &fm.SiteKey, true)
		fakeNullable(body, "turnstileSecretKey", &fm.SecretKey, true)
		if create {
			return
		}
		if v, ok := body["welcomeEmailEnabled"].(bool); ok {
			fm.WelcomeEnabled = v
		}
		fakeNullable(body, "confirmationEmailFrom", &fm.ConfirmationFrom, true)
		fakeNullable(body, "welcomeEmailFrom", &fm.WelcomeFrom, true)
		fakeNullable(body, "welcomeEmailSubject", &fm.WelcomeSubject, true)
		fakeNullable(body, "welcomeEmailMarkdown", &fm.WelcomeMarkdown, false)
	}
	captchaError := "turnstileSiteKey and turnstileSecretKey are required when captchaEnabled is true"

	if method == http.MethodPost && id == "" {
		if _, ok := body["name"].(string); !ok {
			writeError(w, http.StatusBadRequest, "name is required")
			return
		}
		if len(f.forms) >= 25 {
			writeError(w, http.StatusBadRequest, "Maximum 25 forms per account")
			return
		}
		fm := &fakeForm{
			ID: f.nextID("form"), Fields: []string{"email", "first_name"}, ButtonText: "Subscribe", ButtonColor: "#10b981",
			SuccessMessage: "Thanks for subscribing!", DoubleOptIn: true, BlockDisposable: true, Active: true,
		}
		apply(fm, true)
		if fm.CaptchaEnabled && (fm.SiteKey == nil || fm.SecretKey == nil) {
			writeError(w, http.StatusBadRequest, captchaError)
			return
		}
		fm.CreatedAt = f.now()
		fm.UpdatedAt = fm.CreatedAt
		f.forms[fm.ID] = fm
		writeJSON(w, http.StatusCreated, base(fm))
		return
	}
	if method == http.MethodGet && id == "" {
		rows := []map[string]any{}
		for _, fm := range f.forms {
			rows = append(rows, base(fm))
		}
		writeJSON(w, http.StatusOK, map[string]any{"object": "list", "has_more": false, "data": rows})
		return
	}
	fm, ok := f.forms[id]
	if !ok {
		writeError(w, http.StatusNotFound, "Form not found")
		return
	}
	switch method {
	case http.MethodGet:
		writeJSON(w, http.StatusOK, full(fm))
	case http.MethodPatch:
		next := *fm
		apply(&next, false)
		if next.CaptchaEnabled && (next.SiteKey == nil || next.SecretKey == nil) {
			writeError(w, http.StatusBadRequest, captchaError)
			return
		}
		next.UpdatedAt = f.now()
		*fm = next
		writeJSON(w, http.StatusOK, full(fm))
	case http.MethodDelete:
		delete(f.forms, id)
		writeJSON(w, http.StatusOK, map[string]any{"object": "signup_form", "id": id, "deleted": true})
	}
}

func (f *fakeAPI) serveSegments(w http.ResponseWriter, method, id string, body map[string]any) {
	format := func(sg *fakeSegment) map[string]any {
		return map[string]any{"id": sg.ID, "name": sg.Name, "description": sg.Description, "color": sg.Color, "contact_count": 0, "created_at": sg.CreatedAt, "updated_at": sg.UpdatedAt}
	}
	withObject := func(sg *fakeSegment) map[string]any {
		out := format(sg)
		out["object"] = "segment"
		return out
	}
	nameTaken := func(name, except string) bool {
		for _, other := range f.segments {
			if other.Name == name && other.ID != except {
				return true
			}
		}
		return false
	}
	apply := func(sg *fakeSegment) {
		if v, ok := body["name"].(string); ok {
			sg.Name = strings.TrimSpace(v)
		}
		fakeNullable(body, "description", &sg.Description, true)
		fakeNullable(body, "color", &sg.Color, true)
	}

	switch {
	case method == http.MethodGet && id == "":
		rows := []map[string]any{}
		for _, sg := range f.segments {
			rows = append(rows, format(sg))
		}
		writeJSON(w, http.StatusOK, map[string]any{"object": "list", "data": rows, "total": len(rows), "segment_limit": 10, "tier": "free"})
		return
	case method == http.MethodPost && id == "":
		name, _ := body["name"].(string)
		if strings.TrimSpace(name) == "" {
			writeError(w, http.StatusBadRequest, "name is required")
			return
		}
		sg := &fakeSegment{ID: f.nextID("seg")}
		apply(sg)
		if nameTaken(sg.Name, "") {
			writeError(w, http.StatusConflict, "Segment already exists")
			return
		}
		sg.CreatedAt = f.now()
		sg.UpdatedAt = sg.CreatedAt
		f.segments[sg.ID] = sg
		writeJSON(w, http.StatusCreated, withObject(sg))
		return
	}
	sg, ok := f.segments[id]
	if !ok {
		writeError(w, http.StatusNotFound, "Segment not found")
		return
	}
	switch method {
	case http.MethodGet:
		writeJSON(w, http.StatusOK, withObject(sg))
	case http.MethodPatch:
		_, hasName := body["name"]
		_, hasDescription := body["description"]
		_, hasColor := body["color"]
		if !hasName && !hasDescription && !hasColor {
			writeError(w, http.StatusBadRequest, "No valid fields to update")
			return
		}
		next := *sg
		apply(&next)
		if nameTaken(next.Name, sg.ID) {
			writeError(w, http.StatusConflict, "Segment already exists")
			return
		}
		next.UpdatedAt = f.now()
		*sg = next
		writeJSON(w, http.StatusOK, withObject(sg))
	case http.MethodDelete:
		delete(f.segments, id)
		writeJSON(w, http.StatusOK, map[string]any{"object": "segment", "id": id, "deleted": true})
	}
}

func (f *fakeAPI) serveInboxLabels(w http.ResponseWriter, method, inboxRef, labelID string, body map[string]any) {
	var inbox *fakeInbox
	for _, candidate := range f.inboxes {
		if candidate.ID == inboxRef || candidate.EmailAddress == strings.ToLower(inboxRef) {
			inbox = candidate
		}
	}
	if inbox == nil {
		writeError(w, http.StatusNotFound, "Inbox not found")
		return
	}
	format := func(l *fakeLabel) map[string]any {
		return map[string]any{"object": "inbox_label", "id": l.ID, "name": l.Name, "color": l.Color, "created_at": l.CreatedAt}
	}
	var mine []*fakeLabel
	for _, l := range f.labels {
		if l.InboxID == inbox.ID {
			mine = append(mine, l)
		}
	}
	sort.Slice(mine, func(i, j int) bool { return mine[i].CreatedAt < mine[j].CreatedAt })
	clash := func(name, except string) bool {
		for _, l := range mine {
			if strings.EqualFold(l.Name, name) && l.ID != except {
				return true
			}
		}
		return false
	}

	switch {
	case method == http.MethodGet && labelID == "":
		rows := []map[string]any{}
		for _, l := range mine {
			rows = append(rows, format(l))
		}
		writeJSON(w, http.StatusOK, map[string]any{"object": "list", "data": rows})
		return
	case method == http.MethodPost && labelID == "":
		name, _ := body["name"].(string)
		name = strings.TrimSpace(name)
		if name == "" {
			writeError(w, http.StatusBadRequest, "name must not be empty")
			return
		}
		color := "mauve"
		if v, ok := body["color"]; ok {
			c, _ := v.(string)
			if !slices.Contains(inboxLabelColors, c) {
				writeError(w, http.StatusBadRequest, "color must be one of "+strings.Join(inboxLabelColors, ", "))
				return
			}
			color = c
		}
		if len(mine) >= 100 {
			writeError(w, http.StatusUnprocessableEntity, "An inbox can have at most 100 labels")
			return
		}
		if clash(name, "") {
			writeError(w, http.StatusConflict, "A label named "+name+" already exists")
			return
		}
		l := &fakeLabel{ID: f.nextID("lbl"), InboxID: inbox.ID, Name: name, Color: color, CreatedAt: f.now()}
		f.labels[l.ID] = l
		writeJSON(w, http.StatusCreated, format(l))
		return
	}
	l, ok := f.labels[labelID]
	if !ok || l.InboxID != inbox.ID {
		writeError(w, http.StatusNotFound, "Label not found")
		return
	}
	switch method {
	case http.MethodPatch:
		name, hasName := body["name"].(string)
		name = strings.TrimSpace(name)
		if hasName && clash(name, l.ID) {
			writeError(w, http.StatusConflict, "A label named "+name+" already exists")
			return
		}
		if hasName {
			l.Name = name
		}
		if c, ok := body["color"].(string); ok {
			l.Color = c
		}
		writeJSON(w, http.StatusOK, map[string]any{"object": "inbox_label", "id": l.ID})
	case http.MethodDelete:
		delete(f.labels, l.ID)
		writeJSON(w, http.StatusOK, map[string]any{"object": "inbox_label", "id": l.ID, "deleted": true})
	default:
		writeError(w, http.StatusMethodNotAllowed, "Method not allowed")
	}
}

func (f *fakeAPI) callerManages() bool { return f.callerRole == "owner" || f.callerRole == "admin" }

// acceptInvite is a person accepting their invitation: they join the team
// with the invited role, and the invitation leaves the pending list.
func (f *fakeAPI) acceptInvite(email string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, inv := range f.invites {
		if inv.Email == email && !inv.Accepted {
			inv.Accepted = true
			m := &fakeMember{ID: f.nextID("mem"), UserID: f.nextID("usr"), Email: email, Role: inv.Role, CreatedAt: f.now()}
			f.members[m.ID] = m
		}
	}
}

func (f *fakeAPI) serveInvites(w http.ResponseWriter, method, id string, body map[string]any) {
	switch {
	case method == http.MethodGet && id == "":
		if !f.callerManages() {
			writeError(w, http.StatusForbidden, "Only team owners and admins can view invitations")
			return
		}
		var pending []*fakeInvite
		for _, inv := range f.invites {
			if !inv.Accepted && !inv.Expired {
				pending = append(pending, inv)
			}
		}
		sort.Slice(pending, func(i, j int) bool { return pending[i].CreatedAt > pending[j].CreatedAt })
		rows := []map[string]any{}
		for _, inv := range pending {
			rows = append(rows, map[string]any{"object": "team_invite", "id": inv.ID, "email": inv.Email, "role": inv.Role, "created_at": inv.CreatedAt, "expires_at": inv.ExpiresAt})
		}
		writeJSON(w, http.StatusOK, map[string]any{"object": "list", "has_more": false, "data": rows})
	case method == http.MethodPost && id == "":
		if !f.callerManages() {
			writeError(w, http.StatusForbidden, "Only team owners and admins can invite members")
			return
		}
		email, _ := body["email"].(string)
		email = strings.ToLower(strings.TrimSpace(email))
		if !strings.Contains(email, "@") {
			writeError(w, http.StatusBadRequest, "A valid email is required")
			return
		}
		role := "member"
		if v, ok := body["role"].(string); ok {
			role = strings.ToLower(strings.TrimSpace(v))
		}
		if role != "admin" && role != "member" {
			writeError(w, http.StatusBadRequest, "Role must be ADMIN or MEMBER")
			return
		}
		if f.freeTeam {
			writeError(w, http.StatusForbidden, "Inviting teammates requires a paid plan. Upgrade to add your team.")
			return
		}
		for _, m := range f.members {
			if m.Email == email {
				writeError(w, http.StatusConflict, "That person is already a member of this team")
				return
			}
		}
		// Upserted by address: inviting again reuses the invitation.
		var inv *fakeInvite
		for _, existing := range f.invites {
			if existing.Email == email {
				inv = existing
			}
		}
		if inv == nil {
			inv = &fakeInvite{ID: f.nextID("inv"), Email: email, CreatedAt: f.now()}
			f.invites[inv.ID] = inv
		}
		inv.Role, inv.Accepted, inv.Expired = role, false, false
		inv.ExpiresAt = f.clock.Add(7 * 24 * time.Hour).Format("2006-01-02T15:04:05.000Z")
		writeJSON(w, http.StatusCreated, map[string]any{"object": "team_invite", "id": inv.ID, "email": inv.Email, "role": inv.Role, "expires_at": inv.ExpiresAt})
	case method == http.MethodDelete && id != "":
		if !f.callerManages() {
			writeError(w, http.StatusForbidden, "Only team owners and admins can revoke invitations")
			return
		}
		if _, ok := f.invites[id]; !ok {
			writeError(w, http.StatusNotFound, "Invite not found")
			return
		}
		delete(f.invites, id)
		writeJSON(w, http.StatusOK, map[string]any{"object": "team_invite", "id": id, "deleted": true})
	default:
		writeError(w, http.StatusMethodNotAllowed, "Method not allowed")
	}
}

func (f *fakeAPI) serveMembers(w http.ResponseWriter, method, id string, body map[string]any) {
	billing := func(m *fakeMember) bool { return m.Role == "owner" || m.Billing }
	owners := func() int {
		n := 0
		for _, m := range f.members {
			if m.Role == "owner" {
				n++
			}
		}
		return n
	}
	if method == http.MethodGet && id == "" {
		var all []*fakeMember
		for _, m := range f.members {
			all = append(all, m)
		}
		sort.Slice(all, func(i, j int) bool { return all[i].CreatedAt < all[j].CreatedAt })
		rows := []map[string]any{}
		for _, m := range all {
			rows = append(rows, map[string]any{"object": "team_member", "id": m.ID, "user_id": m.UserID, "email": m.Email, "name": m.Name, "role": m.Role, "can_manage_billing": billing(m), "created_at": m.CreatedAt})
		}
		writeJSON(w, http.StatusOK, map[string]any{"object": "list", "has_more": false, "data": rows})
		return
	}
	switch method {
	case http.MethodPatch:
		rawRole, wantsRole := body["role"]
		rawBilling, wantsBilling := body["can_manage_billing"]
		newBilling, billingIsBool := rawBilling.(bool)
		switch {
		case !wantsRole && !wantsBilling:
			writeError(w, http.StatusBadRequest, "Send role and/or can_manage_billing")
			return
		case wantsBilling && !billingIsBool:
			writeError(w, http.StatusBadRequest, "can_manage_billing must be a boolean")
			return
		case wantsBilling && f.callerRole != "owner":
			writeError(w, http.StatusForbidden, "Only an owner can change billing access")
			return
		case wantsRole && !f.callerManages():
			writeError(w, http.StatusForbidden, "Only team owners and admins can change roles")
			return
		}
		role, _ := rawRole.(string)
		role = strings.ToLower(strings.TrimSpace(role))
		if wantsRole && !slices.Contains(teamRoles, role) {
			writeError(w, http.StatusBadRequest, "role must be owner, admin or member")
			return
		}
		m, ok := f.members[id]
		if !ok {
			writeError(w, http.StatusNotFound, "Member not found")
			return
		}
		if wantsRole {
			if f.callerRole == "admin" && (m.Role == "owner" || role == "owner") {
				writeError(w, http.StatusForbidden, "You don't have permission to change this member's role")
				return
			}
			if m.Role == "owner" && role != "owner" && owners() <= 1 {
				writeError(w, http.StatusBadRequest, "A team must have at least one owner")
				return
			}
			m.Role = role
		}
		if wantsBilling {
			m.Billing = newBilling
		}
		writeJSON(w, http.StatusOK, map[string]any{"object": "team_member", "id": m.ID, "role": m.Role, "can_manage_billing": billing(m)})
	case http.MethodDelete:
		if !f.callerManages() {
			writeError(w, http.StatusForbidden, "Only team owners and admins can remove members")
			return
		}
		m, ok := f.members[id]
		if !ok {
			writeError(w, http.StatusNotFound, "Member not found")
			return
		}
		if m.ID == f.callerMemberID {
			writeError(w, http.StatusBadRequest, "Use “Leave team” to remove yourself")
			return
		}
		if f.callerRole == "admin" && m.Role == "owner" {
			writeError(w, http.StatusForbidden, "You don't have permission to remove this member")
			return
		}
		if m.Role == "owner" && owners() <= 1 {
			writeError(w, http.StatusBadRequest, "A team must have at least one owner")
			return
		}
		delete(f.members, id)
		writeJSON(w, http.StatusOK, map[string]any{"object": "team_member", "id": id, "deleted": true})
	default:
		writeError(w, http.StatusMethodNotAllowed, "Method not allowed")
	}
}
