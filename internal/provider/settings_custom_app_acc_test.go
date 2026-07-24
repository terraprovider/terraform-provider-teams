package provider

import (
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
)

// TestAccSettingsCustomApp_toggle exercises the tenant-wide custom-app settings
// singleton (Set-CsTeamsSettingsCustomApp). It flips is_sideloaded_apps_interaction_enabled
// off, then back on, then destroys — which is a no-op (the config persists). The
// final applied value is `true`, restoring the dev tenant's default, so the run is
// self-reverting.
//
// This is a real, tenant-wide mutation (not a no-op adopt like the other config
// singletons), so it runs serially rather than in parallel. The Set is eventually
// consistent (~1-3s); the go-teams binding blocks until the write is readable, so
// the immediate read-back below is reliable.
func TestAccSettingsCustomApp_toggle(t *testing.T) {
	const rn = "teams_settings_custom_app.test"
	cfg := func(v bool) string {
		if v {
			return `resource "teams_settings_custom_app" "test" { is_sideloaded_apps_interaction_enabled = true }`
		}
		return `resource "teams_settings_custom_app" "test" { is_sideloaded_apps_interaction_enabled = false }`
	}
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{ // create: turn it off
				Config: cfg(false),
				Check:  resource.TestCheckResourceAttr(rn, "is_sideloaded_apps_interaction_enabled", "false"),
			},
			{ // update: turn it back on (restores the tenant default)
				Config: cfg(true),
				Check:  resource.TestCheckResourceAttr(rn, "is_sideloaded_apps_interaction_enabled", "true"),
			},
		},
	})
}
