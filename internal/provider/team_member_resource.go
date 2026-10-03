package provider

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework-validators/resourcevalidator"
	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/boolplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/smtpfast/terraform-provider-smtpfast/internal/client"
)

var (
	_ resource.Resource                     = &teamMemberResource{}
	_ resource.ResourceWithConfigure        = &teamMemberResource{}
	_ resource.ResourceWithImportState      = &teamMemberResource{}
	_ resource.ResourceWithConfigValidators = &teamMemberResource{}
	_ resource.ResourceWithValidateConfig   = &teamMemberResource{}
	_ resource.ResourceWithModifyPlan       = &teamMemberResource{}
)

// NewTeamMemberResource returns a new smtpfast_team_member resource.
func NewTeamMemberResource() resource.Resource {
	return &teamMemberResource{}
}

type teamMemberResource struct {
	client *client.Client
}

type teamMemberResourceModel struct {
	ID               types.String `tfsdk:"id"`
	Email            types.String `tfsdk:"email"`
	UserID           types.String `tfsdk:"user_id"`
	Role             types.String `tfsdk:"role"`
	CanManageBilling types.Bool   `tfsdk:"can_manage_billing"`
	Name             types.String `tfsdk:"name"`
	CreatedAt        types.String `tfsdk:"created_at"`
}

func (r *teamMemberResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_team_member"
}

func (r *teamMemberResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Someone who is already on the team, with their role and billing access.\n\n" +
			"The API cannot add people to a team: they join by accepting an invitation (`smtpfast_team_invite`). " +
			"So creating this resource **adopts an existing member**, found by `email` or `user_id`, and then sets `role` and `can_manage_billing` if you set them. " +
			"It fails when nobody with that address or user ID is on the team.\n\n" +
			"**Destroying this resource removes the person from the team** and revokes their API keys for it, and SMTPfast emails them that they were removed. " +
			"That includes `terraform destroy`, deleting the block, and a change of `email` or `user_id`, which points the resource at someone else: " +
			"the old member is removed and the new one adopted. Use `lifecycle { prevent_destroy = true }` for people who must not be removed by accident, " +
			"or a `removed` block with `destroy = false` to stop managing someone without removing them.\n\n" +
			"The API key's own user cannot be removed this way, and a team always keeps at least one owner. " +
			"Role changes follow the Members page: an admin cannot change an owner or make anyone an owner. Each role change emails the member.\n\n" +
			"Needs a provider API key with the `team:read` and `team:manage` scopes, created by a team owner or admin. Changing `can_manage_billing` needs a key created by an owner.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				MarkdownDescription: "The membership ID.",
				Computed:            true,
				PlanModifiers:       []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"email": schema.StringAttribute{
				MarkdownDescription: "The member's address, in lowercase. Set exactly one of `email` and `user_id`; the other is filled in. Changing it adopts a different person and removes this one.",
				Optional:            true,
				Computed:            true,
				Validators:          []validator.String{plainEmailAddress()},
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
					stringplanmodifier.RequiresReplace(),
				},
			},
			"user_id": schema.StringAttribute{
				MarkdownDescription: "The member's user ID. Set exactly one of `email` and `user_id`; the other is filled in. Changing it adopts a different person and removes this one.",
				Optional:            true,
				Computed:            true,
				Validators:          []validator.String{stringvalidator.LengthAtLeast(1)},
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
					stringplanmodifier.RequiresReplace(),
				},
			},
			"role": schema.StringAttribute{
				MarkdownDescription: "`owner`, `admin` or `member`. Updates in place. When omitted, Terraform keeps the member's current role.",
				Optional:            true,
				Computed:            true,
				Validators:          []validator.String{stringvalidator.OneOf(teamRoles...)},
				PlanModifiers:       []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"can_manage_billing": schema.BoolAttribute{
				MarkdownDescription: "Whether the member can manage the subscription and payment. Always `true` for owners. " +
					"Updates in place; when omitted, Terraform keeps the current value. Changing it needs a provider API key created by an owner.",
				Optional:      true,
				Computed:      true,
				PlanModifiers: []planmodifier.Bool{boolplanmodifier.UseStateForUnknown()},
			},
			"name": schema.StringAttribute{
				MarkdownDescription: "The member's name, or null when they have not set one.",
				Computed:            true,
			},
			"created_at": schema.StringAttribute{
				MarkdownDescription: "When they joined the team (RFC 3339).",
				Computed:            true,
				PlanModifiers:       []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
		},
	}
}

func (r *teamMemberResource) ConfigValidators(context.Context) []resource.ConfigValidator {
	return []resource.ConfigValidator{
		resourcevalidator.ExactlyOneOf(path.MatchRoot("email"), path.MatchRoot("user_id")),
	}
}

