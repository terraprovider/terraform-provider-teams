package provider

import (
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
)

// TestAccUpgradePolicyDataSource_readOnly exercises a generated read-only (Get-only)
// data source: teams_upgrade_policy is a RawJSON, schema-less data source for a noun
// (Get-CsTeamsUpgradePolicy) that has no New/Set/Remove, so there is no managed
// resource — only a Get. It looks up one object by identity and exposes it as
// id/identity/name/display_name plus a `json` attribute holding the whole object.
// Every tenant has the built-in "Global" upgrade policy, so the lookup returns data.
func TestAccUpgradePolicyDataSource_readOnly(t *testing.T) {
	const rn = "data.teams_upgrade_policy.global"
	resource.ParallelTest(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: `data "teams_upgrade_policy" "global" { identity = "Global" }`,
				Check: resource.ComposeAggregateTestCheckFunc(
					// the object was found and mapped: id + the raw json blob are populated
					resource.TestCheckResourceAttrSet(rn, "id"),
					resource.TestCheckResourceAttrSet(rn, "json"),
					resource.TestCheckResourceAttrSet(rn, "identity"),
				),
			},
		},
	})
}

// TestAccUpgradePoliciesDataSource_readOnlyList exercises the companion plural
// (list) data source generated alongside the read-only singular: teams_upgrade_policies
// reads every upgrade policy (Get-CsTeamsUpgradePolicy with no key) into a list of
// RawJSON elements. Every tenant has at least the built-in "Global" policy, so the
// list is non-empty and each element carries its id + raw json.
func TestAccUpgradePoliciesDataSource_readOnlyList(t *testing.T) {
	const rn = "data.teams_upgrade_policies.all"
	resource.ParallelTest(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: `data "teams_upgrade_policies" "all" {}`,
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttrSet(rn, "upgrade_policies.#"),
					resource.TestCheckResourceAttrSet(rn, "upgrade_policies.0.id"),
					resource.TestCheckResourceAttrSet(rn, "upgrade_policies.0.identity"),
					resource.TestCheckResourceAttrSet(rn, "upgrade_policies.0.json"),
				),
			},
		},
	})
}
