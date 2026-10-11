# Serve open tracking, click tracking and unsubscribe links from your own
# hostname, and publish its CNAME record to Cloudflare in the same apply.
# Swap cloudflare_record for aws_route53_record, etc. as needed.
#
# The hostname must be a subdomain of a verified sending domain on the team.

resource "smtpfast_link_domain" "links" {
  hostname = "links.example.com"
}

resource "cloudflare_record" "smtpfast_links" {
  zone_id = var.cloudflare_zone_id
  type    = "CNAME"
  name    = smtpfast_link_domain.links.hostname
  content = smtpfast_link_domain.links.cname_target
  proxied = false # DNS only: SMTPfast issues the certificate for the hostname
}

output "link_domain_status" {
  # pending until the CNAME record is seen and the certificate is issued,
  # usually within minutes; refresh to see active.
  value = smtpfast_link_domain.links.status
}
