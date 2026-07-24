package provider

import (
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
)

// TestAccChannelsPolicy_adoptGlobal covers the adopt-on-create semantics for the
// built-in "Global" instance of a CRUD policy: declaring a policy resource with
// identity = "Global" adopts the existing tenant default (Create applies Set, not
// New) instead of failing with "already exists", and Destroy drops it from state
// without removing it (Global can't be deleted). No fields are set, so the adopt is
// a no-op and the tenant is left unchanged.
func TestAccChannelsPolicy_adoptGlobal(t *testing.T) {
	const rn = "teams_channels_policy.global"
	const config = `
resource "teams_channels_policy" "global" {
  identity = "Global"
}
`
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{ // adopt Global (Set no-op) + read back — must not try to create it
				Config: config,
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr(rn, "identity", "Global"),
					resource.TestCheckResourceAttrSet(rn, "id"),
				),
			},
			{ // plan must be empty after adopt (no perpetual diff)
				Config:   config,
				PlanOnly: true,
			},
		},
	})
}
