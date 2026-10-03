package provider

import (
	"context"
	"fmt"
	"regexp"

	"github.com/hashicorp/terraform-plugin-framework-validators/listvalidator"
	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/booldefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/smtpfast/terraform-provider-smtpfast/internal/client"
)

var (
	_ resource.Resource                = &webhookResource{}
	_ resource.ResourceWithConfigure   = &webhookResource{}
	_ resource.ResourceWithImportState = &webhookResource{}
)

// NewWebhookResource returns a new smtpfast_webhook resource.
func NewWebhookResource() resource.Resource {
	return &webhookResource{}
}

type webhookResource struct {
	client *client.Client
}

type webhookResourceModel struct {
	ID            types.String `tfsdk:"id"`
	URL           types.String `tfsdk:"url"`
	Events        types.List   `tfsdk:"events"`
	Format        types.String `tfsdk:"format"`
	Active        types.Bool   `tfsdk:"active"`
	SigningSecret types.String `tfsdk:"signing_secret"`
	CreatedAt     types.String `tfsdk:"created_at"`
}

func (r *webhookResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_webhook"
}

func (r *webhookResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "A webhook subscription that delivers SMTPfast events (delivered, bounced, opened, clicked, received, domain and contact changes, ...) to a URL. Webhooks need a paid plan, and a team can have up to 5.\n\n" +
			"Needs a provider API key with the `webhook:read` and `webhook:write` scopes, created by a team owner or admin.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				MarkdownDescription: "Unique identifier of the webhook.",
				Computed:            true,
				PlanModifiers:       []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"url": schema.StringAttribute{
				MarkdownDescription: "The HTTPS URL that receives webhook POST requests. It must be publicly reachable: localhost and private addresses are refused.",
				Required:            true,
				Validators: []validator.String{
					stringvalidator.RegexMatches(regexp.MustCompile(`^https://\S+$`), "must be an https:// URL"),
				},
			},
			"events": schema.ListAttribute{
				MarkdownDescription: "Event types to subscribe to. One or more of " + backtickList(webhookEvents) + ". Order does not matter.",
				Required:            true,
				ElementType:         types.StringType,
				Validators: []validator.List{
					listvalidator.SizeAtLeast(1),
					listvalidator.UniqueValues(),
					listvalidator.ValueStringsAre(stringvalidator.OneOf(webhookEvents...)),
				},
			},
			"format": schema.StringAttribute{
				MarkdownDescription: "Delivery format: `standard` (signed JSON for your own endpoint), `discord` or `slack` (readable messages for a chat channel, not signed). " +
					"When omitted on create, the API picks `discord` or `slack` for Discord and Slack webhook URLs and `standard` otherwise. It does not pick again when `url` changes later, so set it if you move between them.",
				Optional: true,
				Computed: true,
				Validators: []validator.String{
					stringvalidator.OneOf(webhookFormats...),
				},
				PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"active": schema.BoolAttribute{
				MarkdownDescription: "Whether events are delivered. Set to `false` to pause delivery without deleting the webhook. Defaults to `true`.",
				Optional:            true,
				Computed:            true,
				Default:             booldefault.StaticBool(true),
			},
			"signing_secret": schema.StringAttribute{
				MarkdownDescription: "The secret that signs `standard` deliveries (the `X-SMTPfast-Signature` header). Only returned on create, so it is empty after an import.",
				Computed:            true,
				Sensitive:           true,
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

func (r *webhookResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
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

func (r *webhookResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan webhookResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	var events []string
	resp.Diagnostics.Append(plan.Events.ElementsAs(ctx, &events, false)...)
	if resp.Diagnostics.HasError() {
		return
	}

	wh, err := r.client.CreateWebhook(ctx, client.CreateWebhookRequest{
		URL:    plan.URL.ValueString(),
		Events: events,
		Format: plan.Format.ValueString(),
	})
	if err != nil {
		resp.Diagnostics.AddError("Error creating webhook", err.Error())
		return
	}

	// The signing secret is only in this response: save it before anything
	// else can fail.
	wantActive := plan.Active.IsNull() || plan.Active.ValueBool()
	plan.SigningSecret = types.StringNull()
	if wh.SigningSecret != "" {
		plan.SigningSecret = types.StringValue(wh.SigningSecret)
	}
	resp.Diagnostics.Append(mapWebhookToState(wh, &plan)...)
	if !wantActive && wh.Active {
		// Create always starts active; pause it as configured.
		resp.Diagnostics.Append(resp.State.Set(ctx, plan)...)
		active := false
		updated, err := r.client.UpdateWebhook(ctx, wh.ID, client.UpdateWebhookRequest{Active: &active})
		if err != nil {
			resp.Diagnostics.AddError("Error pausing webhook", err.Error())
			return
		}
		resp.Diagnostics.Append(mapWebhookToState(updated, &plan)...)
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, plan)...)
}

func (r *webhookResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state webhookResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	wh, err := r.client.GetWebhook(ctx, state.ID.ValueString())
	if err != nil {
		if client.IsNotFound(err) {
			resp.State.RemoveResource(ctx)
			return
		}
		resp.Diagnostics.AddError("Error reading webhook", err.Error())
		return
	}

	resp.Diagnostics.Append(mapWebhookToState(wh, &state)...)
	resp.Diagnostics.Append(resp.State.Set(ctx, state)...)
}

func (r *webhookResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan, state webhookResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	var events []string
	resp.Diagnostics.Append(plan.Events.ElementsAs(ctx, &events, false)...)
	if resp.Diagnostics.HasError() {
		return
	}

	url := plan.URL.ValueString()
	active := plan.Active.ValueBool()
	update := client.UpdateWebhookRequest{
		URL:    &url,
		Events: events,
		Active: &active,
	}
	if !plan.Format.IsNull() && !plan.Format.IsUnknown() {
		format := plan.Format.ValueString()
		update.Format = &format
	}
	wh, err := r.client.UpdateWebhook(ctx, state.ID.ValueString(), update)
	if err != nil {
		resp.Diagnostics.AddError("Error updating webhook", err.Error())
		return
	}

	// The update response has no created_at and never the secret: both stay
	// as planned (from state).
	resp.Diagnostics.Append(mapWebhookToState(wh, &plan)...)
	resp.Diagnostics.Append(resp.State.Set(ctx, plan)...)
}

func (r *webhookResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state webhookResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	if err := r.client.DeleteWebhook(ctx, state.ID.ValueString()); err != nil {
		if client.IsNotFound(err) {
			return
		}
		resp.Diagnostics.AddError("Error deleting webhook", err.Error())
	}
}

func (r *webhookResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	resource.ImportStatePassthroughID(ctx, path.Root("id"), req, resp)
}

// mapWebhookToState copies what the API reported onto the model. Fields a
// response leaves out (created_at on update, the secret on everything but
// create) keep their current value.
func mapWebhookToState(wh *client.Webhook, m *webhookResourceModel) diag.Diagnostics {
	if wh.ID != "" {
		m.ID = types.StringValue(wh.ID)
	}
	m.URL = types.StringValue(wh.URL)
	m.Active = types.BoolValue(wh.Active)
	if wh.Format != "" {
		m.Format = types.StringValue(wh.Format)
	} else if m.Format.IsUnknown() {
		m.Format = types.StringValue("standard")
	}
	if wh.CreatedAt != "" {
		m.CreatedAt = types.StringValue(wh.CreatedAt)
	} else if m.CreatedAt.IsUnknown() {
		m.CreatedAt = types.StringNull()
	}
	if m.SigningSecret.IsUnknown() {
		m.SigningSecret = types.StringNull()
	}
	return reconcileStringList(wh.Events, &m.Events)
}
