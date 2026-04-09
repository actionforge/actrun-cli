package cmd

import (
	"os"

	"github.com/actionforge/actrun-cli/sessions"
	"github.com/actionforge/actrun-cli/utils"
	"github.com/spf13/cobra"
)

var (
	flagDebugSessionToken string
	flagDebugConfigFile   string
)

var cmdDebug = &cobra.Command{
	Use:   "debug",
	Short: "Connect to the web app with a session token",
	Run:   cmdDebugRun,
}

func init() {
	cmdRoot.AddCommand(cmdDebug)

	cmdDebug.Flags().StringVar(&flagDebugSessionToken, "session-token", envOr("ACT_SESSION_TOKEN", ""), "Session token from your browser (env: ACT_SESSION_TOKEN)")
	cmdDebug.Flags().StringVar(&flagDebugConfigFile, "config-file", "", "The config file to use")
}

func cmdDebugRun(cmd *cobra.Command, args []string) {
	err := sessions.RunSessionMode(flagDebugConfigFile, "", flagDebugSessionToken, "flag")
	if err != nil {
		utils.LogErr.Print(err.Error())
		os.Exit(1)
	}
}
