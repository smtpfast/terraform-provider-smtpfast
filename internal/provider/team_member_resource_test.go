package provider

import (
	"fmt"
	"os"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/tfversion"
)

// Needs a second person on the test team, named in SMTPFAST_ACC_MEMBER_EMAIL,
// who we own: each role change emails them. The test leaves them a member.
// It ends with a removed block instead of a destroy, so they stay on the team.
func TestAccTeamMemberResource(t *testing.T) {
	email := os.Getenv("SMTPFAST_ACC_MEMBER_EMAIL")
	config := func(role string) string {
		return fmt.Sprintf(`resource "smtpfast_team_member" "test" {
  email = %q
%s
}`, email, role)
	}

	resource.Test(t, resource.TestCase{
		PreCheck: func() {
			testAccPreCheck(t)
			if email == "" {
				t.Skip("set SMTPFAST_ACC_MEMBER_EMAIL to a member of the test team that you own; the test changes their role and emails them")
			}
		},
		TerraformVersionChecks:   []tfversion.TerraformVersionCheck{tfversion.SkipBelow(tfversion.Version1_7_0)},
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: config(""),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttrSet("smtpfast_team_member.test", "id"),
					resource.TestCheckResourceAttrSet("smtpfast_team_member.test", "user_id"),
					resource.TestCheckResourceAttrSet("smtpfast_team_member.test", "role"),
				),
			},
			{
				Config: config(`  role = "admin"`),
				Check:  resource.TestCheckResourceAttr("smtpfast_team_member.test", "role", "admin"),
			},
			{
				Config: config(`  role = "member"`),
				Check:  resource.TestCheckResourceAttr("smtpfast_team_member.test", "role", "member"),
			},
			{
				ResourceName:      "smtpfast_team_member.test",
				ImportState:       true,
				ImportStateVerify: true,
			},
			{
				Config: `removed {
  from = smtpfast_team_member.test
  lifecycle {
    destroy = false
  }
}`,
			},
		},
	})
}
