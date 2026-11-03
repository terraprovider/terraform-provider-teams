package provider

import (
	"context"
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/int64planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/terraprovider/go-msadmin/consistency"
	"github.com/terraprovider/go-teams/cs"
	"github.com/terraprovider/go-teams/teamsapi"
	"github.com/terraprovider/terraform-provider-teams/internal/clients"
	"github.com/terraprovider/tf-msadmin/resourcex"
)

var (
	_ resource.Resource                = &groupPolicyAssignmentResource{}
	_ resource.ResourceWithConfigure   = &groupPolicyAssignmentResource{}
	_ resource.ResourceWithImportState = &groupPolicyAssignmentResource{}

	groupIDPattern = regexp.MustCompile(`(?i)^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)

	groupPolicyAssignmentLocks sync.Map

	// groupPolicyConsistency bounds the polling for asynchronous assignment changes.
	groupPolicyConsistency = consistency.Config{Attempts: 20, Delay: 4 * time.Second}
)

const maxGroupPolicyRank = int64(1<<31 - 1)

var supportedGroupPolicyTypes = map[string]struct{}{
	"ApplicationAccessPolicy":              {},
	"CallingLineIdentity":                  {},
	"ExternalAccessPolicy":                 {},
	"OnlineAudioConferencingRoutingPolicy": {},
	"OnlineVoicemailPolicy":                {},
	"OnlineVoiceRoutingPolicy":             {},
	"TeamsAppSetupPolicy":                  {},
	"TeamsAudioConferencingPolicy":         {},
	"TeamsCallHoldPolicy":                  {},
	"TeamsCallParkPolicy":                  {},
	"TeamsCallingPolicy":                   {},
	"TeamsChannelsPolicy":                  {},
	"TeamsComplianceRecordingPolicy":       {},
	"TeamsCortanaPolicy":                   {},
	"TeamsEmergencyCallingPolicy":          {},
	"TeamsEnhancedEncryptionPolicy":        {},
	"TeamsEventsPolicy":                    {},
	"TeamsFeedbackPolicy":                  {},
	"TeamsFilesPolicy":                     {},
	"TeamsIPPhonePolicy":                   {},
	"TeamsMediaLoggingPolicy":              {},
	"TeamsMeetingBrandingPolicy":           {},
	"TeamsMeetingBroadcastPolicy":          {},
	"TeamsMeetingPolicy":                   {},
	"TeamsMeetingTemplatePermissionPolicy": {},
	"TeamsMessagingPolicy":                 {},
	"TeamsMobilityPolicy":                  {},
	"TeamsRoomVideoTeleConferencingPolicy": {},
	"TeamsSharedCallingRoutingPolicy":      {},
	"TeamsShiftsPolicy":                    {},
	"TeamsUpdateManagementPolicy":          {},
	"TeamsVdiPolicy":                       {},
	"TeamsVideoInteropServicePolicy":       {},
	"TeamsVirtualAppointmentsPolicy":       {},
	"TenantDialPlan":                       {},
}

type groupPolicyAssignmentResource struct {
	client *clients.Client
}

type groupPolicyAssignmentModel struct {
	ID         types.String `tfsdk:"id"`
	GroupID    types.String `tfsdk:"group_id"`
	PolicyType types.String `tfsdk:"policy_type"`
	PolicyName types.String `tfsdk:"policy_name"`
	Rank       types.Int64  `tfsdk:"rank"`
}

type groupPolicyAssignment struct {
	GroupID    string
	PolicyType string
	PolicyName string
	Rank       int64
}

// NewGroupPolicyAssignmentResource manages one Teams policy assignment per group and policy type.
func NewGroupPolicyAssignmentResource() resource.Resource {
	return &groupPolicyAssignmentResource{}
}

func (r *groupPolicyAssignmentResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_group_policy_assignment"
}

func (r *groupPolicyAssignmentResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: fmt.Sprintf(
			"Assigns a Teams policy to a group and manages its rank. Import assignments using `<group_id>/<policy_type>`.\n\n"+
				"Ranks are ordered per policy type and can be renumbered when other group assignments change; coordinate rank values and serialize changes for assignments of the same type.\n\n"+
				"Changes propagate to direct group members asynchronously, and nested groups do not inherit assignments. Direct user assignments take precedence. Groups with hidden membership are not supported, and Teams allows at most 64 group assignments per policy type.\n\n"+
				"Supported policy types: %s.",
			supportedGroupPolicyTypeList(),
		),
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Computed:      true,
				Description:   "Assignment identifier in <group_id>/<policy_type> format.",
				PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"group_id": schema.StringAttribute{
				Required:      true,
				Description:   "Entra group object ID. Changing this forces replacement.",
				PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplace()},
				Validators:    []validator.String{groupIDValidator{}},
			},
			"policy_type": schema.StringAttribute{
				Required:      true,
				Description:   "Teams policy type. Changing this forces replacement. The schema description lists supported values.",
				PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplace()},
				Validators:    []validator.String{groupPolicyTypeValidator{}},
			},
			"policy_name": schema.StringAttribute{
				Required:    true,
				Description: "Name of the Teams policy instance to assign.",
				Validators:  []validator.String{nonEmptyPolicyNameValidator{}},
			},
			"rank": schema.Int64Attribute{
				Required:      true,
				Description:   "Rank among assignments of the same policy type; 1 has highest priority.",
				PlanModifiers: []planmodifier.Int64{int64planmodifier.UseStateForUnknown()},
				Validators:    []validator.Int64{positiveRankValidator{}},
			},
		},
	}
}

func supportedGroupPolicyTypeList() string {
	names := make([]string, 0, len(supportedGroupPolicyTypes))
	for name := range supportedGroupPolicyTypes {
		names = append(names, name)
	}
	sort.Strings(names)
	return strings.Join(names, ", ")
}

func (r *groupPolicyAssignmentResource) Configure(_ context.Context, req resource.ConfigureRequest, _ *resource.ConfigureResponse) {
	if req.ProviderData == nil {
		return
	}
	r.client = req.ProviderData.(*clients.Client)
}

func (r *groupPolicyAssignmentResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan groupPolicyAssignmentModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	lock := r.assignmentLock(plan.PolicyType.ValueString())
	lock.Lock()
	defer lock.Unlock()

	if err := r.grant(ctx, plan); err != nil {
		resp.Diagnostics.AddError("assigning the policy to the group failed", err.Error())
		return
	}
	assignment, ok := r.waitForAssignment(ctx, plan, &resp.Diagnostics)
	if !ok {
		return
	}
	setGroupPolicyAssignmentState(&plan, assignment)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *groupPolicyAssignmentResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state groupPolicyAssignmentModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	lock := r.assignmentLock(state.PolicyType.ValueString())
	lock.Lock()
	defer lock.Unlock()

	assignment, err := r.readAssignment(ctx, state.GroupID.ValueString(), state.PolicyType.ValueString())
	if err != nil {
		if isNotFound(err) {
			resp.State.RemoveResource(ctx)
			return
		}
		resp.Diagnostics.AddError("reading the group policy assignment failed", err.Error())
		return
	}
	if assignment == nil {
		resp.State.RemoveResource(ctx)
		return
	}
	setGroupPolicyAssignmentState(&state, assignment)
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (r *groupPolicyAssignmentResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan groupPolicyAssignmentModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	lock := r.assignmentLock(plan.PolicyType.ValueString())
	lock.Lock()
	defer lock.Unlock()

	if err := r.grant(ctx, plan); err != nil {
		resp.Diagnostics.AddError("updating the group policy assignment failed", err.Error())
		return
	}
	assignment, ok := r.waitForAssignment(ctx, plan, &resp.Diagnostics)
	if !ok {
		return
	}
	setGroupPolicyAssignmentState(&plan, assignment)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *groupPolicyAssignmentResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state groupPolicyAssignmentModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	lock := r.assignmentLock(state.PolicyType.ValueString())
	lock.Lock()
	defer lock.Unlock()

	groupID, policyType := state.GroupID.ValueString(), state.PolicyType.ValueString()
	current, err := r.readAssignment(ctx, groupID, policyType)
	if err != nil {
		resp.Diagnostics.AddError("checking the group policy assignment before removal failed", err.Error())
		return
	}
	if current != nil {
		if err := r.unassign(ctx, groupID, policyType); err != nil {
			resp.Diagnostics.AddError("removing the group policy assignment failed", err.Error())
			return
		}
	}
	_, _, err = resourcex.LoadUntil(ctx, groupPolicyConsistency, func(ctx context.Context) (*groupPolicyAssignment, bool, error) {
		got, readErr := r.readAssignment(ctx, groupID, policyType)
		if readErr != nil {
			return nil, false, readErr
		}
		return got, got != nil, nil
	}, func(got *groupPolicyAssignment) bool { return got == nil })
	if err != nil {
		resp.Diagnostics.AddError("waiting for the group policy assignment to be removed failed", err.Error())
	}
}

func (r *groupPolicyAssignmentResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	groupID, policyType, ok := strings.Cut(req.ID, "/")
	if !ok || groupID == "" || policyType == "" || strings.Contains(policyType, "/") {
		resp.Diagnostics.AddError("invalid import ID", "Expected <group_id>/<policy_type>.")
		return
	}
	if !groupIDPattern.MatchString(groupID) {
		resp.Diagnostics.AddError("invalid import ID", "The group ID must be an Entra object ID (UUID).")
		return
	}
	if _, ok := supportedGroupPolicyTypes[policyType]; !ok {
		resp.Diagnostics.AddError("invalid import ID", fmt.Sprintf("Unsupported Teams policy type %q.", policyType))
		return
	}
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("id"), req.ID)...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("group_id"), groupID)...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("policy_type"), policyType)...)
}

func (r *groupPolicyAssignmentResource) grant(ctx context.Context, model groupPolicyAssignmentModel) error {
	policyType := model.PolicyType.ValueString()
	_, err := r.client.API.Invoke(ctx, teamsapi.Op{
		CmdletName: "Grant-Cs" + policyType,
		Transport:  teamsapi.ConfigAPI,
		Kind:       teamsapi.PolicyGrant,
		PolicyName: policyType,
	}, map[string]any{
		"Group":      model.GroupID.ValueString(),
		"PolicyName": model.PolicyName.ValueString(),
		"Rank":       model.Rank.ValueInt64(),
	})
	return err
}

func (r *groupPolicyAssignmentResource) unassign(ctx context.Context, groupID, policyType string) error {
	_, err := r.client.API.Invoke(ctx, teamsapi.Op{
		CmdletName: "Grant-Cs" + policyType,
		Transport:  teamsapi.ConfigAPI,
		Kind:       teamsapi.PolicyGrant,
		PolicyName: policyType,
	}, map[string]any{
		"Group":      groupID,
		"PolicyName": nil,
	})
	return err
}

func (r *groupPolicyAssignmentResource) readAssignment(ctx context.Context, groupID, policyType string) (*groupPolicyAssignment, error) {
	result, err := r.client.CS.GetCsGroupPolicyAssignment(ctx, cs.GetCsGroupPolicyAssignmentParams{
		GroupId:    groupID,
		PolicyType: policyType,
	})
	if err != nil {
		if isNotFound(err) {
			return nil, nil
		}
		return nil, err
	}
	if result == nil {
		return nil, fmt.Errorf("group policy assignment read returned an empty response")
	}
	return decodeGroupPolicyAssignment(result.Value, groupID, policyType)
}

func decodeGroupPolicyAssignment(rows []map[string]any, groupID, policyType string) (*groupPolicyAssignment, error) {
	var found *groupPolicyAssignment
	for _, row := range rows {
		group := stringField(row, "GroupId", "groupId")
		kind := stringField(row, "PolicyType", "policyType")
		if group == "" || kind == "" {
			return nil, fmt.Errorf("group policy assignment response contained a row without GroupId or PolicyType")
		}
		if !strings.EqualFold(group, groupID) || !strings.EqualFold(kind, policyType) {
			continue
		}
		name := normalizeGroupPolicyName(stringField(row, "PolicyName", "policyName"))
		rank, ok := int64Field(row, "Rank", "rank")
		if name == "" || !ok || rank < 1 {
			return nil, fmt.Errorf("group policy assignment response for %s/%s is missing a valid policy name or rank", groupID, policyType)
		}
		if found != nil {
			return nil, fmt.Errorf("group policy assignment response contained multiple assignments for %s/%s", groupID, policyType)
		}
		found = &groupPolicyAssignment{
			GroupID:    groupID,
			PolicyType: policyType,
			PolicyName: name,
			Rank:       rank,
		}
	}
	return found, nil
}

func (r *groupPolicyAssignmentResource) waitForAssignment(ctx context.Context, plan groupPolicyAssignmentModel, diags *diag.Diagnostics) (*groupPolicyAssignment, bool) {
	groupID, policyType := plan.GroupID.ValueString(), plan.PolicyType.ValueString()
	wantName, wantRank := normalizeGroupPolicyName(plan.PolicyName.ValueString()), plan.Rank.ValueInt64()
	assignment, _, err := resourcex.LoadUntil(ctx, groupPolicyConsistency, func(ctx context.Context) (*groupPolicyAssignment, bool, error) {
		got, readErr := r.readAssignment(ctx, groupID, policyType)
		return got, got != nil, readErr
	}, func(got *groupPolicyAssignment) bool {
		return got != nil && strings.EqualFold(got.PolicyName, wantName) && got.Rank == wantRank
	})
	if err != nil {
		diags.AddError("reading the group policy assignment failed", err.Error())
		return nil, false
	}
	if assignment == nil || !strings.EqualFold(assignment.PolicyName, wantName) || assignment.Rank != wantRank {
		actual := "no assignment"
		if assignment != nil {
			actual = fmt.Sprintf("policy %q at rank %d", assignment.PolicyName, assignment.Rank)
		}
		diags.AddError("group policy assignment did not reach the requested state", fmt.Sprintf("Requested policy %q at rank %d for %s/%s, but Teams reports %s.", wantName, wantRank, groupID, policyType, actual))
		return nil, false
	}
	return assignment, true
}

func (r *groupPolicyAssignmentResource) assignmentLock(policyType string) *sync.Mutex {
	lock, _ := groupPolicyAssignmentLocks.LoadOrStore(r.client.TenantID+"\x00"+policyType, &sync.Mutex{})
	return lock.(*sync.Mutex)
}

func setGroupPolicyAssignmentState(model *groupPolicyAssignmentModel, assignment *groupPolicyAssignment) {
	model.ID = types.StringValue(assignment.GroupID + "/" + assignment.PolicyType)
	model.GroupID = types.StringValue(assignment.GroupID)
	model.PolicyType = types.StringValue(assignment.PolicyType)
	model.PolicyName = types.StringValue(assignment.PolicyName)
	model.Rank = types.Int64Value(assignment.Rank)
}

func normalizeGroupPolicyName(name string) string {
	if len(name) >= len("Tag:") && strings.EqualFold(name[:len("Tag:")], "Tag:") {
		return name[len("Tag:"):]
	}
	return name
}

func stringField(row map[string]any, keys ...string) string {
	for _, key := range keys {
		if value, ok := rowField(row, key).(string); ok {
			return value
		}
	}
	return ""
}

func rowField(row map[string]any, key string) any {
	if value, ok := row[key]; ok {
		return value
	}
	for candidate, value := range row {
		if strings.EqualFold(candidate, key) {
			return value
		}
	}
	return nil
}

func int64Field(row map[string]any, keys ...string) (int64, bool) {
	for _, key := range keys {
		switch value := rowField(row, key).(type) {
		case int:
			return int64(value), true
		case int64:
			return value, true
		case float64:
			return int64(value), value == float64(int64(value))
		case string:
			parsed, err := strconv.ParseInt(value, 10, 64)
			return parsed, err == nil
		}
	}
	return 0, false
}

type groupIDValidator struct{}

func (groupIDValidator) Description(context.Context) string {
	return "value must be an Entra group object ID (UUID)"
}

func (v groupIDValidator) MarkdownDescription(ctx context.Context) string {
	return v.Description(ctx)
}

func (groupIDValidator) ValidateString(_ context.Context, req validator.StringRequest, resp *validator.StringResponse) {
	if req.ConfigValue.IsNull() || req.ConfigValue.IsUnknown() {
		return
	}
	if !groupIDPattern.MatchString(req.ConfigValue.ValueString()) {
		resp.Diagnostics.AddAttributeError(req.Path, "Invalid group ID", "The group_id must be an Entra object ID in UUID format.")
	}
}

type groupPolicyTypeValidator struct{}

func (groupPolicyTypeValidator) Description(context.Context) string {
	return "value must be a supported Teams policy type that allows group assignment"
}

func (v groupPolicyTypeValidator) MarkdownDescription(ctx context.Context) string {
	return v.Description(ctx)
}

func (groupPolicyTypeValidator) ValidateString(_ context.Context, req validator.StringRequest, resp *validator.StringResponse) {
	if req.ConfigValue.IsNull() || req.ConfigValue.IsUnknown() {
		return
	}
	if _, ok := supportedGroupPolicyTypes[req.ConfigValue.ValueString()]; !ok {
		resp.Diagnostics.AddAttributeError(req.Path, "Unsupported policy type", fmt.Sprintf("The policy type %q is not supported for group assignment.", req.ConfigValue.ValueString()))
	}
}

type nonEmptyPolicyNameValidator struct{}

func (nonEmptyPolicyNameValidator) Description(context.Context) string {
	return "value must not be empty or whitespace"
}

func (v nonEmptyPolicyNameValidator) MarkdownDescription(ctx context.Context) string {
	return v.Description(ctx)
}

func (nonEmptyPolicyNameValidator) ValidateString(_ context.Context, req validator.StringRequest, resp *validator.StringResponse) {
	if req.ConfigValue.IsNull() || req.ConfigValue.IsUnknown() {
		return
	}
	if strings.TrimSpace(req.ConfigValue.ValueString()) == "" {
		resp.Diagnostics.AddAttributeError(req.Path, "Invalid policy name", "The policy_name must not be empty or whitespace.")
	}
}

type positiveRankValidator struct{}

func (positiveRankValidator) Description(context.Context) string {
	return fmt.Sprintf("value must be between 1 and %d", maxGroupPolicyRank)
}

func (v positiveRankValidator) MarkdownDescription(ctx context.Context) string {
	return v.Description(ctx)
}

func (positiveRankValidator) ValidateInt64(_ context.Context, req validator.Int64Request, resp *validator.Int64Response) {
	if req.ConfigValue.IsNull() || req.ConfigValue.IsUnknown() {
		return
	}
	if rank := req.ConfigValue.ValueInt64(); rank < 1 || rank > maxGroupPolicyRank {
		resp.Diagnostics.AddAttributeError(req.Path, "Invalid rank", fmt.Sprintf("The rank must be between 1 and %d.", maxGroupPolicyRank))
	}
}
