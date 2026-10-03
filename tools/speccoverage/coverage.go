package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
)

// Operation is one method and path from the OpenAPI spec.
type Operation struct {
	Method      string
	Path        string
	OperationID string
	Summary     string
}

func (o Operation) String() string { return o.Method + " " + o.Path }

var methodOrder = map[string]int{
	"GET": 0, "POST": 1, "PUT": 2, "PATCH": 3, "DELETE": 4, "HEAD": 5, "OPTIONS": 6, "TRACE": 7,
}

// ParseSpec returns the operations in an OpenAPI 3 document, sorted by path
// and then by method.
func ParseSpec(data []byte) ([]Operation, error) {
	var doc struct {
		OpenAPI string                                `json:"openapi"`
		Paths   map[string]map[string]json.RawMessage `json:"paths"`
	}
	if err := json.Unmarshal(data, &doc); err != nil {
		return nil, fmt.Errorf("parse spec: %w", err)
	}
	if !strings.HasPrefix(doc.OpenAPI, "3.") {
		return nil, fmt.Errorf("parse spec: want an OpenAPI 3 document, got openapi=%q", doc.OpenAPI)
	}

	var ops []Operation
	for path, item := range doc.Paths {
		for key, raw := range item {
			method := strings.ToUpper(key)
			if _, ok := methodOrder[method]; !ok {
				continue // parameters, summary, $ref and other path item fields
			}
			var op struct {
				OperationID string `json:"operationId"`
				Summary     string `json:"summary"`
			}
			if err := json.Unmarshal(raw, &op); err != nil {
				return nil, fmt.Errorf("parse spec: %s %s: %w", method, path, err)
			}
			ops = append(ops, Operation{Method: method, Path: path, OperationID: op.OperationID, Summary: op.Summary})
		}
	}
	if len(ops) == 0 {
		return nil, errors.New("parse spec: no operations found")
	}
	sort.Slice(ops, func(i, j int) bool {
		if ops[i].Path != ops[j].Path {
			return ops[i].Path < ops[j].Path
		}
		return methodOrder[ops[i].Method] < methodOrder[ops[j].Method]
	})
	return ops, nil
}

// rule matches operations by optional method and path pattern. In a pattern,
// {name} matches any path parameter (the name does not have to agree with the
// spec), * matches any one segment, and a trailing ** matches the rest of the
// path, including nothing.
type rule struct {
	raw      string
	method   string // empty matches every method
	segments []string
}

func parseRule(s string) (rule, error) {
	r := rule{raw: s}
	fields := strings.Fields(s)
	var path string
	switch len(fields) {
	case 1:
		path = fields[0]
	case 2:
		r.method, path = fields[0], fields[1]
		if _, ok := methodOrder[r.method]; !ok {
			return rule{}, fmt.Errorf("rule %q: unknown method %q (use upper case)", s, r.method)
		}
	default:
		return rule{}, fmt.Errorf("rule %q: want \"METHOD /path\" or \"/path\"", s)
	}
	if !strings.HasPrefix(path, "/") {
		return rule{}, fmt.Errorf("rule %q: path must start with /", s)
	}
	r.segments = splitPath(path)
	for i, seg := range r.segments {
		switch {
		case seg == "":
			return rule{}, fmt.Errorf("rule %q: empty path segment", s)
		case seg == "**" && i != len(r.segments)-1:
			return rule{}, fmt.Errorf("rule %q: ** is only allowed at the end", s)
		case seg != "*" && seg != "**" && strings.Contains(seg, "*"):
			return rule{}, fmt.Errorf("rule %q: * must be a whole segment", s)
		}
	}
	return r, nil
}

func splitPath(p string) []string { return strings.Split(strings.TrimPrefix(p, "/"), "/") }

func isParam(seg string) bool {
	return len(seg) > 2 && strings.HasPrefix(seg, "{") && strings.HasSuffix(seg, "}")
}

func (r rule) exact() bool {
	if r.method == "" {
		return false
	}
	for _, seg := range r.segments {
		if seg == "*" || seg == "**" {
			return false
		}
	}
	return true
}

