package provider

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/boolplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/smtpfast/terraform-provider-smtpfast/internal/client"
)

var (
	_ resource.Resource                = &domainResource{}
	_ resource.ResourceWithConfigure   = &domainResource{}
	_ resource.ResourceWithImportState = &domainResource{}
	_ resource.ResourceWithModifyPlan  = &domainResource{}
)

// receivingMXKey is the private state key holding the MX record receiving
// needs, as the last read reported it.
const receivingMXKey = "receiving_mx"

// NewDomainResource returns a new smtpfast_domain resource.
func NewDomainResource() resource.Resource {
	return &domainResource{}
}

type domainResource struct {
	client *client.Client
}

type domainResourceModel struct {
	ID               types.String `tfsdk:"id"`
	Domain           types.String `tfsdk:"domain"`
	Status           types.String `tfsdk:"status"`
	DNSRecords       types.List   `tfsdk:"dns_records"`
	ReceivingEnabled types.Bool   `tfsdk:"receiving_enabled"`
	ReceivingStatus  types.String `tfsdk:"receiving_status"`
}

// dnsRecordAttrTypes is the object type of a single DNS record, shared by the
// resource and data source.
var dnsRecordAttrTypes = map[string]attr.Type{
	"type":     types.StringType,
	"name":     types.StringType,
	"value":    types.StringType,
	"priority": types.Int64Type,
}

func (r *domainResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_domain"
}

func (r *domainResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "A sending domain on SMTPfast. Creating one returns the DNS records you must publish (DKIM, SPF, DMARC, MAIL FROM) to verify it. Combine `dns_records` with your DNS provider (Cloudflare, Route 53, ...) to provision the whole sending domain from one configuration.\n\n" +
			"The records are only known once the domain exists, so a `for_each` over them cannot plan before that: on the first run, apply the domain alone with `-target`, then apply everything. After that, turning receiving on or off keeps `dns_records` known at plan time.\n\n" +
			"Needs a provider API key with the `domain:read` and `domain:write` scopes, created by a team owner or admin.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				MarkdownDescription: "Unique identifier of the domain.",
				Computed:            true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"domain": schema.StringAttribute{
				MarkdownDescription: "The domain name to send from, e.g. `mail.example.com`, in lowercase and without a trailing dot (the form the API stores). Changing this forces a new resource.",
				Required:            true,
				Validators:          []validator.String{domainName()},
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
			},
			"status": schema.StringAttribute{
				MarkdownDescription: "Verification status: `pending`, `verified`, or `failed`.",
				Computed:            true,
			},
			"receiving_enabled": schema.BoolAttribute{
				MarkdownDescription: "Turn inbound email on for this domain (paid plans). The domain must be verified for sending, or be a subdomain of a domain that is already verified on the team. For a brand-new domain that is not such a subdomain, set this in a later apply once `status` is `verified`. When enabled, `dns_records` gains the MX record (with `priority`) to publish. Leaving it unset keeps whatever the domain currently has; a new domain starts with receiving off.",
				Optional:            true,
				Computed:            true,
				PlanModifiers: []planmodifier.Bool{
					boolplanmodifier.UseStateForUnknown(),
				},
			},
			"receiving_status": schema.StringAttribute{
				MarkdownDescription: "Inbound status: `disabled`, `pending` (MX record not seen yet), `active`, or `failed` (another MX record ties or outranks ours).",
				Computed:            true,
			},
			"dns_records": schema.ListNestedAttribute{
				MarkdownDescription: "DNS records to publish to verify and enable the domain.",
				Computed:            true,
				NestedObject: schema.NestedAttributeObject{
					Attributes: map[string]schema.Attribute{
						"type":     schema.StringAttribute{MarkdownDescription: "DNS record type (CNAME, TXT, MX).", Computed: true},
						"name":     schema.StringAttribute{MarkdownDescription: "Record name/host.", Computed: true},
						"value":    schema.StringAttribute{MarkdownDescription: "Record value.", Computed: true},
						"priority": schema.Int64Attribute{MarkdownDescription: "Priority, set on MX records only.", Computed: true},
					},
				},
			},
		},
	}
}

