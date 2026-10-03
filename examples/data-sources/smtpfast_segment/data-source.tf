# Look a segment up by name, for example one created in the dashboard...
data "smtpfast_segment" "customers" {
  name = "Customers"
}

# ...or by id.
data "smtpfast_segment" "by_id" {
  id = "seg_abc123"
}

output "customers_segment_id" {
  value = data.smtpfast_segment.customers.id
}
