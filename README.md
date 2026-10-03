# Terraform Provider for SMTPfast

Manage your [SMTPfast](https://smtpfa.st) transactional email setup as code: sending domains, API keys, webhooks, templates, inboxes, and contact properties.

Built with the [Terraform Plugin Framework](https://developer.hashicorp.com/terraform/plugin/framework).

## Why

The headline feature is **sending domains with their DNS records as outputs**. Register a domain and publish its DKIM/SPF/DMARC/MAIL FROM records to your DNS provider from the same configuration:

```hcl
resource "smtpfast_domain" "example" {
  domain = "mail.example.com"
}

# The records SMTPfast needs, published to Cloudflare (swap for Route 53, etc.)
resource "cloudflare_record" "smtpfast" {
  for_each = { for idx, rec in smtpfast_domain.example.dns_records : idx => rec }

  zone_id  = var.cloudflare_zone_id
  type     = each.value.type
  name     = each.value.name
  content  = each.value.value
  priority = each.value.priority # set on MX records only, null otherwise
}
```

No copy-pasting DNS records from a dashboard.

The records only exist once the domain does, and `for_each` needs to know them when it plans. So on the very first run, create the domain on its own with `terraform apply -target=smtpfast_domain.example`, then run a normal `terraform apply`. From then on a single apply keeps everything in step, including turning receiving on or off.

### Inbound email

Once a domain is verified, turn on receiving and the MX record shows up in `dns_records` like any other record, so the same `for_each` publishes it:

```hcl
resource "smtpfast_domain" "inbound" {
  domain            = "inbound.example.com"
  receiving_enabled = true # needs a verified domain (or a subdomain of one); set it in a second apply on a brand-new domain
}

output "receiving_status" {
  value = smtpfast_domain.inbound.receiving_status # disabled, pending, active or failed
}
```

### Templates

Hosted templates are Resend-compatible, and sends always use the published version. The provider publishes every change it applies (`published = true` by default), so the template in your repo is the one your emails use:

```hcl
resource "smtpfast_template" "order_confirmation" {
  name    = "Order confirmation"
  alias   = "order-confirmation" # sends pass template: { id: "order-confirmation" }
  from    = "Acme <orders@mail.example.com>"
  subject = "Your order {{{ORDER_ID}}}"
  html    = file("${path.module}/templates/order-confirmation.html")

  variables = [
    { key = "ORDER_ID", type = "string" },
    { key = "PRICE", type = "number", fallback_value = "0" },
  ]
}
```

If someone edits the template in the dashboard after your plan, the apply stops with a conflict instead of overwriting their change, and the next plan shows the difference.

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

Give the key the scopes for what you manage. The default set covers domains, webhooks, templates and contact properties. Add `apikey:manage` to manage `smtpfast_api_key`, and `inbound:read` for `smtpfast_inbox`. Each resource page lists its scopes.

### Resources and data sources

| Type | Name | Description |
| --- | --- | --- |
| Resource | `smtpfast_domain` | A sending domain, with its required DNS records as outputs and an inbound receiving toggle. |
| Resource | `smtpfast_api_key` | An API key (the secret is returned once, on create). Name and scopes update in place. |
| Resource | `smtpfast_webhook` | An event-delivery webhook, with its signing secret. |
| Resource | `smtpfast_template` | A hosted email template, published whenever Terraform changes it. |
| Resource | `smtpfast_inbox` | An inbox for one address on a receiving domain. |
| Resource | `smtpfast_contact_property` | A declared custom contact field with its default value. |
| Data source | `smtpfast_domain` | Look up an existing domain by ID or name. |

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

When the Terraform CLI is on your `PATH`, `make test` also runs real plan, apply and import cycles against an in-memory fake of the API (`internal/provider/fake_api_test.go`). They catch perpetual diffs and inconsistent results without credentials; without Terraform they are skipped.

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

Use a test account, not production: adding a domain provisions a real sending identity, and a created API key is a real secret. The tests use randomized names and clean up after themselves. The key needs `apikey:manage` for the API key test. The inbox test needs a domain that can already receive mail, so it only runs when you set `SMTPFAST_ACC_INBOX_ADDRESS` to an address on one (and the key has `inbound:read`).

## Releasing

Releases are cut by GoReleaser on a `v*` tag via GitHub Actions. Publishing to the Terraform Registry needs a GPG signing key exposed to the workflow as the `GPG_PRIVATE_KEY` and `PASSPHRASE` secrets, and the public key registered with the registry. See the [registry publishing docs](https://developer.hashicorp.com/terraform/registry/providers/publishing).

## Contributing

Issues and pull requests welcome. Keep the API client, resources, and tests in step, and run `make fmt vet test docs` before opening a PR.

## License

[MPL-2.0](LICENSE). Built with help from [DevOps Daily](https://devops-daily.com).
