# Segments for targeting broadcasts. Contacts join them at runtime through the
# contacts API, not through Terraform.
resource "smtpfast_segment" "customers" {
  name        = "Customers"
  description = "Everyone with a paid plan."
  color       = "#10b981"
}

resource "smtpfast_segment" "beta" {
  name = "Beta testers"
}

# Hand the IDs to the application that adds contacts to the segments.
output "segment_ids" {
  value = {
    customers = smtpfast_segment.customers.id
    beta      = smtpfast_segment.beta.id
  }
}
