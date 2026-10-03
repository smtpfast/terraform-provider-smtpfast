package provider

import (
	"context"
	"fmt"

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
	_ resource.Resource                   = &contactPropertyResource{}
	_ resource.ResourceWithConfigure      = &contactPropertyResource{}
	_ resource.ResourceWithImportState    = &contactPropertyResource{}
	_ resource.ResourceWithValidateConfig = &contactPropertyResource{}
)

// NewContactPropertyResource returns a new smtpfast_contact_property resource.
func NewContactPropertyResource() resource.Resource {
	return &contactPropertyResource{}
}

type contactPropertyResource struct {
	client *client.Client
}

type contactPropertyResourceModel struct {
	ID            types.String `tfsdk:"id"`
	Key           types.String `tfsdk:"key"`
	Type          types.String `tfsdk:"type"`
	FallbackValue types.String `tfsdk:"fallback_value"`
	CreatedAt     types.String `tfsdk:"created_at"`
}

func (r *contactPropertyResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_contact_property"
}

func (r *contactPropertyResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "A declared custom contact field. Declaring is optional, since contacts can carry undeclared properties, but it gives the field a default " +
			"for contacts that have no value and turns on type checking for writes to it.\n\n" +
			"Deleting the declaration leaves the values stored on contacts in place, so declaring the key again finds them.\n\n" +
			"Needs a provider API key with the `contact:read` and `contact:write` scopes.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				MarkdownDescription: "Unique identifier of the contact property.",
				Computed:            true,
				PlanModifiers:       []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"key": schema.StringAttribute{
				MarkdownDescription: "The property key, such as `plan`. 1 to 64 letters, digits, underscores, dots or hyphens. The built-in merge tags (`email`, `first_name`, `last_name`, `unsubscribe_url`, ...) cannot be declared. Changing this forces a new resource.",
				Required:            true,
				Validators: []validator.String{
					stringvalidator.RegexMatches(contactPropertyKeyRegex, "must be 1 to 64 letters, digits, underscores, dots or hyphens"),
					notReserved(reservedContactPropertyKeys),
				},
				PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplace()},
			},
			"type": schema.StringAttribute{
				MarkdownDescription: "`string` or `number`. Changing this forces a new resource.",
				Required:            true,
				Validators:          []validator.String{stringvalidator.OneOf(variableTypes...)},
				PlanModifiers:       []planmodifier.String{stringplanmodifier.RequiresReplace()},
			},
			"fallback_value": schema.StringAttribute{
				MarkdownDescription: "Used when a contact has no value. Always written as a string; for a `number` property it must be a number, such as `\"0\"`. Up to 500 characters. Updates in place; omit it for no default.",
				Optional:            true,
				Validators:          []validator.String{stringvalidator.UTF8LengthAtMost(500)},
			},
			"created_at": schema.StringAttribute{
				MarkdownDescription: "Creation timestamp (RFC 3339).",
				Computed:            true,
				PlanModifiers:       []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
		},
	}
}

func (r *contactPropertyResource) ValidateConfig(ctx context.Context, req resource.ValidateConfigRequest, resp *resource.ValidateConfigResponse) {
	var m contactPropertyResourceModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &m)...)
	if resp.Diagnostics.HasError() || m.Type.IsUnknown() || m.FallbackValue.IsUnknown() {
		return
	}
	if _, err := fallbackToAPI(m.Type.ValueString(), m.FallbackValue); err != nil {
		resp.Diagnostics.AddAttributeError(path.Root("fallback_value"), "Invalid fallback value", err.Error())
	}
}

func (r *contactPropertyResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
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

func (r *contactPropertyResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan contactPropertyResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	fallback, err := fallbackToAPI(plan.Type.ValueString(), plan.FallbackValue)
	if err != nil {
		resp.Diagnostics.AddAttributeError(path.Root("fallback_value"), "Invalid fallback value", err.Error())
		return
	}
	prop, err := r.client.CreateContactProperty(ctx, client.CreateContactPropertyRequest{
		Key:           plan.Key.ValueString(),
		Type:          plan.Type.ValueString(),
		FallbackValue: fallback,
	})
	if err != nil {
		resp.Diagnostics.AddError("Error creating contact property", err.Error())
		return
	}

	mapContactPropertyToState(prop, &plan)
	resp.Diagnostics.Append(resp.State.Set(ctx, plan)...)
}

func (r *contactPropertyResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state contactPropertyResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	prop, err := r.client.GetContactProperty(ctx, state.ID.ValueString())
	if err != nil {
		if client.IsNotFound(err) {
			resp.State.RemoveResource(ctx)
			return
		}
		resp.Diagnostics.AddError("Error reading contact property", err.Error())
		return
	}

	mapContactPropertyToState(prop, &state)
	resp.Diagnostics.Append(resp.State.Set(ctx, state)...)
}

// Update changes the fallback value, the only field the API lets change.
func (r *contactPropertyResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan, state contactPropertyResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	fallback, err := fallbackToAPI(plan.Type.ValueString(), plan.FallbackValue)
	if err != nil {
		resp.Diagnostics.AddAttributeError(path.Root("fallback_value"), "Invalid fallback value", err.Error())
		return
	}
	prop, err := r.client.UpdateContactPropertyFallback(ctx, state.ID.ValueString(), fallback)
	if err != nil {
		resp.Diagnostics.AddError("Error updating contact property", err.Error())
		return
	}

	mapContactPropertyToState(prop, &plan)
	resp.Diagnostics.Append(resp.State.Set(ctx, plan)...)
}

func (r *contactPropertyResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state contactPropertyResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	if err := r.client.DeleteContactProperty(ctx, state.ID.ValueString()); err != nil {
		if client.IsNotFound(err) {
			return
		}
		resp.Diagnostics.AddError("Error deleting contact property", err.Error())
	}
}

func (r *contactPropertyResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	resource.ImportStatePassthroughID(ctx, path.Root("id"), req, resp)
}

func mapContactPropertyToState(p *client.ContactProperty, m *contactPropertyResourceModel) {
	m.ID = types.StringValue(p.ID)
	m.Key = types.StringValue(p.Key)
	m.Type = types.StringValue(p.Type)
	m.FallbackValue = fallbackFromAPI(p.FallbackValue, m.FallbackValue)
	m.CreatedAt = types.StringValue(p.CreatedAt)
}
