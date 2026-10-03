package provider

import (
	"fmt"
	"os"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
)

// Creating an invitation emails the address, so this only runs against an
// address we own, named in SMTPFAST_ACC_INVITE_EMAIL. It sends one email.
// The test account needs a paid plan and a free seat.
func TestAccTeamInviteResource(t *testing.T) {
	email := os.Getenv("SMTPFAST_ACC_INVITE_EMAIL")

	resource.Test(t, resource.TestCase{
		PreCheck: func() {
			testAccPreCheck(t)
			if email == "" {
				t.Skip("set SMTPFAST_ACC_INVITE_EMAIL to an address you own that is not on the test team; the test emails it an invitation")
			}
		},
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: fmt.Sprintf(`resource "smtpfast_team_invite" "test" {
  email = %q
}`, email),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttrSet("smtpfast_team_invite.test", "id"),
					resource.TestCheckResourceAttr("smtpfast_team_invite.test", "role", "member"),
					resource.TestCheckResourceAttr("smtpfast_team_invite.test", "status", "pending"),
				),
			},
			{
				ResourceName:      "smtpfast_team_invite.test",
				ImportState:       true,
				ImportStateVerify: true,
			},
		},
	})
}
