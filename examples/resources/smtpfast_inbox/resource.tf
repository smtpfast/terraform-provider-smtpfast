# An inbox needs a domain that can send and has receiving turned on. Here
# support.example.com is a subdomain of example.com, which is already verified
# on the team, so receiving can be enabled in the same apply.
resource "smtpfast_domain" "support" {
  domain            = "support.example.com"
  receiving_enabled = true
}

resource "smtpfast_inbox" "help" {
  email_address = "help@${smtpfast_domain.support.domain}"
  name          = "Customer support"
  from_name     = "Acme Support"
}
