// Package logging provides a small leveled logger with size-based rotation,
// shared by the MCP servers in this module.
//
// The default level is warn, so long-running servers stay quiet: routine
// per-request activity is logged at debug/info and hidden unless -v is set.
package logging

import (
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"sync/atomic"

	"gopkg.in/natefinch/lumberjack.v2"
)

// Level is the severity of a log message.
type Level int

const (
	LevelDebug Level = iota
	LevelInfo
	LevelWarn
	LevelError
)

// String returns the lowercase name used as the message prefix.
func (l Level) String() string {
	switch l {
	case LevelDebug:
		return "debug"
	case LevelInfo:
		return "info"
	case LevelWarn:
		return "warn"
	default:
		return "error"
	}
}

// ParseLevel converts a level name to a Level, for use by LOG_LEVEL.
func ParseLevel(s string) (Level, error) {
	switch s {
	case "debug":
		return LevelDebug, nil
	case "info":
		return LevelInfo, nil
	case "warn", "warning", "":
		return LevelWarn, nil
	case "error":
		return LevelError, nil
	default:
		return LevelWarn, fmt.Errorf("invalid log level %q (want debug, info, warn, or error)", s)
	}
}

var (
	level atomic.Int32
	std   = log.New(io.Discard, "", log.LstdFlags|log.Lshortfile)
)

// Options configures Init.
type Options struct {
	// Service identifies the server and names its log file (<service>.log).
	Service string
	// Dir is the log directory. Empty means the directory of the running
	// executable, matching the historical behavior of writing logs beside
	// the binary.
	Dir string
	// Verbose enables debug logging (shorthand for Level debug).
	Verbose bool
	// Stderr mirrors output to stderr in addition to the log file, for
	// servers running with an HTTP transport.
	Stderr bool
	// MaxSizeMB is the size threshold that triggers rotation. 0 means 10 MB.
	MaxSizeMB int
	// MaxBackups is how many rotated files to retain. 0 means 5.
	MaxBackups int
	// MaxAgeDays is how long to retain rotated files. 0 means 30 days.
	MaxAgeDays int
}

// Init configures the package-level logger to write to a rotated log file
// (default: <dir>/<service>.log beside the executable) and returns the log
// path plus a cleanup function that closes the underlying writer.
//
// Init never fails: if the log file cannot be used, output falls back to
// stderr rather than taking the server down.
func Init(opts Options) (path string, cleanup func()) {
	setLevel(opts.Verbose, os.Getenv("LOG_LEVEL"))

	dir := opts.Dir
	if dir == "" {
		execPath, err := os.Executable()
		if err != nil {
			dir = "."
		} else {
			dir = filepath.Dir(execPath)
		}
	}
	path = filepath.Join(dir, opts.Service+".log")

	rotator := &lumberjack.Logger{
		Filename:   path,
		MaxSize:    orDefault(opts.MaxSizeMB, 10), // megabytes
		MaxBackups: orDefault(opts.MaxBackups, 5),
		MaxAge:     orDefault(opts.MaxAgeDays, 30), // days
		Compress:   true,
	}

	var w io.Writer = rotator
	if opts.Stderr {
		w = io.MultiWriter(os.Stderr, rotator)
	}
	std.SetOutput(w)

	return path, func() {
		_ = rotator.Close()
	}
}

func setLevel(verbose bool, env string) {
	lvl := LevelWarn
	if verbose {
		lvl = LevelDebug
	}
	if env != "" {
		if parsed, err := ParseLevel(env); err == nil {
			lvl = parsed
		} else {
			std.SetOutput(os.Stderr)
			Errorf("logging: %v; falling back to warn", err)
		}
	}
	level.Store(int32(lvl))
}

// SetLevel overrides the active level after Init.
func SetLevel(l Level) { level.Store(int32(l)) }

// GetLevel reports the active level.
func GetLevel() Level { return Level(level.Load()) }

func orDefault(v, def int) int {
	if v <= 0 {
		return def
	}
	return v
}

func output(l Level, calldepth int, format string, args ...any) {
	if Level(level.Load()) > l {
		return
	}
	_ = std.Output(calldepth, l.String()+" "+fmt.Sprintf(format, args...))
}

// Debugf logs at debug level: routine per-request detail, hidden by default.
func Debugf(format string, args ...any) { output(LevelDebug, 3, format, args...) }

// Infof logs at info level: notable but non-error events.
func Infof(format string, args ...any) { output(LevelInfo, 3, format, args...) }

// Warnf logs at warn level, the default visibility threshold.
func Warnf(format string, args ...any) { output(LevelWarn, 3, format, args...) }

// Errorf logs at error level and is always shown.
func Errorf(format string, args ...any) { output(LevelError, 3, format, args...) }
