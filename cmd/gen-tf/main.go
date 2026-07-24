// Command gen-tf generates terraform-plugin-framework resources for the Microsoft
// Teams provider from the go-teams cmdlet catalog. It is the Teams-specific
// frontend to the reusable tf-msadmin/genframework engine: it turns the
// live-validated /Skype.Policy nouns into genframework.Resource values (CRUD
// collection policies + Get/Set config singletons) and writes the emitted files
// into internal/provider.
//
// Teams specifics vs the Exchange frontend:
//
//   - scope is the live-validated policy set (spec.ValidatedPolicies), not every
//     CRUD-complete noun;
//
//   - policy settings are tri-state (Nullable<T>) so bool attributes set
//     genframework PointerParam (→ *bool bindings, ValueBoolPointer()).
//
//     go run ./cmd/gen-tf
package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/terraprovider/go-teams/spec"
	"github.com/terraprovider/tf-msadmin/genframework"
)

func main() {
	var only, out string
	flag.StringVar(&only, "noun", "", "generate only this noun")
	flag.StringVar(&out, "out", "internal/provider", "output directory")
	flag.Parse()

	cat, err := spec.Teams()
	check(err)
	validated := spec.ValidatedPolicies()

	cfg := genframework.Config{
		Package:        "provider",
		ClientsImport:  "github.com/terraprovider/terraform-provider-teams/internal/clients",
		ClientField:    "CS",
		BindingsImport: "github.com/terraprovider/go-teams/cs",
		BindingsPkg:    "cs",
	}

	byNoun := cat.ByNoun()
	var resources []genframework.Resource
	var skipped []string
	var crud, conf, readonly, assign int
	for _, noun := range sortedKeys(byNoun) {
		if only != "" && noun != only {
			continue
		}
		verbs := byNoun[noun]
		_, hasNew := verbs["New"]
		_, hasGet := verbs["Get"]
		_, hasSet := verbs["Set"]
		_, hasRemove := verbs["Remove"]
		getOnly := hasGet && !hasNew && !hasSet && !hasRemove
		// CRUD and config resources are gated on the live-validated policy set (the
		// go-teams generator only emits bindings for those). Get-only read-only nouns
		// are gated instead on the live read-probe allowlist (readOnlyProbe) — the set
		// of Get-Cs* cmdlets whose GetCs<Noun> binding exists and was confirmed to
		// return 200 against a real tenant.
		if !getOnly && !validated[spec.PolicyName(noun)] && configProbe[noun] != 200 {
			continue
		}
		switch {
		case hasNew && hasGet && hasSet && hasRemove:
			if r, ok, reason := buildResource(noun, verbs); ok {
				resources = append(resources, r)
				crud++
			} else {
				skipped = append(skipped, fmt.Sprintf("%s (%s)", noun, reason))
			}
			// A grantable policy also gets a per-user assignment resource.
			if _, hasGrant := verbs["Grant"]; hasGrant {
				resources = append(resources, buildAssignment(noun, verbs))
				assign++
			}
		case hasGet && hasSet:
			if r, ok, reason := buildConfigResource(noun, verbs); ok {
				resources = append(resources, r)
				conf++
			} else {
				skipped = append(skipped, fmt.Sprintf("%s config (%s)", noun, reason))
			}
		case getOnly:
			status, hasBinding := readOnlyProbe[noun]
			switch {
			case !hasBinding:
				// No generated GetCs<Noun> read binding — nothing to expose. Not a
				// meaningful skip (these nouns are read via other, non-cs surfaces).
				continue
			case status != 200:
				// The binding exists but the read did not return 200 against the tenant
				// during the read-probe (see readOnlyProbe) — skip and record why.
				skipped = append(skipped, fmt.Sprintf("%s read-only (read returned HTTP %d)", noun, status))
			default:
				resources = append(resources, buildReadOnly(noun, verbs))
				readonly++
			}
		}
	}

	files, err := genframework.Generate(cfg, resources)
	check(err)
	check(os.MkdirAll(out, 0o755))
	for _, f := range files {
		check(os.WriteFile(filepath.Join(out, f.Name), f.Content, 0o644))
	}
	fmt.Printf("generated %d resources (%d crud + %d config + %d read-only + %d assignment), %d files -> %s\n", len(resources), crud, conf, readonly, assign, len(files), out)
	if len(skipped) > 0 {
		sort.Strings(skipped)
		fmt.Printf("skipped %d: %s\n", len(skipped), strings.Join(skipped, ", "))
	}
}

