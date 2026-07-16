package provider

import (
	"fmt"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/acctest"
	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
)

func TestAccAPIKeyResource(t *testing.T) {
	name := fmt.Sprintf("tf-acc-%s", acctest.RandString(8))

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: fmt.Sprintf(`resource "smtpfast_api_key" "test" {
  name   = %q
  scopes = ["emails:send"]
}`, name),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("smtpfast_api_key.test", "name", name),
					resource.TestCheckResourceAttrSet("smtpfast_api_key.test", "id"),
					resource.TestCheckResourceAttrSet("smtpfast_api_key.test", "key"),
					resource.TestCheckResourceAttrSet("smtpfast_api_key.test", "prefix"),
				),
			},
			{
				ResourceName:      "smtpfast_api_key.test",
				ImportState:       true,
				ImportStateVerify: true,
				// The secret and configured scopes cannot be recovered on import.
				ImportStateVerifyIgnore: []string{"key", "scopes"},
			},
		},
	})
}
