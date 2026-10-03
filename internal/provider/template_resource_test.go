package provider

import (
	"fmt"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/acctest"
	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
)

func TestAccTemplateResource(t *testing.T) {
	alias := fmt.Sprintf("tf-acc-%s", acctest.RandString(8))

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: fmt.Sprintf(`resource "smtpfast_template" "test" {
  name    = "tf-acc order confirmation"
  alias   = %q
  subject = "Your order {{{ORDER_ID}}}"
  html    = "<p>{{{PRODUCT}}} costs {{{PRICE}}}</p>"

  variables = [
    { key = "ORDER_ID", type = "string" },
    { key = "PRODUCT", type = "string", fallback_value = "item" },
    { key = "PRICE", type = "number", fallback_value = "25" },
  ]
}`, alias),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttrSet("smtpfast_template.test", "id"),
					resource.TestCheckResourceAttr("smtpfast_template.test", "status", "published"),
					resource.TestCheckResourceAttr("smtpfast_template.test", "has_unpublished_versions", "false"),
				),
			},
			{
				// Edit and publish again in place.
				Config: fmt.Sprintf(`resource "smtpfast_template" "test" {
  name     = "tf-acc order confirmation"
  alias    = %q
  markdown = "# Thanks for your order"
}`, alias),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckNoResourceAttr("smtpfast_template.test", "html"),
					resource.TestCheckResourceAttr("smtpfast_template.test", "has_unpublished_versions", "false"),
				),
			},
			{
				ResourceName:      "smtpfast_template.test",
				ImportState:       true,
				ImportStateVerify: true,
			},
		},
	})
}
