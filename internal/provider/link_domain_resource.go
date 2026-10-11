package provider

import (
	"context"
	"fmt"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/smtpfast/terraform-provider-smtpfast/internal/client"
)

var (
	_ resource.Resource                = &linkDomainResource{}
	_ resource.ResourceWithConfigure   = &linkDomainResource{}
	_ resource.ResourceWithImportState = &linkDomainResource{}
)

// NewLinkDomainResource returns a new smtpfast_link_domain resource.
func NewLinkDomainResource() resource.Resource {
	return &linkDomainResource{}
}

type linkDomainResource struct {
	client *client.Client
}

type linkDomainResourceModel struct {
	ID          types.String `tfsdk:"id"`
	Hostname    types.String `tfsdk:"hostname"`
	DomainID    types.String `tfsdk:"domain_id"`
	Domain      types.String `tfsdk:"domain"`
	Status      types.String `tfsdk:"status"`
	SSLStatus   types.String `tfsdk:"ssl_status"`
	LastError   types.String `tfsdk:"last_error"`
	CNAMETarget types.String `tfsdk:"cname_target"`
	DNSRecords  types.List   `tfsdk:"dns_records"`
	VerifiedAt  types.String `tfsdk:"verified_at"`
	CreatedAt   types.String `tfsdk:"created_at"`
}

type linkDomainDNSRecordModel struct {
	Type     types.String `tfsdk:"type"`
	Name     types.String `tfsdk:"name"`
	Value    types.String `tfsdk:"value"`
	Purpose  types.String `tfsdk:"purpose"`
	Required types.Bool   `tfsdk:"required"`
	Status   types.String `tfsdk:"status"`
}

var linkDomainDNSRecordAttrTypes = map[string]attr.Type{
	"type":     types.StringType,
	"name":     types.StringType,
	"value":    types.StringType,
	"purpose":  types.StringType,
	"required": types.BoolType,
	"status":   types.StringType,
}

func (r *linkDomainResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_link_domain"
}

func (r *linkDomainResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "A custom link domain: your own hostname, such as `links.example.com`, for the open pixel, tracked links and unsubscribe links in email from your domain, instead of `smtpfa.st`. " +
			"Publish the CNAME record (`hostname` pointing at `cname_target`) with your DNS provider; the link domain turns `active` once SMTPfast sees it and issues the certificate, usually within minutes. " +
			"On Cloudflare, keep the record DNS only (not proxied).\n\n" +
			"The hostname must be a subdomain of a verified sending domain on the team, one link domain per sending domain. Link domains are on paid plans.\n\n" +
			"Terraform does not wait for `active`: the CNAME record usually depends on this resource, so both are created in the same apply and `status` updates on the next refresh.\n\n" +
			"Needs a provider API key with the `domain:read` and `domain:write` scopes, from an owner or admin.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				MarkdownDescription: "Unique identifier of the link domain.",
				Computed:            true,
				PlanModifiers:       []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"hostname": schema.StringAttribute{
				MarkdownDescription: "The link hostname, e.g. `links.example.com`, in lowercase and without a trailing dot (the form the API stores). It must be a subdomain of a verified sending domain on the team. Changing this forces a new resource.",
				Required:            true,
				Validators:          []validator.String{domainName()},
				PlanModifiers:       []planmodifier.String{stringplanmodifier.RequiresReplace()},
			},
			"domain_id": schema.StringAttribute{
				MarkdownDescription: "ID of the sending domain the hostname sits under.",
				Computed:            true,
				PlanModifiers:       []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"domain": schema.StringAttribute{
				MarkdownDescription: "The sending domain the hostname sits under.",
				Computed:            true,
				PlanModifiers:       []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"status": schema.StringAttribute{
				MarkdownDescription: "`pending` until the CNAME record is seen and the certificate is issued, then `active`. `failed` means it will not become active without a change on your side; `last_error` says why. Mail uses the link domain only while it is `active`.",
				Computed:            true,
			},
			"ssl_status": schema.StringAttribute{
				MarkdownDescription: "Certificate status as Cloudflare reports it, for troubleshooting.",
				Computed:            true,
			},
			"last_error": schema.StringAttribute{
				MarkdownDescription: "Why the link domain is not active yet, when known.",
				Computed:            true,
			},
			"cname_target": schema.StringAttribute{
				MarkdownDescription: "Where the CNAME record must point, such as `links.smtpfa.st`. Use it with `hostname` in your DNS provider's record resource.",
				Computed:            true,
				PlanModifiers:       []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"dns_records": schema.ListNestedAttribute{
				MarkdownDescription: "Records to publish: the required CNAME first, then any optional TXT record Cloudflare offers to activate the hostname before the CNAME is live.",
				Computed:            true,
				NestedObject: schema.NestedAttributeObject{
					Attributes: map[string]schema.Attribute{
						"type":     schema.StringAttribute{MarkdownDescription: "Record type: `CNAME` or `TXT`.", Computed: true},
						"name":     schema.StringAttribute{MarkdownDescription: "Record name.", Computed: true},
						"value":    schema.StringAttribute{MarkdownDescription: "Record value.", Computed: true},
						"purpose":  schema.StringAttribute{MarkdownDescription: "What the record is for.", Computed: true},
						"required": schema.BoolAttribute{MarkdownDescription: "Whether the record is required. The CNAME is; a TXT record is optional.", Computed: true},
						"status": schema.StringAttribute{
							MarkdownDescription: "`pending` while the link domain is not active. Once it is, the CNAME is `verified` (keep it: certificate renewals need it) and a TXT record is `not_needed`.",
							Computed:            true,
						},
					},
				},
			},
			"verified_at": schema.StringAttribute{
				MarkdownDescription: "When the link domain first became active (RFC 3339), or null.",
				Computed:            true,
			},
			"created_at": schema.StringAttribute{
				MarkdownDescription: "Creation timestamp (RFC 3339).",
				Computed:            true,
				PlanModifiers:       []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
		},
	}
}

func (r *linkDomainResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	if req.ProviderData == nil {
		return
	}
	c, ok := req.ProviderData.(*client.Client)
	if !ok {
		resp.Diagnostics.AddError("Unexpected provider data", fmt.Sprintf("Expected *client.Client, got %T.", req.ProviderData))
		return
	}
	r.client = c
}

// linkDomainCreateError explains the refusals a configuration can fix.
func linkDomainCreateError(err error) string {
	switch client.ErrorCode(err) {
	case "plan_upgrade_required":
		return err.Error() + "\n\nCustom link domains are on paid plans. Upgrade the team's plan at https://smtpfa.st/billing, then apply again."
	case "link_domains_unavailable", "link_domains_at_capacity":
		return err.Error() + "\n\nThis is on the SMTPfast side, not in your configuration. Try again later or contact support."
	case "no_matching_domain", "domain_not_verified":
		return err.Error() + "\n\nThe hostname must be a subdomain of a sending domain on this team that is already verified. If the sending domain is in the same configuration, apply again once its status is verified."
	case "link_domain_exists":
		return err.Error() + "\n\nEach sending domain takes one link domain. Import the existing one with terraform import, or delete it first."
	}
	return err.Error()
}

func (r *linkDomainResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan linkDomainResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	ld, err := r.client.CreateLinkDomain(ctx, plan.Hostname.ValueString())
	if err != nil {
		resp.Diagnostics.AddError("Error creating link domain", linkDomainCreateError(err))
		return
	}

	resp.Diagnostics.Append(mapLinkDomainToState(ctx, ld, &plan)...)
	resp.Diagnostics.Append(resp.State.Set(ctx, plan)...)
}

func (r *linkDomainResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state linkDomainResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	ld, err := r.client.GetLinkDomain(ctx, state.ID.ValueString())
	if err != nil {
		if client.IsNotFound(err) {
			resp.State.RemoveResource(ctx)
			return
		}
		resp.Diagnostics.AddError("Error reading link domain", err.Error())
		return
	}

	resp.Diagnostics.Append(mapLinkDomainToState(ctx, ld, &state)...)
	resp.Diagnostics.Append(resp.State.Set(ctx, state)...)
}

// Update is never called with a change: hostname forces replacement and every
// other attribute is computed. It keeps the planned values.
func (r *linkDomainResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan linkDomainResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, plan)...)
}

