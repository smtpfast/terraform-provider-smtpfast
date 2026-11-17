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

func (r *domainResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_domain"
}

func (r *domainResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: domainResourceDescription,
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
				MarkdownDescription: "Verification status: `pending`, `verified`, or `failed`. `verified` means the domain can send. To wait for it in the same apply, add a `smtpfast_domain_verification`.",
				Computed:            true,
			},
			"dkim_status": schema.StringAttribute{
				MarkdownDescription: "DKIM status from the last check: `pending` or `verified`.",
				Computed:            true,
			},
			"spf_status": schema.StringAttribute{
				MarkdownDescription: "SPF status from the last check: `pending` or `verified`. SPF does not gate sending.",
				Computed:            true,
			},
			"dmarc_status": schema.StringAttribute{
				MarkdownDescription: "DMARC status from the last check: `pending` or `verified`. DMARC does not gate sending.",
				Computed:            true,
			},
			"mail_from_status": schema.StringAttribute{
				MarkdownDescription: "Custom MAIL FROM status from the last check: `pending`, `verified`, or `failed`.",
				Computed:            true,
			},
			"region": schema.StringAttribute{
				MarkdownDescription: "The Amazon SES region that sends for this domain, such as `eu-west-2`. SMTPfast picks it; there is no input for it.",
				Computed:            true,
				PlanModifiers:       []planmodifier.String{useKnownStateForUnknown{}},
			},
			"mail_from_domain": schema.StringAttribute{
				MarkdownDescription: "The custom MAIL FROM (bounce) domain, always `bounce.<domain>`. Its MX and SPF records are `records.mail_from_mx` and `records.mail_from_spf`.",
				Computed:            true,
				PlanModifiers:       []planmodifier.String{useKnownStateForUnknown{}},
			},
			"spf_include": schema.StringAttribute{
				MarkdownDescription: "The domain SMTPfast needs in the apex SPF record, such as `amazonses.com`. Use it to merge SMTPfast into an SPF record that already lists other senders: `include:${smtpfast_domain.example.spf_include}`. A domain must have only one SPF record.",
				Computed:            true,
				PlanModifiers:       []planmodifier.String{useKnownStateForUnknown{}},
			},
			"receiving_enabled": schema.BoolAttribute{
				MarkdownDescription: "Turn inbound email on for this domain (paid plans). The domain must be verified for sending, or be a subdomain of a domain that is already verified on the team. For a brand-new domain that is not such a subdomain, set this in a later apply once `status` is `verified`. When enabled, `records` gains `receiving_mx`, the MX record (with `priority`) to publish. Leaving it unset keeps whatever the domain currently has; a new domain starts with receiving off.",
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
			"records": schema.MapNestedAttribute{
				MarkdownDescription: "DNS records to publish, keyed by role: `dkim_1`, `dkim_2`, `dkim_3`, `spf`, `dmarc`, `mail_from_mx`, `mail_from_spf`, and `receiving_mx` while receiving is on. " +
					"The keys are known when the domain is planned, before it exists, so `for_each = smtpfast_domain.example.records` works on the first apply.",
				Computed:     true,
				NestedObject: schema.NestedAttributeObject{Attributes: dnsRecordSchemaAttributes()},
			},
			"dns_records": schema.ListNestedAttribute{
				MarkdownDescription: "**Deprecated**: use `records`. The same records as a list, which is unknown until the domain exists, so a `for_each` over it needs a `-target` apply first.",
				DeprecationMessage:  "Use records instead. Its keys are known at plan time, so for_each over it works on the first apply.",
				Computed:            true,
				NestedObject:        schema.NestedAttributeObject{Attributes: dnsRecordSchemaAttributes()},
			},
		},
	}
}

const domainResourceDescription = "A sending domain on SMTPfast. Creating one returns the DNS records you must publish (DKIM, SPF, DMARC, MAIL FROM) to verify it. " +
	"Publish `records` with your DNS provider (Cloudflare, Route 53, ...) to set up the whole sending domain in one apply, and add a `smtpfast_domain_verification` to wait until it can send.\n\n" +
	"`records` is a map with fixed keys (`dkim_1`, `dkim_2`, `dkim_3`, `spf`, `dmarc`, `mail_from_mx`, `mail_from_spf`, and `receiving_mx` while receiving is on), so a `for_each` over it plans before the domain exists. " +
	"Use the keys as the `for_each` keys: a change of a record's value then updates that DNS record in place.\n\n" +
	"SPF: a domain may have only one SPF record. If the domain already sends through another service, merge SMTPfast into the existing record with `spf_include` instead of adding a second one (SMTPfast then reports `multiple_spf_records`).\n\n" +
	"Deleting the domain stops all sending from it, and adding it again gives new DKIM records. For a production domain, add `lifecycle { prevent_destroy = true }`.\n\n" +
	"Needs a provider API key with the `domain:read` and `domain:write` scopes, created by a team owner or admin."

