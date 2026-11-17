package provider

import (
	"context"
	"fmt"
	"regexp"
	"sort"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/smtpfast/terraform-provider-smtpfast/internal/client"
)

type dnsRecordModel struct {
	Type     types.String `tfsdk:"type"`
	Name     types.String `tfsdk:"name"`
	Value    types.String `tfsdk:"value"`
	Priority types.Int64  `tfsdk:"priority"`
	Purpose  types.String `tfsdk:"purpose"`
	Category types.String `tfsdk:"category"`
}

// dnsRecordAttrTypes is the object type of a single DNS record, shared by the
// resource and data source, in dns_records and in records.
var dnsRecordAttrTypes = map[string]attr.Type{
	"type":     types.StringType,
	"name":     types.StringType,
	"value":    types.StringType,
	"priority": types.Int64Type,
	"purpose":  types.StringType,
	"category": types.StringType,
}

var dnsRecordObjectType = types.ObjectType{AttrTypes: dnsRecordAttrTypes}

// The keys of the records map. A new domain always gets three DKIM records,
// SPF, DMARC and the two MAIL FROM records, so these keys are known when the
// domain is planned, before it exists.
const (
	recordKeySPF         = "spf"
	recordKeyDMARC       = "dmarc"
	recordKeyMailFromMX  = "mail_from_mx"
	recordKeyMailFromSPF = "mail_from_spf"
	recordKeyReceivingMX = "receiving_mx"
	dkimRecordCount      = 3
)

// plannedRecordKeys lists the records map keys of a new domain.
func plannedRecordKeys(receiving bool) []string {
	keys := make([]string, 0, dkimRecordCount+5)
	for i := 1; i <= dkimRecordCount; i++ {
		keys = append(keys, fmt.Sprintf("dkim_%d", i))
	}
	keys = append(keys, recordKeySPF, recordKeyDMARC, recordKeyMailFromMX, recordKeyMailFromSPF)
	if receiving {
		keys = append(keys, recordKeyReceivingMX)
	}
	return keys
}

// recordRole names what a record is for. The API's purpose decides; when it
// is missing or new, the record type and name decide.
func recordRole(rec client.DNSRecord, domain string) string {
	switch rec.Purpose {
	case "DKIM authentication":
		return "dkim"
	case "SPF authorization":
		return recordKeySPF
	case "DMARC policy":
		return recordKeyDMARC
	case "MAIL FROM bounce delivery":
		return recordKeyMailFromMX
	case "MAIL FROM SPF":
		return recordKeyMailFromSPF
	case "Inbound mail":
		return recordKeyReceivingMX
	}
	name := strings.TrimSuffix(strings.ToLower(rec.Name), ".")
	switch {
	case rec.Type == "CNAME" && strings.HasSuffix(name, "._domainkey."+domain):
		return "dkim"
	case rec.Type == "TXT" && name == domain && isSPF(rec.Value):
		return recordKeySPF
	case rec.Type == "TXT" && name == "_dmarc."+domain:
		return recordKeyDMARC
	case rec.Type == "MX" && (name == domain || rec.Category == "receiving"):
		return recordKeyReceivingMX
	case rec.Type == "MX":
		return recordKeyMailFromMX
	case rec.Type == "TXT" && isSPF(rec.Value):
		return recordKeyMailFromSPF
	}
	return ""
}

var nonKeyChars = regexp.MustCompile(`[^a-z0-9]+`)

// keyedRecords gives each record a stable key for the records map: dkim_1 to
// dkim_3 (sorted by name, so the API's order does not matter), spf, dmarc,
// mail_from_mx, mail_from_spf and receiving_mx. A record the provider does not
// know yet is keyed by its purpose, or its type and name.
func keyedRecords(records []client.DNSRecord, domain string) map[string]client.DNSRecord {
	domain = strings.TrimSuffix(strings.ToLower(domain), ".")
	out := make(map[string]client.DNSRecord, len(records))
	var dkim []client.DNSRecord
	add := func(key string, rec client.DNSRecord) {
		k := key
		for i := 2; ; i++ {
			if _, taken := out[k]; !taken {
				break
			}
			k = fmt.Sprintf("%s_%d", key, i)
		}
		out[k] = rec
	}
	for _, rec := range records {
		role := recordRole(rec, domain)
		switch role {
		case "dkim":
			dkim = append(dkim, rec)
		case "":
			label := rec.Purpose
			if label == "" {
				label = rec.Type + " " + rec.Name
			}
			add(strings.Trim(nonKeyChars.ReplaceAllString(strings.ToLower(label), "_"), "_"), rec)
		default:
			add(role, rec)
		}
	}
	sort.SliceStable(dkim, func(i, j int) bool { return dkim[i].Name < dkim[j].Name })
	for i, rec := range dkim {
		add(fmt.Sprintf("dkim_%d", i+1), rec)
	}
	return out
}

