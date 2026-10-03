package provider

import (
	"context"
	"fmt"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
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
	_ resource.Resource                = &inboxLabelResource{}
	_ resource.ResourceWithConfigure   = &inboxLabelResource{}
	_ resource.ResourceWithImportState = &inboxLabelResource{}
)

// NewInboxLabelResource returns a new smtpfast_inbox_label resource.
func NewInboxLabelResource() resource.Resource {
	return &inboxLabelResource{}
}

type inboxLabelResource struct {
	client *client.Client
}

type inboxLabelResourceModel struct {
	ID        types.String `tfsdk:"id"`
	InboxID   types.String `tfsdk:"inbox_id"`
	Name      types.String `tfsdk:"name"`
	Color     types.String `tfsdk:"color"`
	CreatedAt types.String `tfsdk:"created_at"`
}

func (r *inboxLabelResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_inbox_label"
}

func (r *inboxLabelResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "A named, colored label in an inbox, for tagging its threads. An inbox can have up to 100 labels.\n\n" +
			"Deleting the label takes it off every thread that carries it; no message is removed. Deleting the inbox deletes its labels.\n\n" +
			"Needs a provider API key with the `inbound:read` scope.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				MarkdownDescription: "Unique identifier of the label.",
				Computed:            true,
				PlanModifiers:       []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"inbox_id": schema.StringAttribute{
				MarkdownDescription: "ID of the inbox, such as `smtpfast_inbox.support.id`. Changing this forces a new resource.",
				Required:            true,
				Validators:          []validator.String{stringvalidator.LengthAtLeast(1)},
				PlanModifiers:       []planmodifier.String{stringplanmodifier.RequiresReplace()},
			},
			"name": schema.StringAttribute{
				MarkdownDescription: "Name of the label, up to 64 characters, unique in the inbox ignoring case. Updates in place.",
				Required:            true,
				Validators:          trimmedLine(64),
			},
			"color": schema.StringAttribute{
				MarkdownDescription: "One of " + backtickList(inboxLabelColors) + ". `mauve` on create when omitted. " +
					"When omitted later, Terraform keeps the current color, so a color picked in the dashboard stays.",
				Optional:      true,
				Computed:      true,
				Validators:    []validator.String{stringvalidator.OneOf(inboxLabelColors...)},
				PlanModifiers: []planmodifier.String{keepStateOrDefaultString{def: "mauve"}},
			},
			"created_at": schema.StringAttribute{
				MarkdownDescription: "Creation timestamp (RFC 3339).",
				Computed:            true,
				PlanModifiers:       []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
		},
	}
}

func (r *inboxLabelResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
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

func (r *inboxLabelResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan inboxLabelResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	label, err := r.client.CreateInboxLabel(ctx, plan.InboxID.ValueString(), client.InboxLabelRequest{
		Name:  plan.Name.ValueString(),
		Color: plan.Color.ValueString(),
	})
	if err != nil {
		resp.Diagnostics.AddError("Error creating inbox label", err.Error())
		return
	}

	mapInboxLabelToState(label, &plan)
	resp.Diagnostics.Append(resp.State.Set(ctx, plan)...)
}

func (r *inboxLabelResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state inboxLabelResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	// A 404 means the label or its whole inbox is gone.
	label, err := r.client.GetInboxLabel(ctx, state.InboxID.ValueString(), state.ID.ValueString())
	if err != nil {
		if client.IsNotFound(err) {
			resp.State.RemoveResource(ctx)
			return
		}
		resp.Diagnostics.AddError("Error reading inbox label", err.Error())
		return
	}

	mapInboxLabelToState(label, &state)
	resp.Diagnostics.Append(resp.State.Set(ctx, state)...)
}

func (r *inboxLabelResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan, state inboxLabelResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	inboxID, id := state.InboxID.ValueString(), state.ID.ValueString()
	err := r.client.UpdateInboxLabel(ctx, inboxID, id, client.InboxLabelRequest{
		Name:  plan.Name.ValueString(),
		Color: plan.Color.ValueString(),
	})
	if err != nil {
		resp.Diagnostics.AddError("Error updating inbox label", err.Error())
		return
	}

	label, err := r.client.GetInboxLabel(ctx, inboxID, id)
	if err != nil {
		resp.Diagnostics.AddError("Error reading inbox label after update", err.Error())
		return
	}
	mapInboxLabelToState(label, &plan)
	resp.Diagnostics.Append(resp.State.Set(ctx, plan)...)
}

func (r *inboxLabelResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state inboxLabelResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	if err := r.client.DeleteInboxLabel(ctx, state.InboxID.ValueString(), state.ID.ValueString()); err != nil {
		if client.IsNotFound(err) {
			return
		}
		resp.Diagnostics.AddError("Error deleting inbox label", err.Error())
	}
}

// ImportState takes "<inbox_id>/<label_id>". Label IDs have no slash, so the
// last one separates the two.
func (r *inboxLabelResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	i := strings.LastIndex(req.ID, "/")
	if i <= 0 || i == len(req.ID)-1 {
		resp.Diagnostics.AddError("Invalid import ID", fmt.Sprintf("Expected <inbox_id>/<label_id>, got %q.", req.ID))
		return
	}
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("inbox_id"), req.ID[:i])...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("id"), req.ID[i+1:])...)
}

func mapInboxLabelToState(label *client.InboxLabel, m *inboxLabelResourceModel) {
	m.ID = types.StringValue(label.ID)
	m.Name = types.StringValue(label.Name)
	m.Color = types.StringValue(label.Color)
	m.CreatedAt = types.StringValue(label.CreatedAt)
}
