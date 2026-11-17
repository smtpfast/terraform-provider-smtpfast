# Register a sending domain and publish its DNS records to Cloudflare in one
# apply. The keys of records (dkim_1, dkim_2, dkim_3, spf, dmarc, mail_from_mx,
# mail_from_spf) are known before the domain exists, so the for_each plans on
# the first run. This uses the Cloudflare provider v5 (cloudflare_dns_record,
# ttl required). The "Cloudflare" guide has a complete configuration.

resource "smtpfast_domain" "example" {
  domain = "mail.example.com"

  # Deleting the domain stops all sending from it, and adding it again gives
  # new DKIM records.
  lifecycle {
    prevent_destroy = true
  }
}

resource "cloudflare_dns_record" "smtpfast" {
  for_each = smtpfast_domain.example.records

  zone_id  = var.cloudflare_zone_id
  type     = each.value.type
  name     = each.value.name
  content  = each.value.value
  priority = each.value.priority # MX records only, null otherwise
  ttl      = 1                   # automatic
  proxied  = false               # a proxied DKIM CNAME does not resolve
}

# Wait until the domain can send.
resource "smtpfast_domain_verification" "example" {
  domain_id  = smtpfast_domain.example.id
  depends_on = [cloudflare_dns_record.smtpfast]
}

output "ses_region" {
  value = smtpfast_domain.example.region
}
