package provider

import (
	"fmt"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/acctest"
	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/plancheck"
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
  scopes = ["email:send"]
}`, name),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("smtpfast_api_key.test", "name", name),
					resource.TestCheckResourceAttrSet("smtpfast_api_key.test", "id"),
					resource.TestCheckResourceAttrSet("smtpfast_api_key.test", "key"),
					resource.TestCheckResourceAttrSet("smtpfast_api_key.test", "prefix"),
				),
			},
			{
				// Rename and widen in place; the secret stays the same.
				Config: fmt.Sprintf(`resource "smtpfast_api_key" "test" {
  name   = "%s-renamed"
  scopes = ["email:send", "domain:read"]
}`, name),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{plancheck.ExpectResourceAction("smtpfast_api_key.test", plancheck.ResourceActionUpdate)},
				},
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("smtpfast_api_key.test", "name", name+"-renamed"),
					resource.TestCheckResourceAttr("smtpfast_api_key.test", "scopes.#", "2"),
				),
			},
			{
				ResourceName:      "smtpfast_api_key.test",
				ImportState:       true,
				ImportStateVerify: true,
				// The secret is only returned on create.
				ImportStateVerifyIgnore: []string{"key"},
			},
		},
	})
}
