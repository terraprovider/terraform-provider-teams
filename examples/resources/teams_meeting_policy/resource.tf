resource "teams_meeting_policy" "restricted" {
  identity    = "RestrictedExternal"
  description = "Locked-down meeting policy for external-facing staff."

  allow_meet_now                   = false
  allow_channel_meeting_scheduling = false
  allow_cloud_recording            = false
  allow_transcription              = false

  auto_admitted_users = "EveryoneInCompany"
}
