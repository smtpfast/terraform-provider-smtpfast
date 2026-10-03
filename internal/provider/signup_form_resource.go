package provider

import (
	"context"
	"fmt"

	"github.com/hashicorp/terraform-plugin-framework-validators/listvalidator"
	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
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
	_ resource.Resource                = &signupFormResource{}
	_ resource.ResourceWithConfigure   = &signupFormResource{}
	_ resource.ResourceWithImportState = &signupFormResource{}
	_ resource.ResourceWithModifyPlan  = &signupFormResource{}
)

// NewSignupFormResource returns a new smtpfast_signup_form resource.
func NewSignupFormResource() resource.Resource {
	return &signupFormResource{}
}

type signupFormResource struct {
	client *client.Client
}

type signupFormResourceModel struct {
	ID                        types.String `tfsdk:"id"`
	Name                      types.String `tfsdk:"name"`
	Fields                    types.List   `tfsdk:"fields"`
	ButtonText                types.String `tfsdk:"button_text"`
	ButtonColor               types.String `tfsdk:"button_color"`
	SuccessMessage            types.String `tfsdk:"success_message"`
	DoubleOptIn               types.Bool   `tfsdk:"double_opt_in"`
	RedirectURL               types.String `tfsdk:"redirect_url"`
	CaptchaEnabled            types.Bool   `tfsdk:"captcha_enabled"`
	TurnstileSiteKey          types.String `tfsdk:"turnstile_site_key"`
	TurnstileSecretKey        types.String `tfsdk:"turnstile_secret_key"`
	TurnstileSecretConfigured types.Bool   `tfsdk:"turnstile_secret_configured"`
	BlockDisposableEmails     types.Bool   `tfsdk:"block_disposable_emails"`
	Active                    types.Bool   `tfsdk:"active"`
	ConfirmationEmailFrom     types.String `tfsdk:"confirmation_email_from"`
	WelcomeEmailEnabled       types.Bool   `tfsdk:"welcome_email_enabled"`
	WelcomeEmailFrom          types.String `tfsdk:"welcome_email_from"`
	WelcomeEmailSubject       types.String `tfsdk:"welcome_email_subject"`
	WelcomeEmailMarkdown      types.String `tfsdk:"welcome_email_markdown"`
	CreatedAt                 types.String `tfsdk:"created_at"`
}

// Defaults of a new form, which Create sends explicitly.
const (
	defaultFormButtonText     = "Subscribe"
	defaultFormButtonColor    = "#10b981"
	defaultFormSuccessMessage = "Thanks for subscribing!"
)

func (r *signupFormResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_signup_form"
}

