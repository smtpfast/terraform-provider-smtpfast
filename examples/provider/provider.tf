terraform {
  required_providers {
    smtpfast = {
      source = "smtpfast/smtpfast"
    }
  }
}

# The API key can also be supplied via the SMTPFAST_API_KEY environment variable.
provider "smtpfast" {
  api_key = var.smtpfast_api_key
}
