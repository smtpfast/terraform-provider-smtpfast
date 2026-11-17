package provider

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"

	"github.com/hashicorp/terraform-plugin-framework-timeouts/resource/timeouts"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/smtpfast/terraform-provider-smtpfast/internal/client"
)

var (
	_ resource.Resource              = &domainVerificationResource{}
	_ resource.ResourceWithConfigure = &domainVerificationResource{}
)

const defaultVerificationTimeout = 30 * time.Minute

// verificationPollInterval is the first wait between two checks. It doubles
// up to verificationPollMax. Tests make both short.
var (
	verificationPollInterval = 10 * time.Second
	verificationPollMax      = time.Minute
)

// NewDomainVerificationResource returns a new smtpfast_domain_verification resource.
func NewDomainVerificationResource() resource.Resource {
	return &domainVerificationResource{}
}

type domainVerificationResource struct {
	client *client.Client
}

type domainVerificationResourceModel struct {
	ID       types.String   `tfsdk:"id"`
	DomainID types.String   `tfsdk:"domain_id"`
	Domain   types.String   `tfsdk:"domain"`
	Status   types.String   `tfsdk:"status"`
	Timeouts timeouts.Value `tfsdk:"timeouts"`
}

func (r *domainVerificationResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_domain_verification"
}

func (r *domainVerificationResource) Schema(ctx context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Waits until a sending domain is verified, so the resources that send mail can `depends_on` it. " +
			"Creating it asks SMTPfast to check the domain now (the dashboard's Verify button) and checks again, less often each time, until the domain is verified or the create timeout ends (30 minutes by default). " +
			"On timeout the error lists each DNS record that SMTPfast does not see yet, with what it found instead.\n\n" +
			"Make it depend on the DNS records, so the first check runs after they exist. `verified` means the domain can send: DKIM and the email provider identity are verified. SPF, DMARC and MAIL FROM can still be pending; the domain's `spf_status`, `dmarc_status` and `mail_from_status` show them.\n\n" +
			"It checks once, when it is created; it does not follow the domain's status later. Destroying it changes nothing on SMTPfast.\n\n" +
			"Needs a provider API key with the `domain:read` and `domain:write` scopes.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				MarkdownDescription: "The domain ID.",
				Computed:            true,
				PlanModifiers:       []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"domain_id": schema.StringAttribute{
				MarkdownDescription: "ID of the `smtpfast_domain` to wait for. Changing it forces a new resource.",
				Required:            true,
				PlanModifiers:       []planmodifier.String{stringplanmodifier.RequiresReplace()},
			},
			"domain": schema.StringAttribute{
				MarkdownDescription: "The domain name.",
				Computed:            true,
				PlanModifiers:       []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"status": schema.StringAttribute{
				MarkdownDescription: "`verified` once the wait is over.",
				Computed:            true,
				PlanModifiers:       []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
		},
		Blocks: map[string]schema.Block{
			"timeouts": timeouts.Block(ctx, timeouts.Opts{Create: true}),
		},
	}
}

