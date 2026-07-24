package provider

import (
	"encoding/json"

	"github.com/terraprovider/go-teams/teamsapi"
)

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
