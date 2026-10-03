# Import by form id. The Turnstile secret is never returned, so it is empty
# after an import until the next apply sends the configured one.
terraform import smtpfast_signup_form.newsletter clx_form123
