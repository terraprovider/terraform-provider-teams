// terraform-provider-teams is a Terraform/OpenTofu provider for Microsoft Teams
// admin configuration, generated from the MicrosoftTeams cmdlet metadata via
// go-teams.
package main

import (
	"context"
	"flag"
	"log"

	"github.com/hashicorp/terraform-plugin-framework/providerserver"
	"github.com/terraprovider/terraform-provider-teams/internal/provider"
)

// version is set by goreleaser at build time.
var version = "dev"

func main() {
	var debug bool
	flag.BoolVar(&debug, "debug", false, "run the provider with support for debuggers like delve")
	flag.Parse()

	err := providerserver.Serve(context.Background(), provider.New(version), providerserver.ServeOpts{
		Address: "registry.terraform.io/terraprovider/teams",
		Debug:   debug,
	})
	if err != nil {
		log.Fatal(err.Error())
	}
}
