data "smtpfast_domain" "example" {
  id = "dom_xyz789"
}

output "domain_dns_records" {
  value = data.smtpfast_domain.example.dns_records
}
