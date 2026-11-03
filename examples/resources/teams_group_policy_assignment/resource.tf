resource "teams_group_policy_assignment" "meeting" {
  group_id    = "00000000-0000-0000-0000-000000000000"
  policy_type = "TeamsMeetingPolicy"
  policy_name = "RetailManagers"
  rank        = 1
}
