package cmd

import (
	"fmt"
	"os"

	mcpserver "github.com/actionforge/actrun-cli/mcp"
	"github.com/spf13/cobra"
)

var cmdMcp = &cobra.Command{
	Use:   "mcp",
	Short: "Start the MCP server (stdio transport).",
	Long:  `Starts an MCP server over stdio that exposes graph tools (validate, schema, node types) and debug tools for bridging between an AI agent and an actrun local debug session (WebSocket). Configure this as an MCP server in your AI tool with: {"command": "actrun", "args": ["mcp"]}`,
	Run: func(cmd *cobra.Command, args []string) {
		if err := mcpserver.RunMCPServer(ActfileSchema); err != nil {
			fmt.Fprintf(os.Stderr, "MCP server error: %v\n", err)
			os.Exit(1)
		}
	},
}

func init() {
	cmdRoot.AddCommand(cmdMcp)
}
