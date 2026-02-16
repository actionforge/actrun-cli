package mcpdebug

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

func textResult(text string) *mcp.CallToolResult {
	return &mcp.CallToolResult{
		Content: []mcp.Content{
			mcp.TextContent{
				Type: "text",
				Text: text,
			},
		},
	}
}

func errorResult(msg string) *mcp.CallToolResult {
	return &mcp.CallToolResult{
		Content: []mcp.Content{
			mcp.TextContent{
				Type: "text",
				Text: msg,
			},
		},
		IsError: true,
	}
}

func jsonResult(v any) *mcp.CallToolResult {
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return errorResult(fmt.Sprintf("failed to marshal result: %v", err))
	}
	return textResult(string(data))
}

// blockingResponse builds the standard response for blocking tools.
func blockingResponse(msg *IncomingMessage, logs []LogEntry) *mcp.CallToolResult {
	status := "unknown"
	switch msg.Type {
	case "debug_state":
		status = "paused"
	case "job_finished":
		status = "finished"
	case "job_error":
		status = "error"
	}

	resp := map[string]any{
		"status": status,
	}
	if msg.FullPath != "" {
		resp["current_node"] = msg.FullPath
	}
	if msg.ExecutionContext != nil {
		resp["execution_context"] = msg.ExecutionContext
	}
	if msg.Error != "" {
		resp["error"] = msg.Error
	}
	if len(logs) > 0 {
		resp["logs"] = logs
	}

	return jsonResult(resp)
}

func requireConnected(b *Bridge) error {
	if !b.Connected() {
		return fmt.Errorf("not connected to debug session — call debug_connect first")
	}
	return nil
}

// handleDebugConnect connects to a running local debug server.
func handleDebugConnect(b *Bridge) server.ToolHandlerFunc {
	return func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		port, err := req.RequireInt("port")
		if err != nil {
			return errorResult("missing required parameter: port"), nil
		}

		if err := b.Connect(port); err != nil {
			return errorResult(fmt.Sprintf("connect failed: %v", err)), nil
		}
		return textResult(fmt.Sprintf("Connected to debug server on port %d", port)), nil
	}
}

// handleDebugRun sends a run command with graph content and waits for pause/finish.
func handleDebugRun(b *Bridge) server.ToolHandlerFunc {
	return func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		if err := requireConnected(b); err != nil {
			return errorResult(err.Error()), nil
		}

		graph, err := req.RequireString("graph")
		if err != nil {
			return errorResult("missing required parameter: graph"), nil
		}

		startPaused := req.GetBool("start_paused", true)
		breakpoints := req.GetStringSlice("breakpoints", nil)

		payload := map[string]any{
			"type":         "run",
			"payload":      graph,
			"start_paused": startPaused,
		}
		if len(breakpoints) > 0 {
			payload["breakpoints"] = breakpoints
		}

		msg, logs, err := b.SendAndWait(payload, 120*time.Second)
		if err != nil {
			return errorResult(fmt.Sprintf("run failed: %v", err)), nil
		}
		return blockingResponse(msg, logs), nil
	}
}

// handleDebugStep sends a step-over command and waits.
func handleDebugStep(b *Bridge) server.ToolHandlerFunc {
	return func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		if err := requireConnected(b); err != nil {
			return errorResult(err.Error()), nil
		}
		msg, logs, err := b.SendAndWait(map[string]string{"type": "debug_step"}, 60*time.Second)
		if err != nil {
			return errorResult(fmt.Sprintf("step failed: %v", err)), nil
		}
		return blockingResponse(msg, logs), nil
	}
}

// handleDebugStepInto sends a step-into command and waits.
func handleDebugStepInto(b *Bridge) server.ToolHandlerFunc {
	return func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		if err := requireConnected(b); err != nil {
			return errorResult(err.Error()), nil
		}
		msg, logs, err := b.SendAndWait(map[string]string{"type": "debug_step_into"}, 60*time.Second)
		if err != nil {
			return errorResult(fmt.Sprintf("step into failed: %v", err)), nil
		}
		return blockingResponse(msg, logs), nil
	}
}

