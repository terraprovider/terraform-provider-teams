# terraform-provider-teams

A Terraform / OpenTofu provider for **Microsoft Teams admin configuration** —
the `-Cs*` policy and settings surface behind the MicrosoftTeams PowerShell
module. Resources are **generated** from the [`go-teams`](https://github.com/terraprovider/go-teams)
cmdlet catalog via the shared
[`tf-msadmin/genframework`](https://github.com/terraprovider/tf-msadmin) engine, so the
surface tracks the module.

> Not affiliated with or endorsed by Microsoft. It calls the same documented Teams
> admin API the PowerShell module does.

## Status

- **82 resources** generated (57 policy CRUD + 25 config/settings), each with a
  matching read-only data source. Builds clean.
- Authentication is aligned with the `hashicorp/azuread` and `azurerm` providers
  (same attributes, `ARM_*` / `AZURE_*` env fallbacks, every OIDC / workload-identity
  flavour) via `tf-msadmin/authschema`.

```hcl
terraform {
  required_providers {
    teams = { source = "terraprovider/teams" }
  }
}

provider "teams" {} # ARM_TENANT_ID / ARM_CLIENT_ID / ARM_CLIENT_SECRET (or cert / OIDC)

resource "teams_meeting_policy" "kiosk" {
  identity                = "KioskPolicy"
  allow_meet_now          = false
  allow_channel_meeting_scheduling = false
  meeting_chat_enabled_type        = "Disabled"
}

resource "teams_tenant_federation_configuration" "global" {
  allow_federated_users = true
  allow_teams_consumer  = false
}
```

App-only needs the app's service principal to hold a **Teams admin directory role**
(a `403` is a role gap, not a token problem).

## Generation

```
go-teams/spec catalog  --cmd/gen-tf-->  genframework.Resource  --Generate-->  internal/provider/*.go
```

`cmd/gen-tf` is the Teams frontend: it maps the live-validated `/Skype.Policy`
nouns to normalized resources (bool attributes set `PointerParam` for the tri-state
`Nullable<T>` settings). Generated files are `*_resource.go` / `*_data_source.go` /
`zz_generated_resources.go` — do not edit; re-run `go run ./cmd/gen-tf`.

## License

MIT — see [LICENSE](LICENSE). Not affiliated with or endorsed by Microsoft.
