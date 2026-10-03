# A key with the API's default scopes: everything except logs:read,
# inbound:read, inbound:delete, team:read, team:manage and apikey:manage.
resource "smtpfast_api_key" "ci" {
  name = "ci-pipeline"
}

# A key that can only send email and look up what it sent.
resource "smtpfast_api_key" "send_only" {
  name   = "app-send-only"
  scopes = ["email:send", "email:read"]
}

# The secret is only available at create time. Handle it as a sensitive value.
output "ci_api_key" {
  value     = smtpfast_api_key.ci.key
  sensitive = true
}
