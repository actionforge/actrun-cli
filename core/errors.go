package core

import (
	"bytes"
	"database/sql"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"syscall"

	"github.com/actionforge/actrun-cli/utils"

	"github.com/fatih/color"
)

var (
	errEmoji    = "❌"
	hintEmoji   = "💡"
	stackEmoji  = "🛠️"
	numberEmoji = "🔢"

	errorColor      = color.New(color.FgRed).SprintFunc()
	hintColor       = color.New(color.FgYellow).SprintFunc()
	contextColor    = color.New(color.FgCyan).SprintFunc()
	stackTraceColor = color.New(color.FgMagenta).SprintFunc()
	bold            = color.New(color.Bold).SprintFunc()

	customErrNoOutputValue = &ErrNoOutputValue{}
	customErrNoInputValue  = &ErrNoInputValue{}
)

const HINT_INTERNAL_ERROR = "This is an internal error. Please report it via email or a GitHub issue."

type ErrNoOutputValue struct {
	Message string
}

type ErrNoInputValue struct {
	Message string
}

type CauseError struct {
	Message string
}

func (e *CauseError) Error() string {
	return e.Message
}

type LeafError struct {
	Message    string
	GoStack    []uintptr
	ErrorStack []error
	Cause      error
	Context    *ExecutionState
	Hint       string
}

func (e *LeafError) Error() string {
	return e.Message
}

func (e *LeafError) ErrorWithCauses() string {
	var lines []string

	// 1. Top level error (no prefix)
	// iterate backwards for high-level first
	for i := len(e.ErrorStack) - 1; i >= 0; i-- {
		prefix := ""
		if len(lines) > 0 {
			// add indentation based on depth
			prefix = strings.Repeat(" ", len(lines)) + "↳ "
		}
		lines = append(lines, prefix+e.ErrorStack[i].Error())
	}

	// leaf message
	if e.Message != "" {
		prefix := ""
		if len(lines) > 0 {
			prefix = strings.Repeat(" ", len(lines)) + "↳ "
		}
		lines = append(lines, prefix+e.Message)
	}

	// root cause
	if e.Cause != nil {
		causeMsg := e.Cause.Error()
		if causeMsg != "" {
			p := strings.Repeat(" ", len(lines)) + "↳ "
			lines = append(lines, p+e.Cause.Error())
		}
	}

	return strings.Join(lines, "\n")
}

func (e *LeafError) Unwrap() error {
	return e.Cause
}

func (e *LeafError) SetHint(hint string, formatArgs ...any) *LeafError {
	e.Hint = fmt.Sprintf(hint, formatArgs...)
	return e
}

func CreateErr(c *ExecutionState, cause error, formatAndArgs ...any) *LeafError {
	var (
		message   string
		leafError *LeafError
	)

	if len(formatAndArgs) > 0 {
		format, args := formatAndArgs[0].(string), formatAndArgs[1:]
		message = fmt.Sprintf(format, args...)
	}

	// if cause is a LeafError or contains a LeafError errors.As will
	// recursively unwrap the cause error and loo for a LeafError.
	// If found, it assigns it to 'leafError' and returns true.
	if cause != nil && errors.As(cause, &leafError) {

		// found an existing LeafError, append the new message to its stack.
		// also note, we preserve the original stack trace and root cause.
		leafError.ErrorStack = append(leafError.ErrorStack, &CauseError{
			Message: message,
		})

		// unlikely but in case the original error
		// has no context add the one if we have one now
		if leafError.Context == nil {
			leafError.Context = c
		}

	} else {
		// else case here means no cause or cause is a non leaf error.
		// it also contains no LeafError, so now we need to create it
		stack := make([]uintptr, 64)

		leafError = &LeafError{
			GoStack:    stack[:runtime.Callers(2, stack)],
			Message:    message,
			Context:    c,
			Cause:      cause,
			ErrorStack: make([]error, 0),
		}
	}

	return leafError
}

