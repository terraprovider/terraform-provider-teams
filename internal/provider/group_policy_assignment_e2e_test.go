package provider

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/hashicorp/terraform-plugin-framework/provider"
	"github.com/hashicorp/terraform-plugin-framework/providerserver"
	"github.com/hashicorp/terraform-plugin-go/tfprotov6"
	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/terraform"

	"github.com/terraprovider/go-msadmin/consistency"
	"github.com/terraprovider/go-teams/cs"
	"github.com/terraprovider/go-teams/teamsapi"
	"github.com/terraprovider/terraform-provider-teams/internal/clients"
)

const (
	fakeAssignmentsPath = "/Skype.Policy/groupPolicyAssignments/"
	fakeGroupA          = "6b0a7d1e-0000-4000-8000-00000000000a"
	fakeGroupB          = "6b0a7d1e-0000-4000-8000-00000000000b"
	fakePolicyType      = "TeamsMeetingPolicy"
)

// TestGroupPolicyAssignment_FakeServiceLifecycle drives the resource through the
// real Terraform/OpenTofu CLI against an in-process fake of the Teams group
// assignment API, so it needs no tenant. It covers create, in-place update, rank
// drift caused by another group, import, a rank the service clamps, external
// deletion and destroy, with asynchronous propagation simulated on every write.
//
// It is opt-in because it needs a CLI binary:
//
//	TF_ACC_TERRAFORM_PATH=tofu TF_ACC_PROVIDER_HOST=registry.opentofu.org \
//	  go test -run TestGroupPolicyAssignment_FakeServiceLifecycle ./internal/provider/
//
// With Terraform, set only TF_ACC_TERRAFORM_PATH.
func TestGroupPolicyAssignment_FakeServiceLifecycle(t *testing.T) {
	cli := os.Getenv("TF_ACC_TERRAFORM_PATH")
	if cli == "" {
		t.Skip("set TF_ACC_TERRAFORM_PATH (and TF_ACC_PROVIDER_HOST=registry.opentofu.org for OpenTofu) to run the fake-service lifecycle test")
	}
	// The testing framework requires an absolute path; accept a bare name such as "tofu".
	if !filepath.IsAbs(cli) {
		resolved, err := exec.LookPath(cli)
		if err != nil {
			t.Fatalf("TF_ACC_TERRAFORM_PATH=%q: %v", cli, err)
		}
		if resolved, err = filepath.Abs(resolved); err != nil {
			t.Fatalf("TF_ACC_TERRAFORM_PATH=%q: %v", cli, err)
		}
		t.Setenv("TF_ACC_TERRAFORM_PATH", resolved)
	}

	original := groupPolicyConsistency
	groupPolicyConsistency = consistency.Config{Attempts: 10, Delay: time.Millisecond}
	t.Cleanup(func() { groupPolicyConsistency = original })

	const rn = "teams_group_policy_assignment.test"
	fake := newFakeTeamsService(2)
	client := fakeTeamsClient(t, fake)
	otherPolicy := "Other"

	assigned := func(name, rank string, order ...string) resource.TestCheckFunc {
		return resource.ComposeAggregateTestCheckFunc(
			resource.TestCheckResourceAttr(rn, "id", fakeGroupA+"/"+fakePolicyType),
			resource.TestCheckResourceAttr(rn, "policy_name", name),
			resource.TestCheckResourceAttr(rn, "rank", rank),
			fake.checkOrder(fakePolicyType, order...),
		)
	}

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: map[string]func() (tfprotov6.ProviderServer, error){
			"teams": providerserver.NewProtocol6WithError(&fakeClientProvider{teamsProvider: &teamsProvider{version: "test"}, client: client}),
		},
		CheckDestroy: func(*terraform.State) error {
			for _, entry := range fake.order(fakePolicyType) {
				if strings.HasPrefix(entry, fakeGroupA+"=") {
					return fmt.Errorf("the assignment survived destroy: %s", entry)
				}
			}
			return nil
		},
		Steps: []resource.TestStep{
			{
				Config: groupAssignmentConfig(fakeGroupA, fakePolicyType, "KioskA", 1),
				Check:  assigned("KioskA", "1", fakeGroupA+"=KioskA"),
			},
			{
				Config: groupAssignmentConfig(fakeGroupA, fakePolicyType, "KioskB", 1),
				Check:  assigned("KioskB", "1", fakeGroupA+"=KioskB"),
			},
			{
				// Another group taking rank 1 pushes this assignment to rank 2; the
				// refresh reports the drift and the apply restores rank 1.
				PreConfig: func() { fake.external(fakeGroupB, fakePolicyType, &otherPolicy, 1) },
				Config:    groupAssignmentConfig(fakeGroupA, fakePolicyType, "KioskB", 1),
				Check:     assigned("KioskB", "1", fakeGroupA+"=KioskB", fakeGroupB+"=Other"),
			},
			{
				ResourceName:      rn,
				ImportState:       true,
				ImportStateId:     fakeGroupA + "/" + fakePolicyType,
				ImportStateVerify: true,
			},
			{
				// The service clamps ranks beyond the end, so the requested rank is
				// never reached and the provider must say so instead of recording it.
				Config:      groupAssignmentConfig(fakeGroupA, fakePolicyType, "KioskB", 5),
				ExpectError: regexp.MustCompile(`did not reach the requested state`),
			},
			{
				Config: groupAssignmentConfig(fakeGroupA, fakePolicyType, "KioskB", 1),
				Check:  assigned("KioskB", "1", fakeGroupA+"=KioskB", fakeGroupB+"=Other"),
			},
			{
				PreConfig: func() { fake.external(fakeGroupA, fakePolicyType, nil, 0) },
				Config:    groupAssignmentConfig(fakeGroupA, fakePolicyType, "KioskB", 1),
				Check:     assigned("KioskB", "1", fakeGroupA+"=KioskB", fakeGroupB+"=Other"),
			},
		},
	})

	// Guard against a vacuous pass: reads must have been served stale (so the
	// provider really polled), and every kind of write must have been sent.
	staleReads, grants, unassigns := fake.counts()
	if staleReads == 0 || grants < 5 || unassigns == 0 {
		t.Errorf("the lifecycle did not exercise the service as intended: staleReads=%d grants=%d unassigns=%d", staleReads, grants, unassigns)
	}
}

