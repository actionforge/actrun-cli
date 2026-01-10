package sessions

import (
	"bufio"
	"bytes"
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
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
	"time"

	"github.com/Masterminds/semver/v3"
	"github.com/actionforge/actrun-cli/build"
	"github.com/actionforge/actrun-cli/core"
	"github.com/actionforge/actrun-cli/utils"
	"github.com/gorilla/websocket"
)

var wsWriteMutex sync.Mutex

const (
	// Message Types (from browser)
	MsgTypeRun                   = "run"
	MsgTypeStop                  = "stop"
	MsgTypeDebugPause            = "debug_pause"
	MsgTypeDebugResume           = "debug_resume"
	MsgTypeDebugStep             = "debug_step"
	MsgTypeDebugAddBreakpoint    = "debug_add_breakpoint"
	MsgTypeDebugRemoveBreakpoint = "debug_remove_breakpoint"
	MsgTypeDebugStepInto         = "debug_step_into"
	MsgTypeDebugStepOut          = "debug_step_out"

	// Message Types (to browser)
	MsgTypeLog         = "log"
	MsgTypeLogError    = "log_error"
	MsgTypeJobFinished = "job_finished"
	MsgTypeJobError    = "job_error"
	MsgTypeDebugState  = "debug_state"
	MsgTypeWarning     = "warning"

	// Wrapper/Control Message Types (not E2E encrypted)
	MsgTypeData    = "data"    // Wrapper for E2E encrypted payloads
	MsgTypeControl = "control" // Server-to-runner control messages

	// Control Message Payloads
	ControlBrowserDisconnected = "browser_disconnected"
	ControlBrowserConnected    = "browser_connected"
)

func encryptData(plaintext string, base64Key string) (string, error) {
	key, err := base64.StdEncoding.DecodeString(base64Key)
	if err != nil {
		return "", errors.New("failed to decode base64 key")
	}
	if len(key) != 32 {
		return "", errors.New("invalid key length: must be 32 bytes (AES-256)")
	}

	block, err := aes.NewCipher(key)
	if err != nil {
		return "", err
	}

	aesgcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}

	nonce := make([]byte, aesgcm.NonceSize()) // NonceSize() is 12 bytes for AES-GCM
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return "", err
	}

	// Encrypt the data (nil prefix means append to nonce)
	ciphertext := aesgcm.Seal(nil, nonce, []byte(plaintext), nil)

	ivAndCiphertext := append(nonce, ciphertext...)

	return base64.StdEncoding.EncodeToString(ivAndCiphertext), nil
}

func sendEncryptedJSON(ws *websocket.Conn, payload any, sharedKey string) {
	jsonPayload, err := json.Marshal(payload)
	if err != nil {
		utils.LogOut.Errorf("failed to marshal outgoing JSON: %v\n", err)
		return
	}

	encryptedPayload, err := encryptData(string(jsonPayload), sharedKey)
	if err != nil {
		utils.LogOut.Errorf("failed to encrypt outgoing message: %v\n", err)
		return
	}

	msg := EncryptedMessage{
		Type:    MsgTypeData,
		Payload: encryptedPayload,
	}

	wsWriteMutex.Lock()
	defer wsWriteMutex.Unlock()

	if err := ws.SetWriteDeadline(time.Now().Add(10 * time.Second)); err != nil {
		utils.LogOut.Errorf("failed to set write deadline (connection likely closed): %v\n", err)
		return
	}

	if err := ws.WriteJSON(msg); err != nil {
		utils.LogOut.Errorf("failed to send encrypted message: %v\n", err)
	}
}

// EncryptedMessage is the raw message received from the WebSocket
type EncryptedMessage struct {
	Type    string `json:"type"`
	Payload string `json:"payload"` // Base64-encoded (IV + Ciphertext)
}

// DecryptedPayload is the structure of the data *after* decryption
type DecryptedPayload struct {
	Type              string            `json:"type"`
	Payload           string            `json:"payload"` // The graph JSON (if type is "run")
	Secrets           map[string]string `json:"secrets"`
	Inputs            map[string]any    `json:"inputs"`
	Env               map[string]string `json:"env"`
	IgnoreBreakpoints bool              `json:"ignore_breakpoints"`
	StartPaused       bool              `json:"start_paused"`
	Breakpoints       []string          `json:"breakpoints"`
	RequiredVersion   string            `json:"required_version"`
	NodeID            string            `json:"nodeId"`
}

