package provider

import (
	"fmt"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/acctest"
	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
)

// TestAccTenantTrustedIPAddress_basic covers a CRUD resource unblocked by TypeInt
// support: New-CsTenantTrustedIPAddress requires MaskBits (int / *int64), which the
// generator previously dropped. The test asserts the int round-trips.
//
// online_pstn_gateway and tenant_network_subnet also became creatable (their int
// params — SipSignalingPort, MaskBits — are now sent), but they need tenant fixtures
// beyond the provider (a verified SIP domain; a network site) so they stay deferred.
func TestAccTenantTrustedIPAddress_basic(t *testing.T) {
	const rn = "teams_tenant_trusted_ip_address.test"
	ip := fmt.Sprintf("203.0.113.%d", acctest.RandIntRange(1, 254))
	resource.ParallelTest(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: fmt.Sprintf(`resource "teams_tenant_trusted_ip_address" "test" {
  identity   = %[1]q
  ip_address = %[1]q
  mask_bits  = 32
}`, ip),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr(rn, "mask_bits", "32"),
					resource.TestCheckResourceAttrSet(rn, "id"),
				),
			},
		},
	})
}
