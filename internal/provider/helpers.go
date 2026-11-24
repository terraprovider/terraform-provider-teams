package provider

import (
	"encoding/json"
	"errors"
	"net/http"
	"regexp"
	"strings"

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

// objectParam decodes a System.Object attribute's configured string value for a
// write. Teams' System.Object params (e.g. -AppPresetList, -PinnedAppBarApps) are
// JSON arrays/objects, and the ConfigAPI silently drops a bare JSON *string* — so a
// value that looks like JSON (starts with '[' or '{') is unmarshalled to real
// structured data. Anything else (a plain-string param) is sent as-is.
func objectParam(s string) any {
	t := strings.TrimSpace(s)
	if len(t) > 0 && (t[0] == '[' || t[0] == '{') {
		var v any
		if json.Unmarshal([]byte(t), &v) == nil {
			return v
		}
	}
	return s
}

// getObjectJSON reads a System.Object attribute back as a string. The ConfigAPI
// returns these as structured values (arrays/objects) that getString cannot read
// (it only handles string values), so a non-string value is re-encoded as compact
// JSON to round-trip against a jsonencode() config; a string is returned as-is and a
// missing value yields "".
func getObjectJSON(m map[string]any, key string) string {
	v, ok := m[key]
	if !ok || v == nil {
		return ""
	}
	if s, ok := v.(string); ok {
		return s
	}
	b, err := json.Marshal(v)
	if err != nil {
		return ""
	}
	return string(b)
}

// writeConflictCode matches the errorCode of a lost parallel write in a 409
// body: 40904 (create), 40902 (update) or 40903 (delete conflict). It is a pattern
// rather than a JSON decode so that it still matches when the body is truncated
// (APIError.Body may be cut; errorCode comes early, before the long innerError).
var writeConflictCode = regexp.MustCompile(`"errorCode"\s*:\s*(40904|40902|40903)\b`)

// isWriteConflict reports a write Teams rejected only because a parallel write
// to the same policy type won (one storage document per type and tenant, with an
// etag check): HTTP 409 with errorCode 40904, 40902 or 40903. 40901 (already exists)
// and every other error are not conflicts. genframework retries these
// (Config.WriteConflict).
func isWriteConflict(err error) bool {
	var ae *teamsapi.APIError
	if !errors.As(err, &ae) || ae.Status != http.StatusConflict {
		return false
	}
	return writeConflictCode.MatchString(ae.Body)
}