func indentString(input string, indentSpaces int, numbering bool) string {
	if input == "" {
		return ""
	}
	lines := strings.Split(input, "\n")
	indent := strings.Repeat(" ", indentSpaces)
	const numberWidth = 2

	for i, line := range lines {
		if numbering {
			if strings.HasPrefix(line, " ") || strings.HasPrefix(line, "\t") {
				lines[i] = indent + strings.Repeat(" ", numberWidth) + "  " + line
			} else {
				lines[i] = fmt.Sprintf("%s%*d: %s", indent, numberWidth, i+1, line)
			}
		} else {
			lines[i] = indent + line
		}
	}
	return strings.Join(lines, "\n")
}

func (e *LeafError) Format(f fmt.State, c rune) {
	switch c {
	case 'v':

		var (
			tmpErrEmoji   string
			tmpHintEmoji  string
			tmpStackEmoji string
		)
		if !color.NoColor {
			tmpErrEmoji = errEmoji + " "
			tmpHintEmoji = hintEmoji + " "
			tmpStackEmoji = stackEmoji + " "
		}

		var output string

		// print ❌ error
		if e.Context != nil && len(e.Context.Visited) > 0 {
			var coloredLines []string

			for _, item := range e.Context.Visited {
				var msg string
				if item.Execute {
					msg = fmt.Sprintf("execute '%s' (%s)", item.Node.GetName(), item.Node.GetId())
				} else {
					msg = fmt.Sprintf("request input from '%s' (%s)", item.Node.GetName(), item.NodeID)
				}

				coloredLines = append(coloredLines, contextColor(msg))
			}

			callstackBody := strings.Join(coloredLines, "\n")
			callstackBlock := indentString(callstackBody, 2, true)
			output += fmt.Sprintf("%s%s\n%s\n", tmpErrEmoji, bold("error:"), callstackBlock)

			errorBlock := indentString(e.ErrorWithCauses(), 6, false)
			output += fmt.Sprintf("%s\n\n", errorBlock)
		} else {
			// if error block is printed without prior context, then enumerate lines
			errorBlock := indentString(e.ErrorWithCauses(), 2, true)
			output += fmt.Sprintf("%s%s\n%s", tmpErrEmoji, bold("error:"), errorColor(errorBlock))
		}

		// print 💡 hint
		hint := indentString(getErrorHint(e), 2, false)
		if hint != "" {
			output += fmt.Sprintf("\n\n%s%s\n%s", tmpHintEmoji, bold("hint:"), hintColor(hint))
		}

		// print 🛠️ stack trace
		if f.Flag('+') {
			rawStack := e.StackTrace()
			lines := strings.Split(rawStack, "\n")
			var coloredLines []string

			for _, line := range lines {
				coloredLines = append(coloredLines, stackTraceColor(line))
			}

			output += fmt.Sprintf("\n\n%s%s\n%s",
				tmpStackEmoji,
				stackTraceColor(bold("stack trace:")),
				strings.Join(coloredLines, "\n"),
			)
		}

		fmt.Fprint(f, output)
		return
	case 's':
		fmt.Fprint(f, e.Error())
	}
}

func (e *LeafError) StackTrace() string {
	return GetStacktrace(e.GoStack)
}

func (e *LeafError) Is(target error) bool {
	_, ok := target.(*LeafError)
	return ok
}

func (e *CauseError) Is(target error) bool {
	_, ok := target.(*CauseError)
	return ok
}

func (e *ErrNoInputValue) Is(target error) bool {
	_, ok := target.(*ErrNoInputValue)
	return ok
}

func (e *ErrNoOutputValue) Is(target error) bool {
	_, ok := target.(*ErrNoOutputValue)
	return ok
}

func (e *ErrNoOutputValue) GetMessage() string {
	return e.Message
}

func (m *ErrNoOutputValue) Error() string {
	return m.Message
}

func (e *ErrNoInputValue) GetMessage() string {
	return e.Message
}

func (m *ErrNoInputValue) Error() string {
	return m.Message
}

func isCombinedError(err error) bool {
	joinedError, ok := err.(interface {
		Unwrap() []error
	})
	if !ok {
		return false
	}

	return len(joinedError.Unwrap()) > 0
}

