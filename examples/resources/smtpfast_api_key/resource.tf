# A full-access key.
resource "smtpfast_api_key" "ci" {
  name = "ci-pipeline"
}

# A scoped key that can only send email.
resource "smtpfast_api_key" "send_only" {
  name   = "app-send-only"
  scopes = ["emails:send"]
}

# The secret is only available at create time. Handle it as a sensitive value.
output "ci_api_key" {
  value     = smtpfast_api_key.ci.key
  sensitive = true
}
