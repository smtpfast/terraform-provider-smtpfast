package provider

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

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
	_ resource.Resource                = &teamInviteResource{}
	_ resource.ResourceWithConfigure   = &teamInviteResource{}
	_ resource.ResourceWithImportState = &teamInviteResource{}
)

// Invite statuses. The API only lists pending invitations; an accepted one is
// recognised by its address now being on the team.
const (
	invitePending  = "pending"
	inviteAccepted = "accepted"
)

// NewTeamInviteResource returns a new smtpfast_team_invite resource.
func NewTeamInviteResource() resource.Resource {
	return &teamInviteResource{}
}

type teamInviteResource struct {
	client *client.Client
}

type teamInviteResourceModel struct {
	ID        types.String `tfsdk:"id"`
	Email     types.String `tfsdk:"email"`
	Role      types.String `tfsdk:"role"`
	Status    types.String `tfsdk:"status"`
	ExpiresAt types.String `tfsdk:"expires_at"`
}

func (r *teamInviteResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_team_invite"
}

func (r *teamInviteResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "An invitation to join the team. **Creating it sends a real email** from SMTPfast to the address, with a link that is valid for 7 days. " +
			"The person signs in with that address to accept.\n\n" +
			"Inviting needs a paid plan and a free seat: pending invitations count against the plan's member limit. " +
			"The API also limits invitations to 20 an hour per inviter and 3 an hour per address.\n\n" +
			"An invitation cannot be changed, only replaced: changing `email` or `role` revokes it and sends a new one, which is another email. " +
			"Destroying a pending invitation revokes it, so its link stops working.\n\n" +
			"Once the person accepts, `status` turns to `accepted` and stays that way, even if they later leave or are removed: Terraform never re-invites anyone on its own " +
			"(to invite them again, replace the resource with `terraform apply -replace`). Destroying an accepted invitation does nothing, and the person stays on the team. " +
			"Manage their role with `smtpfast_team_member`, which also removes them on destroy.\n\n" +
			"An invitation that expires unaccepted, or is revoked in the dashboard, is gone from the API: the next plan creates it again, and applying sends a new email.\n\n" +
			"Needs a provider API key with the `team:read` and `team:manage` scopes, created by a team owner or admin.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				MarkdownDescription: "Unique identifier of the invitation.",
				Computed:            true,
				PlanModifiers:       []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"email": schema.StringAttribute{
				MarkdownDescription: "A plain lowercase address to invite, such as `ada@example.com`. Changing this forces a new invitation.",
				Required:            true,
				Validators:          []validator.String{plainEmailAddress()},
				PlanModifiers:       []planmodifier.String{stringplanmodifier.RequiresReplace()},
			},
			"role": schema.StringAttribute{
				MarkdownDescription: "The role the person joins with: `admin` or `member`. `member` when omitted. Owners are made by promoting a member. Changing this forces a new invitation.",
				Optional:            true,
				Computed:            true,
				Validators:          []validator.String{stringvalidator.OneOf(inviteRoles...)},
				PlanModifiers: []planmodifier.String{
					keepStateOrDefaultString{def: "member"},
					stringplanmodifier.RequiresReplace(),
				},
			},
			"status": schema.StringAttribute{
				MarkdownDescription: "`pending` until the person accepts, then `accepted`.",
				Computed:            true,
				PlanModifiers:       []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"expires_at": schema.StringAttribute{
				MarkdownDescription: "When the invitation link stops working (RFC 3339).",
				Computed:            true,
				PlanModifiers:       []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
		},
	}
}

