package provider

import (
	"context"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

func TestGroupPolicyAssignmentSchema(t *testing.T) {
	var resp resource.SchemaResponse
	NewGroupPolicyAssignmentResource().Schema(context.Background(), resource.SchemaRequest{}, &resp)

	for _, name := range []string{"group_id", "policy_type", "policy_name", "rank"} {
		if !resp.Schema.Attributes[name].IsRequired() {
			t.Errorf("%s must be required", name)
		}
	}
	if !resp.Schema.Attributes["id"].IsComputed() {
		t.Error("id must be computed")
	}
	if _, ok := resp.Schema.Attributes["rank"].(schema.Int64Attribute); !ok {
		t.Fatal("rank must use the Terraform integer type")
	}
}

func TestGroupPolicyAssignmentValidators(t *testing.T) {
	ctx := context.Background()
	stringCases := []struct {
		name      string
		validator validator.String
		value     string
		wantError bool
	}{
		{"valid group id", groupIDValidator{}, "d8ebfa45-0f28-4d2d-9bcc-b158a49e2d17", false},
		{"invalid group id", groupIDValidator{}, "not-a-guid", true},
		{"supported policy type", groupPolicyTypeValidator{}, "TeamsMeetingPolicy", false},
		{"unsupported policy type", groupPolicyTypeValidator{}, "TeamsUpgradePolicy", true},
		{"non-empty policy name", nonEmptyPolicyNameValidator{}, "RetailManagers", false},
		{"empty policy name", nonEmptyPolicyNameValidator{}, "   ", true},
	}
	for _, tc := range stringCases {
		t.Run(tc.name, func(t *testing.T) {
			var resp validator.StringResponse
			tc.validator.ValidateString(ctx, validator.StringRequest{ConfigValue: types.StringValue(tc.value)}, &resp)
			if got := resp.Diagnostics.HasError(); got != tc.wantError {
				t.Fatalf("diagnostics.HasError() = %t, want %t", got, tc.wantError)
			}
		})
	}

	rankCases := []struct {
		name      string
		value     int64
		wantError bool
	}{
		{"first rank", 1, false},
		{"positive rank", 4, false},
		{"maximum rank", maxGroupPolicyRank, false},
		{"zero rank", 0, true},
		{"negative rank", -1, true},
		{"rank overflow", maxGroupPolicyRank + 1, true},
	}
	for _, tc := range rankCases {
		t.Run(tc.name, func(t *testing.T) {
			var resp validator.Int64Response
			positiveRankValidator{}.ValidateInt64(ctx, validator.Int64Request{ConfigValue: types.Int64Value(tc.value)}, &resp)
			if got := resp.Diagnostics.HasError(); got != tc.wantError {
				t.Fatalf("diagnostics.HasError() = %t, want %t", got, tc.wantError)
			}
		})
	}
}

func TestNormalizeGroupPolicyName(t *testing.T) {
	for _, tc := range []struct {
		in, want string
	}{
		{"RetailManagers", "RetailManagers"},
		{"Tag:RetailManagers", "RetailManagers"},
		{"tag:RetailManagers", "RetailManagers"},
	} {
		if got := normalizeGroupPolicyName(tc.in); got != tc.want {
			t.Errorf("normalizeGroupPolicyName(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestDecodeGroupPolicyAssignment(t *testing.T) {
	const groupID = "d8ebfa45-0f28-4d2d-9bcc-b158a49e2d17"
	rows := []map[string]any{
		{"GroupId": "other", "PolicyType": "TeamsMeetingPolicy", "PolicyName": "Other", "Rank": float64(1)},
		{"groupId": strings.ToUpper(groupID), "policyType": "teamsmeetingpolicy", "policyName": "Tag:RetailManagers", "rank": float64(2)},
	}
	got, err := decodeGroupPolicyAssignment(rows, groupID, "TeamsMeetingPolicy")
	if err != nil {
		t.Fatalf("decodeGroupPolicyAssignment: %v", err)
	}
	if got.GroupID != groupID || got.PolicyType != "TeamsMeetingPolicy" || got.PolicyName != "RetailManagers" || got.Rank != 2 {
		t.Fatalf("decoded assignment = %+v", got)
	}
}

func TestDecodeGroupPolicyAssignmentErrors(t *testing.T) {
	const groupID = "d8ebfa45-0f28-4d2d-9bcc-b158a49e2d17"
	cases := []struct {
		name string
		rows []map[string]any
		want string
	}{
		{"missing assignment", nil, ""},
		{"malformed row", []map[string]any{{}}, "without GroupId or PolicyType"},
		{"missing rank", []map[string]any{{"GroupId": groupID, "PolicyType": "TeamsMeetingPolicy", "PolicyName": "Kiosk"}}, "valid policy name or rank"},
		{"invalid rank", []map[string]any{{"GroupId": groupID, "PolicyType": "TeamsMeetingPolicy", "PolicyName": "Kiosk", "Rank": float64(1.5)}}, "valid policy name or rank"},
		{"duplicate assignment", []map[string]any{
			{"GroupId": groupID, "PolicyType": "TeamsMeetingPolicy", "PolicyName": "First", "Rank": float64(1)},
			{"GroupId": groupID, "PolicyType": "TeamsMeetingPolicy", "PolicyName": "Second", "Rank": float64(2)},
		}, "multiple assignments"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := decodeGroupPolicyAssignment(tc.rows, groupID, "TeamsMeetingPolicy")
			if tc.want == "" {
				if err != nil || got != nil {
					t.Fatalf("got (%+v, %v), want (nil, nil)", got, err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("error = %v, want substring %q", err, tc.want)
			}
		})
	}
}

func TestGroupPolicyAssignmentRegistered(t *testing.T) {
	p := &teamsProvider{}
	found := false
	for _, newResource := range p.Resources(context.Background()) {
		r := newResource()
		var metadata resource.MetadataResponse
		r.Metadata(context.Background(), resource.MetadataRequest{ProviderTypeName: "teams"}, &metadata)
		if metadata.TypeName == "teams_group_policy_assignment" {
			found = true
			break
		}
	}
	if !found {
		t.Fatal("teams_group_policy_assignment is not registered")
	}
}
