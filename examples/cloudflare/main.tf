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