func groupAssignmentConfig(group, policyType, policyName string, rank int) string {
	return fmt.Sprintf(`
resource "teams_group_policy_assignment" "test" {
  group_id    = %q
  policy_type = %q
  policy_name = %q
  rank        = %d
}
`, group, policyType, policyName, rank)
}

// fakeClientProvider is the real provider with Configure replaced so that it
// hands out a client for the fake service instead of authenticating.
type fakeClientProvider struct {
	*teamsProvider
	client *clients.Client
}

func (p *fakeClientProvider) Configure(_ context.Context, _ provider.ConfigureRequest, resp *provider.ConfigureResponse) {
	resp.ResourceData = p.client
	resp.DataSourceData = p.client
}

func fakeTeamsClient(t *testing.T, handler http.Handler) *clients.Client {
	t.Helper()
	srv := httptest.NewTLSServer(handler)
	t.Cleanup(srv.Close)
	api, err := teamsapi.New(teamsapi.Options{
		Environment: teamsapi.Environment{Name: "Fake", Endpoint: strings.TrimPrefix(srv.URL, "https://"), Resource: "fake"},
		TenantID:    "fake-tenant",
		Tokens:      teamsapi.StaticTokenProvider("token"),
		HTTPClient:  srv.Client(),
	})
	if err != nil {
		t.Fatalf("teamsapi.New: %v", err)
	}
	return &clients.Client{API: api, CS: cs.New(api), TenantID: "fake-tenant"}
}

type fakeAssignment struct{ group, name string }

// fakeTeamsService models the group assignment API: one assignment per group and
// policy type, ranks that are positions in a per-type list, a rank beyond the end
// clamped to the end, and writes that only become visible to reads after a lag.
type fakeTeamsService struct {
	mu      sync.Mutex
	live    map[string][]fakeAssignment
	visible map[string][]fakeAssignment
	lag     int
	stale   int

	staleReads, grants, unassigns int
}

func newFakeTeamsService(lag int) *fakeTeamsService {
	return &fakeTeamsService{live: map[string][]fakeAssignment{}, lag: lag}
}

func (f *fakeTeamsService) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	switch {
	case r.URL.Path == "/Teams.Tenant/serviceDiscovery":
		_, _ = io.WriteString(w, `{"Endpoints":{"AdminServiceEndpoint":"admin.example"},"Headers":{}}`)
	case r.Method == http.MethodGet && r.URL.Path == fakeAssignmentsPath:
		f.serveList(w)
	case r.Method == http.MethodPatch && strings.HasPrefix(r.URL.Path, fakeAssignmentsPath):
		f.serveGrant(w, r)
	default:
		fakeError(w, http.StatusNotFound, "unexpected request "+r.Method+" "+r.URL.Path)
	}
}

func fakeError(w http.ResponseWriter, status int, message string) {
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]any{"error": map[string]any{"code": http.StatusText(status), "message": message}})
}

