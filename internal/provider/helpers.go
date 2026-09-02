package provider

import (
	"context"

	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/smtpfast/terraform-provider-smtpfast/internal/client"
)

type dnsRecordModel struct {
	Type     types.String `tfsdk:"type"`
	Name     types.String `tfsdk:"name"`
	Value    types.String `tfsdk:"value"`
	Priority types.Int64  `tfsdk:"priority"`
}

// dnsRecordsToList converts API DNS records into a Terraform list value, shared
// by the domain resource and data source.
func dnsRecordsToList(ctx context.Context, records []client.DNSRecord) (types.List, diag.Diagnostics) {
	elemType := types.ObjectType{AttrTypes: dnsRecordAttrTypes}
	models := make([]dnsRecordModel, 0, len(records))
	for _, rec := range records {
		m := dnsRecordModel{
			Type:     types.StringValue(rec.Type),
			Name:     types.StringValue(rec.Name),
			Value:    types.StringValue(rec.Value),
			Priority: types.Int64Null(),
		}
		if rec.Priority != nil {
			m.Priority = types.Int64Value(*rec.Priority)
		}
		models = append(models, m)
	}
	return types.ListValueFrom(ctx, elemType, models)
}
