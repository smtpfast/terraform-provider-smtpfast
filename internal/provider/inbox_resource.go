package provider

import (
	"context"
	"fmt"

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
	_ resource.Resource                = &inboxResource{}
	_ resource.ResourceWithConfigure   = &inboxResource{}
	_ resource.ResourceWithImportState = &inboxResource{}
)

// NewInboxResource returns a new smtpfast_inbox resource.
func NewInboxResource() resource.Resource {
	return &inboxResource{}
}

type inboxResource struct {
	client *client.Client
}

type inboxResourceModel struct {
	ID           types.String `tfsdk:"id"`
	EmailAddress types.String `tfsdk:"email_address"`
	Name         types.String `tfsdk:"name"`
	FromName     types.String `tfsdk:"from_name"`
	DomainID     types.String `tfsdk:"domain_id"`
	CreatedAt    types.String `tfsdk:"created_at"`
}

// The API collapses runs of whitespace in from_name and refuses <, > and @,
// so only a name already in that form comes back unchanged.

func (r *inboxResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_inbox"
}

func (r *inboxResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "An inbox: one address on your own domain organised into threads, labels, folders and drafts.\n\n" +
			"The address's domain must have receiving turned on (`receiving_enabled` on `smtpfast_domain`) and be able to send: verified for sending, " +
			"or a receiving subdomain of a verified domain. Inboxes need a paid plan. Mail the address received in the last 30 days is filed into threads when the inbox is created.\n\n" +
			"Deleting the inbox deletes its threads, labels and drafts. The address keeps receiving, because receiving belongs to the domain.\n\n" +
			"Needs a provider API key with the `domain:write` and `inbound:read` scopes, created by a team owner or admin.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				MarkdownDescription: "Unique identifier of the inbox.",
				Computed:            true,
				PlanModifiers:       []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"email_address": schema.StringAttribute{
				MarkdownDescription: "A plain lowercase address on one of your domains, such as `support@example.com`. Changing this forces a new resource.",
				Required:            true,
				Validators:          []validator.String{plainEmailAddress()},
				PlanModifiers:       []planmodifier.String{stringplanmodifier.RequiresReplace()},
			},
			"name": schema.StringAttribute{
				MarkdownDescription: "Internal name, up to 200 characters. Defaults to `email_address`.",
				Optional:            true,
				Computed:            true,
				Validators:          trimmedLine(200),
				PlanModifiers:       []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"from_name": schema.StringAttribute{
				MarkdownDescription: "The name recipients see, such as `Ada from Support`. A plain name, not a `Name <email>` address: no `<`, `>` or `@`. Up to 100 characters. Omit it to send from the bare address.",
				Optional:            true,
				Validators: []validator.String{
					jsLengthBetween(1, 100),
					plainName(),
				},
			},
			"domain_id": schema.StringAttribute{
				MarkdownDescription: "ID of the domain the address is on.",
				Computed:            true,
				PlanModifiers:       []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"created_at": schema.StringAttribute{
				MarkdownDescription: "Creation timestamp (RFC 3339).",
				Computed:            true,
				PlanModifiers:       []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
		},
	}
}

func (r *inboxResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
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

func (r *inboxResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan inboxResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	create := client.CreateInboxRequest{
		EmailAddress: plan.EmailAddress.ValueString(),
		FromName:     plan.FromName.ValueStringPointer(),
	}
	if !plan.Name.IsUnknown() && !plan.Name.IsNull() {
		create.Name = plan.Name.ValueStringPointer()
	}
	inbox, err := r.client.CreateInbox(ctx, create)
	if err != nil {
		resp.Diagnostics.AddError("Error creating inbox", err.Error())
		return
	}

	mapInboxToState(inbox, &plan)
	resp.Diagnostics.Append(resp.State.Set(ctx, plan)...)
}

func (r *inboxResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state inboxResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	inbox, err := r.client.GetInbox(ctx, state.ID.ValueString())
	if err != nil {
		if client.IsNotFound(err) {
			resp.State.RemoveResource(ctx)
			return
		}
		resp.Diagnostics.AddError("Error reading inbox", err.Error())
		return
	}

	mapInboxToState(inbox, &state)
	resp.Diagnostics.Append(resp.State.Set(ctx, state)...)
}

func (r *inboxResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan, state inboxResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	// from_name is always sent, so removing it from the configuration clears it.
	update := client.UpdateInboxRequest{FromName: plan.FromName.ValueStringPointer()}
	if !plan.Name.IsUnknown() && !plan.Name.IsNull() {
		update.Name = plan.Name.ValueStringPointer()
	}
	if err := r.client.UpdateInbox(ctx, state.ID.ValueString(), update); err != nil {
		resp.Diagnostics.AddError("Error updating inbox", err.Error())
		return
	}

	inbox, err := r.client.GetInbox(ctx, state.ID.ValueString())
	if err != nil {
		resp.Diagnostics.AddError("Error reading inbox after update", err.Error())
		return
	}
	mapInboxToState(inbox, &plan)
	resp.Diagnostics.Append(resp.State.Set(ctx, plan)...)
}

func (r *inboxResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state inboxResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	if err := r.client.DeleteInbox(ctx, state.ID.ValueString()); err != nil {
		if client.IsNotFound(err) {
			return
		}
		resp.Diagnostics.AddError("Error deleting inbox", err.Error())
	}
}

// ImportState imports by id. The email address works too: the read resolves
// it to the id.
func (r *inboxResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	resource.ImportStatePassthroughID(ctx, path.Root("id"), req, resp)
}

func mapInboxToState(inbox *client.Inbox, m *inboxResourceModel) {
	m.ID = types.StringValue(inbox.ID)
	m.EmailAddress = types.StringValue(inbox.EmailAddress)
	m.Name = types.StringValue(inbox.Name)
	m.FromName = types.StringPointerValue(inbox.FromName)
	m.DomainID = types.StringValue(inbox.DomainID)
	m.CreatedAt = types.StringValue(inbox.CreatedAt)
}
