package utils

import (
	"io"
	"os"
	"sync"

	"github.com/sirupsen/logrus"
)

type LogLevel int

const (
	LogGhStartGroup = "##[group]"
	LogGhEndGroup   = "##[endgroup]"
)

const (
	LogLevelNormal LogLevel = iota
	LogLevelDebug
	LogLevelVerbose
)

func GetLogLevel() LogLevel {
	logLevel := os.Getenv("ACT_LOGLEVEL")
	switch logLevel {
	case "debug":
		return LogLevelDebug
	case "verbose":
		return LogLevelVerbose
	default:
		return LogLevelNormal
	}
}

var LogOut = logrus.New()
var LogErr = logrus.New()

func ApplyLogLevel() {
	logLevel := GetLogLevel()
	switch logLevel {
	case LogLevelDebug:
		LogOut.SetLevel(logrus.TraceLevel)
	case LogLevelVerbose:
		LogOut.SetLevel(logrus.WarnLevel)
	default:
		LogOut.SetLevel(logrus.InfoLevel)
	}
}

type CustomFormatter struct{}

func (f *CustomFormatter) Format(entry *logrus.Entry) ([]byte, error) {
	return []byte(entry.Message), nil
}

type lockedWriter struct {
	w   io.Writer
	mux *sync.Mutex
}

func (lw *lockedWriter) Write(p []byte) (n int, err error) {
	lw.mux.Lock()
	defer lw.mux.Unlock()
	return lw.w.Write(p)
}

func init() {
	mux := &sync.Mutex{}

	// Logus is thread-safe except when it isn't :-|
	// Ocassionally I still saw concurrent outputs
	// merged into the same line.
	stdout := &lockedWriter{w: os.Stdout, mux: mux}
	stderr := &lockedWriter{w: os.Stderr, mux: mux}
	LogOut.SetOutput(stdout)
	LogErr.SetOutput(stderr)
	LogOut.SetFormatter(&CustomFormatter{})
	LogErr.SetFormatter(&CustomFormatter{})
}
