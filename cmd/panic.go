package cmd

import (
	"github.com/spf13/cobra"
)

var cmdPanic = &cobra.Command{
	Use:    "panic",
	Short:  "Simulates a panic error",
	Hidden: true,
	Run: func(cmd *cobra.Command, args []string) {
		panic("this is a panic")
	},
}

func init() {
	cmdRoot.AddCommand(cmdPanic)
}
