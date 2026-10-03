package provider

import (
	"context"
	"fmt"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

// The keepStateOrDefault modifiers plan an omitted attribute as its current
// state value, or as def on create. A static default would instead plan def
// on every apply and undo a change made outside Terraform, such as in the
// dashboard. Create sends the planned default, so the result never depends
// on the API's own default.

type keepStateOrDefaultBool struct{ def bool }

func (m keepStateOrDefaultBool) Description(context.Context) string {
	return fmt.Sprintf("Keeps the current value when omitted; %t on create.", m.def)
}

func (m keepStateOrDefaultBool) MarkdownDescription(ctx context.Context) string {
	return m.Description(ctx)
}

func (m keepStateOrDefaultBool) PlanModifyBool(_ context.Context, req planmodifier.BoolRequest, resp *planmodifier.BoolResponse) {
	if !req.ConfigValue.IsNull() {
		return
	}
	if req.StateValue.IsNull() || req.StateValue.IsUnknown() {
		resp.PlanValue = types.BoolValue(m.def)
		return
	}
	resp.PlanValue = req.StateValue
}

type keepStateOrDefaultString struct{ def string }

func (m keepStateOrDefaultString) Description(context.Context) string {
	return fmt.Sprintf("Keeps the current value when omitted; %q on create.", m.def)
}

func (m keepStateOrDefaultString) MarkdownDescription(ctx context.Context) string {
	return m.Description(ctx)
}

func (m keepStateOrDefaultString) PlanModifyString(_ context.Context, req planmodifier.StringRequest, resp *planmodifier.StringResponse) {
	if !req.ConfigValue.IsNull() {
		return
	}
	if req.StateValue.IsNull() || req.StateValue.IsUnknown() {
		resp.PlanValue = types.StringValue(m.def)
		return
	}
	resp.PlanValue = req.StateValue
}

type keepStateOrDefaultStringList struct{ def []string }

func (m keepStateOrDefaultStringList) Description(context.Context) string {
	return fmt.Sprintf("Keeps the current value when omitted; [%s] on create.", strings.Join(m.def, ", "))
}

func (m keepStateOrDefaultStringList) MarkdownDescription(ctx context.Context) string {
	return m.Description(ctx)
}

func (m keepStateOrDefaultStringList) PlanModifyList(_ context.Context, req planmodifier.ListRequest, resp *planmodifier.ListResponse) {
	if !req.ConfigValue.IsNull() {
		return
	}
	if req.StateValue.IsNull() || req.StateValue.IsUnknown() {
		resp.PlanValue = stringList(m.def)
		return
	}
	resp.PlanValue = req.StateValue
}
