package provider

import (
	"context"
	"fmt"

	"github.com/hashicorp/terraform-plugin-framework-validators/datasourcevalidator"
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/path"

	"github.com/smtpfast/terraform-provider-smtpfast/internal/client"
)

var (
	_ datasource.DataSource                     = &segmentDataSource{}
	_ datasource.DataSourceWithConfigure        = &segmentDataSource{}
	_ datasource.DataSourceWithConfigValidators = &segmentDataSource{}
)

// NewSegmentDataSource returns a new smtpfast_segment data source.
func NewSegmentDataSource() datasource.DataSource {
	return &segmentDataSource{}
}

type segmentDataSource struct {
	client *client.Client
}

func (d *segmentDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_segment"
}

func (d *segmentDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Look up an existing segment by its name or ID, for example one created in the dashboard, to pass its ID to your application. " +
			"Needs a provider API key with the `contact:read` scope.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				MarkdownDescription: "Unique identifier of the segment. Set exactly one of `id` and `name`.",
				Optional:            true,
				Computed:            true,
			},
			"name": schema.StringAttribute{
				MarkdownDescription: "The segment's name, matched exactly (case matters). Set exactly one of `id` and `name`.",
				Optional:            true,
				Computed:            true,
			},
			"description": schema.StringAttribute{
				MarkdownDescription: "What the segment is for, or null.",
				Computed:            true,
			},
			"color": schema.StringAttribute{
				MarkdownDescription: "The segment's hex color, or null.",
				Computed:            true,
			},
			"created_at": schema.StringAttribute{
				MarkdownDescription: "Creation timestamp (RFC 3339).",
				Computed:            true,
			},
		},
	}
}

func (d *segmentDataSource) ConfigValidators(context.Context) []datasource.ConfigValidator {
	return []datasource.ConfigValidator{
		datasourcevalidator.ExactlyOneOf(path.MatchRoot("id"), path.MatchRoot("name")),
	}
}

func (d *segmentDataSource) Configure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
	if req.ProviderData == nil {
		return
	}
	c, ok := req.ProviderData.(*client.Client)
	if !ok {
		resp.Diagnostics.AddError("Unexpected provider data", fmt.Sprintf("Expected *client.Client, got %T.", req.ProviderData))
		return
	}
	d.client = c
}

func (d *segmentDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var state segmentResourceModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	var seg *client.Segment
	if !state.ID.IsNull() {
		found, err := d.client.GetSegment(ctx, state.ID.ValueString())
		if err != nil {
			resp.Diagnostics.AddError("Error reading segment", err.Error())
			return
		}
		seg = found
	} else {
		segments, err := d.client.ListSegments(ctx)
		if err != nil {
			resp.Diagnostics.AddError("Error listing segments", err.Error())
			return
		}
		for i := range segments {
			if segments[i].Name == state.Name.ValueString() {
				seg = &segments[i]
				break
			}
		}
		if seg == nil {
			resp.Diagnostics.AddAttributeError(path.Root("name"), "Segment not found", fmt.Sprintf("No segment named %q on this team.", state.Name.ValueString()))
			return
		}
	}

	mapSegmentToState(seg, &state)
	resp.Diagnostics.Append(resp.State.Set(ctx, state)...)
}
