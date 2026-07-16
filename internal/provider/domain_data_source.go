package provider

import (
	"context"
	"fmt"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/smtpfast/terraform-provider-smtpfast/internal/client"
)

var (
	_ datasource.DataSource              = &domainDataSource{}
	_ datasource.DataSourceWithConfigure = &domainDataSource{}
)

// NewDomainDataSource returns a new smtpfast_domain data source.
func NewDomainDataSource() datasource.DataSource {
	return &domainDataSource{}
}

type domainDataSource struct {
	client *client.Client
}

type domainDataSourceModel struct {
	ID         types.String `tfsdk:"id"`
	Domain     types.String `tfsdk:"domain"`
	Status     types.String `tfsdk:"status"`
	DNSRecords types.List   `tfsdk:"dns_records"`
}

func (d *domainDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_domain"
}

func (d *domainDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Look up an existing SMTPfast sending domain by its ID, including the DNS records required to verify it.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				MarkdownDescription: "Unique identifier of the domain.",
				Required:            true,
			},
			"domain": schema.StringAttribute{
				MarkdownDescription: "The domain name.",
				Computed:            true,
			},
			"status": schema.StringAttribute{
				MarkdownDescription: "Verification status: `pending`, `verified`, or `failed`.",
				Computed:            true,
			},
			"dns_records": schema.ListNestedAttribute{
				MarkdownDescription: "DNS records required to verify the domain.",
				Computed:            true,
				NestedObject: schema.NestedAttributeObject{
					Attributes: map[string]schema.Attribute{
						"type":  schema.StringAttribute{MarkdownDescription: "DNS record type (CNAME, TXT, MX).", Computed: true},
						"name":  schema.StringAttribute{MarkdownDescription: "Record name/host.", Computed: true},
						"value": schema.StringAttribute{MarkdownDescription: "Record value.", Computed: true},
					},
				},
			},
		},
	}
}

func (d *domainDataSource) Configure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
	if req.ProviderData == nil {
		return
	}
	c, ok := req.ProviderData.(*client.Client)
	if !ok {
		resp.Diagnostics.AddError("Unexpected provider data", fmt.Sprintf("Expected *client.Client, got %T.", req.ProviderData))
		return
	}
	d.client = c
}

func (d *domainDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var state domainDataSourceModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	domain, err := d.client.GetDomain(ctx, state.ID.ValueString())
	if err != nil {
		resp.Diagnostics.AddError("Error reading domain", err.Error())
		return
	}

	state.ID = types.StringValue(domain.ID)
	state.Domain = types.StringValue(domain.Domain)
	state.Status = types.StringValue(domain.Status)

	list, diags := dnsRecordsToList(ctx, domain.DNSRecords)
	resp.Diagnostics.Append(diags...)
	state.DNSRecords = list

	resp.Diagnostics.Append(resp.State.Set(ctx, state)...)
}
