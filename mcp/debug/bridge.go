package mcpdebug

import (
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/actionforge/actrun-cli/sessions"
	"github.com/gorilla/websocket"
)

// LogEntry represents a buffered log message from the debug session.
type LogEntry struct {
	Level   string `json:"level"`
	Message string `json:"message"`
}

// IncomingMessage represents a parsed message from the WebSocket.
type IncomingMessage struct {
	Type             string `json:"type"`
	FullPath         string `json:"fullPath,omitempty"`
	ExecutionContext any    `json:"executionContext,omitempty"`
	Message          string `json:"message,omitempty"`
	Error            string `json:"error,omitempty"`
}

// writeDeadline is the timeout applied to every WebSocket write,
// matching the convention in sessions/protocol.go.
const writeDeadline = 10 * time.Second

// Bridge is a stateful WebSocket client that converts the push-based WS
// stream into pull-based tool results for the MCP server.
type Bridge struct {
	mu        sync.Mutex
	writeMu   sync.Mutex // serialises all WebSocket writes (gorilla/websocket allows one concurrent writer)
	conn      *websocket.Conn
	connected bool
	readErr   error

	logBuffer []LogEntry
	lastState *IncomingMessage

	waiter chan IncomingMessage
	done   chan struct{}
}

// NewBridge creates a new Bridge instance.
func NewBridge() *Bridge {
	return &Bridge{}
}

// Connect dials the local debug server and starts the read loop.
func (b *Bridge) Connect(port int) error {
	b.mu.Lock()
	defer b.mu.Unlock()

	if b.connected {
		return fmt.Errorf("already connected")
	}

	url := fmt.Sprintf("ws://127.0.0.1:%d/ws", port)
	conn, _, err := websocket.DefaultDialer.Dial(url, nil)
	if err != nil {
		return fmt.Errorf("failed to connect to %s: %w", url, err)
	}

	b.conn = conn
	b.connected = true
	b.readErr = nil
	b.logBuffer = nil
	b.lastState = nil
	b.waiter = make(chan IncomingMessage, 1)
	b.done = make(chan struct{})

	go b.readLoop()

	return nil
}

// readLoop reads messages from the WebSocket and dispatches them.
func (b *Bridge) readLoop() {
	defer func() {
		b.mu.Lock()
		b.connected = false
		close(b.done)
		b.mu.Unlock()
	}()

	for {
		_, msgBytes, err := b.conn.ReadMessage()
		if err != nil {
			b.mu.Lock()
			b.readErr = err
			b.mu.Unlock()
			return
		}

		var msg IncomingMessage
		if err := json.Unmarshal(msgBytes, &msg); err != nil {
			continue
		}

		b.mu.Lock()
		switch msg.Type {
		case sessions.MsgTypeLog:
			b.logBuffer = append(b.logBuffer, LogEntry{
				Level:   "log",
				Message: msg.Message,
			})
		case sessions.MsgTypeLogError:
			b.logBuffer = append(b.logBuffer, LogEntry{
				Level:   "error",
				Message: msg.Message,
			})
		case sessions.MsgTypeWarning:
			b.logBuffer = append(b.logBuffer, LogEntry{
				Level:   "warning",
				Message: msg.Message,
			})
		case sessions.MsgTypeDebugState:
			b.lastState = &msg
			// Deliver to waiter if someone is waiting
			select {
			case b.waiter <- msg:
			default:
			}
		case sessions.MsgTypeJobFinished, sessions.MsgTypeJobError:
			select {
			case b.waiter <- msg:
			default:
			}
		case sessions.MsgTypeControl:
			// Control messages (e.g. runner_connected) are informational
			b.logBuffer = append(b.logBuffer, LogEntry{
				Level:   "info",
				Message: fmt.Sprintf("control: %s", msg.Message),
			})
		}
		b.mu.Unlock()
	}
}

// Send marshals payload as JSON and writes it to the WebSocket.
func (b *Bridge) Send(payload any) error {
	b.mu.Lock()
	if !b.connected {
		b.mu.Unlock()
		return fmt.Errorf("not connected")
	}
	conn := b.conn
	b.mu.Unlock()

	data, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("failed to marshal payload: %w", err)
	}

	b.writeMu.Lock()
	defer b.writeMu.Unlock()

	if err := conn.SetWriteDeadline(time.Now().Add(writeDeadline)); err != nil {
		return fmt.Errorf("failed to set write deadline: %w", err)
	}
	if err := conn.WriteMessage(websocket.TextMessage, data); err != nil {
		return fmt.Errorf("failed to write message: %w", err)
	}
	return nil
}

// SendAndWait sends a payload and blocks until a debug_state, job_finished,
// or job_error message is received, or until the timeout expires.
func (b *Bridge) SendAndWait(payload any, timeout time.Duration) (*IncomingMessage, []LogEntry, error) {
	b.mu.Lock()
	if !b.connected {
		b.mu.Unlock()
		return nil, nil, fmt.Errorf("not connected")
	}
	// Drain any stale message from waiter channel
	select {
	case <-b.waiter:
	default:
	}
	done := b.done
	b.mu.Unlock()

	if err := b.Send(payload); err != nil {
		return nil, nil, err
	}

	select {
	case msg := <-b.waiter:
		logs := b.DrainLogs()
		return &msg, logs, nil
	case <-done:
		return nil, nil, fmt.Errorf("connection closed while waiting for response")
	case <-time.After(timeout):
		return nil, nil, fmt.Errorf("timeout waiting for response after %s", timeout)
	}
}

// DrainLogs returns all buffered log entries and clears the buffer.
func (b *Bridge) DrainLogs() []LogEntry {
	b.mu.Lock()
	defer b.mu.Unlock()

	logs := b.logBuffer
	b.logBuffer = nil
	return logs
}

// LastState returns the last received debug_state message.
func (b *Bridge) LastState() *IncomingMessage {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.lastState
}

// Connected returns whether the bridge is connected.
func (b *Bridge) Connected() bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.connected
}

// Disconnect closes the WebSocket connection and waits for the read loop to exit.
func (b *Bridge) Disconnect() error {
	b.mu.Lock()
	if !b.connected {
		b.mu.Unlock()
		return fmt.Errorf("not connected")
	}
	done := b.done
	b.mu.Unlock()

	// Send close frame under the write mutex.
	b.writeMu.Lock()
	_ = b.conn.SetWriteDeadline(time.Now().Add(writeDeadline))
	_ = b.conn.WriteMessage(
		websocket.CloseMessage,
		websocket.FormatCloseMessage(websocket.CloseNormalClosure, ""),
	)
	b.writeMu.Unlock()

	b.conn.Close()

	// Wait for readLoop to finish so the Bridge is fully idle before returning.
	<-done

	return nil
}
