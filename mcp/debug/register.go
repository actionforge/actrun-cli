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
			mcp.WithDescription(
				"Connect to an actrun local debug server via WebSocket. "+
					"This is the entry point for debugging Actionforge graph (.act) files. "+
					"The entire debug flow is automated — the user provides an .act file path and optional flags, you handle the rest. "+
					"\n\nWorkflow: "+
					"(1) Start 'actrun --local' in the background with NO file argument and capture LOCAL_WS_PORT from stdout. "+
					"Pass any user-provided flags (e.g. --env-file <path>, --concurrency <bool>, --local-gh-server) to this command. "+
					"Run 'actrun --help' if you need to discover available flags. "+
					"(2) Call debug_connect with the captured port. "+
					"(3) Read the .act file from disk and pass its YAML to debug_run to start execution. "+
					"(4) Use debug_step / debug_step_into / debug_resume to walk through nodes. "+
					"(5) Call debug_disconnect when done, then kill the background actrun process. "+
					"\n\nSource code: https://github.com/actionforge/actrun-cli (see CLAUDE.md for project structure)."),
			mcp.WithNumber("port",
				mcp.Description("The LOCAL_WS_PORT printed by 'actrun --local' on startup."),
				mcp.Required(),
			),
		),
		handleDebugConnect(bridge),
	)

	s.AddTool(
		mcp.NewTool("debug_run",
			mcp.WithDescription(
				"Send a graph to the debug server and start execution. "+
					"Read the .act file from disk and pass its full YAML content as the 'graph' parameter. "+
					"The server must have been started with 'actrun --local' (no file argument) — this tool sends the graph over the debug protocol. "+
					"Blocks until the graph pauses (if start_paused is true) or completes."),
			mcp.WithString("graph",
				mcp.Description("The full YAML content of the .act graph file. Read the file from disk and pass the contents verbatim. Do NOT fabricate or modify the YAML."),
				mcp.Required(),
			),
			mcp.WithArray("breakpoints",
				mcp.Description("Optional list of node IDs to set as breakpoints before execution. Node IDs can be found in the 'nodes[].id' fields of the .act YAML."),
			),
			mcp.WithBoolean("start_paused",
				mcp.Description("Whether to pause at the first node (default: true). Set to false to run until a breakpoint or completion."),
			),
		),
		handleDebugRun(bridge),
	)

	s.AddTool(
		mcp.NewTool("debug_step",
			mcp.WithDescription("Step over: execute the current node and pause at the next node at the same depth or shallower. Use this to walk through nodes sequentially without entering group nodes."),
		),
		handleDebugStep(bridge),
	)

	s.AddTool(
		mcp.NewTool("debug_step_into",
			mcp.WithDescription("Step into: if the current node is a group, pause at the first node inside it; otherwise behaves like step. Use this to inspect execution within group nodes."),
		),
		handleDebugStepInto(bridge),
	)

	s.AddTool(
		mcp.NewTool("debug_step_out",
			mcp.WithDescription("Step out: resume execution and pause when returning to a shallower depth (parent group). Use this to exit a group node and return to the parent graph level."),
		),
		handleDebugStepOut(bridge),
	)

	s.AddTool(
		mcp.NewTool("debug_resume",
			mcp.WithDescription("Resume execution until the next breakpoint is hit or the graph completes. Use this to skip ahead when you don't need to inspect every node."),
		),
		handleDebugResume(bridge),
	)

	s.AddTool(
		mcp.NewTool("debug_pause",
			mcp.WithDescription("Pause execution at the next node visit. Use this after debug_resume if you want to stop and inspect again."),
		),
		handleDebugPause(bridge),
	)

	s.AddTool(
		mcp.NewTool("debug_set_breakpoint",
			mcp.WithDescription("Set a breakpoint at a node. Execution will pause when this node is visited. Node IDs are the 'id' fields from the .act file's nodes section. For nodes inside groups, use the full path (e.g. 'group-id/node-id')."),
			mcp.WithString("node_id",
				mcp.Description("The full path or ID of the node to set a breakpoint on."),
				mcp.Required(),
			),
		),
		handleDebugSetBreakpoint(bridge),
	)

	s.AddTool(
		mcp.NewTool("debug_remove_breakpoint",
			mcp.WithDescription("Remove a previously set breakpoint from a node."),
			mcp.WithString("node_id",
				mcp.Description("The full path or ID of the node to remove the breakpoint from."),
				mcp.Required(),
			),
		),
		handleDebugRemoveBreakpoint(bridge),
	)

	s.AddTool(
		mcp.NewTool("debug_inspect",
			mcp.WithDescription("Return the last debug state including current node, visited nodes, and execution context (variables, outputs, caches). Use this to examine state without advancing execution."),
		),
		handleDebugInspect(bridge),
	)

	s.AddTool(
		mcp.NewTool("debug_logs",
			mcp.WithDescription("Return and clear buffered log messages (stdout, stderr, warnings) from the debug session. Logs accumulate between calls, so call this periodically to see output from executed nodes."),
		),
		handleDebugLogs(bridge),
	)

	s.AddTool(
		mcp.NewTool("debug_stop",
			mcp.WithDescription("Stop the currently running graph execution. The graph will be cancelled and the debug session returns to idle. You can start a new execution with debug_run afterwards."),
		),
		handleDebugStop(bridge),
	)

	s.AddTool(
		mcp.NewTool("debug_disconnect",
			mcp.WithDescription("Disconnect from the debug server and close the WebSocket connection. Call this when done debugging. After disconnecting, kill the background 'actrun --local' process to clean up."),
		),
		handleDebugDisconnect(bridge),
	)
}
