package main

import (
	_ "github.com/actionforge/actrun-cli/api"
	"github.com/actionforge/actrun-cli/cmd"
	_ "github.com/actionforge/actrun-cli/cmd"
	"github.com/actionforge/actrun-cli/core"
	_ "github.com/actionforge/actrun-cli/tests_unit"
	"github.com/actionforge/actrun-cli/utils"
)

func main() {
	utils.ApplyLogLevel()

	expired, err := core.CheckIfBuildExpired()
	if err != nil {
		utils.LogErr.Errorln("Error checking build expiry:", err)
		return
	}

	if expired {
		utils.LogErr.Errorln("This preview build has expired. Please download the latest version from: https://actionforge.dev")
		return
	}

	cmd.Execute()
}
