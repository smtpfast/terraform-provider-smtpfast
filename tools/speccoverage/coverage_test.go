package main

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func op(method, path string) Operation { return Operation{Method: method, Path: path} }

func TestRuleMatches(t *testing.T) {
	tests := []struct {
		rule string
		op   Operation
		want bool
	}{
		{"GET /v1/domains", op("GET", "/v1/domains"), true},
		{"GET /v1/domains", op("POST", "/v1/domains"), false},
		{"/v1/domains", op("POST", "/v1/domains"), true},
		{"GET /v1/domains", op("GET", "/v1/domains/{id}"), false},
		// Parameter names do not have to agree with the spec.
		{"GET /v1/inboxes/{id}", op("GET", "/v1/inboxes/{inbox_id}"), true},
		// A parameter does not match a literal segment.
		{"GET /v1/domains/{id}", op("GET", "/v1/domains/claim"), false},
		{"GET /v1/domains/claim", op("GET", "/v1/domains/{id}"), false},
		{"/v1/domains/*", op("GET", "/v1/domains/claim"), true},
		{"/v1/domains/*", op("GET", "/v1/domains/{id}"), true},
		{"/v1/domains/*", op("GET", "/v1/domains"), false},
		{"/v1/domains/*", op("POST", "/v1/domains/{id}/verify"), false},
		// ** matches the rest of the path, including nothing.
		{"/v1/emails/**", op("POST", "/v1/emails"), true},
		{"/v1/emails/**", op("GET", "/v1/emails/receiving/{id}/attachments"), true},
		{"/v1/emails/**", op("GET", "/v1/emailsx"), false},
		{"/v1/emails/**", op("GET", "/v1"), false},
		{"POST /v1/emails/**", op("GET", "/v1/emails/{id}"), false},
		{"/v1/inboxes/{id}/threads/**", op("GET", "/v1/inboxes/{inbox_id}/threads/{thread_id}/emails"), true},
		{"/v1/inboxes/{id}/threads/**", op("GET", "/v1/inboxes/{inbox_id}/labels"), false},
	}
	for _, tt := range tests {
		r, err := parseRule(tt.rule)
		if err != nil {
			t.Fatalf("parseRule(%q): %v", tt.rule, err)
		}
		if got := r.matches(tt.op); got != tt.want {
			t.Errorf("%q matches %s = %v, want %v", tt.rule, tt.op, got, tt.want)
		}
	}
}

func TestParseRuleRejects(t *testing.T) {
	for _, s := range []string{
		"",
		"get /v1/domains",
		"FETCH /v1/domains",
		"GET v1/domains",
		"GET /v1/domains extra",
		"/v1//domains",
		"/v1/domains/",
		"/v1/**/domains",
		"/v1/dom*",
	} {
		if _, err := parseRule(s); err == nil {
			t.Errorf("parseRule(%q): want an error", s)
		}
	}
}

func TestLoadCoverageRejects(t *testing.T) {
	for name, data := range map[string]string{
		"unknown field":       `{"covered": {}, "ignore": []}`,
		"wildcard in covered": `{"covered": {"smtpfast_domain": ["GET /v1/domains/*"]}}`,
		"no method covered":   `{"covered": {"smtpfast_domain": ["/v1/domains"]}}`,
		"empty resource name": `{"covered": {" ": ["GET /v1/domains"]}}`,
		"missing reason":      `{"ignored": [{"rules": ["/v1/logs/**"]}]}`,
		"no rules":            `{"ignored": [{"reason": "Reporting data."}]}`,
		"bad ignored rule":    `{"ignored": [{"reason": "Reporting data.", "rules": ["logs"]}]}`,
		"not json":            `covered:`,
	} {
		if _, err := LoadCoverage([]byte(data)); err == nil {
			t.Errorf("%s: want an error", name)
		}
	}
}

const testSpec = `{
  "openapi": "3.1.0",
  "paths": {
    "/v1/domains": {
      "get": {"operationId": "listDomains", "summary": "List domains"},
      "post": {"operationId": "addDomain", "summary": "Add a domain"}
    },
    "/v1/domains/{id}": {
      "parameters": [{"name": "id", "in": "path"}],
      "delete": {"operationId": "deleteDomain"},
      "get": {"operationId": "getDomain"}
    },
    "/v1/domains/claim": {
      "get": {"operationId": "getDomainClaimRecord"}
    },
    "/v1/emails": {
      "post": {"operationId": "sendEmail"}
    },
    "/v1/emails/{id}/cancel": {
      "post": {"operationId": "cancelEmail"}
    },
    "/v1/segments": {
      "post": {"operationId": "createSegment", "summary": "Create a segment | manual"}
    }
  }
}`

