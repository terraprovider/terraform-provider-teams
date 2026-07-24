package provider

import (
	"fmt"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/acctest"
	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
)

// crudFixtureCases are CRUD resources that need specific create inputs beyond a
// bare identity but are still cleanly reverted (a custom instance removed on
// destroy). Each config is interpolated with a unique random name (%[1]s) and run
// as an independent parallel create→destroy test.
var crudFixtureCases = []struct {
	tf     string
	config string // full resource block; %[1]s = random name
}{
	{"translation_rule", `resource "teams_translation_rule" "test" {
  identity    = "tfacc%[1]s"
  pattern     = "^\\+?1?(\\d{10})$"
  translation = "+1$1"
}`},
	{"inbound_blocked_number_pattern", `resource "teams_inbound_blocked_number_pattern" "test" {
  identity    = "tfacc%[1]s"
  pattern     = "^\\+?1900\\d{7}$"
  enabled     = true
  description = "acc test"
}`},
	{"inbound_exempt_number_pattern", `resource "teams_inbound_exempt_number_pattern" "test" {
  identity = "tfacc%[1]s"
  pattern  = "^\\+?1800\\d{7}$"
  enabled  = true
}`},
	{"tenant_network_region", `resource "teams_tenant_network_region" "test" {
  identity          = "tfacc%[1]s"
  network_region_id = "tfacc%[1]s"
  description       = "acc test"
}`},
	{"survivable_branch_appliance", `resource "teams_survivable_branch_appliance" "test" {
  identity = "tfacc%[1]s.acc.example.com"
  fqdn     = "tfacc%[1]s.acc.example.com"
}`},
	{"online_voice_route", `resource "teams_online_voice_route" "test" {
  identity       = "tfacc%[1]s"
  number_pattern = ".*"
  description    = "acc test"
}`},
	{"video_interop_service_provider", `resource "teams_video_interop_service_provider" "test" {
  identity   = "tfacc%[1]s"
  name       = "tfacc%[1]s"
  tenant_key = "tfacc%[1]s"
}`},
}

// TestAccCRUDFixtures create→destroys each fixtured CRUD resource, in parallel.
func TestAccCRUDFixtures(t *testing.T) {
	for _, c := range crudFixtureCases {
		c := c
		t.Run(c.tf, func(t *testing.T) {
			rand := acctest.RandStringFromCharSet(8, acctest.CharSetAlpha)
			addr := "teams_" + c.tf + ".test"
			resource.ParallelTest(t, resource.TestCase{
				PreCheck:                 func() { testAccPreCheck(t) },
				ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
				Steps: []resource.TestStep{
					{
						Config: fmt.Sprintf(c.config, rand),
						Check:  resource.TestCheckResourceAttrSet(addr, "id"),
					},
				},
			})
		})
	}
}
