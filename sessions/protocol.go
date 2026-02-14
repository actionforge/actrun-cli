package sessions

import (
	"bufio"
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/Masterminds/semver/v3"
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

// MessageSender is a function that sends a payload over a WebSocket connection.
// Both encrypted (gateway) and plain (local) modes implement this signature.
type MessageSender func(ws *websocket.Conn, payload any)

func newEncryptedSender(sharedKey string) MessageSender {
	return func(ws *websocket.Conn, payload any) {
		sendEncryptedJSON(ws, payload, sharedKey)
	}
}

func newPlainSender() MessageSender {
	return func(ws *websocket.Conn, payload any) {
		sendPlainJSON(ws, payload)
	}
}

func sendPlainJSON(ws *websocket.Conn, payload any) {
	wsWriteMutex.Lock()
	defer wsWriteMutex.Unlock()

	if err := ws.SetWriteDeadline(time.Now().Add(10 * time.Second)); err != nil {
		utils.LogOut.Errorf("failed to set write deadline (connection likely closed): %v\n", err)
		return
	}

	if err := ws.WriteJSON(payload); err != nil {
		utils.LogOut.Errorf("failed to send JSON message: %v\n", err)
	}
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

type debugOps struct {
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

func (d *debugOps) cleanup() {
	d.Lock()
	d.pause = nil
	d.resume = nil
	d.step = nil
	d.stepInto = nil
	d.stepOut = nil
	d.addBreakpoint = nil
	d.removeBreakpoint = nil
	d.cachedState = nil
	d.Unlock()
}

func (d *debugOps) dispatch(msgType string, nodeID string) {
	d.Lock()
	var fn func()
	var fnStr func(string)
	switch msgType {
	case MsgTypeDebugStep:
		fn = d.step
	case MsgTypeDebugStepInto:
		fn = d.stepInto
	case MsgTypeDebugStepOut:
		fn = d.stepOut
	case MsgTypeDebugPause:
		fn = d.pause
	case MsgTypeDebugResume:
		fn = d.resume
	case MsgTypeDebugAddBreakpoint:
		fnStr = d.addBreakpoint
	case MsgTypeDebugRemoveBreakpoint:
		fnStr = d.removeBreakpoint
	}
	d.Unlock()

	if fn != nil {
		fn()
	}
	if fnStr != nil {
		fnStr(nodeID)
	}
}

func (d *debugOps) cancelAndResume() {
	cancelLock.Lock()
	if currentGraphCancel != nil {
		currentGraphCancel()
	}
	cancelLock.Unlock()

	d.Lock()
	resumeFn := d.resume
	d.Unlock()
	if resumeFn != nil {
		resumeFn()
	}
}

func triggerGraphExecution(
	ops *debugOps,
	ws *websocket.Conn,
	send MessageSender,
	configFile string,
	graphPayload string,
	secrets map[string]string,
	inputs map[string]any,
	env map[string]string,
	breakpoints []string,
	startPaused bool,
	ignoreBreakpoints bool,
	shouldSkipPause func() bool,
	onGraphComplete func(),
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
		ops.Lock()

		ops.pause = func() {
			debugMu.Lock()
			isPaused = true
			currentStepMode = StepRun
			debugMu.Unlock()
			utils.LogOut.Debug("pausing execution...\n")
		}

		ops.resume = func() {
			debugMu.Lock()
			isPaused = false
			currentStepMode = StepRun
			ops.Lock()
			ops.cachedState = nil
			ops.Unlock()
			debugCond.Broadcast()
			debugMu.Unlock()
			utils.LogOut.Debug("resuming execution...\n")
		}

		ops.step = func() {
			debugMu.Lock()
			currentStepMode = StepOver
			debugMu.Unlock()
			ops.Lock()
			ops.cachedState = nil
			ops.Unlock()
			debugMu.Lock()
			debugCond.Signal()
			debugMu.Unlock()
			utils.LogOut.Debug("stepping Over...\n")
		}

		ops.stepInto = func() {
			debugMu.Lock()
			currentStepMode = StepInto
			debugMu.Unlock()
			ops.Lock()
			ops.cachedState = nil
			ops.Unlock()
			debugMu.Lock()
			debugCond.Signal()
			debugMu.Unlock()
			utils.LogOut.Debug("stepping Into...\n")
		}

		ops.stepOut = func() {
			debugMu.Lock()
			currentStepMode = StepOut
			debugMu.Unlock()
			ops.Lock()
			ops.cachedState = nil
			ops.Unlock()
			debugMu.Lock()
			debugCond.Signal()
			debugMu.Unlock()
			utils.LogOut.Debug("stepping Out...\n")
		}

		ops.addBreakpoint = func(nodeId string) {
			bpMutex.Lock()
			activeBreakpoints[nodeId] = true
			bpMutex.Unlock()
			utils.LogOut.Debugf("breakpoint added at %s\n", nodeId)
		}

		ops.removeBreakpoint = func(nodeId string) {
			bpMutex.Lock()
			delete(activeBreakpoints, nodeId)
			bpMutex.Unlock()
			utils.LogOut.Debugf("breakpoint removed at %s\n", nodeId)
		}
		ops.Unlock()

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

			if shouldSkipPause != nil && shouldSkipPause() {
				isPaused = false
			}

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

				go send(ws, debugState)

				ops.Lock()
				ops.cachedState = debugState
				ops.Unlock()

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
			}, ws, send, debugCb)

			ops.cleanup()

			if onGraphComplete != nil {
				onGraphComplete()
			}
		}()

	default:
		utils.LogOut.Warn("Cannot run graph: another graph is already in progress.\n")
		send(ws, map[string]string{
			"type":  MsgTypeJobError,
			"error": "A graph is already running.",
		})
	}
}

func runGraphFromConn(ctx context.Context, graphData string, opts core.RunOpts, ws *websocket.Conn, send MessageSender, debugCb core.DebugCallback) {

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
		send(ws, map[string]string{
			"type":  MsgTypeJobError,
			"error": fmt.Sprintf("Failed to capture stdout/log: %v", errOut),
		})
		return
	}

	rErr, wErr, errErr := os.Pipe()
	if errErr != nil {
		wOut.Close()
		utils.LogOut.Debugf("failed to create pipe for stderr capture: %v\n", errErr)
		send(ws, map[string]string{
			"type":  MsgTypeJobError,
			"error": fmt.Sprintf("Failed to capture stderr: %v", errErr),
		})
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

			send(ws, map[string]string{
				"type":    MsgTypeLog,
				"message": fmt.Sprintf("[%s] %s", time.Now().Format("2006-01-02 15:04:05"), line),
			})
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

			send(ws, map[string]string{
				"type":    MsgTypeLogError,
				"message": line,
			})
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
		send(ws, map[string]string{
			"type":  MsgTypeJobError,
			"error": fmt.Sprintf("%#v", runErr),
		})
		return // Exit, the deferred lock release will still run
	}

	send(ws, map[string]string{
		"type": MsgTypeJobFinished,
	})
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
