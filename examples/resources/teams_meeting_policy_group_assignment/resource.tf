# Assign a custom meeting policy to an Entra group. A user in several groups gets
# the policy of the group with the lowest rank. Teams keeps ranks dense (1..n per
# policy type), so give each group of the type its own rank.
resource "teams_meeting_policy" "restricted" {
  identity       = "RestrictedExternal"
  allow_meet_now = false
}

resource "teams_meeting_policy_group_assignment" "contractors" {
  group_id    = "00000000-0000-0000-0000-000000000000" # Entra group object ID
  policy_name = teams_meeting_policy.restricted.identity
  rank        = 1
}
