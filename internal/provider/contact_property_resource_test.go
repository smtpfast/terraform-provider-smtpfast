package provider

import (
	"fmt"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/acctest"
	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
)

func TestAccContactPropertyResource(t *testing.T) {
	key := fmt.Sprintf("tf_acc_%s", acctest.RandString(8))

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: fmt.Sprintf(`resource "smtpfast_contact_property" "test" {
  key            = %q
  type           = "number"
  fallback_value = "1"
}`, key),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttrSet("smtpfast_contact_property.test", "id"),
					resource.TestCheckResourceAttr("smtpfast_contact_property.test", "fallback_value", "1"),
				),
			},
			{
				Config: fmt.Sprintf(`resource "smtpfast_contact_property" "test" {
  key            = %q
  type           = "number"
  fallback_value = "5"
}`, key),
				Check: resource.TestCheckResourceAttr("smtpfast_contact_property.test", "fallback_value", "5"),
			},
			{
				ResourceName:      "smtpfast_contact_property.test",
				ImportState:       true,
				ImportStateVerify: true,
			},
		},
	})
}
