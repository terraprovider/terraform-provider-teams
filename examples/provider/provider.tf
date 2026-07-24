terraform {
  required_providers {
    teams = { source = "terraprovider/teams" }
  }
}

# Auth resolves ARM_* / AZURE_* env vars (cert, secret, OIDC, MSI, CLI); the app's
# service principal needs a Teams admin directory role for app-only.
provider "teams" {}

resource "teams_meeting_policy" "restricted" {
  identity                         = "RestrictedMeetings"
  allow_meet_now                   = false
  allow_channel_meeting_scheduling = false
}
