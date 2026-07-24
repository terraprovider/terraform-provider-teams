package provider

import (
	"fmt"
	"os"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/acctest"
	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
)

// TestAccMeetingPolicyAssignment_basic exercises the async per-user policy
// assignment: it creates a throwaway meeting policy, assigns it to a test user
// (Grant → poll effectivePolicyAssignments until it settles), reads it back, then
// destroys — which unassigns (PolicyName="") and removes the policy.
//
// Needs TEAMS_ACC_USER set to a real user on the dev tenant (objectId or UPN); the
// grant is a no-op-safe change that is reverted on destroy.
func TestAccMeetingPolicyAssignment_basic(t *testing.T) {
	user := os.Getenv("TEAMS_ACC_USER")
	if user == "" {
		t.Skip("set TEAMS_ACC_USER (objectId or UPN of a test user) to run the assignment test")
	}
	pol := acctest.RandomWithPrefix("tfacc-asg")
	const rn = "teams_meeting_policy_assignment.test"
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: fmt.Sprintf(`
resource "teams_meeting_policy" "p" {
  identity = %[1]q
}

resource "teams_meeting_policy_assignment" "test" {
  user        = %[2]q
  policy_name = teams_meeting_policy.p.identity
}
`, pol, user),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr(rn, "user", user),
					resource.TestCheckResourceAttr(rn, "policy_name", pol),
					resource.TestCheckResourceAttrSet(rn, "id"),
				),
			},
		},
	})
}
