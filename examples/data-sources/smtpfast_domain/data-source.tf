# Look a domain up by name...
data "smtpfast_domain" "example" {
  domain = "mail.example.com"
}

# ...or by id.
data "smtpfast_domain" "by_id" {
  id = "dom_xyz789"
}

output "domain_dns_records" {
  value = data.smtpfast_domain.example.dns_records
}
