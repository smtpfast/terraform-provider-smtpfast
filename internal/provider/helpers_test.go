package provider

import (
	"context"
	"reflect"
	"sort"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-framework/types/basetypes"

	"github.com/smtpfast/terraform-provider-smtpfast/internal/client"
)

func TestKeyedRecords(t *testing.T) {
	ten := int64(10)
	records := []client.DNSRecord{
		{Type: "CNAME", Name: "ccc._domainkey.example.com", Value: "ccc.dkim.amazonses.com", Purpose: "DKIM authentication"},
		{Type: "CNAME", Name: "aaa._domainkey.example.com", Value: "aaa.dkim.amazonses.com", Purpose: "DKIM authentication"},
		{Type: "CNAME", Name: "bbb._domainkey.example.com", Value: "bbb.dkim.amazonses.com", Purpose: "DKIM authentication"},
		{Type: "TXT", Name: "example.com", Value: "v=spf1 include:amazonses.com ~all", Purpose: "SPF authorization"},
		{Type: "TXT", Name: "_dmarc.example.com", Value: "v=DMARC1; p=none;", Purpose: "DMARC policy"},
		{Type: "MX", Name: "bounce.example.com", Value: "feedback-smtp.eu-west-2.amazonses.com", Priority: &ten, Purpose: "MAIL FROM bounce delivery"},
		{Type: "TXT", Name: "bounce.example.com", Value: "v=spf1 include:amazonses.com ~all", Purpose: "MAIL FROM SPF"},
		{Type: "MX", Name: "example.com", Value: "inbound-smtp.eu-west-2.amazonaws.com", Priority: &ten, Purpose: "Inbound mail", Category: "receiving"},
	}
	got := keyedRecords(records, "example.com")
	want := map[string]string{
		"dkim_1":        "aaa._domainkey.example.com",
		"dkim_2":        "bbb._domainkey.example.com",
		"dkim_3":        "ccc._domainkey.example.com",
		"spf":           "example.com",
		"dmarc":         "_dmarc.example.com",
		"mail_from_mx":  "bounce.example.com",
		"mail_from_spf": "bounce.example.com",
		"receiving_mx":  "example.com",
	}
	names := map[string]string{}
	for k, rec := range got {
		names[k] = rec.Name
	}
	if !reflect.DeepEqual(names, want) {
		t.Fatalf("keys = %v, want %v", names, want)
	}
	if got["mail_from_mx"].Type != "MX" || got["mail_from_spf"].Type != "TXT" || got["receiving_mx"].Type != "MX" {
		t.Fatalf("records mixed up: %+v", got)
	}

	// The planned keys of a new domain are the keys the API's records get.
	keys := make([]string, 0, len(got))
	for k := range got {
		keys = append(keys, k)
	}
	planned := plannedRecordKeys(true)
	sort.Strings(keys)
	sort.Strings(planned)
	if !reflect.DeepEqual(keys, planned) {
		t.Fatalf("planned keys %v, API keys %v", planned, keys)
	}
}

// Without a purpose (or with one the provider does not know), the record type
// and name decide, so a reworded purpose keeps the keys.
func TestKeyedRecordsWithoutPurpose(t *testing.T) {
	ten := int64(10)
	records := []client.DNSRecord{
		{Type: "CNAME", Name: "t1._domainkey.mail.example.com", Value: "t1.dkim.amazonses.com"},
		{Type: "TXT", Name: "mail.example.com", Value: "v=spf1 include:amazonses.com ~all", Purpose: "Sender policy"},
		{Type: "TXT", Name: "_dmarc.mail.example.com", Value: "v=DMARC1; p=none;"},
		{Type: "MX", Name: "bounce.mail.example.com", Value: "feedback-smtp.us-east-1.amazonses.com", Priority: &ten},
		{Type: "TXT", Name: "bounce.mail.example.com", Value: "v=spf1 include:amazonses.com ~all"},
		{Type: "MX", Name: "mail.example.com", Value: "inbound-smtp.us-east-1.amazonaws.com", Priority: &ten},
		{Type: "TXT", Name: "_bimi.mail.example.com", Value: "v=BIMI1;", Purpose: "BIMI logo"},
		{Type: "TXT", Name: "other.mail.example.com", Value: "x"},
	}
	got := keyedRecords(records, "mail.example.com")
	for key, name := range map[string]string{
		"dkim_1":                     "t1._domainkey.mail.example.com",
		"spf":                        "mail.example.com",
		"dmarc":                      "_dmarc.mail.example.com",
		"mail_from_mx":               "bounce.mail.example.com",
		"mail_from_spf":              "bounce.mail.example.com",
		"receiving_mx":               "mail.example.com",
		"bimi_logo":                  "_bimi.mail.example.com",
		"txt_other_mail_example_com": "other.mail.example.com",
	} {
		if got[key].Name != name {
			t.Errorf("%s = %q, want %q (all: %v)", key, got[key].Name, name, got)
		}
	}
	if len(got) != 8 {
		t.Errorf("got %d records, want 8", len(got))
	}
}

