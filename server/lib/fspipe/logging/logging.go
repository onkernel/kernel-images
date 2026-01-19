package logging

import (
	"fmt"
	"io"
	"os"
	"runtime"
	"strings"
	"sync"
	"time"
)

// Level represents log severity
type Level int

const (
	LevelDebug Level = iota
	LevelInfo
	LevelWarn
	LevelError
	LevelSilent
)

var levelNames = map[Level]string{
	LevelDebug: "DEBUG",
	LevelInfo:  "INFO",
	LevelWarn:  "WARN",
	LevelError: "ERROR",
}

var levelColors = map[Level]string{
	LevelDebug: "\033[36m", // Cyan
	LevelInfo:  "\033[32m", // Green
	LevelWarn:  "\033[33m", // Yellow
	LevelError: "\033[31m", // Red
}

const colorReset = "\033[0m"

// Logger provides structured logging with levels
type Logger struct {
	mu       sync.Mutex
	out      io.Writer
	level    Level
	prefix   string
	useColor bool
}

var defaultLogger = &Logger{
	out:      os.Stderr,
	level:    LevelInfo,
	useColor: true,
}

// SetOutput sets the output destination
func SetOutput(w io.Writer) {
	defaultLogger.mu.Lock()
	defer defaultLogger.mu.Unlock()
	defaultLogger.out = w
}

// SetLevel sets the minimum log level
func SetLevel(level Level) {
	defaultLogger.mu.Lock()
	defer defaultLogger.mu.Unlock()
	defaultLogger.level = level
}

// SetSilent enables/disables silent mode
func SetSilent(silent bool) {
	defaultLogger.mu.Lock()
	defer defaultLogger.mu.Unlock()
	if silent {
		defaultLogger.level = LevelSilent
	} else {
		defaultLogger.level = LevelInfo
	}
}

// SetVerbose enables verbose (debug) logging
func SetVerbose(verbose bool) {
	defaultLogger.mu.Lock()
	defer defaultLogger.mu.Unlock()
	if verbose {
		defaultLogger.level = LevelDebug
	}
}

// SetColor enables/disables color output
func SetColor(useColor bool) {
	defaultLogger.mu.Lock()
	defer defaultLogger.mu.Unlock()
	defaultLogger.useColor = useColor
}

// SetPrefix sets a prefix for all log messages
func SetPrefix(prefix string) {
	defaultLogger.mu.Lock()
	defer defaultLogger.mu.Unlock()
	defaultLogger.prefix = prefix
}

func (l *Logger) log(level Level, format string, args ...interface{}) {
	l.mu.Lock()
	defer l.mu.Unlock()

	if level < l.level {
		return
	}

	// Get caller info
	_, file, line, ok := runtime.Caller(2)
	if ok {
		// Extract just the filename
		if idx := strings.LastIndex(file, "/"); idx >= 0 {
			file = file[idx+1:]
		}
	} else {
		file = "???"
		line = 0
	}

	// Format timestamp
	now := time.Now()
	timestamp := now.Format("15:04:05.000")

	// Format message
	msg := fmt.Sprintf(format, args...)

	// Build log line
	var buf strings.Builder

	if l.useColor {
		buf.WriteString(levelColors[level])
	}

	buf.WriteString(timestamp)
	buf.WriteString(" [")
	buf.WriteString(levelNames[level])
	buf.WriteString("] ")

	if l.prefix != "" {
		buf.WriteString(l.prefix)
		buf.WriteString(" ")
	}

	buf.WriteString(file)
	buf.WriteString(":")
	buf.WriteString(fmt.Sprintf("%d", line))
	buf.WriteString(" ")

	if l.useColor {
		buf.WriteString(colorReset)
	}

	buf.WriteString(msg)
	buf.WriteString("\n")

	l.out.Write([]byte(buf.String()))
}

// Debug logs a debug message
func Debug(format string, args ...interface{}) {
	defaultLogger.log(LevelDebug, format, args...)
}

// Info logs an info message
func Info(format string, args ...interface{}) {
	defaultLogger.log(LevelInfo, format, args...)
}

// Warn logs a warning message
func Warn(format string, args ...interface{}) {
	defaultLogger.log(LevelWarn, format, args...)
}

// Error logs an error message
func Error(format string, args ...interface{}) {
	defaultLogger.log(LevelError, format, args...)
}

// Debugf is an alias for Debug
func Debugf(format string, args ...interface{}) {
	Debug(format, args...)
}

// Infof is an alias for Info
func Infof(format string, args ...interface{}) {
	Info(format, args...)
}

// Warnf is an alias for Warn
func Warnf(format string, args ...interface{}) {
	Warn(format, args...)
}

// Errorf is an alias for Error
func Errorf(format string, args ...interface{}) {
	Error(format, args...)
}

// TraceOp logs an operation start and returns a function to log completion
func TraceOp(op string, details string) func(error) {
	start := time.Now()
	Debug("→ %s: %s", op, details)

	return func(err error) {
		elapsed := time.Since(start)
		if err != nil {
			Error("✗ %s: %s (elapsed=%v, error=%v)", op, details, elapsed, err)
		} else {
			Debug("✓ %s: %s (elapsed=%v)", op, details, elapsed)
		}
	}
}

// FormatBytes formats byte slice for logging (truncated if too long)
func FormatBytes(data []byte, maxLen int) string {
	if len(data) <= maxLen {
		return fmt.Sprintf("[%d bytes]", len(data))
	}
	return fmt.Sprintf("[%d bytes, first %d: %x...]", len(data), maxLen, data[:maxLen])
}
