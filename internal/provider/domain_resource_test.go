package provider

import (
	"fmt"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/acctest"
	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
)

func TestAccDomainResource(t *testing.T) {
	domain := fmt.Sprintf("tf-acc-%s.example.com", acctest.RandString(10))

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: fmt.Sprintf(`resource "smtpfast_domain" "test" {
  domain = %q
}`, domain),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("smtpfast_domain.test", "domain", domain),
					resource.TestCheckResourceAttrSet("smtpfast_domain.test", "id"),
					resource.TestCheckResourceAttrSet("smtpfast_domain.test", "status"),
					resource.TestCheckResourceAttrSet("smtpfast_domain.test", "dns_records.#"),
				),
			},
			{
				ResourceName:      "smtpfast_domain.test",
				ImportState:       true,
				ImportStateVerify: true,
			},
		},
	})
}