// buildResource maps a CRUD-complete policy noun (New/Get/Set/Remove-Cs<Noun>) to
// a resource. All Teams policy cmdlets key on -Identity (the instance name).
func buildResource(noun string, verbs map[string]spec.Cmdlet) (genframework.Resource, bool, string) {
	newCmd, getCmd, setCmd, removeCmd := verbs["New"], verbs["Get"], verbs["Set"], verbs["Remove"]
	inNew := paramSet(newCmd)
	inSet := paramSet(setCmd)
	names := unionKeys(inNew, inSet)

	var attrs []genframework.Attribute
	for _, name := range names {
		if skipParam(name) || name == "Identity" {
			continue
		}
		p := firstParam(name, newCmd, setCmd)
		at, ok := attrType(p)
		if !ok {
			continue
		}
		field := exportName(name)
		_, inC := inNew[name]
		_, inU := inSet[name]
		attrs = append(attrs, genframework.Attribute{
			TFName:       tfName(field),
			Field:        field,
			APIName:      field,
			Type:         at,
			Computed:     true,
			Sensitive:    sensitive(name),
			Description:  describe(name, p),
			InCreate:     inC,
			InUpdate:     inU,
			Object:       goType(p) == "any",
			PointerParam: pointerParam(p),
		})
	}
	if len(attrs) == 0 {
		return genframework.Resource{}, false, "no mappable params"
	}
	return genframework.Resource{
		Noun:        cleanNoun(noun),
		TFName:      pascalToSnake(cleanNoun(noun)),
		Description: fmt.Sprintf("Manages the %s policy via %s / %s / %s / %s.", cleanNoun(noun), newCmd.Cmdlet, getCmd.Cmdlet, setCmd.Cmdlet, removeCmd.Cmdlet),
		// Teams policies are named by -Identity (the create key == the instance
		// name); there is no separate Name attribute.
		IdentityIsName: true,
		// Every Teams policy type has a built-in "Global" instance (the tenant
		// default) that already exists and cannot be created or removed. Declaring
		// it adopts the existing object (Create applies Set instead of New) and
		// Delete drops it from state — no manual `terraform import`. Custom
		// instances (any other identity) keep normal New/Remove CRUD.
		AdoptIdentity: "Global",
		// Tri-state policy settings: create/update must send only the fields the
		// operator set (New/Set-Cs* semantics). Otherwise unconfigured *bool
		// params serialize as explicit false and the API 403s on gated toggles.
		SparseWrite: true,
		// A policy noun has many instances (Global + Tag:*), so emit a companion
		// list data source (data.teams_<noun_plural>) that reads them all.
		Plural:     true,
		Attributes: attrs,
		Create:     op(newCmd, "Identity"),
		Read:       op(getCmd, "Identity"),
		Update:     op(setCmd, "Identity"),
		Delete:     op(removeCmd, "Identity"),
	}, true, ""
}

// buildConfigResource maps a Get+Set config/settings noun (managed in place).
func buildConfigResource(noun string, verbs map[string]spec.Cmdlet) (genframework.Resource, bool, string) {
	getc, setc := verbs["Get"], verbs["Set"]
	singleton := !paramSet(getc)["Identity"]

	var names []string
	for n := range paramSet(setc) {
		names = append(names, n)
	}
	sort.Strings(names)

	var attrs []genframework.Attribute
	for _, name := range names {
		if skipParam(name) || name == "Identity" {
			continue
		}
		p := firstParam(name, setc)
		at, ok := attrType(p)
		if !ok {
			continue
		}
		field := exportName(name)
		// A write-only setting has an unreliable (replica-inconsistent) read-back, so
		// it is generated as Required + WriteOnly: the operator declares the value, it
		// is written authoritatively, and a refresh never reads it back (which would
		// otherwise flap the plan). All other settings stay Optional+Computed.
		wo := writeOnlyParams[noun][name]
		attrs = append(attrs, genframework.Attribute{
			TFName: tfName(field),
			Field:  field,
			// APIName is the read-back JSON key: the raw cmdlet parameter name, which
			// equals the response key on both transports. For /Skype.Policy configs the
			// param name is already PascalCase (== exportName), but autorest configs use
			// camelCase (e.g. isSideloadedAppsInteractionEnabled) — exportName would
			// wrongly capitalise it and the read-back would never match.
			APIName:      name,
			Type:         at,
			Required:     wo,
			Computed:     !wo,
			WriteOnly:    wo,
			Sensitive:    sensitive(name),
			Description:  describe(name, p),
			InCreate:     true,
			InUpdate:     true,
			Object:       goType(p) == "any",
			PointerParam: pointerParam(p),
		})
	}
	if len(attrs) == 0 {
		return genframework.Resource{}, false, "no mappable settings"
	}
	updID := ""
	if !singleton && paramSet(setc)["Identity"] {
		updID = "Identity"
	}
	readID := ""
	if paramSet(getc)["Identity"] {
		readID = "Identity"
	}
	return genframework.Resource{
		Noun:        cleanNoun(noun),
		TFName:      pascalToSnake(cleanNoun(noun)),
		Description: fmt.Sprintf("Manages the %s configuration via %s.", cleanNoun(noun), setc.Cmdlet),
		Attributes:  attrs,
		Read:        genframework.Op{Method: goName(getc.Cmdlet), Params: goName(getc.Cmdlet) + "Params", IdentityField: readID},
		Update:      genframework.Op{Method: goName(setc.Cmdlet), Params: goName(setc.Cmdlet) + "Params", IdentityField: updID},
		Config:      true,
		Singleton:   singleton,
		// Set-Cs* config cmdlets likewise touch only the parameters passed.
		SparseWrite: true,
	}, true, ""
}

