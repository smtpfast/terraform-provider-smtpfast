package provider

import (
	"context"
	"fmt"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework-validators/listvalidator"
	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/listdefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-log/tflog"

	"github.com/smtpfast/terraform-provider-smtpfast/internal/client"
)

var (
	_ resource.Resource                = &apiKeyResource{}
	_ resource.ResourceWithConfigure   = &apiKeyResource{}
	_ resource.ResourceWithImportState = &apiKeyResource{}
)

// NewAPIKeyResource returns a new smtpfast_api_key resource.
func NewAPIKeyResource() resource.Resource {
	return &apiKeyResource{}
}

type apiKeyResource struct {
	client *client.Client
}

type apiKeyResourceModel struct {
	ID        types.String `tfsdk:"id"`
	Name      types.String `tfsdk:"name"`
	Scopes    types.List   `tfsdk:"scopes"`
	Key       types.String `tfsdk:"key"`
	Prefix    types.String `tfsdk:"prefix"`
	CreatedAt types.String `tfsdk:"created_at"`
}

func (r *apiKeyResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_api_key"
}

func (r *apiKeyResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "An SMTPfast API key. The full secret (`key`) is only returned once, on create, and stored in state. Treat your state as sensitive. " +
			"`name` and `scopes` update in place and the secret stays the same; scope changes apply on the key's next request.\n\n" +
			"Managing API keys needs a provider API key with the `apikey:manage` scope, created by a team owner or admin.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				MarkdownDescription: "Unique identifier of the API key.",
				Computed:            true,
				PlanModifiers:       []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"name": schema.StringAttribute{
				MarkdownDescription: "Human-readable name for the key, up to 100 characters. Updates in place.",
				Required:            true,
				Validators:          trimmedLine(100),
			},
			"scopes": schema.ListAttribute{
				MarkdownDescription: "What the key may do. One or more of " + backtickList(allAPIKeyScopes) + ". " +
					"When omitted, the key gets the API's default set: every scope except `logs:read`, `inbound:read`, `inbound:delete`, `team:read`, `team:manage` and `apikey:manage`. " +
					"Order does not matter. Updates in place, and changes made outside Terraform show up as drift.",
				Optional:    true,
				Computed:    true,
				ElementType: types.StringType,
				Default:     listdefault.StaticValue(stringList(defaultAPIKeyScopes)),
				Validators: []validator.List{
					listvalidator.SizeAtLeast(1),
					listvalidator.UniqueValues(),
					listvalidator.ValueStringsAre(stringvalidator.OneOf(allAPIKeyScopes...)),
				},
				PlanModifiers: []planmodifier.List{keepStateOrderForDefault{}},
			},
			"key": schema.StringAttribute{
				MarkdownDescription: "The full API key secret. Only known immediately after creation; empty after an import.",
				Computed:            true,
				Sensitive:           true,
				PlanModifiers:       []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"prefix": schema.StringAttribute{
				MarkdownDescription: "Non-secret prefix of the key, useful for identifying it.",
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

func (r *apiKeyResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
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

func (r *apiKeyResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan apiKeyResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	var scopes []string
	resp.Diagnostics.Append(plan.Scopes.ElementsAs(ctx, &scopes, false)...)
	if resp.Diagnostics.HasError() {
		return
	}

	// Scopes are always sent, the default set included, so the key gets
	// exactly what the plan shows.
	key, err := r.client.CreateAPIKey(ctx, client.CreateAPIKeyRequest{
		Name:   plan.Name.ValueString(),
		Scopes: scopes,
	})
	if err != nil {
		resp.Diagnostics.AddError("Error creating API key", err.Error())
		return
	}

	plan.ID = types.StringValue(key.ID)
	plan.Name = types.StringValue(key.Name)
	plan.Key = types.StringValue(key.Key)
	plan.Prefix = types.StringValue(key.Prefix)
	plan.CreatedAt = types.StringValue(key.CreatedAt)
	resp.Diagnostics.Append(reconcileStringList(key.Scopes, &plan.Scopes)...)

	resp.Diagnostics.Append(resp.State.Set(ctx, plan)...)
}

func (r *apiKeyResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state apiKeyResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	key, err := r.client.GetAPIKey(ctx, state.ID.ValueString())
	if err != nil {
		if client.IsNotFound(err) {
			resp.State.RemoveResource(ctx)
			return
		}
		resp.Diagnostics.AddError("Error reading API key", err.Error())
		return
	}
	if key.RevokedAt != "" {
		// A revoked key cannot be used or edited again: let Terraform create a new one.
		tflog.Warn(ctx, "API key was revoked outside Terraform, removing it from state", map[string]any{"id": key.ID, "revoked_at": key.RevokedAt})
		resp.State.RemoveResource(ctx)
		return
	}

	// The secret is never returned by reads; keep it from state.
	state.ID = types.StringValue(key.ID)
	state.Name = types.StringValue(key.Name)
	if key.Prefix != "" {
		state.Prefix = types.StringValue(key.Prefix)
	}
	if key.CreatedAt != "" {
		state.CreatedAt = types.StringValue(key.CreatedAt)
	}
	resp.Diagnostics.Append(reconcileStringList(key.Scopes, &state.Scopes)...)

	resp.Diagnostics.Append(resp.State.Set(ctx, state)...)
}

func (r *apiKeyResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan, state apiKeyResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	var planScopes, stateScopes []string
	resp.Diagnostics.Append(plan.Scopes.ElementsAs(ctx, &planScopes, false)...)
	if !state.Scopes.IsNull() && !state.Scopes.IsUnknown() {
		resp.Diagnostics.Append(state.Scopes.ElementsAs(ctx, &stateScopes, false)...)
	}
	if resp.Diagnostics.HasError() {
		return
	}

	// Only send what changed: the API records every scopes write in the team
	// audit log, even one that changes nothing.
	var update client.UpdateAPIKeyRequest
	if !plan.Name.Equal(state.Name) {
		name := plan.Name.ValueString()
		update.Name = &name
	}
	if !sameStringSet(planScopes, stateScopes) {
		update.Scopes = planScopes
	}

	if update.Name != nil || update.Scopes != nil {
		key, err := r.client.UpdateAPIKey(ctx, state.ID.ValueString(), update)
		if err != nil {
			resp.Diagnostics.AddError("Error updating API key", err.Error())
			return
		}
		plan.Name = types.StringValue(key.Name)
		resp.Diagnostics.Append(reconcileStringList(key.Scopes, &plan.Scopes)...)
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, plan)...)
}

func (r *apiKeyResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state apiKeyResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	if err := r.client.DeleteAPIKey(ctx, state.ID.ValueString()); err != nil {
		if client.IsNotFound(err) || strings.Contains(err.Error(), "already revoked") {
			return
		}
		resp.Diagnostics.AddError("Error deleting API key", err.Error())
	}
}

func (r *apiKeyResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	resource.ImportStatePassthroughID(ctx, path.Root("id"), req, resp)
}

// reconcileStringList sets current to the values the API reports, keeping the
// current order when the two hold the same values. Order means nothing for
// scopes or webhook events, and the API may store them in another order.
func reconcileStringList(api []string, current *types.List) diag.Diagnostics {
	if api == nil {
		// Nothing reported: keep what we have.
		return nil
	}
	if !current.IsNull() && !current.IsUnknown() {
		existing := make([]string, 0, len(current.Elements()))
		for _, e := range current.Elements() {
			if s, ok := e.(types.String); ok {
				existing = append(existing, s.ValueString())
			}
		}
		if sameStringSet(existing, api) {
			return nil
		}
	}
	list, diags := types.ListValueFrom(context.Background(), types.StringType, api)
	*current = list
	return diags
}

// keepStateOrderForDefault keeps the scopes in state when scopes is not
// configured and the state already holds the default set in another order,
// such as after an import.
type keepStateOrderForDefault struct{}

func (keepStateOrderForDefault) Description(context.Context) string {
	return "Keeps the state order of the default scopes."
}

func (m keepStateOrderForDefault) MarkdownDescription(ctx context.Context) string {
	return m.Description(ctx)
}

func (keepStateOrderForDefault) PlanModifyList(ctx context.Context, req planmodifier.ListRequest, resp *planmodifier.ListResponse) {
	if !req.ConfigValue.IsNull() || req.StateValue.IsNull() || req.StateValue.IsUnknown() || req.PlanValue.IsUnknown() {
		return
	}
	var planned, state []string
	resp.Diagnostics.Append(req.PlanValue.ElementsAs(ctx, &planned, false)...)
	resp.Diagnostics.Append(req.StateValue.ElementsAs(ctx, &state, false)...)
	if !resp.Diagnostics.HasError() && sameStringSet(planned, state) {
		resp.PlanValue = req.StateValue
	}
}

func stringList(values []string) types.List {
	elems := make([]types.String, len(values))
	for i, v := range values {
		elems[i] = types.StringValue(v)
	}
	list, _ := types.ListValueFrom(context.Background(), types.StringType, elems)
	return list
}

func backtickList(values []string) string {
	quoted := make([]string, len(values))
	for i, v := range values {
		quoted[i] = "`" + v + "`"
	}
	return strings.Join(quoted, ", ")
}
