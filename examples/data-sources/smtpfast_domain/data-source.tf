# Look a domain up by name...
data "smtpfast_domain" "example" {
  domain = "mail.example.com"
}

# ...or by id.
data "smtpfast_domain" "by_id" {
  id = "dom_xyz789"
}

output "domain_records" {
  value = data.smtpfast_domain.example.records
}

output "spf_record_name" {
  value = data.smtpfast_domain.example.records["spf"].name
}
