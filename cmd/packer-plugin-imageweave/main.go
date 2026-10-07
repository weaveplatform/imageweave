package main

import (
	"log"

	"github.com/hashicorp/packer-plugin-sdk/plugin"
	"github.com/hashicorp/packer-plugin-sdk/version"

	"github.com/weaveplatform/imageweave/internal/packerplugin"
)

func main() {
	set := plugin.NewSet()
	set.SetVersion(version.NewPluginVersion("0.1.0", "", ""))
	set.RegisterBuilder("native", &packerplugin.Builder{})
	if err := set.Run(); err != nil {
		log.Fatal(err)
	}
}