// buildReadOnly maps a Get-only noun (a Get-Cs* cmdlet with no New/Set/Remove) to
// a schema-less, read-only data source: a RawJSON singular data source that looks
// up one object (id/identity/name/display_name + a json attribute holding the whole
// object) plus a companion Plural ("list") data source that returns every object.
// There is no managed resource. Only nouns in readOnlyProbe with a 200 read reach
// here; the caller filters the rest.
func buildReadOnly(noun string, verbs map[string]spec.Cmdlet) genframework.Resource {
	getc := verbs["Get"]
	// Read.IdentityField is set only when the Get cmdlet actually takes -Identity;
	// otherwise the singular read calls Get with no key and returns the first object.
	readID := ""
	if paramSet(getc)["Identity"] {
		readID = "Identity"
	}
	return genframework.Resource{
		Noun:        cleanNoun(noun),
		TFName:      pascalToSnake(cleanNoun(noun)),
		Description: fmt.Sprintf("Reads the %s object(s) via %s (read-only).", cleanNoun(noun), getc.Cmdlet),
		// Schema-less read-only exposure: the property schema of these Get-only nouns
		// is not machine-readable, so expose the whole object as a `json` attribute.
		DataSourceOnly: true,
		RawJSON:        true,
		// Also emit a companion list data source (data.teams_<noun_plural>) that reads
		// every object with Get-<Noun> and no key.
		Plural: true,
		Read:   genframework.Op{Method: goName(getc.Cmdlet), Params: goName(getc.Cmdlet) + "Params", IdentityField: readID},
	}
}

// configProbe is the live-validated allowlist of Get+Set config singletons that do
// NOT live under /Skype.Policy (so they are absent from spec.ValidatedPolicies) but
// are exposed as managed config resources. Keyed by full noun; the value is the live
// HTTP status of the read probe against a real tenant (200 = exposed). These use a
// different transport (e.g. Teams.MiddletierService autorest GET/PUT), which the
// go-teams cs bindings route transparently.
//
//	CsTeamsSettingsCustomApp: GET/PUT /Teams.MiddletierService/tenantWideAppsSettingsGlobal;
//	  the Set is a read-modify-write override (see go-teams cs/customapp.go).
var configProbe = map[string]int{
	"CsTeamsSettingsCustomApp": 200,
}

// writeOnlyParams marks config settings whose read-back is unreliable: the API
// accepts the write but returns replica-inconsistent values on read, so no read is
// authoritative. These are generated as Required + WriteOnly — the operator declares
// the value, it is written, and a refresh never reads it back (which would flap the
// plan; see genframework Attribute.WriteOnly). Keyed by noun -> raw parameter name.
//
//	CsTeamsSettingsCustomApp.isSideloadedAppsInteractionEnabled: GET load-balances
//	  across backend replicas that converge slowly, so the value flaps between reads.
var writeOnlyParams = map[string]map[string]bool{
	"CsTeamsSettingsCustomApp": {"isSideloadedAppsInteractionEnabled": true},
}