func (r *domainResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
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

func (r *domainResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan domainResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	domain, err := r.client.CreateDomain(ctx, plan.Domain.ValueString())
	if err != nil {
		resp.Diagnostics.AddError("Error creating domain", err.Error())
		return
	}
	// The domain exists now: record it before anything else can fail, so a
	// failed receiving toggle never leaves an unmanaged domain behind.
	resp.Diagnostics.Append(r.mapToState(ctx, domain, &plan)...)
	resp.Diagnostics.Append(resp.State.Set(ctx, plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if plan.ReceivingEnabled.ValueBool() {
		if _, err := r.client.SetDomainReceiving(ctx, domain.ID, true); err != nil {
			resp.Diagnostics.AddError("Error enabling receiving", receivingErrorHint(err))
			return
		}
		refreshed, err := r.client.GetDomain(ctx, domain.ID)
		if err != nil {
			resp.Diagnostics.AddError("Error reading domain after enabling receiving", err.Error())
			return
		}
		domain = refreshed
	}

	resp.Diagnostics.Append(r.mapToState(ctx, domain, &plan)...)
	resp.Diagnostics.Append(resp.State.Set(ctx, plan)...)
	resp.Diagnostics.Append(saveReceivingMX(ctx, domain, resp.Private)...)
}

func (r *domainResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state domainResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	domain, err := r.client.GetDomain(ctx, state.ID.ValueString())
	if err != nil {
		if client.IsNotFound(err) {
			resp.State.RemoveResource(ctx)
			return
		}
		resp.Diagnostics.AddError("Error reading domain", err.Error())
		return
	}

	resp.Diagnostics.Append(r.mapToState(ctx, domain, &state)...)
	resp.Diagnostics.Append(resp.State.Set(ctx, state)...)
	resp.Diagnostics.Append(saveReceivingMX(ctx, domain, resp.Private)...)
}

// Update handles the receiving toggle; the domain name itself forces replacement.
func (r *domainResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan, state domainResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	// Unset in config means "leave it as it is" (UseStateForUnknown gives us the state value).
	if !plan.ReceivingEnabled.IsNull() && !plan.ReceivingEnabled.IsUnknown() && !plan.ReceivingEnabled.Equal(state.ReceivingEnabled) {
		if _, err := r.client.SetDomainReceiving(ctx, state.ID.ValueString(), plan.ReceivingEnabled.ValueBool()); err != nil {
			resp.Diagnostics.AddError("Error updating domain receiving", receivingErrorHint(err))
			return
		}
	}
	domain, err := r.client.GetDomain(ctx, state.ID.ValueString())
	if err != nil {
		resp.Diagnostics.AddError("Error reading domain", err.Error())
		return
	}
	resp.Diagnostics.Append(r.mapToState(ctx, domain, &plan)...)
	resp.Diagnostics.Append(resp.State.Set(ctx, plan)...)
	resp.Diagnostics.Append(saveReceivingMX(ctx, domain, resp.Private)...)
}

// ModifyPlan predicts dns_records when receiving is turned on or off, so a
// for_each over them stays known at plan time. Turning receiving on appends
// the MX record the last read reported; turning it off removes it. Without
// that record (no refresh yet), dns_records stays unknown.
func (r *domainResource) ModifyPlan(ctx context.Context, req resource.ModifyPlanRequest, resp *resource.ModifyPlanResponse) {
	if req.State.Raw.IsNull() || req.Plan.Raw.IsNull() {
		return
	}
	var plan, state domainResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if !plan.Domain.Equal(state.Domain) || !plan.DNSRecords.IsUnknown() || state.DNSRecords.IsNull() || state.DNSRecords.IsUnknown() ||
		plan.ReceivingEnabled.IsUnknown() || plan.ReceivingEnabled.Equal(state.ReceivingEnabled) {
		return
	}

	var records []dnsRecordModel
	resp.Diagnostics.Append(state.DNSRecords.ElementsAs(ctx, &records, false)...)
	if resp.Diagnostics.HasError() {
		return
	}
	// The receiving MX sits on the domain itself; the MAIL FROM MX is on its
	// bounce subdomain and stays.
	kept := make([]dnsRecordModel, 0, len(records)+1)
	for _, rec := range records {
		if rec.Type.ValueString() == "MX" && rec.Name.ValueString() == state.Domain.ValueString() {
			continue
		}
		kept = append(kept, rec)
	}
	if plan.ReceivingEnabled.ValueBool() {
		raw, diags := req.Private.GetKey(ctx, receivingMXKey)
		resp.Diagnostics.Append(diags...)
		var mx client.DNSRecord
		if len(raw) == 0 || json.Unmarshal(raw, &mx) != nil || mx.Value == "" || mx.Priority == nil {
			return
		}
		name := mx.Name
		if name == "" {
			name = state.Domain.ValueString()
		}
		kept = append(kept, dnsRecordModel{
			Type:     types.StringValue("MX"),
			Name:     types.StringValue(name),
			Value:    types.StringValue(mx.Value),
			Priority: types.Int64Value(*mx.Priority),
		})
	}
	list, diags := types.ListValueFrom(ctx, types.ObjectType{AttrTypes: dnsRecordAttrTypes}, kept)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}
	resp.Diagnostics.Append(resp.Plan.SetAttribute(ctx, path.Root("dns_records"), list)...)
}

// saveReceivingMX keeps the MX record receiving needs in private state, for
// ModifyPlan.
func saveReceivingMX(ctx context.Context, d *client.Domain, private interface {
	SetKey(context.Context, string, []byte) diag.Diagnostics
}) diag.Diagnostics {
	if d.Receiving == nil || d.Receiving.MXRecord == nil {
		return nil
	}
	raw, err := json.Marshal(d.Receiving.MXRecord)
	if err != nil {
		return nil
	}
	return private.SetKey(ctx, receivingMXKey, raw)
}

func (r *domainResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state domainResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	if err := r.client.DeleteDomain(ctx, state.ID.ValueString()); err != nil {
		if client.IsNotFound(err) {
			return
		}
		resp.Diagnostics.AddError("Error deleting domain", err.Error())
	}
}

func (r *domainResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	resource.ImportStatePassthroughID(ctx, path.Root("id"), req, resp)
}

func (r *domainResource) mapToState(ctx context.Context, d *client.Domain, m *domainResourceModel) diag.Diagnostics {
	m.ID = types.StringValue(d.ID)
	m.Domain = types.StringValue(d.Domain)
	m.Status = types.StringValue(d.Status)

	list, diags := dnsRecordsToList(ctx, d.DNSRecords)
	m.DNSRecords = list
	if d.Receiving != nil {
		m.ReceivingEnabled = types.BoolValue(d.Receiving.Enabled)
		m.ReceivingStatus = types.StringValue(d.Receiving.Status)
	} else {
		m.ReceivingEnabled = types.BoolValue(false)
		m.ReceivingStatus = types.StringValue("disabled")
	}
	return diags
}

// receivingErrorHint turns the API's receiving errors into actionable text.
func receivingErrorHint(err error) string {
	var apiErr *client.APIError
	if errors.As(err, &apiErr) {
		switch apiErr.StatusCode {
		case 400:
			return err.Error() + ". Receiving needs a domain that is verified for sending, or a subdomain of a verified domain: keep receiving_enabled unset or false until status is \"verified\", then set it to true."
		case 402:
			return err.Error() + ". Inbound email is available on paid plans."
		case 403:
			return err.Error() + ". The API key must belong to a team owner or admin."
		case 503:
			return err.Error() + ". Inbound email is not enabled on this SMTPfast deployment yet."
		}
	}
	return err.Error()
}
