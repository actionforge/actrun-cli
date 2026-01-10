package cmd

import (
	"fmt"

	"github.com/actionforge/actrun-cli/build"

	"github.com/spf13/cobra"
)

var cmdVersion = &cobra.Command{
	Use:   "version",
	Short: "Print the version number of actrun",
	Run: func(cmd *cobra.Command, args []string) {
		fmt.Printf("actrun version v%s\n", build.GetFulllVersionInfo())
	},
}

func init() {
	cmdRoot.AddCommand(cmdVersion)
}
