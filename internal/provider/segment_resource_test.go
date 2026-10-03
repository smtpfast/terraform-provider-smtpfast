package provider

import (
	"fmt"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/acctest"
	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
)

func TestAccSegmentResource(t *testing.T) {
	name := fmt.Sprintf("tf-acc segment %s", acctest.RandString(8))

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: fmt.Sprintf(`resource "smtpfast_segment" "test" {
  name        = %q
  description = "Created by the acceptance tests."
  color       = "#10b981"
}`, name),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttrSet("smtpfast_segment.test", "id"),
					resource.TestCheckResourceAttr("smtpfast_segment.test", "color", "#10b981"),
				),
			},
			{
				Config: fmt.Sprintf(`resource "smtpfast_segment" "test" { name = "%s renamed" }

data "smtpfast_segment" "test" {
  name = smtpfast_segment.test.name
}`, name),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckNoResourceAttr("smtpfast_segment.test", "description"),
					resource.TestCheckResourceAttrPair("data.smtpfast_segment.test", "id", "smtpfast_segment.test", "id"),
				),
			},
			{
				ResourceName:      "smtpfast_segment.test",
				ImportState:       true,
				ImportStateVerify: true,
			},
		},
	})
}
