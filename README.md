# Terraform Provider for SMTPfast

Manage your [SMTPfast](https://smtpfa.st) transactional email setup as code: sending domains, API keys, and webhooks.

Built with the [Terraform Plugin Framework](https://developer.hashicorp.com/terraform/plugin/framework).

## Why

The headline feature is **sending domains with their DNS records as outputs**. Register a domain and publish its DKIM/SPF/DMARC/MAIL FROM records to your DNS provider in a single `terraform apply`:

```hcl
resource "smtpfast_domain" "example" {
  domain = "mail.example.com"
}

# The records SMTPfast needs, published to Cloudflare (swap for Route 53, etc.)
resource "cloudflare_record" "smtpfast" {
  for_each = { for idx, rec in smtpfast_domain.example.dns_records : idx => rec }

  zone_id = var.cloudflare_zone_id
  type    = each.value.type
  name    = each.value.name
  content = each.value.value
}
```

No copy-pasting DNS records from a dashboard.

## Usage

```hcl
terraform {
  required_providers {
    smtpfast = {
      source = "smtpfast/smtpfast"
    }
  }
}

provider "smtpfast" {
  # api_key = "sf_live_..."   # or set SMTPFAST_API_KEY
}
```

Create an API key in the [SMTPfast dashboard](https://smtpfa.st) and export it:

```bash
export SMTPFAST_API_KEY="sf_live_..."
```

### Resources and data sources

| Type | Name | Description |
| --- | --- | --- |
| Resource | `smtpfast_domain` | A sending domain, with its required DNS records as outputs. |
| Resource | `smtpfast_api_key` | An API key (the secret is returned once, on create). |
| Resource | `smtpfast_webhook` | An event-delivery webhook. |
| Data source | `smtpfast_domain` | Look up an existing domain by ID. |

Full reference docs live in [`docs/`](docs/) and, once published, on the Terraform Registry. Runnable examples are in [`examples/`](examples/).

## Development

Requires Go (see `go.mod` for the version) and, for docs generation, the Terraform CLI.

```bash
make build     # compile the provider
make test      # unit tests (no network, no credentials)
make fmt vet   # format and vet
make lint      # golangci-lint
make docs      # regenerate docs/ from schema + examples
```

### Try it locally

Build and point Terraform at your local build with a [dev override](https://developer.hashicorp.com/terraform/cli/config/config-file#development-overrides) in `~/.terraformrc`:

```hcl
provider_installation {
  dev_overrides {
    "smtpfast/smtpfast" = "/path/to/your/GOBIN"
  }
  direct {}
}
```

Then `go install` and run `terraform plan` against a config that uses the provider.

### Acceptance tests

Acceptance tests create and destroy **real** resources against the SMTPfast API. They are gated behind `TF_ACC` and only run when you opt in with credentials:

```bash
export SMTPFAST_API_KEY="sf_live_..."   # use a dedicated test account
make testacc
```

Use a test account, not production: adding a domain provisions a real sending identity, and a created API key is a real secret. The tests use randomized names and clean up after themselves.

## Releasing

Releases are cut by GoReleaser on a `v*` tag via GitHub Actions. Publishing to the Terraform Registry needs a GPG signing key exposed to the workflow as the `GPG_PRIVATE_KEY` and `PASSPHRASE` secrets, and the public key registered with the registry. See the [registry publishing docs](https://developer.hashicorp.com/terraform/registry/providers/publishing).

## Contributing

Issues and pull requests welcome. Keep the API client, resources, and tests in step, and run `make fmt vet test docs` before opening a PR.

## License

[MPL-2.0](LICENSE). Built with help from [DevOps Daily](https://devops-daily.com).