// Global State
var (
	// Use a channel to signal that a graph is currently running
	graphRunning = make(chan bool, 1)
	// Mutex to protect access to the cancel function
	cancelLock sync.Mutex
	// Holds the cancel function for the *current* running graph
	currentGraphCancel context.CancelFunc
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

	// if browser disconnects during a --create_debug_session run, we switch to detached mode
	// to ensure the graph finishes execution instead of hanging on a breakpoint.
	var detachMu sync.Mutex
	var detachedMode bool

	// current debug op state
	var currentDebugOps struct {
		sync.Mutex
		pause            func()
		resume           func()
		step             func()
		stepInto         func()
		stepOut          func()
		addBreakpoint    func(string)
		removeBreakpoint func(string)
		cachedState      any
	}

	triggerGraphExecution := func(
		graphPayload string,
		secrets map[string]string,
		inputs map[string]any,
		env map[string]string,
		breakpoints []string,
		startPaused bool,
		ignoreBreakpoints bool,
	) {
		select {
		case graphRunning <- true:
			ctx, cancel := context.WithCancel(context.Background())

			cancelLock.Lock()
			currentGraphCancel = cancel
			cancelLock.Unlock()

			var debugMu sync.Mutex
			debugCond := sync.NewCond(&debugMu)

			var bpMutex sync.RWMutex
			activeBreakpoints := make(map[string]bool)

			type StepMode int
			const (
				StepRun StepMode = iota
				StepOver
				StepInto
				StepOut
			)

			currentStepMode := StepRun
			stepReferenceDepth := 0

			if len(breakpoints) > 0 {
				bpMutex.Lock()
				for _, bp := range breakpoints {
					activeBreakpoints[bp] = true
				}
				bpMutex.Unlock()
			}

			isPaused := startPaused

			// Setup control functions
			currentDebugOps.Lock()

			currentDebugOps.pause = func() {
				debugMu.Lock()
				isPaused = true
				currentStepMode = StepRun
				debugMu.Unlock()
				utils.LogOut.Debug("pausing execution...\n")
			}

			currentDebugOps.resume = func() {
				debugMu.Lock()
				isPaused = false
				currentStepMode = StepRun
				currentDebugOps.Lock()
				currentDebugOps.cachedState = nil
				currentDebugOps.Unlock()
				debugCond.Broadcast()
				debugMu.Unlock()
				utils.LogOut.Debug("resuming execution...\n")
			}

			currentDebugOps.step = func() {
				debugMu.Lock()
				currentStepMode = StepOver
				debugMu.Unlock()
				currentDebugOps.Lock()
				currentDebugOps.cachedState = nil
				currentDebugOps.Unlock()
				debugMu.Lock()
				debugCond.Signal()
				debugMu.Unlock()
				utils.LogOut.Debug("stepping Over...\n")
			}

			currentDebugOps.stepInto = func() {
				debugMu.Lock()
				currentStepMode = StepInto
				debugMu.Unlock()
				currentDebugOps.Lock()
				currentDebugOps.cachedState = nil
				currentDebugOps.Unlock()
				debugMu.Lock()
				debugCond.Signal()
				debugMu.Unlock()
				utils.LogOut.Debug("stepping Into...\n")
			}

			currentDebugOps.stepOut = func() {
				debugMu.Lock()
				currentStepMode = StepOut
				debugMu.Unlock()
				currentDebugOps.Lock()
				currentDebugOps.cachedState = nil
				currentDebugOps.Unlock()
				debugMu.Lock()
				debugCond.Signal()
				debugMu.Unlock()
				utils.LogOut.Debug("stepping Out...\n")
			}

			currentDebugOps.addBreakpoint = func(nodeId string) {
				bpMutex.Lock()
				activeBreakpoints[nodeId] = true
				bpMutex.Unlock()
				utils.LogOut.Debugf("breakpoint added at %s\n", nodeId)
			}

			currentDebugOps.removeBreakpoint = func(nodeId string) {
				bpMutex.Lock()
				delete(activeBreakpoints, nodeId)
				bpMutex.Unlock()
				utils.LogOut.Debugf("breakpoint removed at %s\n", nodeId)
			}
			currentDebugOps.Unlock()

			lastKnownDepth := 0

			debugCb := func(ec *core.ExecutionState, nodeVisit core.ContextVisit) {
				fullPath := nodeVisit.Node.GetFullPath()
				currentDepth := calculateGraphDepth(fullPath)
				utils.LogOut.Debugf("visiting %s | Paused: %v\n", fullPath, isPaused)

				bpMutex.RLock()
				hasBreakpoint := activeBreakpoints[fullPath]
				bpMutex.RUnlock()

				debugMu.Lock()

				if hasBreakpoint {
					utils.LogOut.Debugf("hit explicit breakpoint at %s\n", fullPath)
					isPaused = true
					currentStepMode = StepRun
				} else if !isPaused {
					switch currentStepMode {
					case StepInto:
						isPaused = true
						currentStepMode = StepRun
					case StepOver:
						if currentDepth <= stepReferenceDepth {
							isPaused = true
							currentStepMode = StepRun
						}
					case StepOut:
						if currentDepth < stepReferenceDepth {
							isPaused = true
							currentStepMode = StepRun
						}
					}
				}

				if isPaused {
					lastKnownDepth = currentDepth
				}

				// if browser disconnected override pause to ensure the graph finishes
				// Its the same behaviour if you detach a debugger in an IDE
				detachMu.Lock()
				if detachedMode {
					isPaused = false
				}
				detachMu.Unlock()

				if isPaused {
					utils.LogOut.Infof("debugging paused at node: %s\n", fullPath)

					var rootEc *core.ExecutionState = ec
					for rootEc.ParentExecution != nil {
						rootEc = rootEc.ParentExecution
					}

					debugState := map[string]any{
						"type":             MsgTypeDebugState,
						"fullPath":         fullPath,
						"executionContext": *rootEc,
					}

					go sendEncryptedJSON(ws, debugState, sharedKey)

					currentDebugOps.Lock()
					currentDebugOps.cachedState = debugState
					currentDebugOps.Unlock()

					debugCond.Wait()

					stepReferenceDepth = lastKnownDepth
					isPaused = false
				}

				debugMu.Unlock()
			}

			if ignoreBreakpoints {
				activeBreakpoints = make(map[string]bool)
				debugCb = nil
			}

			go func() {
				runGraphFromConn(ctx, graphPayload, core.RunOpts{
					ConfigFile:      configFile,
					OverrideSecrets: secrets,
					OverrideInputs:  inputs,
					OverrideEnv:     env,
					Args:            []string{},
				}, ws, sharedKey, debugCb)

				// Cleanup
				currentDebugOps.Lock()
				currentDebugOps.pause = nil
				currentDebugOps.resume = nil
				currentDebugOps.step = nil
				currentDebugOps.stepInto = nil
				currentDebugOps.stepOut = nil
				currentDebugOps.addBreakpoint = nil
				currentDebugOps.removeBreakpoint = nil
				currentDebugOps.cachedState = nil
				currentDebugOps.Unlock()

				// if this was a one-off debug session (initiated by --create_debug_session), exit the process when graph completes
				if graphFileForDebugSession != "" {
					done <- syscall.SIGTERM
				}
			}()

		default:
			utils.LogOut.Warn("Cannot run graph: another graph is already in progress.\n")
			sendEncryptedJSON(ws, map[string]string{
				"type":  MsgTypeJobError,
				"error": "A graph is already running.",
			}, sharedKey)
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
			triggerGraphExecution(string(graphContent), nil, nil, nil, nil, true, false)
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

						currentDebugOps.Lock()
						resumeFn := currentDebugOps.resume
						currentDebugOps.Unlock()

						if resumeFn != nil {
							resumeFn()
						}
					}

				case ControlBrowserConnected:
					utils.LogOut.Debug("browser connected. Checking for active debug state...\n")
					currentDebugOps.Lock()
					if currentDebugOps.cachedState != nil {
						utils.LogOut.Debug("resending execution state to new browser connection...\n")
						go sendEncryptedJSON(ws, currentDebugOps.cachedState, sharedKey)
					}
					currentDebugOps.Unlock()
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
				sendEncryptedJSON(ws, map[string]string{
					"type":  MsgTypeJobError,
					"error": "Decryption failed. Check your key.",
				}, sharedKey)
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
				sendEncryptedJSON(ws, map[string]string{
					"type":    MsgTypeWarning,
					"message": fmt.Sprintf("WARNING: Runner version %s is older than required %s", currentVer, payload.RequiredVersion),
				}, sharedKey)
			}

			switch payload.Type {

			case MsgTypeRun:
				triggerGraphExecution(
					payload.Payload,
					payload.Secrets,
					payload.Inputs,
					payload.Env,
					payload.Breakpoints,
					payload.StartPaused,
					payload.IgnoreBreakpoints,
				)

			case MsgTypeStop:
				utils.LogOut.Debug("received stop signal\n")
				sendEncryptedJSON(ws, map[string]string{
					"type":    MsgTypeLog,
					"message": "Stop signal received. Attempting to cancel...",
				}, sharedKey)

				cancelLock.Lock()
				if currentGraphCancel != nil {
					currentGraphCancel()
				}
				cancelLock.Unlock()

				currentDebugOps.Lock()
				resumeFn := currentDebugOps.resume
				currentDebugOps.Unlock()

				if resumeFn != nil {
					resumeFn()
				}

			case MsgTypeDebugStep:
				currentDebugOps.Lock()
				stepFn := currentDebugOps.step
				currentDebugOps.Unlock()

				if stepFn != nil {
					stepFn()
				}

			case MsgTypeDebugStepInto:
				currentDebugOps.Lock()
				stepIntoFn := currentDebugOps.stepInto
				currentDebugOps.Unlock()

				if stepIntoFn != nil {
					stepIntoFn()
				}

			case MsgTypeDebugStepOut:
				currentDebugOps.Lock()
				stepOutFn := currentDebugOps.stepOut
				currentDebugOps.Unlock()

				if stepOutFn != nil {
					stepOutFn()
				}

			case MsgTypeDebugPause:
				currentDebugOps.Lock()
				pauseFn := currentDebugOps.pause
				currentDebugOps.Unlock()

				if pauseFn != nil {
					pauseFn()
				}

			case MsgTypeDebugResume:
				currentDebugOps.Lock()
				resumeFn := currentDebugOps.resume
				currentDebugOps.Unlock()

				if resumeFn != nil {
					resumeFn()
				}

			case MsgTypeDebugAddBreakpoint:
				currentDebugOps.Lock()
				addBpFn := currentDebugOps.addBreakpoint
				currentDebugOps.Unlock()

				if addBpFn != nil {
					addBpFn(payload.NodeID)
				}

			case MsgTypeDebugRemoveBreakpoint:
				currentDebugOps.Lock()
				removeBpFn := currentDebugOps.removeBreakpoint
				currentDebugOps.Unlock()

				if removeBpFn != nil {
					removeBpFn(payload.NodeID)
				}

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

func runGraphFromConn(ctx context.Context, graphData string, opts core.RunOpts, ws *websocket.Conn, sharedKey string, debugCb core.DebugCallback) {

	// *must* release the lock when it's done
	defer func() {
		<-graphRunning

		// cleanup the cancel function so "stop" can't be called on a finished job
		cancelLock.Lock()
		currentGraphCancel = nil
		cancelLock.Unlock()
	}()

	origStdout := os.Stdout
	origStderr := os.Stderr
	origLogOutput := utils.LogOut.Out // <-- this is logruses original output

	rOut, wOut, errOut := os.Pipe()
	if errOut != nil {
		utils.LogOut.Debugf("failed to create pipe for stdout/log capture: %v\n", errOut)
		sendEncryptedJSON(ws, map[string]string{
			"type":  MsgTypeJobError,
			"error": fmt.Sprintf("Failed to capture stdout/log: %v", errOut),
		}, sharedKey)
		return
	}

	rErr, wErr, errErr := os.Pipe()
	if errErr != nil {
		wOut.Close()
		utils.LogOut.Debugf("failed to create pipe for stderr capture: %v\n", errErr)
		sendEncryptedJSON(ws, map[string]string{
			"type":  MsgTypeJobError,
			"error": fmt.Sprintf("Failed to capture stderr: %v", errErr),
		}, sharedKey)
		return
	}

	os.Stdout = wOut
	utils.LogOut.SetOutput(wOut)

	os.Stderr = wErr

	startTime := time.Now()
	fmt.Printf("🚀 Task started...\n")

	var wg sync.WaitGroup
	wg.Add(2)

	// for stdout
	go func() {
		defer wg.Done()
		scanner := bufio.NewScanner(rOut)
		for scanner.Scan() {
			line := scanner.Text()

			if strings.TrimSpace(line) == "" {
				continue
			}

			// here we write to original console
			fmt.Fprintln(origStdout, line)

			sendEncryptedJSON(ws, map[string]string{
				"type":    MsgTypeLog,
				"message": fmt.Sprintf("[%s] %s", time.Now().Format("2006-01-02 15:04:05"), line),
			}, sharedKey)
		}
		if err := scanner.Err(); err != nil {
			utils.LogOut.Debugf("error reading from stdout/log pipe: %v\n", err)
		}
	}()

	// for stderr
	go func() {
		defer wg.Done()
		scanner := bufio.NewScanner(rErr)
		for scanner.Scan() {
			line := scanner.Text()

			if strings.TrimSpace(line) == "" {
				continue
			}

			// here we write to original console
			fmt.Fprintln(origStderr, line)

			sendEncryptedJSON(ws, map[string]string{
				"type":    MsgTypeLogError,
				"message": line,
			}, sharedKey)
		}
		if err := scanner.Err(); err != nil {
			utils.LogOut.Debugf("error reading from stderr pipe: %v\n", err)
		}
	}()

	runErr := func() (err error) {
		defer core.RecoverHandler(false)
		return core.RunGraphFromString(ctx, "browser", graphData, core.RunOpts{
			ConfigFile:      opts.ConfigFile,
			OverrideSecrets: opts.OverrideSecrets,
			OverrideInputs:  opts.OverrideInputs,
			OverrideEnv:     opts.OverrideEnv,
			Args:            []string{},
		}, debugCb)
	}()

	endTime := time.Now()
	duration := endTime.Sub(startTime)
	durationStr := fmt.Sprintf("%.2fs", duration.Seconds())

	// we print this *before* closing the pipes, so it still gets captured
	if runErr != nil {
		fmt.Printf("\n❌ Job failed. (Total time: %s)\n", durationStr)
	} else {
		fmt.Printf("\n✅ Job succeeded. (Total time: %s)\n", durationStr)
	}

	wOut.Close()
	wErr.Close()

	os.Stdout = origStdout
	os.Stderr = origStderr
	utils.LogOut.SetOutput(origLogOutput)

	wg.Wait()

	// all output has already been streamed, including the summary line.
	// now we just send the final status message.
	if runErr != nil {
		utils.LogOut.Debugf("graph execution failed: %v\n", runErr)
		// send final error, even if error lines were already streamed
		sendEncryptedJSON(ws, map[string]string{
			"type":  MsgTypeJobError,
			"error": fmt.Sprintf("Graph execution failed: %v", runErr),
		}, sharedKey)
		return // Exit, the deferred lock release will still run
	}

	sendEncryptedJSON(ws, map[string]string{
		"type": MsgTypeJobFinished,
	}, sharedKey)
}

// decryptData decrypts the Base64-encoded (IV + Ciphertext) string
func decryptData(base64Ciphertext string, base64Key string) (string, error) {
	key, err := base64.StdEncoding.DecodeString(base64Key)
	if err != nil {
		return "", errors.New("failed to decode base64 key")
	}
	if len(key) != 32 {
		return "", errors.New("invalid key length: must be 32 bytes (AES-256)")
	}

	data, err := base64.StdEncoding.DecodeString(base64Ciphertext)
	if err != nil {
		return "", errors.New("failed to decode base64 ciphertext")
	}

	// The browser prepends the 12-byte IV to the ciphertext
	const ivSize = 12
	if len(data) <= ivSize {
		return "", errors.New("invalid ciphertext length")
	}
	iv := data[:ivSize]
	ciphertext := data[ivSize:]

	block, err := aes.NewCipher(key)
	if err != nil {
		return "", err
	}

	aesgcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}

	plaintext, err := aesgcm.Open(nil, iv, ciphertext, nil)
	if err != nil {
		// Decryption failed (invalid key or tampered message)
		return "", err
	}

	return string(plaintext), nil
}

func calculateGraphDepth(fullPath string) int {
	if fullPath == "" {
		return 0
	}
	return strings.Count(fullPath, "/")
}

func isVersionOutdated(current, required string) bool {
	if required == "" {
		return false
	}

	// If the CLI is built locally or has a non-semver version like `dev`
	// or something, skip the check to not block anyone
	currentVer, err := semver.NewVersion(current)
	if err != nil {
		return false
	}

	requiredVer, err := semver.NewVersion(required)
	if err != nil {
		return false
	}

	return currentVer.LessThan(requiredVer)
}
