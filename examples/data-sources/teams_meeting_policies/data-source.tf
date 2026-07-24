# List every meeting policy in the tenant (the built-in Global plus any custom
# Tag: policies).
data "teams_meeting_policies" "all" {}

output "meeting_policy_names" {
  value = [for p in data.teams_meeting_policies.all.meeting_policies : p.identity]
}
