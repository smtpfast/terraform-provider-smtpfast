# Signed JSON events for your own endpoint.
resource "smtpfast_webhook" "events" {
  url = "https://example.com/webhooks/smtpfast"
  events = [
    "email.delivered",
    "email.bounced",
    "email.complained",
  ]
}

# Verify deliveries with this secret (X-SMTPfast-Signature header).
output "webhook_signing_secret" {
  value     = smtpfast_webhook.events.signing_secret
  sensitive = true
}

# Readable alerts in a Discord channel. The format is picked from the URL, and
# set here so it stays put if the URL changes.
resource "smtpfast_webhook" "alerts" {
  url    = var.discord_webhook_url
  format = "discord"
  events = ["email.bounced", "email.complained", "domain.updated"]
}
