package provider

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/provider"
	"github.com/hashicorp/terraform-plugin-framework/provider/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource"

	"github.com/terraprovider/go-teams/cs"
	"github.com/terraprovider/go-teams/teamsapi"
	"github.com/terraprovider/terraform-provider-teams/internal/clients"
	"github.com/terraprovider/tf-msadmin/authschema"
)

// teamsProvider implements the Microsoft Teams admin provider.
type teamsProvider struct{ version string }

// New returns the provider constructor for the given version.
func New(version string) func() provider.Provider {
	return func() provider.Provider { return &teamsProvider{version: version} }
}

func (p *teamsProvider) Metadata(_ context.Context, _ provider.MetadataRequest, resp *provider.MetadataResponse) {
	resp.TypeName = "teams"
	resp.Version = p.version
}

// providerModel is the shared azuread/azurerm-aligned auth block. Teams needs no
// routing hint (it self-discovers the regional backend via serviceDiscovery).
type providerModel struct {
	authschema.Model
}

func (p *teamsProvider) Schema(_ context.Context, _ provider.SchemaRequest, resp *provider.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "Manage Microsoft Teams admin configuration via the Teams config API. Authentication mirrors the AzureAD/AzureRM providers (ARM_*/AZURE_* env vars supported). App-only needs a Teams admin directory role on the service principal.",
		Attributes:  authschema.Attributes(),
	}
}

func (p *teamsProvider) Configure(ctx context.Context, req provider.ConfigureRequest, resp *provider.ConfigureResponse) {
	var m providerModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &m)...)
	if resp.Diagnostics.HasError() {
		return
	}

	// Explicit config overlaid onto the ARM_*/AZURE_* environment.
	tp, err := m.Config().Build()
	if err != nil {
		resp.Diagnostics.AddError("Authentication configuration error", err.Error())
		return
	}

	// Resolve the tenant GUID from a Teams-admin token (the tid claim).
	tok, err := tp.Token(ctx, teamsapi.Public.Resource)
	if err != nil {
		resp.Diagnostics.AddError("Authentication failed", err.Error())
		return
	}
	tid := jwtClaim(tok, "tid")
	if tid == "" {
		resp.Diagnostics.AddError("Authentication failed", "could not read tenant id (tid) from the access token")
		return
	}

	api, err := teamsapi.New(teamsapi.Options{
		Environment: teamsapi.Public,
		TenantID:    tid,
		Tokens:      tp,
	})
	if err != nil {
		resp.Diagnostics.AddError("Client initialisation error", err.Error())
		return
	}

	c := &clients.Client{API: api, CS: cs.New(api), TenantID: tid}
	resp.ResourceData = c
	resp.DataSourceData = c
}

func (p *teamsProvider) Resources(_ context.Context) []func() resource.Resource {
	return generatedResources()
}

func (p *teamsProvider) DataSources(_ context.Context) []func() datasource.DataSource {
	return generatedDataSources()
}

func jwtClaim(jwt, name string) string {
	parts := strings.Split(jwt, ".")
	if len(parts) < 2 {
		return ""
	}
	b, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return ""
	}
	var claims map[string]any
	if json.Unmarshal(b, &claims) != nil {
		return ""
	}
	if v, ok := claims[name].(string); ok {
		return v
	}
	return ""
}
