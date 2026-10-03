package provider

import (
	"fmt"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/acctest"
	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
)

func TestAccSignupFormResource(t *testing.T) {
	name := fmt.Sprintf("tf-acc form %s", acctest.RandString(8))
	const res = "smtpfast_signup_form.test"

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: fmt.Sprintf(`resource "smtpfast_signup_form" "test" {
  name   = %q
  active = false
}`, name),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttrSet(res, "id"),
					resource.TestCheckResourceAttr(res, "fields.#", "2"),
					resource.TestCheckResourceAttr(res, "button_text", "Subscribe"),
					resource.TestCheckResourceAttr(res, "double_opt_in", "true"),
					resource.TestCheckResourceAttr(res, "active", "false"),
				),
			},
			{
				// The keys are placeholders: the API does not check them until
				// someone submits the form, and this form stays paused.
				Config: fmt.Sprintf(`resource "smtpfast_signup_form" "test" {
  name                   = %q
  active                 = false
  fields                 = ["email", "first_name", "last_name"]
  button_color           = "#4f46e5"
  redirect_url           = "https://example.com/tf-acc"
  captcha_enabled        = true
  turnstile_site_key     = "tf-acc-site-key"
  turnstile_secret_key   = "tf-acc-secret-key"
  welcome_email_enabled  = true
  welcome_email_subject  = "Welcome"
  welcome_email_markdown = "# Welcome\n"
}`, name),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr(res, "fields.#", "3"),
					resource.TestCheckResourceAttr(res, "turnstile_secret_configured", "true"),
					resource.TestCheckResourceAttr(res, "welcome_email_subject", "Welcome"),
				),
			},
			{
				ResourceName:            res,
				ImportState:             true,
				ImportStateVerify:       true,
				ImportStateVerifyIgnore: []string{"turnstile_secret_key"},
			},
		},
	})
}
