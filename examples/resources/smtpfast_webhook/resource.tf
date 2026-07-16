resource "smtpfast_webhook" "events" {
  url = "https://example.com/webhooks/smtpfast"
  events = [
    "email.delivered",
    "email.bounced",
    "email.complained",
  ]
}