func isSPF(value string) bool {
	return strings.HasPrefix(strings.ToLower(strings.TrimSpace(value)), "v=spf1")
}

// spfInclude returns the domain of the first include: mechanism in an SPF
// record, such as amazonses.com, or "" when it has none.
func spfInclude(value string) string {
	for _, part := range strings.Fields(value) {
		part = strings.TrimLeft(part, "+?~-")
		if len(part) > len("include:") && strings.EqualFold(part[:len("include:")], "include:") {
			return strings.TrimSuffix(part[len("include:"):], ".")
		}
	}
	return ""
}

func dnsRecordToModel(rec client.DNSRecord) dnsRecordModel {
	m := dnsRecordModel{
		Type:     types.StringValue(rec.Type),
		Name:     types.StringValue(rec.Name),
		Value:    types.StringValue(rec.Value),
		Priority: types.Int64Null(),
		Purpose:  types.StringNull(),
		Category: types.StringNull(),
	}
	if rec.Priority != nil {
		m.Priority = types.Int64Value(*rec.Priority)
	}
	if rec.Purpose != "" {
		m.Purpose = types.StringValue(rec.Purpose)
	}
	if rec.Category != "" {
		m.Category = types.StringValue(rec.Category)
	}
	return m
}

// dnsRecordsToList converts API DNS records into a Terraform list value, shared
// by the domain resource and data source.
func dnsRecordsToList(ctx context.Context, records []client.DNSRecord) (types.List, diag.Diagnostics) {
	models := make([]dnsRecordModel, 0, len(records))
	for _, rec := range records {
		models = append(models, dnsRecordToModel(rec))
	}
	return types.ListValueFrom(ctx, dnsRecordObjectType, models)
}

// dnsRecordsToMap converts API DNS records into the records map.
func dnsRecordsToMap(ctx context.Context, records []client.DNSRecord, domain string) (types.Map, diag.Diagnostics) {
	keyed := keyedRecords(records, domain)
	models := make(map[string]dnsRecordModel, len(keyed))
	for k, rec := range keyed {
		models[k] = dnsRecordToModel(rec)
	}
	return types.MapValueFrom(ctx, dnsRecordObjectType, models)
}

// domainDetails holds the computed values the domain resource and data source
// share beyond the records.
type domainDetails struct {
	Region         types.String
	MailFromDomain types.String
	SPFInclude     types.String
	DKIMStatus     types.String
	SPFStatus      types.String
	DMARCStatus    types.String
	MailFromStatus types.String
}

var sesRegionInMX = regexp.MustCompile(`^feedback-smtp\.([a-z0-9-]+)\.amazonses\.com\.?$`)

// describeDomain reads the region, the MAIL FROM domain, the SPF include and
// the check statuses from a domain. The create response has no region or
// statuses, so the region and MAIL FROM domain then come from the MAIL FROM MX
// record and the statuses are "pending", which is what a new domain has.
func describeDomain(d *client.Domain) domainDetails {
	keyed := keyedRecords(d.DNSRecords, d.Domain)
	region, mailFrom := d.Region, d.MailFromDomain
	if mx, ok := keyed[recordKeyMailFromMX]; ok {
		if mailFrom == "" {
			mailFrom = mx.Name
		}
		if m := sesRegionInMX.FindStringSubmatch(strings.ToLower(mx.Value)); region == "" && m != nil {
			region = m[1]
		}
	}
	include := ""
	if spf, ok := keyed[recordKeySPF]; ok {
		include = spfInclude(spf.Value)
	}
	status := func(s string) types.String {
		if s == "" {
			s = "pending"
		}
		return types.StringValue(s)
	}
	return domainDetails{
		Region:         stringOrNull(region),
		MailFromDomain: stringOrNull(mailFrom),
		SPFInclude:     stringOrNull(include),
		DKIMStatus:     status(d.DKIMStatus),
		SPFStatus:      status(d.SPFStatus),
		DMARCStatus:    status(d.DMARCStatus),
		MailFromStatus: status(d.MailFromStatus),
	}
}

func stringOrNull(s string) types.String {
	if s == "" {
		return types.StringNull()
	}
	return types.StringValue(s)
}
