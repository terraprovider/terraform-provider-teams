# Assign a custom meeting policy to a user. The grant is asynchronous; Terraform
# waits until the assignment is effective.
resource "teams_meeting_policy" "restricted" {
  identity       = "RestrictedExternal"
  allow_meet_now = false
}

resource "teams_meeting_policy_assignment" "exec" {
  user        = "user@contoso.com" # object ID or UPN
  policy_name = teams_meeting_policy.restricted.identity
}
