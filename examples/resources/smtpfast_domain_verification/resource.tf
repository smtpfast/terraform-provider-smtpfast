resource "smtpfast_domain" "example" {
  domain = "mail.example.com"
}

resource "cloudflare_dns_record" "smtpfast" {
  for_each = smtpfast_domain.example.records

  zone_id  = var.cloudflare_zone_id
  type     = each.value.type
  name     = each.value.name
  content  = each.value.value
  priority = each.value.priority
  ttl      = 1
  proxied  = false
}

# Checks after the records exist, then again until the domain is verified or
# the timeout ends. On timeout the error lists the records SMTPfast does not
# see yet.
resource "smtpfast_domain_verification" "example" {
  domain_id  = smtpfast_domain.example.id
  depends_on = [cloudflare_dns_record.smtpfast]

  timeouts {
    create = "30m"
  }
}

# Anything that sends mail can wait for it.
resource "smtpfast_api_key" "app" {
  name       = "app"
  scopes     = ["email:send"]
  depends_on = [smtpfast_domain_verification.example]
}