type errorIterator struct {
	err error
	idx int
}

func iterateCombinedError(err error) <-chan errorIterator {
	joinedError := err.(interface {
		Unwrap() []error
	})
	ch := make(chan errorIterator)
	go func() {
		defer close(ch)
		for i, e := range joinedError.Unwrap() {
			ch <- errorIterator{err: e, idx: i}
		}
	}()
	return ch
}

func PrintError(graphFile string, err error) {

	if isCombinedError(err) {
		for e := range iterateCombinedError(err) {
			printError(graphFile, e.err, e.idx)
		}
		return
	}

	printError(graphFile, err, -1)
}

func printError(graphFile string, err error, index int) {
	output := ""

	if index >= 0 {
		output += fmt.Sprintf("%s %s\n   %s\n\n", numberEmoji, bold("concurrent error index:"), fmt.Sprintf("%d", index))
	}

	switch utils.GetLogLevel() {
	case utils.LogLevelNormal:
		output += fmt.Sprintf("%v\n", err)
	default: // debug or verbose is fully detailed
		output += fmt.Sprintf("%+v\n", err)
	}

	if graphFile != "" {
		utils.LogErr.Errorf("actrun: %s\n\n", graphFile)
	}

	utils.LogErr.Error(output)
}

func getErrorHint(leafError *LeafError) string {
	if leafError == nil {
		return "No error."
	}

	if leafError.Hint != "" {
		return leafError.Hint
	}

	err := leafError.Cause
	if err == nil {
		return ""
	}

	switch {
	case errors.Is(err, customErrNoOutputValue):
		// check if the node that hasn't provided any values, has been executed before
		if leafError.Context != nil || len(leafError.Context.Visited) > 0 {
			lastVisited := leafError.Context.Visited[len(leafError.Context.Visited)-1]
			if lastVisited.Node.IsExecutionNode() {
				nodeWasExecuted := false

				visits := slices.Clone(leafError.Context.Visited)
				slices.Reverse(visits)
				for _, visit := range visits {
					if visit.Node.GetId() == lastVisited.Node.GetId() && visit.Execute {
						nodeWasExecuted = true
						break
					}
				}

				if !nodeWasExecuted {
					return fmt.Sprintf("The node '%s' (%s) needs to be executed before it can provide values. It appears this node hasn't been executed yet, or its execution input is not connected.", lastVisited.Node.GetName(), lastVisited.Node.GetId())
				}
			}

			return fmt.Sprintf("No output value provided. Check the settings of '%s' (%s) node", lastVisited.Node.GetName(), lastVisited.Node.GetId())
		}
		// should never happen here
		return ""

	case errors.Is(err, customErrNoInputValue):
		return "No input value provided. Set a value or connect the input with a node"

	// OS errors
	case errors.Is(err, os.ErrNotExist):
		return "The specified file or directory does not exist. Check the path and try again."

	case errors.Is(err, os.ErrPermission):
		return "You do not have the necessary permissions to perform this action. Try running the command with elevated privileges."

	case errors.Is(err, os.ErrExist):
		return "The file or directory already exists. Consider renaming or removing the existing one."

	case errors.Is(err, os.ErrClosed):
		return "The file is closed. Ensure the file is open before performing this action."

	// Database errors
	case errors.Is(err, sql.ErrNoRows):
		return "No matching records found in the database. Verify your query or ensure the data exists."

	case errors.Is(err, sql.ErrConnDone):
		return "The database connection is closed. Check your database connection settings."

	case errors.Is(err, sql.ErrTxDone):
		return "The transaction has already been committed or rolled back. Ensure you're using a valid transaction."

	// Network errors
	case errors.Is(err, net.ErrClosed):
		return "The network connection is closed. Verify your network settings and try reconnecting."

	case errors.Is(err, syscall.ECONNREFUSED):
		return "Connection refused. Ensure the server is running and accepting connections."

	case errors.Is(err, syscall.ECONNRESET):
		return "Connection reset by peer. The remote server might be down. Try reconnecting later."

	case errors.Is(err, syscall.ETIMEDOUT):
		return "Connection timed out. Check your network connection and try again."

	case errors.Is(err, syscall.EADDRINUSE):
		return "Address already in use. Ensure the address/port is not being used by another application."

	case errors.Is(err, syscall.EHOSTUNREACH):
		return "Host unreachable. Verify the network configuration and try again."

	// File errors
	case errors.Is(err, syscall.EIO):
		return "Input/output error. Check the device or file system for issues."

	case errors.Is(err, syscall.ENOSPC):
		return "No space left on device. Free up some space and try again."

	case errors.Is(err, syscall.ENOTDIR):
		return "A component of the path is not a directory. Check the path and try again."

	case errors.Is(err, syscall.EISDIR):
		return "The specified path is a directory, not a file. Provide a valid file path."

	case errors.Is(err, syscall.ENOTEMPTY):
		return "The directory is not empty. Ensure the directory is empty before performing this action."

	case errors.Is(err, syscall.EINVAL):
		return "Invalid argument. Check the inputs and try again."

	case errors.Is(err, syscall.EPIPE):
		return "Broken pipe. The connection was closed unexpectedly."

	// Network address errors
	case errors.Is(err, net.UnknownNetworkError("tcp")):
		return "Unknown network. Ensure the network type is correct."

	case errors.Is(err, net.InvalidAddrError("example")):
		return "Invalid address. Verify the address format and try again."

	// Authentication errors
	case strings.Contains(err.Error(), "authentication failed"):
		return "Authentication failed. Verify your credentials and try again."

	case strings.Contains(err.Error(), "authorization failed"):
		return "Authorization failed. Ensure you have the necessary permissions to perform this action."

	// Parsing errors
	case strings.Contains(err.Error(), "syntax error"):
		return "Syntax error. Check the syntax of your input and try again."

	case strings.Contains(err.Error(), "parsing error"):
		return "Parsing error. Verify the input format and try again."

	// Generic errors
	case strings.Contains(err.Error(), "timeout"):
		return "Operation timed out. Check your network connection or server status."

	case strings.Contains(err.Error(), "connection refused"):
		return "Connection refused. Ensure the server is running and accepting connections."

	case strings.Contains(err.Error(), "no such file or directory"):
		return "The specified file or directory does not exist. Check the path and try again."
	}

	return ""
}