func (f *fakeTeamsService) serveList(w http.ResponseWriter) {
	f.mu.Lock()
	defer f.mu.Unlock()
	source := f.live
	if f.stale > 0 {
		f.stale--
		f.staleReads++
		source = f.visible
	}
	types := make([]string, 0, len(source))
	for policyType := range source {
		types = append(types, policyType)
	}
	sort.Strings(types)
	rows := []map[string]any{}
	for _, policyType := range types {
		for i, a := range source[policyType] {
			rows = append(rows, map[string]any{
				"GroupId":     a.group,
				"PolicyType":  policyType,
				"PolicyName":  "Tag:" + a.name,
				"Rank":        i + 1,
				"CreatedTime": "2026-01-01T00:00:00Z",
				"CreatedBy":   "fake",
			})
		}
	}
	_ = json.NewEncoder(w).Encode(rows)
}

// serveGrant accepts only the two bodies the provider is meant to send, so a
// change in the wire format fails the test instead of being tolerated.
func (f *fakeTeamsService) serveGrant(w http.ResponseWriter, r *http.Request) {
	parts := strings.Split(strings.TrimPrefix(r.URL.Path, fakeAssignmentsPath), "/")
	if len(parts) != 3 || parts[0] == "" || parts[1] != "policyTypes" || parts[2] == "" {
		fakeError(w, http.StatusNotFound, "unexpected grant route "+r.URL.Path)
		return
	}
	group, policyType := parts[0], parts[2]
	var body map[string]json.RawMessage
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		fakeError(w, http.StatusBadRequest, "invalid body: "+err.Error())
		return
	}
	rawName, ok := body["PolicyName"]
	if !ok {
		fakeError(w, http.StatusBadRequest, "PolicyName is required")
		return
	}

	f.mu.Lock()
	defer f.mu.Unlock()
	if string(rawName) == "null" {
		if len(body) != 1 {
			fakeError(w, http.StatusBadRequest, "an unassign carries only PolicyName")
			return
		}
		f.beginWrite()
		f.unassigns++
		f.set(group, policyType, nil, 0)
		w.WriteHeader(http.StatusAccepted)
		return
	}
	var name string
	var rank int
	rawRank, hasRank := body["Rank"]
	switch {
	case json.Unmarshal(rawName, &name) != nil || name == "":
		fakeError(w, http.StatusBadRequest, "PolicyName must be a non-empty string or null")
	case len(body) != 2 || !hasRank || json.Unmarshal(rawRank, &rank) != nil || rank < 1:
		fakeError(w, http.StatusBadRequest, "a grant carries PolicyName and a positive integer Rank")
	default:
		f.beginWrite()
		f.grants++
		f.set(group, policyType, &name, rank)
		w.WriteHeader(http.StatusAccepted)
	}
}

// beginWrite freezes what reads return for the next lag reads. The caller holds mu.
func (f *fakeTeamsService) beginWrite() {
	if f.lag > 0 {
		f.visible = map[string][]fakeAssignment{}
		for policyType, list := range f.live {
			f.visible[policyType] = slices.Clone(list)
		}
		f.stale = f.lag
	}
}

// set replaces the group's assignment of the type (nil name removes it). The
// caller holds mu.
func (f *fakeTeamsService) set(group, policyType string, name *string, rank int) {
	kept := make([]fakeAssignment, 0, len(f.live[policyType])+1)
	for _, a := range f.live[policyType] {
		if !strings.EqualFold(a.group, group) {
			kept = append(kept, a)
		}
	}
	if name != nil {
		kept = slices.Insert(kept, min(rank, len(kept)+1)-1, fakeAssignment{group: group, name: *name})
	}
	f.live[policyType] = kept
}

// external changes the service the way another administrator would: immediately
// visible and not through the provider.
func (f *fakeTeamsService) external(group, policyType string, name *string, rank int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.set(group, policyType, name, rank)
}

func (f *fakeTeamsService) counts() (staleReads, grants, unassigns int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.staleReads, f.grants, f.unassigns
}

func (f *fakeTeamsService) order(policyType string) []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]string, 0, len(f.live[policyType]))
	for _, a := range f.live[policyType] {
		out = append(out, a.group+"="+a.name)
	}
	return out
}

func (f *fakeTeamsService) checkOrder(policyType string, want ...string) resource.TestCheckFunc {
	return func(*terraform.State) error {
		if got := f.order(policyType); !slices.Equal(got, want) {
			return fmt.Errorf("the service holds %v, want %v", got, want)
		}
		return nil
	}
}
