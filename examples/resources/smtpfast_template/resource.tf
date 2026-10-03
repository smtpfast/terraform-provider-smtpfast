# A transactional template managed as code. Sends name it by alias, and the
# provider publishes every change, so what you apply is what goes out.
resource "smtpfast_template" "order_confirmation" {
  name         = "Order confirmation"
  alias        = "order-confirmation"
  from         = "Acme <orders@mail.example.com>"
  reply_to     = ["support@example.com"]
  subject      = "Your order {{{ORDER_ID}}}"
  preview_text = "Thanks for your order"

  html = <<-EOT
    <h1>Thanks for your order</h1>
    <p>{{{PRODUCT}}}: {{{PRICE}}}</p>
  EOT

  variables = [
    { key = "ORDER_ID", type = "string" },
    { key = "PRODUCT", type = "string", fallback_value = "your item" },
    { key = "PRICE", type = "number", fallback_value = "0" },
  ]
}

# A Markdown body rendered into SMTPfast's email layout. With published =
# false, Terraform only manages the draft and someone publishes it from the
# dashboard after a review.
resource "smtpfast_template" "welcome" {
  name      = "Welcome"
  subject   = "Welcome to Acme"
  published = false

  markdown = <<-EOT
    # Welcome aboard

    Your account is ready. Reply to this email if you need a hand.
  EOT
}
