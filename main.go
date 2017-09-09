package main

import (
	"github.com/golvteppe/terraform-provider-rancher/rancher"
	"github.com/hashicorp/terraform/plugin"
)

func main() {
	plugin.Serve(&plugin.ServeOpts{
		ProviderFunc: rancher.Provider})
}