func GetStacktrace(stack []uintptr) string {
	var buffer bytes.Buffer
	frames := runtime.CallersFrames(stack)

	for {
		frame, more := frames.Next()

		file := frame.File

		if IsTestE2eRunning() {
			if strings.Contains(strings.ToLower(file), "go/") {
				// Some tests print the stack trace, and we need
				// to replace the path with a placeholder to make
				// the tests deterministic on all platforms.
				file = strings.ReplaceAll(file, "amd64", "{..}")
				file = strings.ReplaceAll(file, "arm64", "{..}")
				file = strings.ReplaceAll(file, "x86", "{..}")
				file = strings.ReplaceAll(file, "x64", "{..}")
				file = strings.ReplaceAll(file, "x86_64", "{..}")
				file = strings.ReplaceAll(file, "darwin", "{..}")
				file = strings.ReplaceAll(file, "linux", "{..}")
				file = strings.ReplaceAll(file, "windows", "{..}")
				file = strings.ReplaceAll(file, "win", "{..}")
				file = strings.ReplaceAll(file, "win32", "{..}")
				file = strings.ReplaceAll(file, "win64", "{..}")
				file = strings.ReplaceAll(file, "libexec", "{..}")

				// external dependencies have different lines for different platforms
				// - .../go/1.22.4/{..}/src/runtime/asm_{..}.s:1222
				// + .../go/1.22.4/{..}/src/runtime/asm_{..}.s:1695
				frame.Line = -1
			}

			// For e2e tests, we need a deterministic stack trace
			file = filepath.Base(file)
		}

		buffer.WriteString(fmt.Sprintf("%s\n\t%s:%d\n", frame.Function, file, frame.Line))
		if !more {
			break
		}
	}

	return buffer.String()
}
