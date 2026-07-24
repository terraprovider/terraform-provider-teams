package provider

import (
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
)

// TestAccTenantFederationConfiguration_adopt covers the config-singleton lifecycle:
// the org-wide TenantFederationConfiguration (Identity "Global") is adopted (Create
// applies Set with no changes), read back, and imported. Destroy is a no-op (the
// config persists), so the run leaves tenant state untouched — no setting is
// modified, only read.
func TestAccTenantFederationConfiguration_adopt(t *testing.T) {
	const rn = "teams_tenant_federation_configuration.test"
	const config = `
resource "teams_tenant_federation_configuration" "test" {
  identity = "Global"
}
`
	resource.ParallelTest(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{ // adopt (Set is a no-op) + read back
				Config: config,
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr(rn, "identity", "Global"),
					resource.TestCheckResourceAttrSet(rn, "id"),
					// a stable bool setting is populated from the read-back
					resource.TestCheckResourceAttrSet(rn, "allow_teams_consumer_inbound"),
				),
			},
			{ // import by identity
				ResourceName:      rn,
				ImportState:       true,
				ImportStateId:     "Global",
				ImportStateVerify: true,
			},
		},
	})
}
