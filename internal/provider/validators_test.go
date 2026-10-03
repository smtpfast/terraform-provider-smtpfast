package provider

import (
	"context"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

func TestFallbackToAPI(t *testing.T) {
	got, err := fallbackToAPI("number", types.StringValue("9.50"))
	if err != nil || got != 9.5 {
		t.Fatalf("number: got %v, %v", got, err)
	}
	got, err = fallbackToAPI("string", types.StringValue("25"))
	if err != nil || got != "25" {
		t.Fatalf("string: got %v, %v", got, err)
	}
	got, err = fallbackToAPI("number", types.StringNull())
	if err != nil || got != nil {
		t.Fatalf("null: got %v, %v", got, err)
	}
	for _, bad := range []string{"cheap", "", "Inf", "NaN"} {
		if _, err := fallbackToAPI("number", types.StringValue(bad)); err == nil {
			t.Errorf("%q: expected an error", bad)
		}
	}
}

func TestFallbackFromAPI(t *testing.T) {
	cases := []struct {
		name  string
		api   any
		prior types.String
		want  types.String
	}{
		{"null", nil, types.StringValue("1"), types.StringNull()},
		{"string", "item", types.StringNull(), types.StringValue("item")},
		{"number without prior", 25.0, types.StringNull(), types.StringValue("25")},
		{"fraction", 9.5, types.StringNull(), types.StringValue("9.5")},
		{"same number, other spelling", 25.0, types.StringValue("25.0"), types.StringValue("25.0")},
		{"changed number", 30.0, types.StringValue("25.0"), types.StringValue("30")},
	}
	for _, tc := range cases {
		if got := fallbackFromAPI(tc.api, tc.prior); !got.Equal(tc.want) {
			t.Errorf("%s: got %s, want %s", tc.name, got, tc.want)
		}
	}
}

func TestSameStringSet(t *testing.T) {
	if !sameStringSet([]string{"a", "b"}, []string{"b", "a"}) {
		t.Error("reordered sets should match")
	}
	if sameStringSet([]string{"a", "a"}, []string{"a", "b"}) {
		t.Error("different sets should not match")
	}
	if sameStringSet([]string{"a"}, []string{"a", "b"}) {
		t.Error("different lengths should not match")
	}
}

func TestReconcileStringListKeepsOrder(t *testing.T) {
	current := stringList([]string{"email:send", "logs:read"})
	if diags := reconcileStringList([]string{"logs:read", "email:send"}, &current); diags.HasError() {
		t.Fatal(diags)
	}
	if !current.Equal(stringList([]string{"email:send", "logs:read"})) {
		t.Fatalf("order changed: %s", current)
	}
	if diags := reconcileStringList([]string{"email:send"}, &current); diags.HasError() {
		t.Fatal(diags)
	}
	if !current.Equal(stringList([]string{"email:send"})) {
		t.Fatalf("drift not picked up: %s", current)
	}
}

func TestDefaultScopesMatchTheAPIDefault(t *testing.T) {
	excluded := map[string]bool{"logs:read": true, "inbound:read": true, "inbound:delete": true, "team:read": true, "team:manage": true, "apikey:manage": true}
	var want []string
	for _, s := range allAPIKeyScopes {
		if !excluded[s] {
			want = append(want, s)
		}
	}
	if !sameStringSet(defaultAPIKeyScopes, want) {
		t.Fatalf("defaultAPIKeyScopes = %v, want %v", defaultAPIKeyScopes, want)
	}
}

func runStringValidator(v validator.String, value string) bool {
	resp := &validator.StringResponse{}
	v.ValidateString(context.Background(), validator.StringRequest{Path: path.Root("x"), ConfigValue: types.StringValue(value)}, resp)
	return !resp.Diagnostics.HasError()
}

func TestDomainNameValidator(t *testing.T) {
	for _, ok := range []string{"mail.example.com", "inbound.devops-daily.com", "a.io"} {
		if !runStringValidator(domainName(), ok) {
			t.Errorf("%q should be valid", ok)
		}
	}
	for _, bad := range []string{"Mail.example.com", "mail.example.com.", "localhost", "-x.example.com", "mail_example.com"} {
		if runStringValidator(domainName(), bad) {
			t.Errorf("%q should be invalid", bad)
		}
	}
}

func TestPlainEmailAddressValidator(t *testing.T) {
	for _, ok := range []string{"support@example.com", "first.last+tag@mail.example.co"} {
		if !runStringValidator(plainEmailAddress(), ok) {
			t.Errorf("%q should be valid", ok)
		}
	}
	for _, bad := range []string{"Support@example.com", "Ada <ada@example.com>", "ada", "@example.com", "a@b@example.com", "ada@localhost"} {
		if runStringValidator(plainEmailAddress(), bad) {
			t.Errorf("%q should be invalid", bad)
		}
	}
}

func TestNotReserved(t *testing.T) {
	v := notReserved(reservedTemplateVariableKeys)
	if runStringValidator(v, "FIRST_NAME") || runStringValidator(v, "this") {
		t.Error("reserved keys should be refused, ignoring case")
	}
	if !runStringValidator(v, "ORDER_ID") {
		t.Error("ORDER_ID should be allowed")
	}
}
