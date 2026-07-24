// Package clients holds the configured Teams admin API client shared by all
// resources.
package clients

import (
	"github.com/terraprovider/go-teams/cs"
	"github.com/terraprovider/go-teams/teamsapi"
)

// Client is passed to every resource/data source via Configure.
type Client struct {
	API      *teamsapi.Client
	CS       *cs.Service
	TenantID string
}
