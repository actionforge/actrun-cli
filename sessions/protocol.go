package sessions

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"io"
	"strings"
	"sync"

	"github.com/Masterminds/semver/v3"
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
