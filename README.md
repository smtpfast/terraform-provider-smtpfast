# Terraform Provider for SMTPfast

Manage your [SMTPfast](https://smtpfa.st) transactional email setup as code: sending domains, API keys, webhooks, templates, inboxes and their labels, contact properties, segments, signup forms, and your team's members and invitations.

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

### Team

People join a team by accepting an invitation, so the two team resources split the work. `smtpfast_team_invite` sends the invitation (a real email, and it needs a paid plan and a free seat). Once the person has accepted, `smtpfast_team_member` adopts them and manages their role:

```hcl
resource "smtpfast_team_invite" "ada" {
  email = "ada@example.com"
  role  = "admin"
}

# Added after Ada accepts. Destroying it removes her from the team.
resource "smtpfast_team_member" "ada" {
  email = "ada@example.com"
  role  = "admin"

  lifecycle {
    prevent_destroy = true
  }
}
```

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

Give the key the scopes for what you manage. The default set covers domains, webhooks, templates, contact properties, segments and signup forms. Add `apikey:manage` to manage `smtpfast_api_key`, `inbound:read` for `smtpfast_inbox` and `smtpfast_inbox_label`, and `team:read` and `team:manage` for `smtpfast_team_invite` and `smtpfast_team_member`. Each resource page lists its scopes.

### Resources and data sources

| Type | Name | Description |
| --- | --- | --- |
| Resource | `smtpfast_domain` | A sending domain, with its required DNS records as outputs and an inbound receiving toggle. |
| Resource | `smtpfast_api_key` | An API key (the secret is returned once, on create). Name and scopes update in place. |
| Resource | `smtpfast_webhook` | An event-delivery webhook, with its signing secret. |
| Resource | `smtpfast_template` | A hosted email template, published whenever Terraform changes it. |
| Resource | `smtpfast_inbox` | An inbox for one address on a receiving domain. |
| Resource | `smtpfast_contact_property` | A declared custom contact field with its default value. |
| Resource | `smtpfast_inbox_label` | A named, colored label in an inbox. |
| Resource | `smtpfast_segment` | A contact segment for targeting broadcasts (the segment, not who is in it). |
| Resource | `smtpfast_signup_form` | A hosted signup form, with double opt-in, Turnstile and a welcome email. |
| Resource | `smtpfast_team_invite` | An invitation to the team. Creating it emails the person. |
| Resource | `smtpfast_team_member` | An existing member's role and billing access. Destroying it removes them from the team. |
| Data source | `smtpfast_domain` | Look up an existing domain by ID or name. |
| Data source | `smtpfast_segment` | Look up an existing segment by ID or name. |

Full reference docs live in [`docs/`](docs/) and, once published, on the Terraform Registry. Runnable examples are in [`examples/`](examples/).

## Development

Requires Go (see `go.mod` for the version) and, for docs generation, the Terraform CLI.

```bash
make build     # compile the provider
make test      # unit tests (no network, no credentials)
make fmt vet   # format and vet
make lint      # golangci-lint
make docs      # regenerate docs/ from schema + examples
make spec-coverage  # list API operations not classified in spec-coverage.json
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

Use a test account, not production: adding a domain provisions a real sending identity, and a created API key is a real secret. The tests use randomized names and clean up after themselves. The key needs `apikey:manage` for the API key test. The inbox test needs a domain that can already receive mail, so it only runs when you set `SMTPFAST_ACC_INBOX_ADDRESS` to an address on one (and the key has `inbound:read`). The inbox label test uses the same address. The team tests send real email, so they only run against addresses you own: `SMTPFAST_ACC_INVITE_EMAIL` is an address that is not on the team, which the invite test emails one invitation (the account needs a paid plan and a free seat), and `SMTPFAST_ACC_MEMBER_EMAIL` is a member of the team whose role the member test changes, emailing them each time, and leaves as `member` without removing them. Both need a key with `team:read` and `team:manage` created by an owner or admin.

### Spec coverage

A daily workflow (`.github/workflows/spec-coverage.yml`) checks the live [OpenAPI spec](https://smtpfa.st/api/v1/openapi.json) against `spec-coverage.json`. It keeps one issue, labelled `spec-coverage`, that lists every operation the file does not classify, and closes it when nothing is left. Run the same check locally with `make spec-coverage`.

Each operation in the spec is either:

- **covered**: listed under the resource that calls it, as one exact `"METHOD /path"`. Keep this in step with `internal/client/`.
- **ignored**: matched by a rule in a group with a short reason. This is for runtime work and data that do not belong in Terraform, such as sending mail, logs, contacts and broadcasts.

A rule is `"METHOD /path"`, or `"/path"` for every method. In a path, `{id}` matches any path parameter (whatever the spec calls it) but not a fixed segment, `*` matches one segment, and a trailing `**` matches the path and everything below it.

When the issue lists an operation:

- If the provider should manage it, add or extend the resource and list the operation under it in `covered`, in the same PR.
- If it is runtime work or data, add a rule to the `ignored` group that fits, or a new group with its reason. Ignore a whole family with `/v1/things/**` only when nothing in it could be infrastructure, so later endpoints there need no edits. Where a family mixes both, use exact rules, so a new sub-resource still shows up.
- If it could be a resource but nobody has built it yet, leave it unclassified. The issue is the to-do list.

Do not close the issue by hand; the next run reopens it while anything is left. The issue also lists rules that match no operation, because a covered rule there can mean the provider calls an endpoint the API no longer has.

## Releasing

Releases are cut by GoReleaser on a `v*` tag via GitHub Actions. Publishing to the Terraform Registry needs a GPG signing key exposed to the workflow as the `GPG_PRIVATE_KEY` and `PASSPHRASE` secrets, and the public key registered with the registry. See the [registry publishing docs](https://developer.hashicorp.com/terraform/registry/providers/publishing).

## Contributing

Issues and pull requests welcome. Keep the API client, resources, tests, and `spec-coverage.json` in step, and run `make fmt vet test docs` before opening a PR.

## License

[MPL-2.0](LICENSE). Built with help from [DevOps Daily](https://devops-daily.com).
