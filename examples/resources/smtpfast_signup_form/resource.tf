# A newsletter signup form with double opt-in and a welcome email.
resource "smtpfast_signup_form" "newsletter" {
  name            = "Newsletter"
  fields          = ["email", "first_name"]
  button_text     = "Join the newsletter"
  button_color    = "#4f46e5"
  success_message = "Check your inbox to confirm your subscription."

  confirmation_email_from = "Acme <hello@mail.example.com>"

  welcome_email_enabled  = true
  welcome_email_from     = "Acme <hello@mail.example.com>"
  welcome_email_subject  = "Welcome to the Acme newsletter"
  welcome_email_markdown = file("${path.module}/welcome.md")
}

# A form behind Cloudflare Turnstile that sends people to a thank-you page.
resource "smtpfast_signup_form" "waitlist" {
  name         = "Beta waitlist"
  redirect_url = "https://example.com/thanks"

  captcha_enabled      = true
  turnstile_site_key   = var.turnstile_site_key
  turnstile_secret_key = var.turnstile_secret_key
}

# The embed snippet for a page.
output "newsletter_embed" {
  value = <<-HTML
    <div data-smtpfast-form="${smtpfast_signup_form.newsletter.id}"></div>
    <script src="https://smtpfa.st/api/forms/${smtpfast_signup_form.newsletter.id}/embed.js" async></script>
  HTML
}