// readOnlyProbe is the live read-probe result for the Get-only nouns that have a
// generated GetCs<Noun> binding: noun -> HTTP status of Get-Cs<Noun> called with
// empty params against a real tenant (read-only). 200 means the read works and the
// noun is exposed as a read-only data source; any other status means it is skipped
// (permission-gated 403, requires an association 404, needs a special header 400,
// GET unsupported 405, ...). Nouns absent from this map have no cs read binding at
// all and are not Get-only-exposable. Regenerate this map with the throwaway probe
// (READ-ONLY) if the go-teams catalog/bindings change.
var readOnlyProbe = map[string]int{
	// working (HTTP 200) — exposed as read-only data sources
	"CsAutoAttendantTenantInformation":              200,
	"CsMainlineAttendantFlow":                       200,
	"CsMainlineAttendantSupportedLanguages":         200,
	"CsMainlineAttendantSupportedVoices":            200,
	"CsMainlineAttendantTenantInformation":          200,
	"CsMeetingMigrationStatus":                      200,
	"CsOnlineDialOutPolicy":                         200,
	"CsOnlineDialinConferencingBridge":              200,
	"CsOnlineDialinConferencingLanguagesSupported":  200,
	"CsOnlineDialinConferencingPolicy":              200,
	"CsOnlineDialinConferencingTenantConfiguration": 200,
	"CsOnlineSipDomain":                             200,
	"CsOnlineUser":                                  200,
	"CsTeamsMediaLoggingPolicy":                     200,
	"CsTeamsMeetingTemplateConfiguration":           200,
	"CsTeamsRemoteLogCollectionConfiguration":       200,
	"CsTeamsUpgradePolicy":                          200,
	"CsTeamsVideoInteropServicePolicy":              200,
	"CsTenant":                                      200,
	"CsTenantNetworkConfiguration":                  200,
	// binding exists but the read did not return 200 against the tenant — skipped
	"CsAutoAttendantStatus":                        404,
	"CsAutoAttendantSupportedLanguage":             404,
	"CsAutoAttendantSupportedTimeZone":             404,
	"CsBatchTeamsDeploymentStatus":                 403,
	"CsExportAcquiredPhoneNumberStatus":            400,
	"CsOnlineApplicationInstanceAssociationStatus": 405,
	"CsOnlineTelephoneNumberCountry":               400,
	"CsOnlineTelephoneNumberType":                  400,
	"CsPolicyPackage":                              403,
	"CsSdgBulkSignInRequestStatus":                 403,
	"CsSdgBulkSignInRequestsSummary":               403,
	"CsTeamTemplateList":                           404,
	"CsTeamsShiftsConnectionConnector":             403,
	"CsTeamsShiftsConnectionErrorReport":           403,
	"CsTeamsShiftsConnectionOperation":             403,
	"CsTeamsShiftsConnectionSyncResult":            403,
	"CsTeamsShiftsConnectionWfmTeam":               403,
	"CsTeamsShiftsConnectionWfmUser":               403,
	"CsTenantLicensingConfiguration":               403,
	"CsUserPolicyAssignment":                       404,
	"CsUserPolicyPackage":                          403,
	"CsUserPolicyPackageRecommendation":            403,
}

func op(c spec.Cmdlet, identityField string) genframework.Op {
	m := goName(c.Cmdlet)
	return genframework.Op{Method: m, Params: m + "Params", IdentityField: identityField}
}

// buildAssignment maps a grantable policy noun to a per-user assignment resource
// (teams_<policy>_assignment): { user, policy_name }. Create/Update call the grant
// cmdlet (Identity=user, PolicyName=instance), Delete unassigns (PolicyName=""),
// and Read pulls the user's effective assignment for this policy type.
func buildAssignment(noun string, verbs map[string]spec.Cmdlet) genframework.Resource {
	grant := verbs["Grant"]
	clean := cleanNoun(noun)
	return genframework.Resource{
		Noun:                 clean + "Assignment",
		TFName:               pascalToSnake(clean) + "_assignment",
		Description:          fmt.Sprintf("Assigns a %s instance to a user via %s.", clean, grant.Cmdlet),
		Assignment:           true,
		AssignmentPolicyType: spec.PolicyName(noun),
		AssignmentUserRead:   "EffectiveUserPolicy",
		Create:               op(grant, "Identity"),
	}
}

// cleanNoun strips the "Cs" and a redundant leading "Teams" from the noun, so the
// resource is teams_meeting_policy rather than teams_cs_teams_meeting_policy.
func cleanNoun(noun string) string {
	return strings.TrimPrefix(strings.TrimPrefix(noun, "Cs"), "Teams")
}

// ---- Teams type mapping ----

func goType(p spec.Param) string {
	switch p.Type {
	case "switch", "bool":
		return "bool"
	case "string":
		return "string"
	case "stringArray", "array":
		return "[]string"
	case "int":
		return "int"
	case "float":
		return "float"
	default:
		return "any"
	}
}

