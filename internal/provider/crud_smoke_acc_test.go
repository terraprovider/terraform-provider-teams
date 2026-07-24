package provider

import (
	"fmt"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/acctest"
	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
)

// crudSmokeResources are the IdentityIsName CRUD resources that create cleanly
// from just an -Identity (server defaults for everything else) and are trivially
// reverted (a custom Tag: instance removed on destroy). Each is exercised as an
// independent, parallel create→destroy smoke test.
//
// Resources whose New cmdlet needs additional required inputs (voice routes/
// gateways, number patterns, network topology, translation rules, …) are NOT
// here — they can't be created from an identity alone and need purpose-built
// fixtures; see crudNeedsFixtureResources for the deferred list.
var crudSmokeResources = []string{
	"ai_policy",
	"app_permission_policy",
	"app_setup_policy",
	"application_access_policy",
	"audio_conferencing_policy",
	"byod_and_desks_policy",
	"call_park_policy",
	"calling_policy",
	"channels_policy",
	"compliance_recording_policy",
	"cortana_policy",
	"emergency_call_routing_policy",
	"emergency_calling_policy",
	"enhanced_encryption_policy",
	"events_policy",
	"external_access_policy",
	"feedback_policy",
	"files_policy",
	"ip_phone_policy",
	"media_connectivity_policy",
	"meeting_branding_policy",
	"meeting_broadcast_policy",
	"meeting_policy",
	"meeting_template_permission_policy",
	"messaging_policy",
	"mobility_policy",
	"network_roaming_policy",
	"online_audio_conferencing_routing_policy",
	"online_voice_routing_policy",
	"online_voicemail_policy",
	"recording_roll_out_policy",
	"room_video_tele_conferencing_policy",
	"shifts_policy",
	"survivable_branch_appliance_policy",
	"template_permission_policy",
	"tenant_dial_plan",
	"update_management_policy",
	"vdi_policy",
	"virtual_appointments_policy",
	"voice_applications_policy",
	"work_load_policy",
	"work_location_detection_policy",
	"calling_line_identity",
}

// crudNeedsFixtureResources are CRUD resources deliberately excluded from the
// identity-only smoke sweep because they need specific create inputs. Seven of
// them are now covered by TestAccCRUDFixtures (translation_rule, both number
// patterns, tenant_network_region, survivable_branch_appliance, online_voice_route,
// video_interop_service_provider). The ones below remain deferred with a concrete
// blocker; they surface as skips so the gap stays visible:
//
//	online_pstn_gateway            needs a verified SIP domain configured on the tenant
//	tenant_network_subnet          needs an existing network site to attach to
//	call_hold_policy               needs a real uploaded audio file / valid streaming URL
//	unassigned_number_treatment    needs a real routing target (resource account/announcement)
//	shared_calling_routing_policy  needs a Resource Account object
//	personal_attendant_policy      Remove is server-restricted ("temporarily restricted")
//
// (tenant_trusted_ip_address, previously int-blocked, is now covered by
// TestAccTenantTrustedIPAddress_basic thanks to TypeInt support.)
var crudNeedsFixtureResources = []string{
	"online_pstn_gateway", "tenant_network_subnet",
	"call_hold_policy", "unassigned_number_treatment", "shared_calling_routing_policy",
	"personal_attendant_policy",
}

// TestAccCRUDSmoke creates each identity-only CRUD resource and lets the framework
// destroy it, all as parallel subtests (unrelated resources run concurrently).
func TestAccCRUDSmoke(t *testing.T) {
	for _, tf := range crudSmokeResources {
		tf := tf
		t.Run(tf, func(t *testing.T) {
			resType := "teams_" + tf
			name := acctest.RandomWithPrefix("tfacc")
			addr := resType + ".test"
			resource.ParallelTest(t, resource.TestCase{
				PreCheck:                 func() { testAccPreCheck(t) },
				ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
				Steps: []resource.TestStep{
					{
						Config: fmt.Sprintf("resource %q \"test\" {\n  identity = %q\n}\n", resType, name),
						Check: resource.ComposeAggregateTestCheckFunc(
							resource.TestCheckResourceAttr(addr, "identity", name),
							resource.TestCheckResourceAttrSet(addr, "id"),
						),
					},
				},
			})
		})
	}
	// Deferred CRUD resources surface as explicit skips so the coverage gap is
	// visible in the run instead of silently absent.
	for _, tf := range crudNeedsFixtureResources {
		tf := tf
		t.Run(tf, func(t *testing.T) {
			t.Skip("needs a purpose-built fixture (not creatable/revertible from a bare identity)")
		})
	}
}
