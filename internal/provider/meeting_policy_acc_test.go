package provider

import (
	"fmt"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/acctest"
	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/terraform"
)

// TestAccMeetingPolicy_basic exercises the full lifecycle of a teams_meeting_policy
// (an IdentityIsName CRUD resource) against a live tenant: create a custom named
// policy, read it back through the data source, import by identity, and update a
// tri-state bool in place. The framework destroys the policy at the end. Run with
// TF_ACC=1 and ARM_* credentials pointing at a disposable dev tenant.
func TestAccMeetingPolicy_basic(t *testing.T) {
	name := acctest.RandomWithPrefix("tf-acc-mp")
	const rn = "teams_meeting_policy.test"

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{ // create + data-source read-back
				Config: testAccMeetingPolicyConfig(name, false),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr(rn, "identity", name),
					resource.TestCheckResourceAttrSet(rn, "id"),
					resource.TestCheckResourceAttr(rn, "allow_meet_now", "false"),
					resource.TestCheckResourceAttr(rn, "description", "acceptance test"),
					resource.TestCheckResourceAttr("data.teams_meeting_policy.by_identity", "allow_meet_now", "false"),
				),
			},
			{ // import by identity (the policy name)
				ResourceName:      rn,
				ImportState:       true,
				ImportStateVerify: true,
				ImportStateIdFunc: func(*terraform.State) (string, error) { return name, nil },
			},
			{ // in-place update of a tri-state bool
				Config: testAccMeetingPolicyConfig(name, true),
				Check:  resource.TestCheckResourceAttr(rn, "allow_meet_now", "true"),
			},
		},
	})
}

func testAccMeetingPolicyConfig(name string, allowMeetNow bool) string {
	return fmt.Sprintf(`
resource "teams_meeting_policy" "test" {
  identity                         = %[1]q
  description                      = "acceptance test"
  allow_meet_now                   = %[2]t
  allow_channel_meeting_scheduling = false
}

data "teams_meeting_policy" "by_identity" {
  identity = teams_meeting_policy.test.identity
}
`, name, allowMeetNow)
}
