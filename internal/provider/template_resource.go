package provider

import (
	"context"
	"fmt"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework-validators/listvalidator"
	"github.com/hashicorp/terraform-plugin-framework-validators/resourcevalidator"
	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/attr"
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
	_ resource.Resource                     = &templateResource{}
	_ resource.ResourceWithConfigure        = &templateResource{}
	_ resource.ResourceWithImportState      = &templateResource{}
	_ resource.ResourceWithConfigValidators = &templateResource{}
	_ resource.ResourceWithValidateConfig   = &templateResource{}
)

// NewTemplateResource returns a new smtpfast_template resource.
func NewTemplateResource() resource.Resource {
	return &templateResource{}
}

type templateResource struct {
	client *client.Client
}

type templateResourceModel struct {
	ID                     types.String `tfsdk:"id"`
	Name                   types.String `tfsdk:"name"`
	Alias                  types.String `tfsdk:"alias"`
	Subject                types.String `tfsdk:"subject"`
	From                   types.String `tfsdk:"from"`
	ReplyTo                types.List   `tfsdk:"reply_to"`
	PreviewText            types.String `tfsdk:"preview_text"`
	HTML                   types.String `tfsdk:"html"`
	Markdown               types.String `tfsdk:"markdown"`
	Text                   types.String `tfsdk:"text"`
	Variables              types.List   `tfsdk:"variables"`
	Published              types.Bool   `tfsdk:"published"`
	Status                 types.String `tfsdk:"status"`
	PublishedAt            types.String `tfsdk:"published_at"`
	CurrentVersionID       types.String `tfsdk:"current_version_id"`
	HasUnpublishedVersions types.Bool   `tfsdk:"has_unpublished_versions"`
	CreatedAt              types.String `tfsdk:"created_at"`
	UpdatedAt              types.String `tfsdk:"updated_at"`
}

type templateVariableModel struct {
	Key           types.String `tfsdk:"key"`
	Type          types.String `tfsdk:"type"`
	FallbackValue types.String `tfsdk:"fallback_value"`
}

var templateVariableAttrTypes = map[string]attr.Type{
	"key":            types.StringType,
	"type":           types.StringType,
	"fallback_value": types.StringType,
}

func (r *templateResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_template"
}