// dnsRecordSchemaAttributes are the attributes of one DNS record, in records
// and dns_records.
func dnsRecordSchemaAttributes() map[string]schema.Attribute {
	return map[string]schema.Attribute{
		"type":     schema.StringAttribute{MarkdownDescription: "DNS record type (CNAME, TXT, MX).", Computed: true},
		"name":     schema.StringAttribute{MarkdownDescription: "Record name/host.", Computed: true},
		"value":    schema.StringAttribute{MarkdownDescription: "Record value.", Computed: true},
		"priority": schema.Int64Attribute{MarkdownDescription: "Priority, set on MX records only.", Computed: true},
		"purpose":  schema.StringAttribute{MarkdownDescription: "What the record is for, as the API words it, such as `DKIM authentication`, `SPF authorization`, `DMARC policy`, `MAIL FROM bounce delivery`, `MAIL FROM SPF` or `Inbound mail`.", Computed: true},
		"category": schema.StringAttribute{MarkdownDescription: "Record group: `verification` (DKIM, DMARC), `sending` (SPF, MAIL FROM) or `receiving` (inbound MX).", Computed: true},
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

	// Read before mapToState overwrites the plan with the create response,
	// which has receiving off.
	receiving := plan.ReceivingEnabled.ValueBool()
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
	if receiving {
		if _, err := r.client.SetDomainReceiving(ctx, domain.ID, true); err != nil {
			resp.Diagnostics.AddError("Error enabling receiving", receivingErrorHint(err))
			return
		}
	}
	// GET has the region and the check statuses, which the create response
	// does not. Without receiving, the create response is enough to go on.
	refreshed, err := r.client.GetDomain(ctx, domain.ID)
	switch {
	case err == nil:
		domain = refreshed
	case receiving:
		resp.Diagnostics.AddError("Error reading domain after enabling receiving", err.Error())
		return
	default:
		resp.Diagnostics.AddWarning("Could not read the domain after creating it", err.Error()+". The next refresh reads it again.")
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

// ModifyPlan keeps the records known at plan time, so a for_each over them
// works:
//
//   - On create, records gets its fixed keys, with values known after apply.
//   - When receiving is turned on or off, records and dns_records are the
//     current ones with the receiving MX record added or removed. The MX
//     values come from the last read; without them, dns_records stays
//     unknown, and records still gets its receiving_mx key.
func (r *domainResource) ModifyPlan(ctx context.Context, req resource.ModifyPlanRequest, resp *resource.ModifyPlanResponse) {
	if req.Plan.Raw.IsNull() {
		return
	}
	if req.State.Raw.IsNull() {
		// A create, or the create half of a replace: Terraform plans that
		// with no prior state.
		var receiving types.Bool
		resp.Diagnostics.Append(req.Config.GetAttribute(ctx, path.Root("receiving_enabled"), &receiving)...)
		if resp.Diagnostics.HasError() || receiving.IsUnknown() {
			return
		}
		elems := map[string]attr.Value{}
		for _, key := range plannedRecordKeys(receiving.ValueBool()) {
			elems[key] = types.ObjectUnknown(dnsRecordAttrTypes)
		}
		records, diags := types.MapValue(dnsRecordObjectType, elems)
		resp.Diagnostics.Append(diags...)
		if resp.Diagnostics.HasError() {
			return
		}
		resp.Diagnostics.Append(resp.Plan.SetAttribute(ctx, path.Root("records"), records)...)
		return
	}

	var plan, state domainResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if !plan.Domain.Equal(state.Domain) || plan.ReceivingEnabled.IsUnknown() || plan.ReceivingEnabled.Equal(state.ReceivingEnabled) {
		return
	}

	var mx *dnsRecordModel
	raw, diags := req.Private.GetKey(ctx, receivingMXKey)
	resp.Diagnostics.Append(diags...)
	var saved client.DNSRecord
	if plan.ReceivingEnabled.ValueBool() && len(raw) > 0 && json.Unmarshal(raw, &saved) == nil && saved.Value != "" && saved.Priority != nil {
		name := saved.Name
		if name == "" {
			name = state.Domain.ValueString()
		}
		// Purpose and category are left to the apply.
		mx = &dnsRecordModel{
			Type:     types.StringValue("MX"),
			Name:     types.StringValue(name),
			Value:    types.StringValue(saved.Value),
			Priority: types.Int64Value(*saved.Priority),
			Purpose:  types.StringUnknown(),
			Category: types.StringUnknown(),
		}
	}

	stateRecords := state.Records
	if stateRecords.IsNull() {
		// State from 0.6 has only dns_records until the next refresh.
		rebuilt, diags := recordsFromList(ctx, state.DNSRecords, state.Domain.ValueString())
		resp.Diagnostics.Append(diags...)
		stateRecords = rebuilt
	}
	if plan.Records.IsUnknown() && !stateRecords.IsNull() && !stateRecords.IsUnknown() {
		elems := map[string]attr.Value{}
		for key, value := range stateRecords.Elements() {
			if key != recordKeyReceivingMX {
				elems[key] = value
			}
		}
		if plan.ReceivingEnabled.ValueBool() {
			elems[recordKeyReceivingMX] = types.ObjectUnknown(dnsRecordAttrTypes)
			if mx != nil {
				obj, diags := types.ObjectValueFrom(ctx, dnsRecordAttrTypes, *mx)
				resp.Diagnostics.Append(diags...)
				elems[recordKeyReceivingMX] = obj
			}
		}
		records, diags := types.MapValue(dnsRecordObjectType, elems)
		resp.Diagnostics.Append(diags...)
		if resp.Diagnostics.HasError() {
			return
		}
		resp.Diagnostics.Append(resp.Plan.SetAttribute(ctx, path.Root("records"), records)...)
	}

	if !plan.DNSRecords.IsUnknown() || state.DNSRecords.IsNull() || state.DNSRecords.IsUnknown() || (plan.ReceivingEnabled.ValueBool() && mx == nil) {
		return
	}
	var list []dnsRecordModel
	resp.Diagnostics.Append(state.DNSRecords.ElementsAs(ctx, &list, false)...)
	if resp.Diagnostics.HasError() {
		return
	}
	// The receiving MX sits on the domain itself; the MAIL FROM MX is on its
	// bounce subdomain and stays.
	kept := make([]dnsRecordModel, 0, len(list)+1)
	for _, rec := range list {
		if rec.Type.ValueString() == "MX" && rec.Name.ValueString() == state.Domain.ValueString() {
			continue
		}
		kept = append(kept, rec)
	}
	if mx != nil {
		kept = append(kept, *mx)
	}
	dnsRecords, diags := types.ListValueFrom(ctx, dnsRecordObjectType, kept)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}
	resp.Diagnostics.Append(resp.Plan.SetAttribute(ctx, path.Root("dns_records"), dnsRecords)...)
}

// recordsFromList builds the records map from a dns_records list in state,
// for state written before records existed. The list has no purpose or
// category, so those are left to the apply.
func recordsFromList(ctx context.Context, list types.List, domain string) (types.Map, diag.Diagnostics) {
	if list.IsNull() || list.IsUnknown() {
		return types.MapNull(dnsRecordObjectType), nil
	}
	var models []dnsRecordModel
	diags := list.ElementsAs(ctx, &models, false)
	if diags.HasError() {
		return types.MapNull(dnsRecordObjectType), diags
	}
	records := make([]client.DNSRecord, 0, len(models))
	for _, m := range models {
		rec := client.DNSRecord{Type: m.Type.ValueString(), Name: m.Name.ValueString(), Value: m.Value.ValueString()}
		if !m.Priority.IsNull() && !m.Priority.IsUnknown() {
			p := m.Priority.ValueInt64()
			rec.Priority = &p
		}
		records = append(records, rec)
	}
	elems := map[string]attr.Value{}
	for k, rec := range keyedRecords(records, domain) {
		m := dnsRecordToModel(rec)
		m.Purpose, m.Category = types.StringUnknown(), types.StringUnknown()
		obj, d := types.ObjectValueFrom(ctx, dnsRecordAttrTypes, m)
		diags.Append(d...)
		elems[k] = obj
	}
	out, d := types.MapValue(dnsRecordObjectType, elems)
	diags.Append(d...)
	return out, diags
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
	var diags diag.Diagnostics
	m.ID = types.StringValue(d.ID)
	m.Domain = types.StringValue(d.Domain)
	m.Status = types.StringValue(d.Status)

	list, listDiags := dnsRecordsToList(ctx, d.DNSRecords)
	diags.Append(listDiags...)
	m.DNSRecords = list
	records, mapDiags := dnsRecordsToMap(ctx, d.DNSRecords, d.Domain)
	diags.Append(mapDiags...)
	m.Records = records

	details := describeDomain(d)
	m.Region = details.Region
	m.MailFromDomain = details.MailFromDomain
	m.SPFInclude = details.SPFInclude
	m.DKIMStatus = details.DKIMStatus
	m.SPFStatus = details.SPFStatus
	m.DMARCStatus = details.DMARCStatus
	m.MailFromStatus = details.MailFromStatus

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