// handleDebugStepOut sends a step-out command and waits.
func handleDebugStepOut(b *Bridge) server.ToolHandlerFunc {
	return func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		if err := requireConnected(b); err != nil {
			return errorResult(err.Error()), nil
		}
		msg, logs, err := b.SendAndWait(map[string]string{"type": "debug_step_out"}, 60*time.Second)
		if err != nil {
			return errorResult(fmt.Sprintf("step out failed: %v", err)), nil
		}
		return blockingResponse(msg, logs), nil
	}
}

// handleDebugResume sends a resume command and waits for next pause/finish.
func handleDebugResume(b *Bridge) server.ToolHandlerFunc {
	return func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		if err := requireConnected(b); err != nil {
			return errorResult(err.Error()), nil
		}
		msg, logs, err := b.SendAndWait(map[string]string{"type": "debug_resume"}, 120*time.Second)
		if err != nil {
			return errorResult(fmt.Sprintf("resume failed: %v", err)), nil
		}
		return blockingResponse(msg, logs), nil
	}
}

// handleDebugPause sends a pause command (fire-and-forget).
func handleDebugPause(b *Bridge) server.ToolHandlerFunc {
	return func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		if err := requireConnected(b); err != nil {
			return errorResult(err.Error()), nil
		}
		if err := b.Send(map[string]string{"type": "debug_pause"}); err != nil {
			return errorResult(fmt.Sprintf("pause failed: %v", err)), nil
		}
		return textResult("Pause signal sent"), nil
	}
}

// handleDebugSetBreakpoint adds a breakpoint at a node.
func handleDebugSetBreakpoint(b *Bridge) server.ToolHandlerFunc {
	return func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		if err := requireConnected(b); err != nil {
			return errorResult(err.Error()), nil
		}
		nodeID, err := req.RequireString("node_id")
		if err != nil {
			return errorResult("missing required parameter: node_id"), nil
		}
		if err := b.Send(map[string]string{"type": "debug_add_breakpoint", "nodeId": nodeID}); err != nil {
			return errorResult(fmt.Sprintf("set breakpoint failed: %v", err)), nil
		}
		return textResult(fmt.Sprintf("Breakpoint set at %s", nodeID)), nil
	}
}

// handleDebugRemoveBreakpoint removes a breakpoint from a node.
func handleDebugRemoveBreakpoint(b *Bridge) server.ToolHandlerFunc {
	return func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		if err := requireConnected(b); err != nil {
			return errorResult(err.Error()), nil
		}
		nodeID, err := req.RequireString("node_id")
		if err != nil {
			return errorResult("missing required parameter: node_id"), nil
		}
		if err := b.Send(map[string]string{"type": "debug_remove_breakpoint", "nodeId": nodeID}); err != nil {
			return errorResult(fmt.Sprintf("remove breakpoint failed: %v", err)), nil
		}
		return textResult(fmt.Sprintf("Breakpoint removed from %s", nodeID)), nil
	}
}

// handleDebugInspect returns the last debug state.
func handleDebugInspect(b *Bridge) server.ToolHandlerFunc {
	return func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		if err := requireConnected(b); err != nil {
			return errorResult(err.Error()), nil
		}
		state := b.LastState()
		if state == nil {
			return textResult("No debug state available yet — the graph may not have paused."), nil
		}
		return jsonResult(state), nil
	}
}

// handleDebugLogs drains and returns buffered log entries.
func handleDebugLogs(b *Bridge) server.ToolHandlerFunc {
	return func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		if err := requireConnected(b); err != nil {
			return errorResult(err.Error()), nil
		}
		logs := b.DrainLogs()
		if len(logs) == 0 {
			return textResult("No new log entries."), nil
		}
		return jsonResult(logs), nil
	}
}

// handleDebugStop sends a stop command (fire-and-forget).
func handleDebugStop(b *Bridge) server.ToolHandlerFunc {
	return func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		if err := requireConnected(b); err != nil {
			return errorResult(err.Error()), nil
		}
		if err := b.Send(map[string]string{"type": "stop"}); err != nil {
			return errorResult(fmt.Sprintf("stop failed: %v", err)), nil
		}
		return textResult("Stop signal sent"), nil
	}
}

// handleDebugDisconnect closes the WebSocket connection.
func handleDebugDisconnect(b *Bridge) server.ToolHandlerFunc {
	return func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		if err := b.Disconnect(); err != nil {
			return errorResult(fmt.Sprintf("disconnect failed: %v", err)), nil
		}
		return textResult("Disconnected from debug server"), nil
	}
}