func (r *domainVerificationResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
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

func (r *domainVerificationResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan domainVerificationResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	timeout, diags := plan.Timeouts.Create(ctx, defaultVerificationTimeout)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	id := plan.DomainID.ValueString()
	waitCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	var last *client.DomainVerification
	var lastErr error
	wait := verificationPollInterval
	for {
		res, err := r.client.VerifyDomain(waitCtx, id)
		switch {
		case err == nil:
			last, lastErr = res, nil
			if res.Status == "verified" {
				plan.ID = types.StringValue(id)
				plan.Domain = types.StringValue(res.Domain)
				plan.Status = types.StringValue(res.Status)
				resp.Diagnostics.Append(resp.State.Set(ctx, plan)...)
				return
			}
		case waitCtx.Err() != nil:
			// The timeout ended during the call.
		case retryableVerifyError(err):
			lastErr = err
		default:
			resp.Diagnostics.AddError("Error verifying domain", err.Error())
			return
		}

		select {
		case <-waitCtx.Done():
			if ctx.Err() != nil {
				resp.Diagnostics.AddError("Domain verification interrupted", ctx.Err().Error())
				return
			}
			resp.Diagnostics.AddError("Domain not verified in time", r.timeoutDetail(ctx, id, timeout, last, lastErr))
			return
		case <-time.After(wait):
		}
		wait = min(wait*2, verificationPollMax)
	}
}

// retryableVerifyError reports whether a failed check is worth another try:
// rate limits, server errors and network errors. A missing key is not.
func retryableVerifyError(err error) bool {
	var apiErr *client.APIError
	if errors.As(err, &apiErr) {
		return apiErr.StatusCode == http.StatusTooManyRequests || apiErr.StatusCode >= http.StatusInternalServerError
	}
	var netErr *url.Error
	return errors.As(err, &netErr)
}

// timeoutDetail explains why the domain is not verified: the last check, and
// each DNS record whose live lookup does not match yet.
func (r *domainVerificationResource) timeoutDetail(ctx context.Context, id string, timeout time.Duration, last *client.DomainVerification, lastErr error) string {
	var b strings.Builder
	name := id
	if last != nil && last.Domain != "" {
		name = last.Domain
	}
	fmt.Fprintf(&b, "Domain %s is not verified after %s.", name, timeout)
	if last != nil {
		fmt.Fprintf(&b, " Last check: status %s, phase %s.", last.Status, last.Phase)
		if last.Message != "" {
			fmt.Fprintf(&b, " %s", last.Message)
		}
		if len(last.Checks) > 0 {
			names := make([]string, 0, len(last.Checks))
			for name := range last.Checks {
				names = append(names, name)
			}
			sort.Strings(names)
			parts := make([]string, 0, len(names))
			for _, name := range names {
				parts = append(parts, name+" "+last.Checks[name])
			}
			fmt.Fprintf(&b, " Checks: %s.", strings.Join(parts, ", "))
		}
	} else if lastErr != nil {
		fmt.Fprintf(&b, " The last check failed: %s.", lastErr)
	}

	readCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	domain, err := r.client.GetDomain(readCtx, id)
	if err != nil {
		return b.String()
	}
	var lines []string
	for _, rec := range domain.DNSRecords {
		if rec.LiveCheck == "" || rec.LiveCheck == "match" {
			continue
		}
		expected := rec.Value
		if rec.Priority != nil {
			expected = fmt.Sprintf("%d %s", *rec.Priority, rec.Value)
		}
		line := fmt.Sprintf("  - %s %s (%s): %s, expected %q", rec.Type, rec.Name, rec.Purpose, rec.LiveCheck, expected)
		if rec.ActualValue != nil && *rec.ActualValue != "" {
			line += fmt.Sprintf(", found %q", *rec.ActualValue)
		}
		lines = append(lines, line)
	}
	if len(lines) > 0 {
		b.WriteString("\n\nDNS records SMTPfast does not see as expected yet:\n")
		b.WriteString(strings.Join(lines, "\n"))
	}
	b.WriteString("\n\nCheck the records in your DNS provider, then apply again; a longer wait can be set with timeouts { create = \"60m\" }.")
	return b.String()
}

// Read drops the resource when its domain is gone. It does not check the
// status again: the resource only gates the first apply.
func (r *domainVerificationResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state domainVerificationResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	domain, err := r.client.GetDomain(ctx, state.DomainID.ValueString())
	if err != nil {
		if client.IsNotFound(err) {
			resp.State.RemoveResource(ctx)
			return
		}
		resp.Diagnostics.AddError("Error reading domain", err.Error())
		return
	}
	state.Domain = types.StringValue(domain.Domain)
	resp.Diagnostics.Append(resp.State.Set(ctx, state)...)
}

// Update only stores a changed timeouts block; every other change replaces
// the resource.
func (r *domainVerificationResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan domainVerificationResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(resp.State.Set(ctx, plan)...)
}

// Delete forgets the resource. The domain stays as it is.
func (r *domainVerificationResource) Delete(context.Context, resource.DeleteRequest, *resource.DeleteResponse) {
}
