package provider

import (
	"fmt"
	"os"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/acctest"
	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/terraform"
)

// Labels need an inbox, which needs a receiving domain on the test account,
// so this runs with the inbox test's address.
func TestAccInboxLabelResource(t *testing.T) {
	address := os.Getenv("SMTPFAST_ACC_INBOX_ADDRESS")
	name := fmt.Sprintf("tf-acc %s", acctest.RandString(8))
	config := func(label string) string {
		return fmt.Sprintf(`resource "smtpfast_inbox" "test" {
  email_address = %q
}

resource "smtpfast_inbox_label" "test" {
  inbox_id = smtpfast_inbox.test.id
%s
}`, address, label)
	}

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
				Config: config(fmt.Sprintf("  name = %q", name)),
				Check:  resource.TestCheckResourceAttr("smtpfast_inbox_label.test", "color", "mauve"),
			},
			{
				Config: config(fmt.Sprintf("  name  = \"%s renamed\"\n  color = \"crimson\"", name)),
				Check:  resource.TestCheckResourceAttr("smtpfast_inbox_label.test", "color", "crimson"),
			},
			{
				ResourceName:      "smtpfast_inbox_label.test",
				ImportState:       true,
				ImportStateVerify: true,
				ImportStateIdFunc: func(s *terraform.State) (string, error) {
					rs := s.RootModule().Resources["smtpfast_inbox_label.test"]
					return rs.Primary.Attributes["inbox_id"] + "/" + rs.Primary.ID, nil
				},
			},
		},
	})
}
