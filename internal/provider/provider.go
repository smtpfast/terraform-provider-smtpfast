// Package provider implements the SMTPfast Terraform provider.
package provider

import (
	"context"
	"errors"
	"os"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/provider"
	"github.com/hashicorp/terraform-plugin-framework/provider/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/smtpfast/terraform-provider-smtpfast/internal/client"
)

// Ensure the implementation satisfies the provider.Provider interface.
var _ provider.Provider = &smtpfastProvider{}

type smtpfastProvider struct {
	version string
}

// New returns a provider factory for the given build version.
func New(version string) func() provider.Provider {
	return func() provider.Provider {
		return &smtpfastProvider{version: version}
	}
}

type smtpfastProviderModel struct {
	APIKey  types.String `tfsdk:"api_key"`
	BaseURL types.String `tfsdk:"base_url"`
}

func (p *smtpfastProvider) Metadata(_ context.Context, _ provider.MetadataRequest, resp *provider.MetadataResponse) {
	resp.TypeName = "smtpfast"
	resp.Version = p.version
}

func (p *smtpfastProvider) Schema(_ context.Context, _ provider.SchemaRequest, resp *provider.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "The SMTPfast provider manages resources on [SMTPfast](https://smtpfa.st), a transactional email API: sending domains, API keys, webhooks, templates, " +
			"inboxes and their labels, contact properties, segments, signup forms, and the team's members and invitations.\n\n" +
			"Each resource needs the provider's API key to hold the scopes its page lists. A key created without scopes gets the default set, which does not include " +
			"`apikey:manage`, `inbound:read`, `team:read` or `team:manage`: managing `smtpfast_api_key`, `smtpfast_inbox`, `smtpfast_inbox_label`, `smtpfast_team_invite` " +
			"and `smtpfast_team_member` needs a key created with them.\n\n" +
			"Use two keys. Terraform's key manages the setup: for a sending domain and the app's key it needs `domain:read`, `domain:write` and `apikey:manage`; create it in the dashboard " +
			"(as a team owner or admin) and keep it with Terraform. The app gets its own key from `smtpfast_api_key` with `scopes = [\"email:send\"]`, which cannot change the domain or create keys. " +
			"The [Cloudflare guide](guides/cloudflare) shows both.\n\n" +
			"The key is checked when a resource or data source calls the API, so a configuration where nothing uses SMTPfast (such as `count = 0` in one environment) plans and applies without one.",
		Attributes: map[string]schema.Attribute{
			"api_key": schema.StringAttribute{
				MarkdownDescription: "SMTPfast API key. Can also be set with the `SMTPFAST_API_KEY` environment variable. Create one in the [SMTPfast dashboard](https://smtpfa.st). Needed only when a resource or data source calls the API.",
				Optional:            true,
				Sensitive:           true,
			},
			"base_url": schema.StringAttribute{
				MarkdownDescription: "Base URL of the SMTPfast API. Defaults to `https://smtpfa.st/api`. Can also be set with the `SMTPFAST_API_URL` environment variable.",
				Optional:            true,
			},
		},
	}
}

func (p *smtpfastProvider) Configure(ctx context.Context, req provider.ConfigureRequest, resp *provider.ConfigureResponse) {
	var config smtpfastProviderModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &config)...)
	if resp.Diagnostics.HasError() {
		return
	}

	// The key is checked when a resource or data source calls the API, not
	// here: a configuration where nothing uses SMTPfast (count = 0 in one
	// environment) plans and applies without a key.
	var keyErr error
	apiKey := os.Getenv("SMTPFAST_API_KEY")
	switch {
	case config.APIKey.IsUnknown():
		apiKey = ""
		keyErr = errors.New("the SMTPfast api_key is unknown when this call is made. Set it to a value known at plan time, or use the SMTPFAST_API_KEY environment variable")
	case !config.APIKey.IsNull():
		apiKey = config.APIKey.ValueString()
	}
	if keyErr == nil && apiKey == "" {
		keyErr = errors.New("missing SMTPfast API key. Set the api_key argument of the smtpfast provider or the SMTPFAST_API_KEY environment variable")
	}

	baseURL := os.Getenv("SMTPFAST_API_URL")
	if !config.BaseURL.IsNull() {
		baseURL = config.BaseURL.ValueString()
	}

	c := client.New(apiKey, baseURL, "terraform-provider-smtpfast/"+p.version)
	c.KeyError = keyErr
	resp.ResourceData = c
	resp.DataSourceData = c
}

func (p *smtpfastProvider) Resources(_ context.Context) []func() resource.Resource {
	return []func() resource.Resource{
		NewDomainResource,
		NewDomainVerificationResource,
		NewLinkDomainResource,
		NewAPIKeyResource,
		NewWebhookResource,
		NewTemplateResource,
		NewInboxResource,
		NewContactPropertyResource,
		NewInboxLabelResource,
		NewSegmentResource,
		NewSignupFormResource,
		NewTeamInviteResource,
		NewTeamMemberResource,
	}
}

func (p *smtpfastProvider) DataSources(_ context.Context) []func() datasource.DataSource {
	return []func() datasource.DataSource{
		NewDomainDataSource,
		NewSegmentDataSource,
	}
}
