package provider

import (
	"context"
	"fmt"

	"github.com/hashicorp/terraform-plugin-framework-validators/datasourcevalidator"
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/smtpfast/terraform-provider-smtpfast/internal/client"
)

var (
	_ datasource.DataSource                     = &domainDataSource{}
	_ datasource.DataSourceWithConfigure        = &domainDataSource{}
	_ datasource.DataSourceWithConfigValidators = &domainDataSource{}
)

// NewDomainDataSource returns a new smtpfast_domain data source.
func NewDomainDataSource() datasource.DataSource {
	return &domainDataSource{}
}

type domainDataSource struct {
	client *client.Client
}

type domainDataSourceModel struct {
	ID               types.String `tfsdk:"id"`
	Domain           types.String `tfsdk:"domain"`
	Status           types.String `tfsdk:"status"`
	DNSRecords       types.List   `tfsdk:"dns_records"`
	Records          types.Map    `tfsdk:"records"`
	ReceivingEnabled types.Bool   `tfsdk:"receiving_enabled"`
	ReceivingStatus  types.String `tfsdk:"receiving_status"`
	Region           types.String `tfsdk:"region"`
	MailFromDomain   types.String `tfsdk:"mail_from_domain"`
	SPFInclude       types.String `tfsdk:"spf_include"`
	DKIMStatus       types.String `tfsdk:"dkim_status"`
	SPFStatus        types.String `tfsdk:"spf_status"`
	DMARCStatus      types.String `tfsdk:"dmarc_status"`
	MailFromStatus   types.String `tfsdk:"mail_from_status"`
}

func (d *domainDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_domain"
}

func (d *domainDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Look up an existing SMTPfast sending domain by its ID or its name, including the DNS records required to verify it. Needs a provider API key with the `domain:read` scope.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				MarkdownDescription: "Unique identifier of the domain. Set exactly one of `id` and `domain`.",
				Optional:            true,
				Computed:            true,
			},
			"domain": schema.StringAttribute{
				MarkdownDescription: "The domain name, such as `mail.example.com`. Set exactly one of `id` and `domain`.",
				Optional:            true,
				Computed:            true,
				Validators:          []validator.String{domainName()},
			},
			"status": schema.StringAttribute{
				MarkdownDescription: "Verification status: `pending`, `verified`, or `failed`.",
				Computed:            true,
			},
			"receiving_enabled": schema.BoolAttribute{
				MarkdownDescription: "Whether inbound email is turned on for this domain.",
				Computed:            true,
			},
			"receiving_status": schema.StringAttribute{
				MarkdownDescription: "Inbound status: `disabled`, `pending`, `active`, or `failed`.",
				Computed:            true,
			},
			"dkim_status":      schema.StringAttribute{MarkdownDescription: "DKIM status from the last check: `pending` or `verified`.", Computed: true},
			"spf_status":       schema.StringAttribute{MarkdownDescription: "SPF status from the last check: `pending` or `verified`.", Computed: true},
			"dmarc_status":     schema.StringAttribute{MarkdownDescription: "DMARC status from the last check: `pending` or `verified`.", Computed: true},
			"mail_from_status": schema.StringAttribute{MarkdownDescription: "Custom MAIL FROM status from the last check: `pending`, `verified`, or `failed`.", Computed: true},
			"region":           schema.StringAttribute{MarkdownDescription: "The Amazon SES region that sends for this domain, such as `eu-west-2`.", Computed: true},
			"mail_from_domain": schema.StringAttribute{MarkdownDescription: "The custom MAIL FROM (bounce) domain, `bounce.<domain>`.", Computed: true},
			"spf_include":      schema.StringAttribute{MarkdownDescription: "The domain SMTPfast needs in the apex SPF record, such as `amazonses.com`, to merge into an existing SPF record.", Computed: true},
			"records": schema.MapNestedAttribute{
				MarkdownDescription: "DNS records required to verify the domain, keyed by role: `dkim_1`, `dkim_2`, `dkim_3`, `spf`, `dmarc`, `mail_from_mx`, `mail_from_spf`, and `receiving_mx` while receiving is on.",
				Computed:            true,
				NestedObject:        schema.NestedAttributeObject{Attributes: dnsRecordDataSourceAttributes()},
			},
			"dns_records": schema.ListNestedAttribute{
				MarkdownDescription: "**Deprecated**: use `records`. DNS records required to verify the domain, as a list.",
				DeprecationMessage:  "Use records instead, which is keyed by the record's role.",
				Computed:            true,
				NestedObject:        schema.NestedAttributeObject{Attributes: dnsRecordDataSourceAttributes()},
			},
		},
	}
}

func dnsRecordDataSourceAttributes() map[string]schema.Attribute {
	return map[string]schema.Attribute{
		"type":     schema.StringAttribute{MarkdownDescription: "DNS record type (CNAME, TXT, MX).", Computed: true},
		"name":     schema.StringAttribute{MarkdownDescription: "Record name/host.", Computed: true},
		"value":    schema.StringAttribute{MarkdownDescription: "Record value.", Computed: true},
		"priority": schema.Int64Attribute{MarkdownDescription: "Priority, set on MX records only.", Computed: true},
		"purpose":  schema.StringAttribute{MarkdownDescription: "What the record is for, as the API words it, such as `SPF authorization`.", Computed: true},
		"category": schema.StringAttribute{MarkdownDescription: "Record group: `verification`, `sending` or `receiving`.", Computed: true},
	}
}

func (d *domainDataSource) ConfigValidators(context.Context) []datasource.ConfigValidator {
	return []datasource.ConfigValidator{
		datasourcevalidator.ExactlyOneOf(path.MatchRoot("id"), path.MatchRoot("domain")),
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

	id := state.ID.ValueString()
	if state.ID.IsNull() {
		domains, err := d.client.ListDomains(ctx)
		if err != nil {
			resp.Diagnostics.AddError("Error listing domains", err.Error())
			return
		}
		for _, dom := range domains {
			if dom.Domain == state.Domain.ValueString() {
				id = dom.ID
				break
			}
		}
		if id == "" {
			resp.Diagnostics.AddAttributeError(path.Root("domain"), "Domain not found", fmt.Sprintf("No domain named %q on this team.", state.Domain.ValueString()))
			return
		}
	}

	domain, err := d.client.GetDomain(ctx, id)
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
	records, diags := dnsRecordsToMap(ctx, domain.DNSRecords, domain.Domain)
	resp.Diagnostics.Append(diags...)
	state.Records = records
	details := describeDomain(domain)
	state.Region = details.Region
	state.MailFromDomain = details.MailFromDomain
	state.SPFInclude = details.SPFInclude
	state.DKIMStatus = details.DKIMStatus
	state.SPFStatus = details.SPFStatus
	state.DMARCStatus = details.DMARCStatus
	state.MailFromStatus = details.MailFromStatus
	state.ReceivingEnabled = types.BoolValue(domain.Receiving != nil && domain.Receiving.Enabled)
	if domain.Receiving != nil {
		state.ReceivingStatus = types.StringValue(domain.Receiving.Status)
	} else {
		state.ReceivingStatus = types.StringValue("disabled")
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, state)...)
}