func (r *templateResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	bodyValidators := []validator.String{
		jsLengthAtMost(100000),
		notBlank(),
	}
	resp.Schema = schema.Schema{
		MarkdownDescription: "A hosted email template (Resend-compatible). Sends name it by `id` or `alias` and fill its `variables`.\n\n" +
			"A template has a draft and a published version, and sends always use the published one. With `published = true` (the default) " +
			"the provider publishes after every create and update, so what Terraform manages is what your emails use.\n\n" +
			"Updates and publishes carry the `updated_at` Terraform last read, so an edit made in the dashboard after your plan is not overwritten: " +
			"the apply stops with a conflict instead, and the next plan shows the difference.\n\n" +
			"Needs a provider API key with the `email:send` and `email:read` scopes.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				MarkdownDescription: "Unique identifier of the template.",
				Computed:            true,
				PlanModifiers:       []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"name": schema.StringAttribute{
				MarkdownDescription: "Name of the template, up to 200 characters.",
				Required:            true,
				Validators:          trimmedLine(200),
			},
			"alias": schema.StringAttribute{
				MarkdownDescription: "A second handle, unique in the team, that works wherever the id does, such as `order-confirmation`. Letters, digits, dot, underscore and hyphen, starting with a letter or digit, up to 100 characters.",
				Optional:            true,
				Validators: []validator.String{
					stringvalidator.RegexMatches(templateAliasRegexp, "must start with a letter or digit and use only letters, digits, dot, underscore or hyphen (up to 100 characters)"),
				},
			},
			"subject": schema.StringAttribute{
				MarkdownDescription: "Default subject. Can use variables, such as `Your order {{{ORDER_ID}}}`. A send can override it.",
				Optional:            true,
				Validators:          trimmedLine(900),
			},
			"from": schema.StringAttribute{
				MarkdownDescription: "Default sender, such as `Acme <orders@acme.com>`. A send can override it.",
				Optional:            true,
				Validators:          trimmedLine(320),
			},
			"reply_to": schema.ListAttribute{
				MarkdownDescription: "Default Reply-To addresses, up to 10. A send can override them.",
				Optional:            true,
				ElementType:         types.StringType,
				Validators: []validator.List{
					listvalidator.SizeBetween(1, 10),
					listvalidator.ValueStringsAre(trimmedLine(320)...),
				},
			},
			"preview_text": schema.StringAttribute{
				MarkdownDescription: "The inbox preview line, added to the email as a hidden first line. Up to 255 characters.",
				Optional:            true,
				Validators:          trimmedLine(255),
			},
			"html": schema.StringAttribute{
				MarkdownDescription: "The HTML body, up to 100,000 characters. Write variables as `{{{KEY}}}`, optionally with an inline fallback: `{{{KEY|default}}}`. Set exactly one of `html` and `markdown`.",
				Optional:            true,
				Validators:          bodyValidators,
			},
			"markdown": schema.StringAttribute{
				MarkdownDescription: "A Markdown body, rendered into SMTPfast's email layout at send time. Set exactly one of `html` and `markdown`.",
				Optional:            true,
				Validators:          bodyValidators,
			},
			"text": schema.StringAttribute{
				MarkdownDescription: "The plain-text body. When omitted, the text part is generated from the HTML at send time. Set it to `\"\"` to send an empty plain-text part instead.",
				Optional:            true,
				Validators:          []validator.String{jsLengthAtMost(100000)},
			},
			"variables": schema.ListNestedAttribute{
				MarkdownDescription: "Variables the content uses, up to 50. A send must pass a value for every variable that has no `fallback_value`.",
				Optional:            true,
				Validators:          []validator.List{listvalidator.SizeAtMost(50)},
				NestedObject: schema.NestedAttributeObject{
					Attributes: map[string]schema.Attribute{
						"key": schema.StringAttribute{
							MarkdownDescription: "Used in the content as `{{{KEY}}}`. 1 to 50 letters, digits or underscores. `FIRST_NAME`, `LAST_NAME`, `EMAIL`, `UNSUBSCRIBE_URL`, `RESEND_UNSUBSCRIBE_URL`, `contact` and `this` are reserved. Keys must be unique, ignoring case.",
							Required:            true,
							Validators: []validator.String{
								stringvalidator.RegexMatches(templateVariableRegexp, "must be 1 to 50 letters, digits or underscores"),
								notReserved(reservedTemplateVariableKeys),
							},
						},
						"type": schema.StringAttribute{
							MarkdownDescription: "`string` or `number`.",
							Required:            true,
							Validators:          []validator.String{stringvalidator.OneOf(variableTypes...)},
						},
						"fallback_value": schema.StringAttribute{
							MarkdownDescription: "Used when a send gives no value. Always written as a string; for a `number` variable it must be a number, such as `\"25\"`. Up to 2,000 characters.",
							Optional:            true,
							Validators:          []validator.String{jsLengthAtMost(2000)},
						},
					},
				},
			},
			"published": schema.BoolAttribute{
				MarkdownDescription: "Publish the draft after every create and update, so sends use what Terraform manages. Defaults to `true`. " +
					"With `false`, Terraform only manages the draft and publishing is left to the dashboard or the API; Terraform never unpublishes. " +
					"When `true` and the draft has changes that are not published (for example, edits saved in the dashboard), the next plan publishes again.",
				Optional: true,
				Computed: true,
				Default:  booldefault.StaticBool(true),
			},
			"status": schema.StringAttribute{
				MarkdownDescription: "`draft` until the template is first published, then `published`.",
				Computed:            true,
			},
			"published_at": schema.StringAttribute{
				MarkdownDescription: "When the template was last published (RFC 3339), or null.",
				Computed:            true,
			},
			"current_version_id": schema.StringAttribute{
				MarkdownDescription: "Id of the published version that sends use, or null before the first publish.",
				Computed:            true,
			},
			"has_unpublished_versions": schema.BoolAttribute{
				MarkdownDescription: "Whether the draft differs from the published version.",
				Computed:            true,
			},
			"created_at": schema.StringAttribute{
				MarkdownDescription: "Creation timestamp (RFC 3339).",
				Computed:            true,
				PlanModifiers:       []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"updated_at": schema.StringAttribute{
				MarkdownDescription: "The revision Terraform last read or wrote (RFC 3339). Sent as `expected_updated_at` on updates and publishes.",
				Computed:            true,
			},
		},
	}
}

