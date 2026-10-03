package provider

import (
	"context"
	"fmt"
	"math"
	"regexp"
	"strconv"
	"strings"
	"unicode"

	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

// allAPIKeyScopes is every scope a key can hold, in the API's order.
var allAPIKeyScopes = []string{
	"email:send",
	"email:read",
	"domain:read",
	"domain:write",
	"contact:read",
	"contact:write",
	"form:read",
	"form:write",
	"webhook:read",
	"webhook:write",
	"logs:read",
	"inbound:read",
	"inbound:delete",
	"team:read",
	"team:manage",
	"apikey:manage",
}

// defaultAPIKeyScopes is what the API grants a key created without scopes:
// everything except logs:read, inbound:read, inbound:delete, team:read,
// team:manage and apikey:manage.
var defaultAPIKeyScopes = allAPIKeyScopes[:10]

// webhookEvents are the event types a webhook can subscribe to.
var webhookEvents = []string{
	"email.scheduled",
	"email.sent",
	"email.delivered",
	"email.delivery_delayed",
	"email.bounced",
	"email.complained",
	"email.opened",
	"email.clicked",
	"email.failed",
	"email.suppressed",
	"email.unsubscribed",
	"email.received",
	"domain.created",
	"domain.updated",
	"domain.deleted",
	"contact.created",
	"contact.updated",
	"contact.deleted",
	"contact.subscribed",
}

var webhookFormats = []string{"standard", "discord", "slack"}

var variableTypes = []string{"string", "number"}

// Reserved keys, compared lowercased. Templates reserve these because the
// renderer answers them itself; contact properties reserve the built-in
// merge tags.
var (
	reservedTemplateVariableKeys = []string{"first_name", "firstname", "last_name", "lastname", "email", "unsubscribe_url", "resend_unsubscribe_url", "contact", "this"}
	reservedContactPropertyKeys  = []string{"email", "first_name", "firstname", "last_name", "lastname", "unsubscribe_url", "resend_unsubscribe_url"}
)

var (
	// The API's hostname rule, without the 253 character limit, which is a
	// separate length check (RE2 has no lookahead).
	domainNameRegexp        = regexp.MustCompile(`^([a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?\.)+[a-z]{2,63}$`)
	templateAliasRegexp     = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,99}$`)
	templateVariableRegexp  = regexp.MustCompile(`^[A-Za-z0-9_]{1,50}$`)
	contactPropertyKeyRegex = regexp.MustCompile(`^[A-Za-z0-9_.-]{1,64}$`)
)

// jsLength is the length the API measures: JavaScript string length, in
// UTF-16 code units, so a character outside the BMP such as an emoji counts 2.
func jsLength(s string) int {
	n := 0
	for _, r := range s {
		if r > 0xFFFF {
			n += 2
		} else {
			n++
		}
	}
	return n
}

// isJSSpace reports whether JavaScript's String.prototype.trim removes r.
// That is Go's unicode.IsSpace without U+0085, plus U+FEFF.
func isJSSpace(r rune) bool {
	return r == '\uFEFF' || (r != '\u0085' && unicode.IsSpace(r))
}

// jsLengthBetween checks the length the way the API does (see jsLength).
func jsLengthBetween(minLen, maxLen int) validator.String {
	return stringFunc{
		desc: fmt.Sprintf("must be %d to %d characters long", minLen, maxLen),
		fn: func(s string) string {
			if n := jsLength(s); n < minLen || n > maxLen {
				return fmt.Sprintf("must be %d to %d characters long (emoji count as 2), got %d", minLen, maxLen, n)
			}
			return ""
		},
	}
}

func jsLengthAtMost(maxLen int) validator.String { return jsLengthBetween(0, maxLen) }

// notBlank refuses a value that is empty after the API trims it.
func notBlank() validator.String {
	return stringFunc{
		desc: "must not be empty or only whitespace",
		fn: func(s string) string {
			if strings.TrimFunc(s, isJSSpace) == "" {
				return "must not be empty or only whitespace"
			}
			return ""
		},
	}
}

// trimmedLine validates a single-line text field the API trims, up to maxLen
// characters. The value must already be trimmed, or the API would store a
// different value from the plan.
func trimmedLine(maxLen int) []validator.String {
	return []validator.String{
		jsLengthBetween(1, maxLen),
		stringFunc{
			desc: "must be one line without leading or trailing whitespace",
			fn: func(s string) string {
				if strings.ContainsAny(s, "\r\n\u2028\u2029") || strings.TrimFunc(s, isJSSpace) != s {
					return "must be one line without leading or trailing whitespace"
				}
				return ""
			},
		},
	}
}

// plainName checks a display name in the form the API stores: the API turns
// every whitespace run into one space and trims, so only single ASCII spaces
// between words come back unchanged. No <, > or @.
func plainName() validator.String {
	const msg = "must be a plain name: single spaces between words, no leading or trailing whitespace, and no <, > or @"
	return stringFunc{
		desc: msg,
		fn: func(s string) string {
			if strings.ContainsAny(s, "<>@") || strings.Join(strings.FieldsFunc(s, isJSSpace), " ") != s {
				return msg
			}
			return ""
		},
	}
}

// stringFunc is a string validator backed by a function that returns an
// error message, or "" when the value is valid.
type stringFunc struct {
	desc string
	fn   func(string) string
}

func (v stringFunc) Description(context.Context) string         { return v.desc }
func (v stringFunc) MarkdownDescription(context.Context) string { return v.desc }

func (v stringFunc) ValidateString(_ context.Context, req validator.StringRequest, resp *validator.StringResponse) {
	if req.ConfigValue.IsNull() || req.ConfigValue.IsUnknown() {
		return
	}
	if msg := v.fn(req.ConfigValue.ValueString()); msg != "" {
		resp.Diagnostics.AddAttributeError(req.Path, "Invalid value", fmt.Sprintf("%s: %s", req.Path, msg))
	}
}

// notReserved refuses keys in reserved, compared case-insensitively.
func notReserved(reserved []string) validator.String {
	return stringFunc{
		desc: "must not be a reserved key: " + strings.Join(reserved, ", "),
		fn: func(s string) string {
			for _, r := range reserved {
				if strings.EqualFold(s, r) {
					return fmt.Sprintf("%q is reserved and cannot be used", s)
				}
			}
			return ""
		},
	}
}

// domainName checks a domain the way the API does, and insists on the form
// the API stores (lowercase, no trailing dot) so the plan matches the result.
func domainName() validator.String {
	return stringFunc{
		desc: "must be a lowercase domain name such as mail.example.com",
		fn: func(s string) string {
			if s != strings.ToLower(s) {
				return fmt.Sprintf("use lowercase (%q): the API stores domain names in lowercase", strings.ToLower(s))
			}
			if strings.HasSuffix(s, ".") {
				return "remove the trailing dot"
			}
			if len(s) > 253 || !domainNameRegexp.MatchString(s) {
				return fmt.Sprintf("%q is not a valid domain name, expected something like mail.example.com", s)
			}
			return ""
		},
	}
}

// plainEmailAddress checks for a bare, lowercase address such as
// support@example.com.
func plainEmailAddress() validator.String {
	return stringFunc{
		desc: "must be a plain lowercase email address such as support@example.com",
		fn: func(s string) string {
			if s != strings.ToLower(s) {
				return fmt.Sprintf("use lowercase (%q): the API stores addresses in lowercase", strings.ToLower(s))
			}
			at := strings.LastIndex(s, "@")
			if at < 1 || strings.ContainsAny(s, " \t\r\n<>,;\"") || strings.Count(s, "@") != 1 {
				return fmt.Sprintf("%q must be a plain address such as support@example.com, without a display name", s)
			}
			if host := s[at+1:]; len(host) > 253 || !domainNameRegexp.MatchString(host) {
				return fmt.Sprintf("%q does not end in a valid domain", s)
			}
			return ""
		},
	}
}

// fallbackToAPI converts a fallback value from configuration, always a string
// in Terraform, to the JSON value the API expects for typ.
func fallbackToAPI(typ string, v types.String) (any, error) {
	if v.IsNull() || v.IsUnknown() {
		return nil, nil
	}
	if typ != "number" {
		return v.ValueString(), nil
	}
	f, err := strconv.ParseFloat(strings.TrimSpace(v.ValueString()), 64)
	if err != nil || math.IsInf(f, 0) || math.IsNaN(f) {
		return nil, fmt.Errorf("%q is not a number, and a number variable needs a numeric fallback_value such as \"25\" or \"9.99\"", v.ValueString())
	}
	return f, nil
}

// fallbackFromAPI converts an API fallback value back to the Terraform string.
// A number keeps the string already in state or plan when it means the same
// value ("25.0" and 25), so a different spelling is not seen as drift.
func fallbackFromAPI(v any, prior types.String) types.String {
	switch val := v.(type) {
	case nil:
		return types.StringNull()
	case string:
		return types.StringValue(val)
	case float64:
		if !prior.IsNull() && !prior.IsUnknown() {
			if p, err := strconv.ParseFloat(strings.TrimSpace(prior.ValueString()), 64); err == nil && p == val {
				return prior
			}
		}
		return types.StringValue(strconv.FormatFloat(val, 'f', -1, 64))
	default:
		return types.StringValue(fmt.Sprint(val))
	}
}

// sameStringSet reports whether a and b hold the same strings, ignoring order.
func sameStringSet(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	seen := make(map[string]int, len(a))
	for _, s := range a {
		seen[s]++
	}
	for _, s := range b {
		if seen[s] == 0 {
			return false
		}
		seen[s]--
	}
	return true
}