func (r *teamMemberResource) ValidateConfig(ctx context.Context, req resource.ValidateConfigRequest, resp *resource.ValidateConfigResponse) {
	var m teamMemberResourceModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &m)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if m.Role.ValueString() == "owner" && !m.CanManageBilling.IsNull() && !m.CanManageBilling.IsUnknown() && !m.CanManageBilling.ValueBool() {
		resp.Diagnostics.AddAttributeError(path.Root("can_manage_billing"), "Owners always manage billing",
			"An owner can always manage billing, so can_manage_billing cannot be false with role = \"owner\". Remove it or set it to true.")
	}
}

// ModifyPlan keeps can_manage_billing consistent with the role: owners
// always have billing access, and after a role change the API reports the
// member's own flag again.
func (r *teamMemberResource) ModifyPlan(ctx context.Context, req resource.ModifyPlanRequest, resp *resource.ModifyPlanResponse) {
	if req.Plan.Raw.IsNull() {
		return
	}
	var config, plan teamMemberResourceModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &config)...)
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() || plan.Role.IsUnknown() {
		return
	}
	var stateRole types.String
	if !req.State.Raw.IsNull() {
		resp.Diagnostics.Append(req.State.GetAttribute(ctx, path.Root("role"), &stateRole)...)
	}
	switch {
	case plan.Role.ValueString() == "owner" && config.CanManageBilling.IsNull():
		plan.CanManageBilling = types.BoolValue(true)
	case plan.Role.ValueString() == "owner":
		if !config.CanManageBilling.IsUnknown() && !config.CanManageBilling.ValueBool() {
			resp.Diagnostics.AddAttributeError(path.Root("can_manage_billing"), "Owners always manage billing",
				"This member is an owner, and owners always manage billing, so can_manage_billing cannot be false. Remove it or set it to true.")
		}
		return
	case config.CanManageBilling.IsNull() && !stateRole.IsNull() && !plan.Role.Equal(stateRole):
		plan.CanManageBilling = types.BoolUnknown()
	default:
		return
	}
	resp.Diagnostics.Append(resp.Plan.Set(ctx, plan)...)
}

