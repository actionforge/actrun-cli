package sessions

import (
	"bufio"
	"bytes"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
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

func RunSessionMode(configFile string, graphFileForDebugSession string, sessionToken string, configValueSource string) error {

	if graphFileForDebugSession != "" && sessionToken != "" {
		return errors.New("both createDebugSession and sessionToken cannot be set")
	}

	if graphFileForDebugSession == "" {
		PrintWelcomeMessage()
	}

	if configFile != "" {
		utils.LogOut.Infof("👉 Configs will be loaded from: %s\n", configFile)
		_, err := utils.LoadConfig(configFile)
		if err != nil {
			return fmt.Errorf("error loading config: %v", err) // fmt.Errorf doesn't strictly need \n if returned as error
		}
	} else {
		utils.LogOut.Info("No config file specified, config values will be derived from environment variables and flags")
	}

	apiGatewayUrl := GetGatewayURL()

	wsScheme := "wss"
	httpScheme := "https"
	if apiGatewayUrl == "localhost" || strings.HasPrefix(apiGatewayUrl, "localhost:") {
		wsScheme = "ws"
		httpScheme = "http"
	}

	var err error
	if graphFileForDebugSession != "" {
		sessionData, err := StartNewSession(httpScheme, apiGatewayUrl)
		if err != nil {
			return fmt.Errorf("error creating new debug session: %v", err)
		}
		sessionToken = sessionData.Token

		utils.LogOut.Infof("👉 Created new debug session for graph file: %s\n", graphFileForDebugSession)
		utils.LogOut.Infof("Debug Session: %s\n", fmt.Sprintf("%s//%s/graph#%s", httpScheme, APP_URL, ""))
	} else {
		sessionToken, err = GetSessionToken(sessionToken, configValueSource)
		if err != nil {
			return fmt.Errorf("error reading session token: %v", err)
		}
	}

	if sessionToken == "" {
		return fmt.Errorf("no session token provided, exiting.")
	}

	// token validation and parsing
	packet, err := base64.StdEncoding.DecodeString(sessionToken)
	if err != nil {
		return fmt.Errorf("invalid token string (not Base64): %v", err)
	}

	if len(packet) < 38 {
		return errors.New("invalid token (too short).")
	}

	expectedChecksum := packet[len(packet)-4:]
	dataPayload := packet[:len(packet)-4]

	idLength := int(packet[0])
	if idLength <= 0 || (1+idLength+32) > len(dataPayload) {
		return fmt.Errorf("invalid token (malformed structure).")
	}

	sessionIDBytes := packet[1 : 1+idLength]
	keyBytes := packet[1+idLength : 1+idLength+32]

	dataToHash := append([]byte{}, sessionIDBytes...)
	dataToHash = append(dataToHash, keyBytes...)

	hash := sha256.Sum256(dataToHash)
	calculatedChecksum := hash[:4]

	if !bytes.Equal(expectedChecksum, calculatedChecksum) {
		return fmt.Errorf("❌ INTEGRITY CHECK FAILED: The token appears to be modified or typo'd.\nCheck the last few characters")
	}

	sessionID := string(sessionIDBytes)
	sharedKey := base64.StdEncoding.EncodeToString(keyBytes)
	send := newEncryptedSender(sharedKey)

	uAddr := url.URL{Scheme: wsScheme, Host: apiGatewayUrl, Path: "/api/v2/ws/runner/" + sessionID}
	utils.LogOut.Info("Connecting to Actionforge\n")

	ws, resp, err := websocket.DefaultDialer.Dial(uAddr.String(), nil)
	if err != nil {
		if resp != nil {
			body, readErr := io.ReadAll(resp.Body)
			if readErr == nil {
				var errMsg map[string]string
				if json.Unmarshal(body, &errMsg) == nil && errMsg["error"] != "" {
					return fmt.Errorf("🚨 Error: %s", errMsg["error"])
				}
				return fmt.Errorf("handshake failed (Status %s): %s", resp.Status, string(body))
			}
			return fmt.Errorf("handshake failed: Server returned HTTP status: %s", resp.Status)
		}
		return fmt.Errorf("failed to connect to %v: %v", apiGatewayUrl, err)
	}
	defer ws.Close()

	utils.LogOut.Info("Successfully connected to your browser session. Waiting for commands...\n")

	done := make(chan os.Signal, 1)
	signal.Notify(done, syscall.SIGINT, syscall.SIGTERM)

	// if browser disconnects during a --create-debug-session run, we switch to detached mode
	// to ensure the graph finishes execution instead of hanging on a breakpoint.
	var detachMu sync.Mutex
	var detachedMode bool

	var ops debugOps

	// if browser disconnected override pause to ensure the graph finishes.
	// Its the same behaviour if you detach a debugger in an IDE.
	shouldSkipPause := func() bool {
		detachMu.Lock()
		defer detachMu.Unlock()
		return detachedMode
	}

	var onGraphComplete func()
	if graphFileForDebugSession != "" {
		onGraphComplete = func() {
			done <- syscall.SIGTERM
		}
	}

	// cli auto start logic
	if graphFileForDebugSession != "" {
		graphContent, err := os.ReadFile(graphFileForDebugSession)
		if err != nil {
			return fmt.Errorf("failed to read debug graph file: %v", err)
		}

		go func() {
			graphContentBase64 := base64.URLEncoding.EncodeToString(graphContent)

			fragmentParams := url.Values{}
			fragmentParams.Set("graph", graphContentBase64)
			fragmentParams.Set("session_token", sessionToken)

			fragmentString := fragmentParams.Encode()

			utils.LogOut.Infof("👉 Debug Session: %s\n", fmt.Sprintf("%s://%s/graph#%s", httpScheme, APP_URL, fragmentString))

			// Force StartPaused = true
			triggerGraphExecution(&ops, ws, send, configFile, string(graphContent), nil, nil, nil, nil, true, false, shouldSkipPause, onGraphComplete)
		}()
	}

	// this is the main message loop
	go func() {
		defer func() {
			if r := recover(); r != nil {
				utils.LogOut.Errorf("recovered from panic in message loop: %v\n%s", r, debug.Stack())
			}
			done <- syscall.SIGTERM
		}()

		for {
			var rawMsg EncryptedMessage
			err := ws.ReadJSON(&rawMsg)
			if err != nil {
				if websocket.IsCloseError(err, websocket.CloseNormalClosure) {
					utils.LogOut.Debug("server closed connection cleanly.\n")
				} else if strings.Contains(err.Error(), "use of closed network connection") {
					// TODO: (Seb) check if there is a better way to handle this
					// We reach this when the session shuts down and closes the socket
					// while this loop is still waiting for a read. We just ignore it as
					// its not really a bug
				} else {
					utils.LogOut.Warnf("WebSocket Error: %v\n", err)
				}
				break
			}

			if rawMsg.Type == MsgTypeControl {
				utils.LogOut.Debugf("received control message: %s\n", rawMsg.Payload)

				switch rawMsg.Payload {
				case ControlBrowserDisconnected:
					utils.LogOut.Debug("browser disconnected (waiting for reconnect...)\n")

					// if browser disconnected override pause to ensure the graph finishes
					// its the same behaviour if you detach a debugger in an IDE
					if graphFileForDebugSession != "" {
						utils.LogOut.Debug("debug session detected: Resuming graph to completion...\n")
						detachMu.Lock()
						detachedMode = true
						detachMu.Unlock()

						ops.dispatch(MsgTypeDebugResume, "")
					}

				case ControlBrowserConnected:
					utils.LogOut.Debug("browser connected. Checking for active debug state...\n")
					ops.Lock()
					if ops.cachedState != nil {
						utils.LogOut.Debug("resending execution state to new browser connection...\n")
						go send(ws, ops.cachedState)
					}
					ops.Unlock()
				}

				continue
			}

			if rawMsg.Type != MsgTypeData {
				utils.LogOut.Warnf("Received non-data message type, ignoring: %v\n", rawMsg.Type)
				continue
			}

			decryptedJSON, err := decryptData(rawMsg.Payload, sharedKey)
			if err != nil {
				utils.LogOut.Errorf("dECRYPTION FAILED: %v", err)
				send(ws, map[string]string{
					"type":  MsgTypeJobError,
					"error": "Decryption failed. Check your key.",
				})
				continue
			}

			var payload DecryptedPayload
			if err := json.Unmarshal([]byte(decryptedJSON), &payload); err != nil {
				utils.LogOut.Warnf("Failed to parse decrypted JSON: %v\n", err)
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
					shouldSkipPause,
					onGraphComplete,
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
	}()

	<-done
	utils.LogOut.Debug("shutting down runtime...\n")

	wsWriteMutex.Lock()
	_ = ws.WriteMessage(websocket.CloseMessage, websocket.FormatCloseMessage(websocket.CloseNormalClosure, ""))
	wsWriteMutex.Unlock()

	return nil
}

// GetSessionToken waits for the user to paste a token into standard input,
// reads it, trims it, and returns it.
// It returns the token (string) and any error encountered during reading.
func GetSessionToken(sessionToken string, configValueSource string) (string, error) {
	fmt.Println()
	fmt.Print("🔑 Enter session token: ")

	if sessionToken != "" {
		fmt.Printf("<session token was provided via %s>\n\n", configValueSource)
		return sessionToken, nil
	}

	for {

		scanner := bufio.NewScanner(os.Stdin)

		if scanner.Scan() {
			token := strings.TrimSpace(scanner.Text())

			if token == "" || strings.EqualFold(token, "exit") || strings.EqualFold(token, "quit") {
				return "", nil
			}

			if len(token) < 16 {
				fmt.Print("  Warning: That doesn't look like a valid session token. Please try again or type 'exit' to quit.\n")
				fmt.Print("🔑 Enter session token: ")
				continue
			}

			return token, nil
		}

		if err := scanner.Err(); err != nil {
			return "", err
		}

		return "", nil
	}
}

func PrintWelcomeMessage() {
	welcomeText := `Welcome to your Actionforge Runner

----------------------[ HOW TO RUN ]----------------------

[ 🚀 OPTION 1: RUN LOCAL ACTION GRAPH ]
    Execute a local graph file directly from your terminal.
    Example: $ actrun my-graph.act

[ 🔗 OPTION 2: CONNECT TO WEB APP ]
    Please paste the session token from your browser to connect.

----------------------------------------------------------

📖 Docs: https://docs.actionforge.dev

`

	// Print the message to standard output.
	// We use fmt.Print here instead of Println to avoid adding an extra
	// newline at the very end, keeping the cursor right after the prompt.
	fmt.Print(welcomeText)
}
