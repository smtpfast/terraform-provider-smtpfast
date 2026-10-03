# Import by webhook id. The signing secret is only returned on create, so
# `signing_secret` stays empty after an import.
terraform import smtpfast_webhook.events wh_abc123