func (r rule) matches(op Operation) bool {
	if r.method != "" && r.method != op.Method {
		return false
	}
	path := splitPath(op.Path)
	for i, p := range r.segments {
		if p == "**" {
			return true
		}
		if i >= len(path) {
			return false
		}
		switch {
		case p == "*":
		case isParam(p):
			// A parameter only matches a parameter, so /domains/{id} does
			// not also claim /domains/claim.
			if !isParam(path[i]) {
				return false
			}
		case p != path[i]:
			return false
		}
	}
	return len(r.segments) == len(path)
}

type coveredRule struct {
	rule
	resource string
}

type ignoredRule struct {
	rule
	reason string
}

// Coverage is the compiled form of spec-coverage.json.
type Coverage struct {
	covered []coveredRule
	ignored []ignoredRule
}

// LoadCoverage parses and validates a coverage file.
func LoadCoverage(data []byte) (*Coverage, error) {
	var file struct {
		Comment string              `json:"$comment"`
		Covered map[string][]string `json:"covered"`
		Ignored []struct {
			Reason string   `json:"reason"`
			Rules  []string `json:"rules"`
		} `json:"ignored"`
	}
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&file); err != nil {
		return nil, fmt.Errorf("parse coverage file: %w", err)
	}

	c := &Coverage{}
	resources := make([]string, 0, len(file.Covered))
	for name := range file.Covered {
		resources = append(resources, name)
	}
	sort.Strings(resources)
	for _, name := range resources {
		if strings.TrimSpace(name) == "" {
			return nil, errors.New("coverage file: covered entry with an empty resource name")
		}
		for _, s := range file.Covered[name] {
			r, err := parseRule(s)
			if err != nil {
				return nil, fmt.Errorf("coverage file: covered by %s: %w", name, err)
			}
			if !r.exact() {
				return nil, fmt.Errorf("coverage file: covered by %s: rule %q must be one exact operation (METHOD /path, no wildcards)", name, s)
			}
			c.covered = append(c.covered, coveredRule{rule: r, resource: name})
		}
	}
	for i, g := range file.Ignored {
		if strings.TrimSpace(g.Reason) == "" {
			return nil, fmt.Errorf("coverage file: ignored group %d has no reason", i+1)
		}
		if len(g.Rules) == 0 {
			return nil, fmt.Errorf("coverage file: ignored group %d (%q) has no rules", i+1, g.Reason)
		}
		for _, s := range g.Rules {
			r, err := parseRule(s)
			if err != nil {
				return nil, fmt.Errorf("coverage file: ignored: %w", err)
			}
			c.ignored = append(c.ignored, ignoredRule{rule: r, reason: g.Reason})
		}
	}
	return c, nil
}

// Report is the result of checking a spec against the coverage file.
type Report struct {
	Total   int
	Covered int
	Ignored int
	// Unclassified lists operations no rule matches.
	Unclassified []Operation
	// UnusedRules lists rules that match no operation. A covered rule here can
	// mean the provider calls an endpoint the API no longer has.
	UnusedRules []string
}

// Check classifies every operation. A covered rule wins over an ignored one.
func (c *Coverage) Check(ops []Operation) Report {
	rep := Report{Total: len(ops)}
	usedCovered := make([]bool, len(c.covered))
	usedIgnored := make([]bool, len(c.ignored))
	for _, op := range ops {
		covered, ignored := false, false
		for i, r := range c.covered {
			if r.matches(op) {
				usedCovered[i], covered = true, true
			}
		}
		for i, r := range c.ignored {
			if r.matches(op) {
				usedIgnored[i], ignored = true, true
			}
		}
		switch {
		case covered:
			rep.Covered++
		case ignored:
			rep.Ignored++
		default:
			rep.Unclassified = append(rep.Unclassified, op)
		}
	}
	for i, r := range c.covered {
		if !usedCovered[i] {
			rep.UnusedRules = append(rep.UnusedRules, fmt.Sprintf("covered by %s: %s", r.resource, r.raw))
		}
	}
	for i, r := range c.ignored {
		if !usedIgnored[i] {
			rep.UnusedRules = append(rep.UnusedRules, "ignored: "+r.raw)
		}
	}
	return rep
}