func (r *teamInviteResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
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

func (r *teamInviteResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan teamInviteResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	inv, err := r.client.CreateTeamInvite(ctx, plan.Email.ValueString(), plan.Role.ValueString())
	if err != nil {
		resp.Diagnostics.AddError("Error inviting to the team", err.Error())
		return
	}

	mapTeamInviteToState(inv, &plan)
	resp.Diagnostics.Append(saveInviteSend(ctx, inv, resp.Private)...)
	resp.Diagnostics.Append(resp.State.Set(ctx, plan)...)
}

func (r *teamInviteResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state teamInviteResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	// Accepted is final. If the person later leaves or is removed, the
	// invitation stays accepted rather than being planned again, so Terraform
	// never re-invites someone on its own.
	if state.Status.ValueString() == inviteAccepted {
		return
	}

	invites, err := r.client.ListTeamInvites(ctx)
	if err != nil {
		resp.Diagnostics.AddError("Error listing team invitations", err.Error())
		return
	}
	// The id, or an address on import.
	id := state.ID.ValueString()
	for i := range invites {
		if invites[i].ID == id || (strings.Contains(id, "@") && invites[i].Email == id) {
			mapTeamInviteToState(&invites[i], &state)
			// The first read after an import records the send; later reads
			// never change it.
			if send, diags := loadInviteSend(ctx, req.Private); !diags.HasError() && send == nil {
				resp.Diagnostics.Append(saveInviteSend(ctx, &invites[i], resp.Private)...)
			}
			resp.Diagnostics.Append(resp.State.Set(ctx, state)...)
			return
		}
	}

	// Not pending any more: accepted if the address is on the team now,
	// otherwise expired or revoked.
	if email := state.Email.ValueString(); email != "" {
		members, err := r.client.ListTeamMembers(ctx)
		if err != nil {
			resp.Diagnostics.AddError("Error listing team members", err.Error())
			return
		}
		for _, m := range members {
			if strings.EqualFold(m.Email, email) {
				state.Status = types.StringValue(inviteAccepted)
				resp.Diagnostics.Append(resp.State.Set(ctx, state)...)
				return
			}
		}
	}
	resp.State.RemoveResource(ctx)
}

// Update is never called with a change: every argument forces a new
// invitation. It only stores the plan.
func (r *teamInviteResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan teamInviteResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(resp.State.Set(ctx, plan)...)
}

func (r *teamInviteResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state teamInviteResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() || state.Status.ValueString() == inviteAccepted {
		// An accepted invitation has nothing left to revoke, and the person
		// stays on the team.
		return
	}

	// Inviting an address that has a pending invitation sends that same
	// invitation again: same id, new link and expiry (and the new role). So
	// the invitation under this id may no longer be the send this instance
	// made, for example when its create_before_destroy replacement has just
	// sent it again. Revoke it only while its expiry and role are still the
	// ones in state.
	invites, err := r.client.ListTeamInvites(ctx)
	if err != nil {
		resp.Diagnostics.AddError("Error listing team invitations", err.Error())
		return
	}
	// Compare with the send this instance made, kept in private state, which
	// a refresh never updates. A deposed instance left behind by an
	// interrupted replacement is refreshed with the new send's values, so the
	// visible attributes alone would match the replacement's invitation.
	wantExpires, wantRole := state.ExpiresAt.ValueString(), state.Role.ValueString()
	send, diags := loadInviteSend(ctx, req.Private)
	resp.Diagnostics.Append(diags...)
	if send != nil {
		wantExpires, wantRole = send.ExpiresAt, send.Role
	}
	for _, inv := range invites {
		if inv.ID == state.ID.ValueString() && (inv.ExpiresAt != wantExpires || inv.Role != wantRole) {
			resp.Diagnostics.AddWarning("Invitation left in place",
				fmt.Sprintf("The invitation to %s was sent again after Terraform last read it (by a replacement of this resource, or from the dashboard), "+
					"so it belongs to that newer send and was not revoked.", inv.Email))
			return
		}
	}

	if err := r.client.RevokeTeamInvite(ctx, state.ID.ValueString()); err != nil {
		if client.IsNotFound(err) {
			return
		}
		resp.Diagnostics.AddError("Error revoking team invitation", err.Error())
	}
}

// ImportState imports a pending invitation by id or by its email address.
func (r *teamInviteResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	resource.ImportStatePassthroughID(ctx, path.Root("id"), req, resp)
}

func mapTeamInviteToState(inv *client.TeamInvite, m *teamInviteResourceModel) {
	m.ID = types.StringValue(inv.ID)
	m.Email = types.StringValue(inv.Email)
	m.Role = types.StringValue(inv.Role)
	m.Status = types.StringValue(invitePending)
	m.ExpiresAt = types.StringValue(inv.ExpiresAt)
}

// inviteSendKey holds, in private state, the expiry and role of the
// invitation this instance sent. See Delete.
const inviteSendKey = "send"

type inviteSend struct {
	ExpiresAt string `json:"expires_at"`
	Role      string `json:"role"`
}

type privateSetter interface {
	SetKey(ctx context.Context, key string, value []byte) diag.Diagnostics
}

type privateGetter interface {
	GetKey(ctx context.Context, key string) ([]byte, diag.Diagnostics)
}

func saveInviteSend(ctx context.Context, inv *client.TeamInvite, p privateSetter) diag.Diagnostics {
	raw, err := json.Marshal(inviteSend{ExpiresAt: inv.ExpiresAt, Role: inv.Role})
	if err != nil {
		var d diag.Diagnostics
		d.AddError("Error saving invitation state", err.Error())
		return d
	}
	return p.SetKey(ctx, inviteSendKey, raw)
}

func loadInviteSend(ctx context.Context, p privateGetter) (*inviteSend, diag.Diagnostics) {
	raw, diags := p.GetKey(ctx, inviteSendKey)
	if diags.HasError() || len(raw) == 0 {
		return nil, diags
	}
	var s inviteSend
	if err := json.Unmarshal(raw, &s); err != nil {
		return nil, diags
	}
	return &s, diags
}