const testCoverage = `{
  "$comment": "test",
  "covered": {
    "smtpfast_domain": ["POST /v1/domains", "GET /v1/domains/{id}", "DELETE /v1/domains/{id}", "GET /v1/domains"],
    "smtpfast_gone": ["GET /v1/gone"]
  },
  "ignored": [
    {"reason": "Runtime.", "rules": ["/v1/emails/**", "/v1/logs/**"]},
    {"reason": "Overlaps a covered rule.", "rules": ["GET /v1/domains"]}
  ]
}`

func TestCheck(t *testing.T) {
	ops, err := ParseSpec([]byte(testSpec))
	if err != nil {
		t.Fatal(err)
	}
	cov, err := LoadCoverage([]byte(testCoverage))
	if err != nil {
		t.Fatal(err)
	}
	rep := cov.Check(ops)

	if rep.Total != 8 || rep.Covered != 4 || rep.Ignored != 2 {
		t.Errorf("totals = %d/%d/%d, want 8 total, 4 covered, 2 ignored", rep.Total, rep.Covered, rep.Ignored)
	}
	var got []string
	for _, op := range rep.Unclassified {
		got = append(got, op.String())
	}
	if want := []string{"GET /v1/domains/claim", "POST /v1/segments"}; !reflect.DeepEqual(got, want) {
		t.Errorf("unclassified = %v, want %v", got, want)
	}
	// The ignored GET /v1/domains rule still counts as used even though the
	// covered rule wins.
	if want := []string{"covered by smtpfast_gone: GET /v1/gone", "ignored: /v1/logs/**"}; !reflect.DeepEqual(rep.UnusedRules, want) {
		t.Errorf("unused rules = %v, want %v", rep.UnusedRules, want)
	}
}

func TestParseSpecSortsAndSkipsPathFields(t *testing.T) {
	ops, err := ParseSpec([]byte(testSpec))
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, op := range ops {
		got = append(got, op.String())
	}
	want := []string{
		"GET /v1/domains", "POST /v1/domains",
		"GET /v1/domains/claim",
		"GET /v1/domains/{id}", "DELETE /v1/domains/{id}",
		"POST /v1/emails",
		"POST /v1/emails/{id}/cancel",
		"POST /v1/segments",
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("operations = %v\nwant %v", got, want)
	}
}

func TestParseSpecRejects(t *testing.T) {
	for name, data := range map[string]string{
		"not json":      `<html>`,
		"swagger 2":     `{"swagger": "2.0", "paths": {"/a": {"get": {}}}}`,
		"no operations": `{"openapi": "3.1.0", "paths": {"/a": {"parameters": []}}}`,
	} {
		if _, err := ParseSpec([]byte(data)); err == nil {
			t.Errorf("%s: want an error", name)
		}
	}
}

func TestFormatMarkdown(t *testing.T) {
	if got := formatMarkdown(Report{Total: 3, Covered: 3}, "spec-coverage.json"); got != "" {
		t.Errorf("nothing to report: got %q, want empty", got)
	}

	rep := Report{Unclassified: []Operation{{Method: "POST", Path: "/v1/segments", OperationID: "createSegment", Summary: "Create a segment | manual"}}}
	got := formatMarkdown(rep, "spec-coverage.json")
	for _, want := range []string{
		"1 SMTPfast API operation is not classified",
		"| POST | `/v1/segments` | `createSegment` | Create a segment \\| manual |",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("markdown missing %q:\n%s", want, got)
		}
	}
	if strings.Contains(got, "match no operation") {
		t.Errorf("markdown has an unused rules section with no unused rules:\n%s", got)
	}

	// Totals do not appear, so the issue body only changes when a list does.
	again := formatMarkdown(Report{Total: 200, Covered: 50, Ignored: 149, Unclassified: rep.Unclassified}, "spec-coverage.json")
	if again != got {
		t.Errorf("body changed with the totals:\n%s\n---\n%s", got, again)
	}
}

func TestRunWithLocalFiles(t *testing.T) {
	dir := t.TempDir()
	specPath := filepath.Join(dir, "openapi.json")
	covPath := filepath.Join(dir, "coverage.json")
	if err := os.WriteFile(specPath, []byte(testSpec), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(covPath, []byte(testCoverage), 0o600); err != nil {
		t.Fatal(err)
	}

	var out strings.Builder
	if err := run([]string{"-spec", specPath, "-coverage", covPath}, &out); err != nil {
		t.Fatalf("run: %v", err)
	}
	for _, want := range []string{"8 operations, 4 covered, 2 ignored, 2 not classified", "GET     /v1/domains/claim", "covered by smtpfast_gone"} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("text output missing %q:\n%s", want, out.String())
		}
	}

	if err := run([]string{"-spec", specPath, "-coverage", covPath, "-format", "json"}, &out); err == nil {
		t.Error("unknown format: want an error")
	}
}

// The checked-in file must load, so a typo fails CI instead of the daily run.
func TestCheckedInCoverageFile(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "..", "spec-coverage.json"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := LoadCoverage(data); err != nil {
		t.Fatal(err)
	}
}