func (r *templateResource) ConfigValidators(context.Context) []resource.ConfigValidator {
	return []resource.ConfigValidator{
		resourcevalidator.ExactlyOneOf(path.MatchRoot("html"), path.MatchRoot("markdown")),
	}
}

// ValidateConfig checks what a single attribute validator cannot: variable
// keys unique ignoring case, and numeric fallbacks on number variables.
func (r *templateResource) ValidateConfig(ctx context.Context, req resource.ValidateConfigRequest, resp *resource.ValidateConfigResponse) {
	var variables types.List
	resp.Diagnostics.Append(req.Config.GetAttribute(ctx, path.Root("variables"), &variables)...)
	if resp.Diagnostics.HasError() || variables.IsNull() || variables.IsUnknown() {
		return
	}
	var vars []templateVariableModel
	resp.Diagnostics.Append(variables.ElementsAs(ctx, &vars, true)...)
	if resp.Diagnostics.HasError() {
		return
	}
	seen := map[string]bool{}
	for i, v := range vars {
		if v.Key.IsUnknown() || v.Key.IsNull() {
			continue
		}
		p := path.Root("variables").AtListIndex(i)
		lower := strings.ToLower(v.Key.ValueString())
		if seen[lower] {
			resp.Diagnostics.AddAttributeError(p.AtName("key"), "Duplicate variable key", fmt.Sprintf("%q is declared more than once (keys are compared ignoring case).", v.Key.ValueString()))
		}
		seen[lower] = true
		if v.Type.IsUnknown() || v.FallbackValue.IsUnknown() {
			continue
		}
		if _, err := fallbackToAPI(v.Type.ValueString(), v.FallbackValue); err != nil {
			resp.Diagnostics.AddAttributeError(p.AtName("fallback_value"), "Invalid fallback value", err.Error())
		}
	}
}

