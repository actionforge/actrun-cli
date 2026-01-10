//go:build dev

package cmd

import (
	"github.com/spf13/cobra"
)

var cmdDevUpdate = &cobra.Command{
	Use:   "update",
	Short: "Update command for various steps during development.",
}

func init() {
	cmdDev.AddCommand(cmdDevUpdate)
}