func (r *signupFormResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	senderDesc := "A sender such as `Acme <hello@mail.example.com>`, up to 200 characters, on a domain verified for the team."
	resp.Schema = schema.Schema{
		MarkdownDescription: "A hosted signup form that adds subscribers to your contacts. Embed it with the JavaScript widget " +
			"(`https://smtpfa.st/api/forms/<id>/embed.js` in a `<script>` tag next to `<div data-smtpfast-form=\"<id>\"></div>`) " +
			"or post your own HTML form to `https://smtpfa.st/api/forms/<id>/submit`.\n\n" +
			"A team can have up to 25 forms. Deleting a form stops every embedded copy of it from working.\n\n" +
			"Settings with a default (`fields`, `button_text`, `button_color`, `success_message`, `double_opt_in`, `captcha_enabled`, " +
			"`block_disposable_emails`, `active` and `welcome_email_enabled`) get it on create when omitted. After that, Terraform keeps " +
			"their current value while they are omitted, so a change made in the dashboard stays; set one to manage it. " +
			"The optional text settings (`redirect_url`, the Turnstile keys, the senders and the welcome email) are always managed: omitting one clears it.\n\n" +
			"Needs a provider API key with the `form:read` and `form:write` scopes.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				MarkdownDescription: "Unique identifier of the form, used in the embed and submit URLs.",
				Computed:            true,
				PlanModifiers:       []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"name": schema.StringAttribute{
				MarkdownDescription: "Internal name, up to 100 characters.",
				Required:            true,
				Validators:          trimmedLine(100),
			},
			"fields": schema.ListAttribute{
				MarkdownDescription: "Inputs the form shows: `email` (required in the list), `first_name` and `last_name`. The widget renders them in that order whatever the order here. " +
					"`[\"email\", \"first_name\"]` on create when omitted.",
				Optional:    true,
				Computed:    true,
				ElementType: types.StringType,
				Validators: []validator.List{
					listvalidator.UniqueValues(),
					listvalidator.ValueStringsAre(stringvalidator.OneOf(signupFormFields...)),
					listContains{value: "email"},
				},
				PlanModifiers: []planmodifier.List{keepStateOrDefaultStringList{def: defaultSignupFormFields}},
			},
			"button_text": schema.StringAttribute{
				MarkdownDescription: "Label of the submit button, up to 50 characters. `" + defaultFormButtonText + "` on create when omitted.",
				Optional:            true,
				Computed:            true,
				Validators:          trimmedLine(50),
				PlanModifiers:       []planmodifier.String{keepStateOrDefaultString{def: defaultFormButtonText}},
			},
			"button_color": schema.StringAttribute{
				MarkdownDescription: "Hex color of the submit button, such as `#10b981` or `#0af`. `" + defaultFormButtonColor + "` on create when omitted.",
				Optional:            true,
				Computed:            true,
				Validators: []validator.String{
					stringvalidator.RegexMatches(formButtonColorRegexp, "must be a hex color such as #10b981 or #0af"),
				},
				PlanModifiers: []planmodifier.String{keepStateOrDefaultString{def: defaultFormButtonColor}},
			},
			"success_message": schema.StringAttribute{
				MarkdownDescription: "Shown after a signup when there is no `redirect_url`, up to 200 characters. `" + defaultFormSuccessMessage + "` on create when omitted.",
				Optional:            true,
				Computed:            true,
				Validators:          []validator.String{jsLengthAtMost(200), trimmed()},
				PlanModifiers:       []planmodifier.String{keepStateOrDefaultString{def: defaultFormSuccessMessage}},
			},
			"double_opt_in": schema.BoolAttribute{
				MarkdownDescription: "Email a confirmation link and add the contact only once it is clicked. `true` on create when omitted.",
				Optional:            true,
				Computed:            true,
				PlanModifiers:       []planmodifier.Bool{keepStateOrDefaultBool{def: true}},
			},
			"redirect_url": schema.StringAttribute{
				MarkdownDescription: "An `http://` or `https://` URL to send people to after they sign up, instead of showing `success_message`. Up to 2,048 characters.",
				Optional:            true,
				Validators: append(trimmedLine(2048),
					stringvalidator.RegexMatches(httpURLRegexp, "must start with http:// or https://"),
				),
			},
			"captcha_enabled": schema.BoolAttribute{
				MarkdownDescription: "Protect the form with Cloudflare Turnstile. Needs `turnstile_site_key` and `turnstile_secret_key` while it is on. `false` on create when omitted.",
				Optional:            true,
				Computed:            true,
				PlanModifiers:       []planmodifier.Bool{keepStateOrDefaultBool{def: false}},
			},
			"turnstile_site_key": schema.StringAttribute{
				MarkdownDescription: "Cloudflare Turnstile site key, up to 255 characters.",
				Optional:            true,
				Validators:          trimmedLine(255),
			},
			"turnstile_secret_key": schema.StringAttribute{
				MarkdownDescription: "Cloudflare Turnstile secret key, up to 255 characters. The API never returns it, so Terraform only sends it when it changes, " +
					"and it is empty after an import. Removing it clears the secret.",
				Optional:   true,
				Sensitive:  true,
				Validators: trimmedLine(255),
			},
			"turnstile_secret_configured": schema.BoolAttribute{
				MarkdownDescription: "Whether the form has a Turnstile secret key, including one set outside Terraform.",
				Computed:            true,
			},
			"block_disposable_emails": schema.BoolAttribute{
				MarkdownDescription: "Refuse signups from disposable email services. `true` on create when omitted.",
				Optional:            true,
				Computed:            true,
				PlanModifiers:       []planmodifier.Bool{keepStateOrDefaultBool{def: true}},
			},
			"active": schema.BoolAttribute{
				MarkdownDescription: "Whether the form accepts signups. Set to `false` to pause it without deleting it. `true` on create when omitted.",
				Optional:            true,
				Computed:            true,
				PlanModifiers:       []planmodifier.Bool{keepStateOrDefaultBool{def: true}},
			},
			"confirmation_email_from": schema.StringAttribute{
				MarkdownDescription: "Sender of the double opt-in confirmation email. " + senderDesc + " When it is omitted or cannot be used, the confirmation comes from SMTPfast's own address.",
				Optional:            true,
				Validators:          trimmedLine(200),
			},
			"welcome_email_enabled": schema.BoolAttribute{
				MarkdownDescription: "Send a one-time welcome email to each new subscriber: on confirmation with double opt-in, otherwise right after the signup. `false` on create when omitted.",
				Optional:            true,
				Computed:            true,
				PlanModifiers:       []planmodifier.Bool{keepStateOrDefaultBool{def: false}},
			},
			"welcome_email_from": schema.StringAttribute{
				MarkdownDescription: "Sender of the welcome email. " + senderDesc,
				Optional:            true,
				Validators:          trimmedLine(200),
			},
			"welcome_email_subject": schema.StringAttribute{
				MarkdownDescription: "Subject of the welcome email, up to 200 characters.",
				Optional:            true,
				Validators:          trimmedLine(200),
			},
			"welcome_email_markdown": schema.StringAttribute{
				MarkdownDescription: "Body of the welcome email in Markdown, up to 50,000 characters.",
				Optional:            true,
				Validators:          []validator.String{jsLengthBetween(1, 50000)},
			},
			"created_at": schema.StringAttribute{
				MarkdownDescription: "Creation timestamp (RFC 3339).",
				Computed:            true,
				PlanModifiers:       []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
		},
	}
}

