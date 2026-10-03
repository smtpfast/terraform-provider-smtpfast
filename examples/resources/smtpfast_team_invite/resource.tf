# Creating an invitation emails the address straight away. Inviting needs a
# paid plan and a free seat on the team.
resource "smtpfast_team_invite" "ada" {
  email = "ada@example.com"
  role  = "admin"
}

output "ada_invite_status" {
  value = smtpfast_team_invite.ada.status # pending, then accepted
}
