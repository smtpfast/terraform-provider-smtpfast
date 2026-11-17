---
page_title: "Sending domain with Cloudflare DNS"
subcategory: ""
description: |-
  Set up a sending domain, its DNS records on Cloudflare, verification and a send-only app key in one apply.
---

# Sending domain with Cloudflare DNS

This configuration sets up a sending domain from nothing in one `terraform apply`:

1. `smtpfast_domain` registers the domain. Its `records` map has fixed keys (`dkim_1`, `dkim_2`, `dkim_3`, `spf`, `dmarc`, `mail_from_mx`, `mail_from_spf`), known before the domain exists, so `for_each` over it plans on the first run.
2. `cloudflare_dns_record` publishes each record. This needs the Cloudflare provider **v5**: v4 called the resource `cloudflare_record`, and v5 makes `ttl` required (`1` is automatic). Keep `proxied = false`.
3. `smtpfast_domain_verification` waits until SMTPfast sees the records and the domain can send.
4. `smtpfast_api_key` creates a send-only key for the app.

```terraform
# A sending domain on SMTPfast with its DNS records on Cloudflare, in one
# apply, with the Cloudflare provider v5. CI plans this file against the
# provider build (see .github/workflows/test.yml).
#
#   export SMTPFAST_API_KEY=...      # the Terraform key, see "Two keys"
#   export CLOUDFLARE_API_TOKEN=...  # Zone DNS edit on the zone
#   terraform apply -var domain=example.com -var cloudflare_zone_id=...

terraform {
  required_version = ">= 1.5"

  required_providers {
    smtpfast = {
      source  = "smtpfast/smtpfast"
      version = ">= 0.7.0"
    }
    cloudflare = {
      source  = "cloudflare/cloudflare"
      version = "~> 5.0"
    }
  }
}

provider "smtpfast" {}
provider "cloudflare" {}

variable "domain" {
  description = "The domain to send from, such as example.com."
  type        = string
}

variable "cloudflare_zone_id" {
  description = "The Cloudflare zone that holds the domain."
  type        = string
}

variable "spf_includes" {
  description = "Other services that send as this domain, such as [\"_spf.google.com\"]. They are merged into the one SPF record."
  type        = list(string)
  default     = []
}

resource "smtpfast_domain" "mail" {
  domain = var.domain

  # Deleting the domain stops all sending from it, and adding it again gives
  # new DKIM records.
  lifecycle {
    prevent_destroy = true
  }
}

locals {
  # A domain may have only one SPF record: SMTPfast's include goes into the
  # same record as the other senders.
  spf_record = "v=spf1 ${join(" ", [for d in concat(var.spf_includes, [smtpfast_domain.mail.spf_include]) : "include:${d}"])} ~all"
}

# The keys are known before the domain exists (dkim_1, dkim_2, dkim_3, spf,
# dmarc, mail_from_mx, mail_from_spf), so this plans on the first apply. A new
# value for a key updates that record in place.
resource "cloudflare_dns_record" "mail" {
  for_each = smtpfast_domain.mail.records

  zone_id  = var.cloudflare_zone_id
  type     = each.value.type
  name     = each.value.name
  content  = each.key == "spf" ? local.spf_record : each.value.value
  priority = each.value.priority # MX records only, null otherwise
  ttl      = 1                   # automatic; ttl is required in v5
  proxied  = false               # a proxied DKIM CNAME does not resolve
  comment  = "SMTPfast ${each.key}"
}

# Waits until the domain can send. On timeout the error lists the records
# SMTPfast does not see yet.
resource "smtpfast_domain_verification" "mail" {
  domain_id  = smtpfast_domain.mail.id
  depends_on = [cloudflare_dns_record.mail]

  timeouts {
    create = "30m"
  }
}

# The app's key can send mail and nothing else. Terraform's own key stays
# with Terraform.
resource "smtpfast_api_key" "app" {
  name       = "app (${var.domain})"
  scopes     = ["email:send"]
  depends_on = [smtpfast_domain_verification.mail]
}

output "app_api_key" {
  description = "Give this to the app, for example as SMTPFAST_API_KEY in its secrets."
  value       = smtpfast_api_key.app.key
  sensitive   = true
}

output "ses_region" {
  value = smtpfast_domain.mail.region
}
```

## SPF: one record per domain

SMTPfast asks for `v=spf1 include:amazonses.com ~all` on the domain. A domain may have only one SPF record: a second one makes SPF fail for every message, and SMTPfast reports `multiple_spf_records`. If the domain already sends through another service, put that service in `spf_includes` and the configuration merges both into one record, with `spf_include` (`amazonses.com`) for SMTPfast's part. Remove any SPF record that Terraform does not manage.

## Two keys

Use two API keys with different jobs:

- **The Terraform key** (`SMTPFAST_API_KEY` for this configuration) manages the domain and creates the app's key. It needs `domain:read`, `domain:write` and `apikey:manage`. Create it in the dashboard by a team owner or admin, tick those scopes, and keep it in your Terraform secrets only. A key with `apikey:manage` can create keys with more access than itself.
- **The app key** (`smtpfast_api_key.app`) can only send. If it leaks, it cannot change the domain or create keys.

If your Terraform key has no `apikey:manage`, leave out `smtpfast_api_key.app` and create the app key in the dashboard with only `email:send`.

## Moving from dns_records

Before `records`, the records came as the `dns_records` list, which needed a `-target` apply first. If your DNS records are keyed by `"${type} ${name}"` over `dns_records`, switch the `for_each` to `smtpfast_domain.mail.records` and add a `moved` block per record, so Terraform renames them instead of deleting and creating them:

```terraform
moved {
  from = cloudflare_dns_record.mail["TXT example.com"]
  to   = cloudflare_dns_record.mail["spf"]
}

moved {
  from = cloudflare_dns_record.mail["TXT _dmarc.example.com"]
  to   = cloudflare_dns_record.mail["dmarc"]
}
```

`terraform state list` shows the old keys, and `terraform console` with `smtpfast_domain.mail.records` shows the new key of each record.

## Keep the domain

`prevent_destroy` stops a `terraform destroy` or a changed `domain` from deleting the domain. Deleting it stops all sending from it, and adding it again gives new DKIM records that must be published and verified again.

## No key in other environments

The provider checks the API key only when a resource or data source calls SMTPfast. A configuration that creates nothing in one environment (for example `count = 0` in staging) plans and applies there without a key.
