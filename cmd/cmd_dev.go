//go:build dev

package cmd

import (
	"github.com/spf13/cobra"

	// initialize all nodes
	_ "github.com/actionforge/actrun-cli/nodes"
)

var cmdDev = &cobra.Command{
	Use: "dev",
}

func init() {
	cmdRoot.AddCommand(cmdDev)
}
