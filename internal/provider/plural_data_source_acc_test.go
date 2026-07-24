package provider

import (
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
)

// TestAccMeetingPoliciesDataSource_list exercises a generated plural (list) data
// source: teams_meeting_policies reads every meeting policy (Get-CsTeamsMeetingPolicy
// with no key) into a list. Every tenant has at least the built-in "Global" policy,
// so the list is non-empty.
func TestAccMeetingPoliciesDataSource_list(t *testing.T) {
	const rn = "data.teams_meeting_policies.all"
	resource.ParallelTest(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: `data "teams_meeting_policies" "all" {}`,
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttrSet(rn, "meeting_policies.#"),
					// elements are populated, including their identity (read-back)
					resource.TestCheckResourceAttrSet(rn, "meeting_policies.0.identity"),
					resource.TestCheckResourceAttrSet(rn, "meeting_policies.0.id"),
				),
			},
		},
	})
}
