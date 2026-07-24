package provider

import (
	"context"
	"encoding/json"

	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/terraprovider/go-teams/teamsapi"
)

// toStringSlice converts a types.Set of strings to []string.
func toStringSlice(ctx context.Context, s types.Set, diags *diag.Diagnostics) []string {
	if s.IsNull() || s.IsUnknown() {
		return nil
	}
	var out []string
	diags.Append(s.ElementsAs(ctx, &out, false)...)
	return out
}

// stringSetValue builds a types.Set from a []string (empty, non-null when vals is nil).
func stringSetValue(ctx context.Context, vals []string) types.Set {
	if vals == nil {
		vals = []string{}
	}
	set, d := types.SetValueFrom(ctx, types.StringType, vals)
	if d.HasError() {
		return types.SetValueMust(types.StringType, nil)
	}
	return set
}

func firstObject(v []map[string]any) map[string]any {
	if len(v) == 0 {
		return nil
	}
	return v[0]
}

func getString(m map[string]any, key string) string {
	if v, ok := m[key]; ok && v != nil {
		if s, ok := v.(string); ok {
			return s
		}
	}
	return ""
}

func getBool(m map[string]any, key string) bool {
	if v, ok := m[key]; ok && v != nil {
		if b, ok := v.(bool); ok {
			return b
		}
	}
	return false
}

// getInt reads an integer read-back field. JSON-decoded numbers arrive as float64.
func getInt(m map[string]any, key string) int64 {
	if v, ok := m[key]; ok && v != nil {
		switch n := v.(type) {
		case float64:
			return int64(n)
		case int64:
			return n
		case int:
			return int64(n)
		}
	}
	return 0
}

func getStringSlice(m map[string]any, key string) []string {
	v, ok := m[key]
	if !ok || v == nil {
		return nil
	}
	arr, ok := v.([]any)
	if !ok {
		return nil
	}
	out := make([]string, 0, len(arr))
	for _, e := range arr {
		if s, ok := e.(string); ok {
			out = append(out, s)
		}
	}
	return out
}

func firstNonEmptyStr(vals ...string) string {
	for _, v := range vals {
		if v != "" {
			return v
		}
	}
	return ""
}

// isNotFound reports whether the error indicates the object no longer exists
// (so the resource should be dropped from state on Read).
func isNotFound(err error) bool {
	return teamsapi.IsNotFound(err)
}

// toJSON encodes a read-back object as a compact JSON string for the RawJSON
// read-only data sources' `json` attribute (decode in HCL with jsondecode()). It
// never fails the read: an unencodable object yields an empty string.
func toJSON(obj map[string]any) string {
	b, err := json.Marshal(obj)
	if err != nil {
		return ""
	}
	return string(b)
}
