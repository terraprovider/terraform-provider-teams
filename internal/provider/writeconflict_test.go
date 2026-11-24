package provider

import (
	"fmt"
	"testing"

	"github.com/terraprovider/go-teams/teamsapi"
)

// TestIsWriteConflict pins the predicate the generated writes retry on
// (Config.WriteConflict). go-teams fills APIError.Code from the response's
// top-level errorCode.
func TestIsWriteConflict(t *testing.T) {
	apiErr := func(status int, code string) error {
		return fmt.Errorf("wrapped: %w", &teamsapi.APIError{Status: status, Code: code, Message: "m"})
	}
	for name, c := range map[string]struct {
		err  error
		want bool
	}{
		"create conflict": {apiErr(409, teamsapi.CodeCreateConflict), true},
		"update conflict": {apiErr(409, teamsapi.CodeUpdateConflict), true},
		"delete conflict": {apiErr(409, teamsapi.CodeDeleteConflict), true},
		"already exists":  {apiErr(409, teamsapi.CodeAlreadyExists), false},
		"no code":         {apiErr(409, ""), false},
		"other status":    {apiErr(400, teamsapi.CodeCreateConflict), false},
		"plain error":     {fmt.Errorf("x"), false},
	} {
		if got := isWriteConflict(c.err); got != c.want {
			t.Errorf("%s: got %v, want %v", name, got, c.want)
		}
	}
}
