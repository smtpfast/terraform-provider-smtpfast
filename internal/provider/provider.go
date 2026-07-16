// Package provider implements the SMTPfast Terraform provider.
package provider

import (
	"context"
	"os"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/path"
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
		MarkdownDescription: "The SMTPfast provider manages resources on [SMTPfast](https://smtpfa.st), a transactional email API: sending domains, API keys, and webhooks.",
		Attributes: map[string]schema.Attribute{
			"api_key": schema.StringAttribute{
				MarkdownDescription: "SMTPfast API key. Can also be set with the `SMTPFAST_API_KEY` environment variable. Create one in the [SMTPfast dashboard](https://smtpfa.st).",
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

	// Values that are still unknown during plan cannot be used to build a
	// client. Surface a clear error rather than sending an empty token.
	if config.APIKey.IsUnknown() {
		resp.Diagnostics.AddAttributeError(
			path.Root("api_key"),
			"Unknown SMTPfast API key",
			"The api_key value is unknown at configuration time. Set it to a known value or via the SMTPFAST_API_KEY environment variable.",
		)
		return
	}

	apiKey := os.Getenv("SMTPFAST_API_KEY")
	if !config.APIKey.IsNull() {
		apiKey = config.APIKey.ValueString()
	}

	baseURL := os.Getenv("SMTPFAST_API_URL")
	if !config.BaseURL.IsNull() {
		baseURL = config.BaseURL.ValueString()
	}

	if apiKey == "" {
		resp.Diagnostics.AddAttributeError(
			path.Root("api_key"),
			"Missing SMTPfast API key",
			"Set the api_key argument or the SMTPFAST_API_KEY environment variable.",
		)
		return
	}

	c := client.New(apiKey, baseURL, "terraform-provider-smtpfast/"+p.version)
	resp.ResourceData = c
	resp.DataSourceData = c
}

func (p *smtpfastProvider) Resources(_ context.Context) []func() resource.Resource {
	return []func() resource.Resource{
		NewDomainResource,
		NewAPIKeyResource,
		NewWebhookResource,
	}
}

func (p *smtpfastProvider) DataSources(_ context.Context) []func() datasource.DataSource {
	return []func() datasource.DataSource{
		NewDomainDataSource,
	}
}