func (r *linkDomainResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state linkDomainResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	if err := r.client.DeleteLinkDomain(ctx, state.ID.ValueString()); err != nil {
		if client.IsNotFound(err) {
			return
		}
		resp.Diagnostics.AddError("Error deleting link domain", err.Error())
	}
}

func (r *linkDomainResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	resource.ImportStatePassthroughID(ctx, path.Root("id"), req, resp)
}

func mapLinkDomainToState(ctx context.Context, ld *client.LinkDomain, m *linkDomainResourceModel) diag.Diagnostics {
	m.ID = types.StringValue(ld.ID)
	m.Hostname = types.StringValue(ld.Hostname)
	m.DomainID = types.StringPointerValue(ld.DomainID)
	m.Domain = types.StringPointerValue(ld.Domain)
	m.Status = types.StringValue(ld.Status)
	m.SSLStatus = types.StringPointerValue(ld.SSLStatus)
	m.LastError = types.StringPointerValue(ld.LastError)
	m.VerifiedAt = types.StringPointerValue(ld.VerifiedAt)
	m.CreatedAt = types.StringValue(ld.CreatedAt)

	m.CNAMETarget = types.StringNull()
	records := make([]linkDomainDNSRecordModel, 0, len(ld.DNSRecords))
	for _, rec := range ld.DNSRecords {
		if rec.Type == "CNAME" && m.CNAMETarget.IsNull() {
			m.CNAMETarget = types.StringValue(rec.Value)
		}
		records = append(records, linkDomainDNSRecordModel{
			Type:     types.StringValue(rec.Type),
			Name:     types.StringValue(rec.Name),
			Value:    types.StringValue(rec.Value),
			Purpose:  types.StringValue(rec.Purpose),
			Required: types.BoolValue(rec.Required),
			Status:   types.StringValue(rec.Status),
		})
	}
	list, diags := types.ListValueFrom(ctx, types.ObjectType{AttrTypes: linkDomainDNSRecordAttrTypes}, records)
	m.DNSRecords = list
	return diags
}
