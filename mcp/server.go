package mcpserver

import (
	"github.com/actionforge/actrun-cli/build"
	mcpdebug "github.com/actionforge/actrun-cli/mcp/debug"
	"github.com/mark3labs/mcp-go/server"
)

// RunMCPServer creates the MCP server, registers all tools (graph + debug),
// and serves over stdio. It blocks until the stdio transport closes.
func RunMCPServer(actfileSchema []byte) error {
	version := build.GetAppVersion()
	s := server.NewMCPServer(
		"actrun",
		version,
		server.WithToolCapabilities(false),
	)

	registerGraphTools(s, actfileSchema)
	mcpdebug.RegisterDebugTools(s)

	return server.ServeStdio(s)
}
