package provider

import (
	"fmt"
	"os"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
)

// An inbox needs a domain on the test account that can send and has
// receiving turned on, so this test only runs when one is named.
func TestAccInboxResource(t *testing.T) {
	address := os.Getenv("SMTPFAST_ACC_INBOX_ADDRESS")

	resource.Test(t, resource.TestCase{
		PreCheck: func() {
			testAccPreCheck(t)
			if address == "" {
				t.Skip("set SMTPFAST_ACC_INBOX_ADDRESS to an address on a receiving domain of the test account")
			}
		},
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: fmt.Sprintf(`resource "smtpfast_inbox" "test" {
  email_address = %q
  from_name     = "TF Acc"
}`, address),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttrSet("smtpfast_inbox.test", "id"),
					resource.TestCheckResourceAttr("smtpfast_inbox.test", "name", address),
				),
			},
			{
				Config: fmt.Sprintf(`resource "smtpfast_inbox" "test" {
  email_address = %q
  name          = "tf-acc inbox"
}`, address),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("smtpfast_inbox.test", "name", "tf-acc inbox"),
					resource.TestCheckNoResourceAttr("smtpfast_inbox.test", "from_name"),
				),
			},
			{
				ResourceName:      "smtpfast_inbox.test",
				ImportState:       true,
				ImportStateVerify: true,
			},
		},
	})
}
