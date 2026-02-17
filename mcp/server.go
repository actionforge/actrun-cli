package mcpserver

import (
	"github.com/actionforge/actrun-cli/build"
	mcpdebug "github.com/actionforge/actrun-cli/mcp/debug"
	"github.com/mark3labs/mcp-go/server"
)

// RunMCPServer creates the MCP server, registers debug tools,
// and serves over stdio. It blocks until the stdio transport closes.
// The instructions parameter is optional; when non-empty it is sent to
// the client in the initialize response.
func RunMCPServer(instructions string) error {
	version := build.GetAppVersion()

	opts := []server.ServerOption{
		server.WithToolCapabilities(false),
	}
	if instructions != "" {
		opts = append(opts, server.WithInstructions(instructions))
	}

	s := server.NewMCPServer("actrun", version, opts...)

	mcpdebug.RegisterDebugTools(s)

	return server.ServeStdio(s)
}
