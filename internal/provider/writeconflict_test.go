package provider

import (
	"fmt"
	"testing"

	"github.com/terraprovider/go-teams/teamsapi"
)

func TestIsWriteConflict(t *testing.T) {
	apiErr := func(status int, body string) error {
		return fmt.Errorf("wrapped: %w", &teamsapi.APIError{Status: status, Message: "m", Body: body})
	}
	body := func(code string) string {
		return `{"code":"ClientError","message":"Identity  has a create conflict","action":"Please check your request parameters.","errorCode":` + code + `,"innerError":{"ErrorCode":"Status: Conflict (409)"}}`
	}
	full := body("40904")
	for name, c := range map[string]struct {
		err  error
		want bool
	}{
		"create conflict":            {apiErr(409, body("40904")), true},
		"update conflict":            {apiErr(409, body("40902")), true},
		"delete conflict":            {apiErr(409, body("40903")), true},
		"already exists":             {apiErr(409, body("40901")), false},
		"other conflict code prefix": {apiErr(409, body("409041")), false},
		"other status":               {apiErr(400, body("40904")), false},
		"truncated after errorCode":  {apiErr(409, full[:len(full)-30]), true},
		"truncated before errorCode": {apiErr(409, full[:40]), false},
		"not json":                   {apiErr(409, "<html>"), false},
		"plain error":                {fmt.Errorf("x"), false},
	} {
		if got := isWriteConflict(c.err); got != c.want {
			t.Errorf("%s: got %v, want %v", name, got, c.want)
		}
	}
}