func (r *teamMemberResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
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

// Create adopts the member: the API has no way to add one.
func (r *teamMemberResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan teamMemberResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	members, err := r.client.ListTeamMembers(ctx)
	if err != nil {
		resp.Diagnostics.AddError("Error listing team members", err.Error())
		return
	}
	var member *client.TeamMember
	for i, m := range members {
		if (!plan.Email.IsUnknown() && !plan.Email.IsNull() && strings.EqualFold(m.Email, plan.Email.ValueString())) ||
			(!plan.UserID.IsUnknown() && !plan.UserID.IsNull() && m.UserID == plan.UserID.ValueString()) {
			member = &members[i]
			break
		}
	}
	if member == nil {
		who := "user ID " + plan.UserID.ValueString()
		hint := "Invite them with smtpfast_team_invite, and add this resource once they have accepted."
		if !plan.Email.IsUnknown() && !plan.Email.IsNull() {
			who = plan.Email.ValueString()
			hint = fmt.Sprintf("Invite them with a smtpfast_team_invite for %q, and add this resource once they have accepted.", who)
		}
		resp.Diagnostics.AddError("Not a team member",
			fmt.Sprintf("Nobody with %s is on this team, and the API cannot add people to a team: they join by accepting an invitation. %s", who, hint))
		return
	}

	// Adopted: record the member before changing anything, so a refused
	// change never leaves them unmanaged.
	desired := plan
	mapTeamMemberToState(member, &plan)
	resp.Diagnostics.Append(resp.State.Set(ctx, plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	update, problem := teamMemberChanges(&desired, member)
	if problem != "" {
		resp.Diagnostics.AddAttributeError(path.Root("can_manage_billing"), "Owners always manage billing", problem)
		return
	}
	if update != nil {
		if err := r.client.UpdateTeamMember(ctx, member.ID, *update); err != nil {
			resp.Diagnostics.AddError("Error updating team member", err.Error())
			return
		}
		if member, err = r.findMember(ctx, member.ID); err != nil {
			resp.Diagnostics.AddError("Error reading team member after update", err.Error())
			return
		}
	}

	mapTeamMemberToState(member, &desired)
	resp.Diagnostics.Append(resp.State.Set(ctx, desired)...)
}

func (r *teamMemberResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state teamMemberResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	member, err := r.findMember(ctx, state.ID.ValueString())
	if err != nil {
		if client.IsNotFound(err) {
			// Removed from the team, or they left.
			resp.State.RemoveResource(ctx)
			return
		}
		resp.Diagnostics.AddError("Error reading team member", err.Error())
		return
	}

	mapTeamMemberToState(member, &state)
	resp.Diagnostics.Append(resp.State.Set(ctx, state)...)
}

func (r *teamMemberResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan, state teamMemberResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	id := state.ID.ValueString()
	update, problem := teamMemberChanges(&plan, &client.TeamMember{
		ID:               id,
		Email:            state.Email.ValueString(),
		Role:             state.Role.ValueString(),
		CanManageBilling: state.CanManageBilling.ValueBool(),
	})
	if problem != "" {
		resp.Diagnostics.AddAttributeError(path.Root("can_manage_billing"), "Owners always manage billing", problem)
		return
	}
	if update != nil {
		if err := r.client.UpdateTeamMember(ctx, id, *update); err != nil {
			resp.Diagnostics.AddError("Error updating team member", err.Error())
			return
		}
	}
	member, err := r.findMember(ctx, id)
	if err != nil {
		resp.Diagnostics.AddError("Error reading team member after update", err.Error())
		return
	}

	mapTeamMemberToState(member, &plan)
	resp.Diagnostics.Append(resp.State.Set(ctx, plan)...)
}

// Delete removes the person from the team.
func (r *teamMemberResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state teamMemberResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	err := r.client.RemoveTeamMember(ctx, state.ID.ValueString())
	switch {
	case err == nil || client.IsNotFound(err):
	case isAPIStatus(err, http.StatusBadRequest) && strings.Contains(err.Error(), "yourself"):
		resp.Diagnostics.AddError("Cannot remove the API key's own user",
			"This member is the user the provider's API key belongs to, and the API does not let a key remove its own user. "+
				"To stop managing them without removing them, use a removed block with destroy = false, or terraform state rm.\n\n"+err.Error())
	default:
		resp.Diagnostics.AddError("Error removing team member", err.Error())
	}
}

// ImportState imports by membership ID. The member's email or user ID works
// too: the read resolves it to the membership.
func (r *teamMemberResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	resource.ImportStatePassthroughID(ctx, path.Root("id"), req, resp)
}

// findMember finds a member by membership ID, user ID or email, answering a
// 404 APIError when nobody matches.
func (r *teamMemberResource) findMember(ctx context.Context, ref string) (*client.TeamMember, error) {
	members, err := r.client.ListTeamMembers(ctx)
	if err != nil {
		return nil, err
	}
	for i, m := range members {
		if m.ID == ref || m.UserID == ref || (strings.Contains(ref, "@") && strings.EqualFold(m.Email, ref)) {
			return &members[i], nil
		}
	}
	return nil, &client.APIError{StatusCode: http.StatusNotFound, Message: "Member not found"}
}

// teamMemberChanges returns the update that brings the member to the plan,
// or nil, or a problem to report instead. Only what changes is sent: sending
// billing at all needs an owner, and an admin may not send a role for an
// owner even when it stays the same.
func teamMemberChanges(plan *teamMemberResourceModel, current *client.TeamMember) (*client.UpdateTeamMemberRequest, string) {
	var update client.UpdateTeamMemberRequest
	role := current.Role
	if !plan.Role.IsUnknown() && !plan.Role.IsNull() {
		role = plan.Role.ValueString()
		if role != current.Role {
			update.Role = &role
		}
	}
	if !plan.CanManageBilling.IsUnknown() && !plan.CanManageBilling.IsNull() {
		want := plan.CanManageBilling.ValueBool()
		switch {
		case role == "owner":
			// Owners always have billing access; the API ignores the flag.
			if !want {
				return nil, fmt.Sprintf("%s is an owner, and owners always manage billing. Remove can_manage_billing or set it to true.", current.Email)
			}
		case update.Role != nil && current.Role == "owner":
			// The API reports true for every owner, and the member's own
			// flag again after a demotion, so send it.
			update.CanManageBilling = &want
		case want != current.CanManageBilling:
			update.CanManageBilling = &want
		}
	}
	if update.Role == nil && update.CanManageBilling == nil {
		return nil, ""
	}
	return &update, ""
}

func isAPIStatus(err error, status int) bool {
	var apiErr *client.APIError
	return errors.As(err, &apiErr) && apiErr.StatusCode == status
}

func mapTeamMemberToState(m *client.TeamMember, s *teamMemberResourceModel) {
	s.ID = types.StringValue(m.ID)
	// Keep the configured spelling of an address that matches.
	if s.Email.IsNull() || s.Email.IsUnknown() || !strings.EqualFold(s.Email.ValueString(), m.Email) {
		s.Email = types.StringValue(m.Email)
	}
	s.UserID = types.StringValue(m.UserID)
	s.Role = types.StringValue(m.Role)
	s.CanManageBilling = types.BoolValue(m.CanManageBilling)
	s.Name = types.StringPointerValue(m.Name)
	s.CreatedAt = types.StringValue(m.CreatedAt)
}