func attrType(p spec.Param) (genframework.AttrType, bool) {
	switch goType(p) {
	case "bool":
		return genframework.TypeBool, true
	case "[]string":
		return genframework.TypeStringSet, true
	case "int":
		return genframework.TypeInt, true
	case "string", "any":
		return genframework.TypeString, true
	default: // float — no framework type yet
		return 0, false
	}
}

// pointerParam is true for tri-state Nullable<T> params — the cs bindings use a
// pointer field (*bool) for these, so create/update must pass ValueBoolPointer().
func pointerParam(p spec.Param) bool { return p.Type == "bool" || p.Type == "int" }

// ---- shared helpers (mirror the Exchange frontend) ----

var skipParams = map[string]bool{
	"Force": true, "Confirm": true, "WhatIf": true, "Verbose": true, "Debug": true,
	"ProgressAction": true, "ErrorAction": true, "WarningAction": true,
	"InformationAction": true, "MsftInternalProcessingMode": true, "AsJob": true,
	// autorest plumbing — infrastructure params on the generated (autorest) cmdlets,
	// never a user-facing setting. They surface once a non-/Skype.Policy config is
	// exposed (see configProbe); dropping them keeps the schema to real settings.
	"HttpPipelinePrepend": true, "HttpPipelineAppend": true, "Break": true,
	"Proxy": true, "ProxyCredential": true, "ProxyUseDefaultCredentials": true,
}

func skipParam(name string) bool { return skipParams[name] }

func sensitive(name string) bool {
	l := strings.ToLower(name)
	return strings.Contains(l, "password") || strings.Contains(l, "secret") || strings.Contains(l, "credential")
}

func describe(name string, p spec.Param) string {
	d := fmt.Sprintf("Maps to the -%s parameter.", name)
	if len(p.ValidateSet) > 0 {
		d += " Allowed values: " + strings.Join(p.ValidateSet, ", ") + "."
	}
	return d
}

func paramSet(c spec.Cmdlet) map[string]bool {
	m := map[string]bool{}
	for _, p := range c.Parameters {
		m[p.Name] = true
	}
	return m
}

func firstParam(name string, cmds ...spec.Cmdlet) spec.Param {
	for _, c := range cmds {
		for _, p := range c.Parameters {
			if p.Name == name {
				return p
			}
		}
	}
	return spec.Param{Name: name}
}

func unionKeys(a, b map[string]bool) []string {
	seen := map[string]bool{}
	var out []string
	for _, m := range []map[string]bool{a, b} {
		for k := range m {
			if !seen[k] {
				seen[k] = true
				out = append(out, k)
			}
		}
	}
	sort.Strings(out)
	return out
}

func sortedKeys(m map[string]map[string]spec.Cmdlet) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func goName(cmdlet string) string {
	parts := strings.FieldsFunc(cmdlet, func(r rune) bool { return r == '-' || r == '_' })
	var sb strings.Builder
	for _, p := range parts {
		sb.WriteString(exportName(p))
	}
	return sb.String()
}

func exportName(s string) string {
	s = strings.Map(func(r rune) rune {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') {
			return r
		}
		return -1
	}, s)
	if s == "" {
		return "X"
	}
	if s[0] >= '0' && s[0] <= '9' {
		s = "N" + s
	}
	return strings.ToUpper(s[:1]) + s[1:]
}

var reservedNames = map[string]bool{
	"alias": true, "count": true, "depends_on": true, "for_each": true,
	"lifecycle": true, "provider": true, "provisioner": true, "connection": true,
	"id": true, "identity": true,
}

func tfName(field string) string {
	n := pascalToSnake(field)
	if reservedNames[n] {
		n += "_"
	}
	return n
}

func pascalToSnake(s string) string {
	var b strings.Builder
	runes := []rune(s)
	for i, r := range runes {
		isUpper := r >= 'A' && r <= 'Z'
		if isUpper && i > 0 {
			prev := runes[i-1]
			prevLower := prev >= 'a' && prev <= 'z' || prev >= '0' && prev <= '9'
			nextLower := i+1 < len(runes) && runes[i+1] >= 'a' && runes[i+1] <= 'z'
			if prevLower || nextLower {
				b.WriteByte('_')
			}
		}
		if isUpper {
			b.WriteRune(r - 'A' + 'a')
		} else {
			b.WriteRune(r)
		}
	}
	return b.String()
}

func check(err error) {
	if err != nil {
		fmt.Fprintln(os.Stderr, "gen-tf:", err)
		os.Exit(1)
	}
}