func (r *templateResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
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

func (r *templateResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan templateResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	fields, diags := templateFieldsFromModel(ctx, &plan)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	created, err := r.client.CreateTemplate(ctx, fields)
	if err != nil {
		resp.Diagnostics.AddError("Error creating template", err.Error())
		return
	}

	// The template exists now: record it as an unpublished draft before
	// anything else can fail, so it is never left unmanaged.
	plan.ID = types.StringValue(created.ID)
	plan.Published = types.BoolValue(false)
	plan.Status = types.StringValue("draft")
	plan.PublishedAt = types.StringNull()
	plan.CurrentVersionID = types.StringNull()
	plan.HasUnpublishedVersions = types.BoolValue(true)
	plan.CreatedAt = types.StringValue(created.UpdatedAt)
	plan.UpdatedAt = types.StringValue(created.UpdatedAt)
	resp.Diagnostics.Append(resp.State.Set(ctx, plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	var wantPublished types.Bool
	resp.Diagnostics.Append(req.Plan.GetAttribute(ctx, path.Root("published"), &wantPublished)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if wantPublished.ValueBool() {
		if _, err := r.client.PublishTemplate(ctx, created.ID, created.UpdatedAt); err != nil {
			resp.Diagnostics.AddError("Error publishing template", templateWriteError(err))
			return
		}
	}

	tpl, err := r.client.GetTemplate(ctx, created.ID)
	if err != nil {
		resp.Diagnostics.AddError("Error reading template after create", err.Error())
		return
	}
	setTemplateComputed(tpl, &plan)
	plan.Published = wantPublished
	resp.Diagnostics.Append(resp.State.Set(ctx, plan)...)
}

func (r *templateResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state templateResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	tpl, err := r.client.GetTemplate(ctx, state.ID.ValueString())
	if err != nil {
		if client.IsNotFound(err) {
			resp.State.RemoveResource(ctx)
			return
		}
		resp.Diagnostics.AddError("Error reading template", err.Error())
		return
	}

	resp.Diagnostics.Append(setTemplateContent(ctx, tpl, &state)...)
	setTemplateComputed(tpl, &state)

	// published reports whether the draft is what sends use. It only ever
	// turns false here (unpublished edits, often from the dashboard), which a
	// configuration with published = true then corrects by publishing again.
	draftIsLive := tpl.Status == "published" && !tpl.HasUnpublishedVersions
	switch {
	case state.Published.IsNull() || state.Published.IsUnknown():
		state.Published = types.BoolValue(draftIsLive)
	case state.Published.ValueBool() && !draftIsLive:
		state.Published = types.BoolValue(false)
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, state)...)
}

func (r *templateResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan, state templateResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	id := state.ID.ValueString()
	revision := state.UpdatedAt.ValueString()
	hasUnpublished := state.HasUnpublishedVersions.ValueBool()

	if templateContentChanged(&plan, &state) {
		fields, diags := templateFieldsFromModel(ctx, &plan)
		resp.Diagnostics.Append(diags...)
		if resp.Diagnostics.HasError() {
			return
		}
		res, err := r.client.UpdateTemplate(ctx, id, fields, revision)
		if err != nil {
			resp.Diagnostics.AddError("Error updating template", templateWriteError(err))
			return
		}
		revision = res.UpdatedAt
		hasUnpublished = res.HasUnpublishedVersions
	}

	if plan.Published.ValueBool() && hasUnpublished {
		if _, err := r.client.PublishTemplate(ctx, id, revision); err != nil {
			resp.Diagnostics.AddError("Error publishing template", templateWriteError(err))
			return
		}
	}

	tpl, err := r.client.GetTemplate(ctx, id)
	if err != nil {
		resp.Diagnostics.AddError("Error reading template after update", err.Error())
		return
	}
	// Content stays as planned; the next refresh compares it with the API.
	setTemplateComputed(tpl, &plan)
	resp.Diagnostics.Append(resp.State.Set(ctx, plan)...)
}

func (r *templateResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state templateResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	if err := r.client.DeleteTemplate(ctx, state.ID.ValueString()); err != nil {
		if client.IsNotFound(err) {
			return
		}
		resp.Diagnostics.AddError("Error deleting template", err.Error())
	}
}

// ImportState imports by id. An alias works too: the read resolves it to the id.
func (r *templateResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	resource.ImportStatePassthroughID(ctx, path.Root("id"), req, resp)
}

// templateWriteError explains a revision conflict, which means someone
// changed the template after Terraform last read it.
func templateWriteError(err error) string {
	if client.ErrorCode(err) == client.RevisionConflictCode {
		return "The template was changed in SMTPfast (in the dashboard or through the API) after Terraform last read it, " +
			"so Terraform did not overwrite or publish it. Run terraform plan again to see the current version and what Terraform would change, then apply.\n\n" +
			err.Error()
	}
	return err.Error()
}

func templateContentChanged(plan, state *templateResourceModel) bool {
	return !plan.Name.Equal(state.Name) ||
		!plan.Alias.Equal(state.Alias) ||
		!plan.Subject.Equal(state.Subject) ||
		!plan.From.Equal(state.From) ||
		!plan.ReplyTo.Equal(state.ReplyTo) ||
		!plan.PreviewText.Equal(state.PreviewText) ||
		!plan.HTML.Equal(state.HTML) ||
		!plan.Markdown.Equal(state.Markdown) ||
		!plan.Text.Equal(state.Text) ||
		!plan.Variables.Equal(state.Variables)
}

// templateFieldsFromModel builds the API content from the configuration.
func templateFieldsFromModel(ctx context.Context, m *templateResourceModel) (client.TemplateFields, diag.Diagnostics) {
	var diags diag.Diagnostics
	f := client.TemplateFields{
		Name:        m.Name.ValueString(),
		Alias:       m.Alias.ValueStringPointer(),
		From:        m.From.ValueStringPointer(),
		Subject:     m.Subject.ValueStringPointer(),
		PreviewText: m.PreviewText.ValueStringPointer(),
		HTML:        m.HTML.ValueStringPointer(),
		Text:        m.Text.ValueStringPointer(),
		Markdown:    m.Markdown.ValueStringPointer(),
	}
	if !m.ReplyTo.IsNull() && !m.ReplyTo.IsUnknown() {
		diags.Append(m.ReplyTo.ElementsAs(ctx, &f.ReplyTo, false)...)
	}
	if !m.Variables.IsNull() && !m.Variables.IsUnknown() {
		var vars []templateVariableModel
		diags.Append(m.Variables.ElementsAs(ctx, &vars, false)...)
		f.Variables = make([]client.TemplateVariable, 0, len(vars))
		for i, v := range vars {
			fallback, err := fallbackToAPI(v.Type.ValueString(), v.FallbackValue)
			if err != nil {
				diags.AddAttributeError(path.Root("variables").AtListIndex(i).AtName("fallback_value"), "Invalid fallback value", err.Error())
				continue
			}
			f.Variables = append(f.Variables, client.TemplateVariable{
				Key:           v.Key.ValueString(),
				Type:          v.Type.ValueString(),
				FallbackValue: fallback,
			})
		}
	}
	return f, diags
}

// setTemplateContent copies the draft content the API reports onto the model.
func setTemplateContent(ctx context.Context, tpl *client.Template, m *templateResourceModel) diag.Diagnostics {
	var diags diag.Diagnostics
	m.ID = types.StringValue(tpl.ID)
	m.Name = types.StringValue(tpl.Name)
	m.Alias = types.StringPointerValue(tpl.Alias)
	m.Subject = types.StringPointerValue(tpl.Subject)
	m.From = types.StringPointerValue(tpl.From)
	m.PreviewText = types.StringPointerValue(tpl.PreviewText)
	m.HTML = types.StringPointerValue(tpl.HTML)
	m.Markdown = types.StringPointerValue(tpl.Markdown)
	m.Text = types.StringPointerValue(tpl.Text)

	if len(tpl.ReplyTo) == 0 {
		m.ReplyTo = types.ListNull(types.StringType)
	} else {
		list, d := types.ListValueFrom(ctx, types.StringType, tpl.ReplyTo)
		diags.Append(d...)
		m.ReplyTo = list
	}

	// Earlier fallback spellings, by key, so "25.0" is not drift against 25.
	prior := map[string]types.String{}
	if !m.Variables.IsNull() && !m.Variables.IsUnknown() {
		var vars []templateVariableModel
		if d := m.Variables.ElementsAs(ctx, &vars, false); !d.HasError() {
			for _, v := range vars {
				prior[v.Key.ValueString()] = v.FallbackValue
			}
		}
	}
	elemType := types.ObjectType{AttrTypes: templateVariableAttrTypes}
	if len(tpl.Variables) == 0 && m.Variables.IsNull() {
		m.Variables = types.ListNull(elemType)
	} else {
		vars := make([]templateVariableModel, 0, len(tpl.Variables))
		for _, v := range tpl.Variables {
			priorFallback, ok := prior[v.Key]
			if !ok {
				priorFallback = types.StringNull()
			}
			vars = append(vars, templateVariableModel{
				Key:           types.StringValue(v.Key),
				Type:          types.StringValue(v.Type),
				FallbackValue: fallbackFromAPI(v.FallbackValue, priorFallback),
			})
		}
		list, d := types.ListValueFrom(ctx, elemType, vars)
		diags.Append(d...)
		m.Variables = list
	}
	return diags
}

// setTemplateComputed copies the server-side fields onto the model.
func setTemplateComputed(tpl *client.Template, m *templateResourceModel) {
	m.ID = types.StringValue(tpl.ID)
	m.Status = types.StringValue(tpl.Status)
	m.PublishedAt = types.StringPointerValue(tpl.PublishedAt)
	m.CurrentVersionID = types.StringPointerValue(tpl.CurrentVersionID)
	m.HasUnpublishedVersions = types.BoolValue(tpl.HasUnpublishedVersions)
	m.CreatedAt = types.StringValue(tpl.CreatedAt)
	m.UpdatedAt = types.StringValue(tpl.UpdatedAt)
}
