package sessions

import (
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/signal"
	"runtime/debug"
	"strings"
	"sync"
	"syscall"

	"github.com/actionforge/actrun-cli/build"
	"github.com/actionforge/actrun-cli/utils"
	"github.com/gorilla/websocket"
)

var localUpgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool {
		return true // Accept all origins for local connections
	},
}

// RunLocalMode starts a local WebSocket server for direct editor connection (no gateway).
func RunLocalMode(configFile string) error {

	if configFile != "" {
		utils.LogOut.Infof("👉 Configs will be loaded from: %s\n", configFile)
		_, err := utils.LoadConfig(configFile)
		if err != nil {
			return fmt.Errorf("error loading config: %v", err)
		}
	} else {
		utils.LogOut.Info("No config file specified, config values will be derived from environment variables and flags")
	}

	send := newPlainSender()

	// listen on a random available port on localhost
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return fmt.Errorf("failed to start local server: %v", err)
	}
	port := listener.Addr().(*net.TCPAddr).Port

	// print the port for the VS Code extension to capture
	fmt.Printf("LOCAL_WS_PORT=%d\n", port)

	done := make(chan os.Signal, 1)
	signal.Notify(done, syscall.SIGINT, syscall.SIGTERM)

	var wsConn *websocket.Conn
	var wsConnMu sync.Mutex

	var ops debugOps

	mux := http.NewServeMux()
	mux.HandleFunc("/ws", func(w http.ResponseWriter, r *http.Request) {
		ws, err := localUpgrader.Upgrade(w, r, nil)
		if err != nil {
			utils.LogOut.Errorf("failed to upgrade local WebSocket connection: %v\n", err)
			return
		}

		wsConnMu.Lock()
		if wsConn != nil {
			wsConnMu.Unlock()
			_ = ws.WriteMessage(websocket.CloseMessage, websocket.FormatCloseMessage(
				websocket.ClosePolicyViolation,
				"Another client is already connected.",
			))
			ws.Close()
			return
		}
		wsConn = ws
		wsConnMu.Unlock()

		utils.LogOut.Info("Editor connected via local WebSocket.\n")

		send(ws, map[string]string{
			"type":    MsgTypeControl,
			"message": "runner_connected",
			"address": "127.0.0.1",
		})

		defer func() {
			if r := recover(); r != nil {
				utils.LogOut.Errorf("recovered from panic in local message loop: %v\n%s", r, debug.Stack())
			}
			wsConnMu.Lock()
			wsConn = nil
			wsConnMu.Unlock()
			ws.Close()
			done <- syscall.SIGTERM
		}()

		for {
			_, msgBytes, err := ws.ReadMessage()
			if err != nil {
				if websocket.IsCloseError(err, websocket.CloseNormalClosure, websocket.CloseGoingAway) {
					utils.LogOut.Debug("editor closed connection cleanly.\n")
				} else if !strings.Contains(err.Error(), "use of closed network connection") {
					utils.LogOut.Warnf("local WebSocket error: %v\n", err)
				}
				break
			}

			var payload DecryptedPayload
			if err := json.Unmarshal(msgBytes, &payload); err != nil {
				utils.LogOut.Warnf("failed to parse JSON from editor: %v\n", err)
				continue
			}

			currentVer := build.Version
			if isVersionOutdated(currentVer, payload.RequiredVersion) {
				utils.LogOut.Warnf("Runner version %s is older than required %s\n", currentVer, payload.RequiredVersion)
				send(ws, map[string]string{
					"type":    MsgTypeWarning,
					"message": fmt.Sprintf("WARNING: Runner version %s is older than required %s", currentVer, payload.RequiredVersion),
				})
			}

			switch payload.Type {

			case MsgTypeRun:
				triggerGraphExecution(
					&ops, ws, send, configFile,
					payload.Payload,
					payload.Secrets,
					payload.Inputs,
					payload.Env,
					payload.Breakpoints,
					payload.StartPaused,
					payload.IgnoreBreakpoints,
					nil, nil,
				)

			case MsgTypeStop:
				utils.LogOut.Debug("received stop signal\n")
				send(ws, map[string]string{
					"type":    MsgTypeLog,
					"message": "Stop signal received. Attempting to cancel...",
				})
				ops.cancelAndResume()

			case MsgTypeDebugStep, MsgTypeDebugStepInto, MsgTypeDebugStepOut,
				MsgTypeDebugPause, MsgTypeDebugResume,
				MsgTypeDebugAddBreakpoint, MsgTypeDebugRemoveBreakpoint:
				ops.dispatch(payload.Type, payload.NodeID)

			default:
				utils.LogOut.Debugf("unknown command type: %s\n", payload.Type)
			}
		}
	})

	server := &http.Server{Handler: mux}
	go func() {
		if err := server.Serve(listener); err != nil && err != http.ErrServerClosed {
			utils.LogOut.Errorf("local HTTP server error: %v\n", err)
			done <- syscall.SIGTERM
		}
	}()

	<-done
	utils.LogOut.Debug("shutting down local runner...\n")

	wsConnMu.Lock()
	if wsConn != nil {
		wsWriteMutex.Lock()
		_ = wsConn.WriteMessage(websocket.CloseMessage, websocket.FormatCloseMessage(websocket.CloseNormalClosure, ""))
		wsWriteMutex.Unlock()
		wsConn.Close()
	}
	wsConnMu.Unlock()

	server.Close()

	return nil
}
