# Tenant-wide custom (sideloaded) app settings — a singleton, one per tenant.
#
# is_sideloaded_apps_interaction_enabled is write-authoritative: the service reads
# this value from backend replicas that converge slowly, so a read-back flaps
# between values. The provider therefore never reads it back and treats the value
# you declare here as the source of truth (it is Required for that reason).
resource "teams_settings_custom_app" "example" {
  is_sideloaded_apps_interaction_enabled = true
}
