# Declared contact fields, with the defaults templates and broadcasts use when
# a contact has no value.
resource "smtpfast_contact_property" "plan" {
  key            = "plan"
  type           = "string"
  fallback_value = "free"
}

resource "smtpfast_contact_property" "seats" {
  key            = "seats"
  type           = "number"
  fallback_value = "1"
}
