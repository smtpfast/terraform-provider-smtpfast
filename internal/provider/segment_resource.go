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
	_ resource.Resource                = &segmentResource{}
	_ resource.ResourceWithConfigure   = &segmentResource{}
	_ resource.ResourceWithImportState = &segmentResource{}
)

// NewSegmentResource returns a new smtpfast_segment resource.
func NewSegmentResource() resource.Resource {
	return &segmentResource{}
}

type segmentResource struct {
	client *client.Client
}

type segmentResourceModel struct {
	ID          types.String `tfsdk:"id"`
	Name        types.String `tfsdk:"name"`
	Description types.String `tfsdk:"description"`
	Color       types.String `tfsdk:"color"`
	CreatedAt   types.String `tfsdk:"created_at"`
}

func (r *segmentResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_segment"
}

func (r *segmentResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "A manual contact segment, for organising contacts and targeting broadcasts. " +
			"Terraform manages the segment itself, not who is in it: contacts join and leave segments at runtime through the contacts API.\n\n" +
			"Each plan allows a set number of segments. Deleting a segment keeps its contacts.\n\n" +
			"Needs a provider API key with the `contact:read` and `contact:write` scopes.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				MarkdownDescription: "Unique identifier of the segment. Contact and broadcast calls take it as a segment ID.",
				Computed:            true,
				PlanModifiers:       []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"name": schema.StringAttribute{
				MarkdownDescription: "Name of the segment, unique in the team, up to 100 characters. Updates in place.",
				Required:            true,
				Validators:          trimmedLine(100),
			},
			"description": schema.StringAttribute{
				MarkdownDescription: "What the segment is for, up to 500 characters. Omit it for none.",
				Optional:            true,
				Validators:          []validator.String{jsLengthBetween(1, 500), trimmed()},
			},
			"color": schema.StringAttribute{
				MarkdownDescription: "A six-digit hex color for the dashboard, such as `#10b981`. Omit it for none.",
				Optional:            true,
				Validators: []validator.String{
					stringvalidator.RegexMatches(segmentColorRegexp, "must be a six-digit hex color such as #10b981"),
				},
			},
			"created_at": schema.StringAttribute{
				MarkdownDescription: "Creation timestamp (RFC 3339).",
				Computed:            true,
				PlanModifiers:       []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
		},
	}
}

func (r *segmentResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
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

func segmentRequest(m *segmentResourceModel) client.SegmentRequest {
	return client.SegmentRequest{
		Name:        m.Name.ValueString(),
		Description: m.Description.ValueStringPointer(),
		Color:       m.Color.ValueStringPointer(),
	}
}

func (r *segmentResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan segmentResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	seg, err := r.client.CreateSegment(ctx, segmentRequest(&plan))
	if err != nil {
		resp.Diagnostics.AddError("Error creating segment", err.Error())
		return
	}

	mapSegmentToState(seg, &plan)
	resp.Diagnostics.Append(resp.State.Set(ctx, plan)...)
}

func (r *segmentResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state segmentResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	seg, err := r.client.GetSegment(ctx, state.ID.ValueString())
	if err != nil {
		if client.IsNotFound(err) {
			resp.State.RemoveResource(ctx)
			return
		}
		resp.Diagnostics.AddError("Error reading segment", err.Error())
		return
	}

	mapSegmentToState(seg, &state)
	resp.Diagnostics.Append(resp.State.Set(ctx, state)...)
}

func (r *segmentResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan, state segmentResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	// Description and color are always sent, so removing them clears them.
	seg, err := r.client.UpdateSegment(ctx, state.ID.ValueString(), segmentRequest(&plan))
	if err != nil {
		resp.Diagnostics.AddError("Error updating segment", err.Error())
		return
	}

	mapSegmentToState(seg, &plan)
	resp.Diagnostics.Append(resp.State.Set(ctx, plan)...)
}

func (r *segmentResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state segmentResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	if err := r.client.DeleteSegment(ctx, state.ID.ValueString()); err != nil {
		if client.IsNotFound(err) {
			return
		}
		resp.Diagnostics.AddError("Error deleting segment", err.Error())
	}
}

func (r *segmentResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	resource.ImportStatePassthroughID(ctx, path.Root("id"), req, resp)
}

func mapSegmentToState(seg *client.Segment, m *segmentResourceModel) {
	m.ID = types.StringValue(seg.ID)
	m.Name = types.StringValue(seg.Name)
	m.Description = types.StringPointerValue(seg.Description)
	m.Color = types.StringPointerValue(seg.Color)
	m.CreatedAt = types.StringValue(seg.CreatedAt)
}