func TestSPFInclude(t *testing.T) {
	for value, want := range map[string]string{
		"v=spf1 include:amazonses.com ~all":                    "amazonses.com",
		"v=spf1 ip4:192.0.2.1 +INCLUDE:_spf.example.com. -all": "_spf.example.com",
		"v=spf1 -all": "",
	} {
		if got := spfInclude(value); got != want {
			t.Errorf("spfInclude(%q) = %q, want %q", value, got, want)
		}
	}
}

// The create response has no region or MAIL FROM domain: both come from the
// MAIL FROM MX record.
func TestDescribeDomainFromCreateResponse(t *testing.T) {
	ten := int64(10)
	d := &client.Domain{
		Domain: "example.com",
		Status: "pending",
		DNSRecords: []client.DNSRecord{
			{Type: "TXT", Name: "example.com", Value: "v=spf1 include:amazonses.com ~all", Purpose: "SPF authorization"},
			{Type: "MX", Name: "bounce.example.com", Value: "feedback-smtp.eu-west-2.amazonses.com", Priority: &ten, Purpose: "MAIL FROM bounce delivery"},
		},
	}
	got := describeDomain(d)
	if got.Region.ValueString() != "eu-west-2" || got.MailFromDomain.ValueString() != "bounce.example.com" || got.SPFInclude.ValueString() != "amazonses.com" {
		t.Fatalf("describeDomain = %+v", got)
	}
	if got.DKIMStatus.ValueString() != "pending" || got.MailFromStatus.ValueString() != "pending" {
		t.Fatalf("statuses = %+v", got)
	}

	// GET's own fields win.
	d.Region, d.MailFromDomain, d.DKIMStatus = "us-east-1", "mf.example.com", "verified"
	got = describeDomain(d)
	if got.Region.ValueString() != "us-east-1" || got.MailFromDomain.ValueString() != "mf.example.com" || got.DKIMStatus.ValueString() != "verified" {
		t.Fatalf("describeDomain = %+v", got)
	}
}

// State from 0.6 has dns_records and no records; a plan without a refresh
// rebuilds the map from the list.
func TestRecordsFromList(t *testing.T) {
	ctx := context.Background()
	ten := int64(10)
	list, diags := dnsRecordsToList(ctx, []client.DNSRecord{
		{Type: "CNAME", Name: "b._domainkey.example.com", Value: "b.dkim.amazonses.com"},
		{Type: "CNAME", Name: "a._domainkey.example.com", Value: "a.dkim.amazonses.com"},
		{Type: "TXT", Name: "example.com", Value: "v=spf1 include:amazonses.com ~all"},
		{Type: "MX", Name: "bounce.example.com", Value: "feedback-smtp.eu-west-2.amazonses.com", Priority: &ten},
	})
	if diags.HasError() {
		t.Fatal(diags)
	}
	got, diags := recordsFromList(ctx, list, "example.com")
	if diags.HasError() {
		t.Fatal(diags)
	}
	elems := got.Elements()
	if len(elems) != 4 {
		t.Fatalf("got %d records: %v", len(elems), elems)
	}
	var mx dnsRecordModel
	if d := elems["mail_from_mx"].(types.Object).As(ctx, &mx, basetypes.ObjectAsOptions{}); d.HasError() {
		t.Fatal(d)
	}
	if mx.Priority.ValueInt64() != 10 || !mx.Purpose.IsUnknown() || mx.Name.ValueString() != "bounce.example.com" {
		t.Fatalf("mail_from_mx = %+v", mx)
	}
	var dkim dnsRecordModel
	_ = elems["dkim_1"].(types.Object).As(ctx, &dkim, basetypes.ObjectAsOptions{})
	if dkim.Name.ValueString() != "a._domainkey.example.com" {
		t.Fatalf("dkim_1 = %+v", dkim)
	}
	if null, _ := recordsFromList(ctx, types.ListNull(dnsRecordObjectType), "example.com"); !null.IsNull() {
		t.Fatalf("null list gave %v", null)
	}
}
