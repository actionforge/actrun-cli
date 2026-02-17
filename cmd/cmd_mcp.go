package cmd

import (
	"fmt"
	"os"
	"strings"

	mcpserver "github.com/actionforge/actrun-cli/mcp"
	"github.com/spf13/cobra"
)

// buildMCPInstructions generates the MCP server instructions from the
// actual flags registered on cmdRoot so they stay in sync automatically.
func buildMCPInstructions() string {
	var b strings.Builder

	b.WriteString("This MCP server provides tools for debugging and running ActionForge graph (.act) files interactively. ")
	b.WriteString("Use the debug_* tools to step through graph execution node by node, set breakpoints, and inspect state.\n\n")

	b.WriteString("If you just need to run a graph without debugging, use the actrun CLI directly instead of this MCP server:\n\n")
	fmt.Fprintf(&b, "  %s\n\n", cmdRoot.Use)

	b.WriteString("Available flags:\n")
	// Include both persistent and local flags from the root command.
	b.WriteString(cmdRoot.PersistentFlags().FlagUsages())
	b.WriteString(cmdRoot.LocalNonPersistentFlags().FlagUsages())

	b.WriteString("\nTo pass arguments to the graph itself, append them after the file: actrun file.act arg1 arg2\n")
	b.WriteString("Use '--' to separate actrun flags from graph arguments: actrun --env-file .env file.act -- --graph-flag")

	return b.String()
}

var cmdMcp = &cobra.Command{
	Use:   "mcp",
	Short: "Start the MCP server (stdio transport).",
	Long:  `Starts an MCP server over stdio that exposes debug tools for bridging between an AI agent and an actrun local debug session (WebSocket). Configure this as an MCP server in your AI tool with: {"command": "actrun", "args": ["mcp"]}`,
	Run: func(cmd *cobra.Command, args []string) {
		if err := mcpserver.RunMCPServer(buildMCPInstructions()); err != nil {
			fmt.Fprintf(os.Stderr, "MCP server error: %v\n", err)
			os.Exit(1)
		}
	},
}

func init() {
	cmdRoot.AddCommand(cmdMcp)
}
