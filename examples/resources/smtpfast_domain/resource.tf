# Register a sending domain and publish its DNS records to Cloudflare in one
# apply. Swap cloudflare_record for aws_route53_record, etc. as needed.

resource "smtpfast_domain" "example" {
  domain = "mail.example.com"
}

resource "cloudflare_record" "smtpfast" {
  for_each = { for idx, rec in smtpfast_domain.example.dns_records : idx => rec }

  zone_id = var.cloudflare_zone_id
  type    = each.value.type
  name    = each.value.name
  content = each.value.value
  proxied = false
}

output "domain_status" {
  value = smtpfast_domain.example.status
}