func (r *signupFormResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
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

// ModifyPlan predicts turnstile_secret_configured, and refuses a plan that
// leaves captcha on without both Turnstile keys, which the API would reject.
func (r *signupFormResource) ModifyPlan(ctx context.Context, req resource.ModifyPlanRequest, resp *resource.ModifyPlanResponse) {
	if req.Plan.Raw.IsNull() {
		return
	}
	var plan signupFormResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	var state *signupFormResourceModel
	if !req.State.Raw.IsNull() {
		state = &signupFormResourceModel{}
		resp.Diagnostics.Append(req.State.Get(ctx, state)...)
		if resp.Diagnostics.HasError() {
			return
		}
	}

	secret := plan.TurnstileSecretKey
	switch {
	case secret.IsUnknown():
		plan.TurnstileSecretConfigured = types.BoolUnknown()
	case !secret.IsNull():
		plan.TurnstileSecretConfigured = types.BoolValue(true)
	case state == nil || !state.TurnstileSecretKey.IsNull():
		// A new form without a secret, or the secret is being removed.
		plan.TurnstileSecretConfigured = types.BoolValue(false)
	default:
		// Not sent, so a secret set outside Terraform stays.
		plan.TurnstileSecretConfigured = state.TurnstileSecretConfigured
	}

	if plan.CaptchaEnabled.ValueBool() && (plan.TurnstileSiteKey.IsNull() || secret.IsNull()) {
		resp.Diagnostics.AddAttributeError(path.Root("captcha_enabled"), "Turnstile keys required",
			"Captcha is on for this form (captcha_enabled is true, set in the configuration or kept from its current value), and the API needs both Turnstile keys while it is. "+
				"Set turnstile_site_key and turnstile_secret_key, or set captcha_enabled = false.")
		return
	}

	resp.Diagnostics.Append(resp.Plan.Set(ctx, plan)...)
}

func (r *signupFormResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan signupFormResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	body, diags := signupFormRequest(ctx, &plan)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}
	body.RedirectURL = plan.RedirectURL.ValueStringPointer()
	body.TurnstileSiteKey = plan.TurnstileSiteKey.ValueStringPointer()
	body.TurnstileSecretKey = plan.TurnstileSecretKey.ValueStringPointer()

	created, err := r.client.CreateSignupForm(ctx, body)
	if err != nil {
		resp.Diagnostics.AddError("Error creating signup form", err.Error())
		return
	}

	// Create ignores the confirmation and welcome email settings, which take
	// an update. Record the form first, so a failed update never leaves it
	// unmanaged.
	desired := plan
	resp.Diagnostics.Append(mapSignupFormToState(created, &plan)...)
	resp.Diagnostics.Append(resp.State.Set(ctx, plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	if desired.WelcomeEmailEnabled.ValueBool() || !desired.ConfirmationEmailFrom.IsNull() || !desired.WelcomeEmailFrom.IsNull() ||
		!desired.WelcomeEmailSubject.IsNull() || !desired.WelcomeEmailMarkdown.IsNull() {
		_, err := r.client.UpdateSignupForm(ctx, created.ID, client.SignupFormRequest{
			ConfirmationEmailFrom: clearableString(desired.ConfirmationEmailFrom),
			WelcomeEmailEnabled:   desired.WelcomeEmailEnabled.ValueBoolPointer(),
			WelcomeEmailFrom:      clearableString(desired.WelcomeEmailFrom),
			WelcomeEmailSubject:   clearableString(desired.WelcomeEmailSubject),
			WelcomeEmailMarkdown:  clearableString(desired.WelcomeEmailMarkdown),
		})
		if err != nil {
			resp.Diagnostics.AddError("Error setting the signup form's emails", err.Error())
			return
		}
	}

	form, err := r.client.GetSignupForm(ctx, created.ID)
	if err != nil {
		resp.Diagnostics.AddError("Error reading signup form after create", err.Error())
		return
	}
	resp.Diagnostics.Append(mapSignupFormToState(form, &desired)...)
	resp.Diagnostics.Append(resp.State.Set(ctx, desired)...)
}

func (r *signupFormResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state signupFormResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	form, err := r.client.GetSignupForm(ctx, state.ID.ValueString())
	if err != nil {
		if client.IsNotFound(err) {
			resp.State.RemoveResource(ctx)
			return
		}
		resp.Diagnostics.AddError("Error reading signup form", err.Error())
		return
	}

	resp.Diagnostics.Append(mapSignupFormToState(form, &state)...)
	resp.Diagnostics.Append(resp.State.Set(ctx, state)...)
}

func (r *signupFormResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan, state signupFormResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	body, diags := signupFormRequest(ctx, &plan)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}
	// The optional text settings are always sent, so removing one clears it.
	body.RedirectURL = clearableString(plan.RedirectURL)
	body.TurnstileSiteKey = clearableString(plan.TurnstileSiteKey)
	body.ConfirmationEmailFrom = clearableString(plan.ConfirmationEmailFrom)
	body.WelcomeEmailEnabled = plan.WelcomeEmailEnabled.ValueBoolPointer()
	body.WelcomeEmailFrom = clearableString(plan.WelcomeEmailFrom)
	body.WelcomeEmailSubject = clearableString(plan.WelcomeEmailSubject)
	body.WelcomeEmailMarkdown = clearableString(plan.WelcomeEmailMarkdown)
	// The secret is write-only: send it only when the configuration changed
	// it, so a secret set outside Terraform is not cleared by other edits.
	if !plan.TurnstileSecretKey.Equal(state.TurnstileSecretKey) {
		body.TurnstileSecretKey = clearableString(plan.TurnstileSecretKey)
	}

	form, err := r.client.UpdateSignupForm(ctx, state.ID.ValueString(), body)
	if err != nil {
		resp.Diagnostics.AddError("Error updating signup form", err.Error())
		return
	}

	resp.Diagnostics.Append(mapSignupFormToState(form, &plan)...)
	resp.Diagnostics.Append(resp.State.Set(ctx, plan)...)
}

func (r *signupFormResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state signupFormResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	if err := r.client.DeleteSignupForm(ctx, state.ID.ValueString()); err != nil {
		if client.IsNotFound(err) {
			return
		}
		resp.Diagnostics.AddError("Error deleting signup form", err.Error())
	}
}

func (r *signupFormResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	resource.ImportStatePassthroughID(ctx, path.Root("id"), req, resp)
}

// signupFormRequest builds the settings that create and update share. The
// planned values of the settings with a default are always known.
func signupFormRequest(ctx context.Context, m *signupFormResourceModel) (client.SignupFormRequest, diag.Diagnostics) {
	var fields []string
	diags := m.Fields.ElementsAs(ctx, &fields, false)
	return client.SignupFormRequest{
		Name:                  m.Name.ValueStringPointer(),
		Fields:                fields,
		ButtonText:            m.ButtonText.ValueStringPointer(),
		ButtonColor:           m.ButtonColor.ValueStringPointer(),
		SuccessMessage:        m.SuccessMessage.ValueStringPointer(),
		DoubleOptIn:           m.DoubleOptIn.ValueBoolPointer(),
		CaptchaEnabled:        m.CaptchaEnabled.ValueBoolPointer(),
		BlockDisposableEmails: m.BlockDisposableEmails.ValueBoolPointer(),
		Active:                m.Active.ValueBoolPointer(),
	}, diags
}

// clearableString is the value to send for a nullable text setting: the API
// clears the setting when it gets an empty string.
func clearableString(v types.String) *string {
	if v.IsNull() || v.IsUnknown() {
		empty := ""
		return &empty
	}
	return v.ValueStringPointer()
}

// mapSignupFormToState copies what the API reported onto the model. The
// Turnstile secret is never returned: the model keeps its value unless the
// API says the form has no secret any more.
func mapSignupFormToState(f *client.SignupForm, m *signupFormResourceModel) diag.Diagnostics {
	m.ID = types.StringValue(f.ID)
	m.Name = types.StringValue(f.Name)
	m.ButtonText = types.StringValue(f.ButtonText)
	m.ButtonColor = types.StringValue(f.ButtonColor)
	m.SuccessMessage = types.StringValue(f.SuccessMessage)
	m.DoubleOptIn = types.BoolValue(f.DoubleOptIn)
	m.RedirectURL = types.StringPointerValue(f.RedirectURL)
	m.CaptchaEnabled = types.BoolValue(f.CaptchaEnabled)
	m.TurnstileSiteKey = types.StringPointerValue(f.TurnstileSiteKey)
	m.TurnstileSecretConfigured = types.BoolValue(f.TurnstileSecretConfigured)
	if !f.TurnstileSecretConfigured || m.TurnstileSecretKey.IsUnknown() {
		m.TurnstileSecretKey = types.StringNull()
	}
	m.BlockDisposableEmails = types.BoolValue(f.BlockDisposableEmails)
	m.Active = types.BoolValue(f.Active)
	m.ConfirmationEmailFrom = types.StringPointerValue(f.ConfirmationEmailFrom)
	m.WelcomeEmailEnabled = types.BoolValue(f.WelcomeEmailEnabled)
	m.WelcomeEmailFrom = types.StringPointerValue(f.WelcomeEmailFrom)
	m.WelcomeEmailSubject = types.StringPointerValue(f.WelcomeEmailSubject)
	m.WelcomeEmailMarkdown = types.StringPointerValue(f.WelcomeEmailMarkdown)
	m.CreatedAt = types.StringValue(f.CreatedAt)
	if m.Fields.IsUnknown() {
		m.Fields = types.ListNull(types.StringType)
	}
	return reconcileStringList(f.Fields, &m.Fields)
}
