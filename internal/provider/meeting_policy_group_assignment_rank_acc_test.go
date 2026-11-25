package provider

import (
	"fmt"
	"os"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/acctest"
	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
)

// TestAccMeetingPolicyGroupAssignment_rankOrder covers the rank shift between two
// group assignments of one policy type: group B is the only assignment, at rank 1;
// then one apply adds group A at rank 1 and moves B to rank 2. Teams keeps ranks
// dense, so the result depends on the order Terraform runs the two writes, and
// both orders must converge in that one apply:
//
//   - Move first (A depends on B): B's update to rank 2 runs while B is still the
//     only assignment, so Teams keeps it at rank 1. The update accepts that clamped
//     rank (with a warning) instead of waiting for rank 2, and A's insert at rank 1
//     then shifts B to rank 2.
//   - Insert first (B depends on A): A's insert at rank 1 shifts B to rank 2, and
//     B's update finds rank 2 already in place.
//
// Each order is checked by the post-apply refresh and empty plan of its step. A
// middle step goes back to B alone at rank 1. Destroy unassigns both groups.
//
// Needs TEAMS_ACC_GROUP and TEAMS_ACC_GROUP_2 set to the object ids of two Entra
// groups on the dev tenant, and no other TeamsMeetingPolicy group assignment, so
// that the ranks are exactly 1 and 2.
func TestAccMeetingPolicyGroupAssignment_rankOrder(t *testing.T) {
	groupA, groupB := os.Getenv("TEAMS_ACC_GROUP"), os.Getenv("TEAMS_ACC_GROUP_2")
	if groupA == "" || groupB == "" {
		t.Skip("set TEAMS_ACC_GROUP and TEAMS_ACC_GROUP_2 (object ids of two test groups) to run the rank order test")
	}
	pol := acctest.RandomWithPrefix("tfacc-gasr")
	const (
		rnA = "teams_meeting_policy_group_assignment.a"
		rnB = "teams_meeting_policy_group_assignment.b"
	)
	policy := fmt.Sprintf(`
resource "teams_meeting_policy" "p" {
  identity = %q
}
`, pol)
	// assignment renders one group assignment of the policy; dependsOn is a
	// resource address or empty.
	assignment := func(name, group string, rank int, dependsOn string) string {
		dep := ""
		if dependsOn != "" {
			dep = fmt.Sprintf("\n  depends_on  = [%s]", dependsOn)
		}
		return fmt.Sprintf(`
resource "teams_meeting_policy_group_assignment" %q {
  group_id    = %q
  policy_name = teams_meeting_policy.p.identity
  rank        = %d%s
}
`, name, group, rank, dep)
	}
	onlyB := policy + assignment("b", groupB, 1, "")
	bothChecks := resource.ComposeAggregateTestCheckFunc(
		resource.TestCheckResourceAttr(rnA, "group_id", groupA),
		resource.TestCheckResourceAttr(rnA, "rank", "1"),
		resource.TestCheckResourceAttr(rnB, "group_id", groupB),
		resource.TestCheckResourceAttr(rnB, "rank", "2"),
	)
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: onlyB,
				Check:  resource.TestCheckResourceAttr(rnB, "rank", "1"),
			},
			{
				// Move first: B's update runs before A's insert.
				Config: policy + assignment("a", groupA, 1, rnB) + assignment("b", groupB, 2, ""),
				Check:  bothChecks,
			},
			{
				Config: onlyB,
				Check:  resource.TestCheckResourceAttr(rnB, "rank", "1"),
			},
			{
				// Insert first: A's insert runs before B's update.
				Config: policy + assignment("a", groupA, 1, "") + assignment("b", groupB, 2, rnA),
				Check:  bothChecks,
			},
		},
	})
}
