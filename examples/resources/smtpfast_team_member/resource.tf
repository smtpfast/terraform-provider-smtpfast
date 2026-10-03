# People join a team by accepting an invitation. Once they have, manage their
# role here. Destroying this resource removes them from the team.
resource "smtpfast_team_member" "grace" {
  email = "grace@example.com"
  role  = "admin"

  lifecycle {
    prevent_destroy = true
  }
}

# Billing access for a member who is not an owner. Changing it needs an API
# key created by an owner.
resource "smtpfast_team_member" "finance" {
  email              = "finance@example.com"
  role               = "member"
  can_manage_billing = true
}

# To stop managing someone without removing them from the team:
#
# removed {
#   from = smtpfast_team_member.finance
#   lifecycle {
#     destroy = false
#   }
# }
