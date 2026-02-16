package mcpdebug

import (
	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

// RegisterDebugTools creates a WebSocket bridge and registers all debug
// tools on the given MCP server.
func RegisterDebugTools(s *server.MCPServer) {
	bridge := NewBridge()

	s.AddTool(
		mcp.NewTool("debug_connect",
			mcp.WithDescription("Connect to a running actrun local debug server"),
			mcp.WithNumber("port",
				mcp.Description("The port number of the local debug server (printed as LOCAL_WS_PORT when starting actrun --local)"),
				mcp.Required(),
			),
		),
		handleDebugConnect(bridge),
	)

	s.AddTool(
		mcp.NewTool("debug_run",
			mcp.WithDescription("Start executing a graph in the debug session. Sends the graph YAML content and waits for the first pause or completion."),
			mcp.WithString("graph",
				mcp.Description("The full YAML content of the .act graph file"),
				mcp.Required(),
			),
			mcp.WithArray("breakpoints",
				mcp.Description("List of node IDs to set as breakpoints before execution"),
			),
			mcp.WithBoolean("start_paused",
				mcp.Description("Whether to pause at the first node (default: true)"),
			),
		),
		handleDebugRun(bridge),
	)

	s.AddTool(
		mcp.NewTool("debug_step",
			mcp.WithDescription("Step over: execute the current node and pause at the next node at the same depth or shallower"),
		),
		handleDebugStep(bridge),
	)

	s.AddTool(
		mcp.NewTool("debug_step_into",
			mcp.WithDescription("Step into: if the current node is a group, pause at the first node inside it; otherwise behaves like step"),
		),
		handleDebugStepInto(bridge),
	)

	s.AddTool(
		mcp.NewTool("debug_step_out",
			mcp.WithDescription("Step out: resume execution and pause when returning to a shallower depth (parent group)"),
		),
		handleDebugStepOut(bridge),
	)

	s.AddTool(
		mcp.NewTool("debug_resume",
			mcp.WithDescription("Resume execution until the next breakpoint is hit or the graph completes"),
		),
		handleDebugResume(bridge),
	)

	s.AddTool(
		mcp.NewTool("debug_pause",
			mcp.WithDescription("Pause execution at the next node visit"),
		),
		handleDebugPause(bridge),
	)

	s.AddTool(
		mcp.NewTool("debug_set_breakpoint",
			mcp.WithDescription("Set a breakpoint at a node. Execution will pause when this node is visited."),
			mcp.WithString("node_id",
				mcp.Description("The full path or ID of the node to set a breakpoint on"),
				mcp.Required(),
			),
		),
		handleDebugSetBreakpoint(bridge),
	)

	s.AddTool(
		mcp.NewTool("debug_remove_breakpoint",
			mcp.WithDescription("Remove a previously set breakpoint from a node"),
			mcp.WithString("node_id",
				mcp.Description("The full path or ID of the node to remove the breakpoint from"),
				mcp.Required(),
			),
		),
		handleDebugRemoveBreakpoint(bridge),
	)

	s.AddTool(
		mcp.NewTool("debug_inspect",
			mcp.WithDescription("Return the last debug state including current node, visited nodes, and execution context (variables, outputs, etc.)"),
		),
		handleDebugInspect(bridge),
	)

	s.AddTool(
		mcp.NewTool("debug_logs",
			mcp.WithDescription("Return and clear buffered log messages (stdout, stderr, warnings) from the debug session"),
		),
		handleDebugLogs(bridge),
	)

	s.AddTool(
		mcp.NewTool("debug_stop",
			mcp.WithDescription("Stop the currently running graph execution"),
		),
		handleDebugStop(bridge),
	)

	s.AddTool(
		mcp.NewTool("debug_disconnect",
			mcp.WithDescription("Disconnect from the debug server and close the WebSocket connection"),
		),
		handleDebugDisconnect(bridge),
	)
}
