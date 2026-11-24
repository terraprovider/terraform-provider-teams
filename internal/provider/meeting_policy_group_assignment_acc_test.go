package provider

import (
	"fmt"
	"os"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/acctest"
	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
)

// TestAccMeetingPolicyGroupAssignment_basic exercises a group policy assignment:
// it creates two throwaway meeting policies, assigns the first to a test group at
// rank 1 (Grant -Group, then poll the group's assignment until it settles),
// switches the assignment to the second policy in place, imports it by group id,
// then destroys, which unassigns the group and removes both policies.
//
// Needs TEAMS_ACC_GROUP set to the object id of an Entra group on the dev tenant
// that has no TeamsMeetingPolicy group assignment and is the only group with one
// (rank 1 must be free, or the assignment shifts others). Destroy reverts it.
func TestAccMeetingPolicyGroupAssignment_basic(t *testing.T) {
	group := os.Getenv("TEAMS_ACC_GROUP")
	if group == "" {
		t.Skip("set TEAMS_ACC_GROUP (object id of a test group) to run the group assignment test")
	}
	pol := acctest.RandomWithPrefix("tfacc-gasg")
	const rn = "teams_meeting_policy_group_assignment.test"
	cfg := func(assigned string) string {
		return fmt.Sprintf(`
resource "teams_meeting_policy" "a" {
  identity = "%[1]s-a"
}

resource "teams_meeting_policy" "b" {
  identity = "%[1]s-b"

  # Teams answers concurrent creates of one policy type with 409 "create conflict".
  depends_on = [teams_meeting_policy.a]
}

resource "teams_meeting_policy_group_assignment" "test" {
  group_id    = %[2]q
  policy_name = teams_meeting_policy.%[3]s.identity
  rank        = 1
}
`, pol, group, assigned)
	}
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: cfg("a"),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr(rn, "group_id", group),
					resource.TestCheckResourceAttr(rn, "policy_name", pol+"-a"),
					resource.TestCheckResourceAttr(rn, "rank", "1"),
					resource.TestCheckResourceAttr(rn, "id", group),
				),
			},
			{
				Config: cfg("b"),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr(rn, "policy_name", pol+"-b"),
					resource.TestCheckResourceAttr(rn, "rank", "1"),
				),
			},
			{
				ResourceName:      rn,
				ImportState:       true,
				ImportStateId:     group,
				ImportStateVerify: true,
			},
		},
	})
}
