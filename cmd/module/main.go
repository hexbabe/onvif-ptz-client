package main

import (
	"onvifptzclient"

	"go.viam.com/rdk/components/generic"
	"go.viam.com/rdk/module"
	"go.viam.com/rdk/resource"
)

func main() {
	module.ModularMain(resource.APIModel{generic.API, onvifptzclient.Client})
}
