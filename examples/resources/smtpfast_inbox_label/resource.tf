resource "smtpfast_inbox" "support" {
  email_address = "support@inbound.example.com"
}

resource "smtpfast_inbox_label" "urgent" {
  inbox_id = smtpfast_inbox.support.id
  name     = "Urgent"
  color    = "crimson"
}

resource "smtpfast_inbox_label" "billing" {
  inbox_id = smtpfast_inbox.support.id
  name     = "Billing"
  # color is mauve when omitted
}
