package logging

import (
	"bytes"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type writerFunc func([]byte) (int, error)

func (f writerFunc) Write(p []byte) (int, error) { return f(p) }

func TestFallbackWriter(t *testing.T) {
	for _, tt := range []struct {
		name                string
		mirror, fail, short bool
	}{
		{"file only", false, false, false},
		{"mirror", true, false, false},
		{"file failure", false, true, false},
		{"mirror with file failure", true, true, false},
		{"short file write", false, false, true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			var file, stderr bytes.Buffer
			primary := writerFunc(func(p []byte) (int, error) {
				if tt.fail {
					return 0, errors.New("file unavailable")
				}
				if tt.short {
					return file.Write(p[:3])
				}
				return file.Write(p)
			})
			w := &fallbackWriter{primary: primary, fallback: &stderr, mirror: tt.mirror}
			message := []byte("complete log message\n")
			if n, err := w.Write(message); err != nil || n != len(message) {
				t.Fatalf("Write = (%d, %v), want (%d, nil)", n, err, len(message))
			}
			want := ""
			if tt.mirror || tt.fail || tt.short {
				want = string(message)
			}
			if stderr.String() != want {
				t.Errorf("stderr = %q, want %q (one complete copy)", stderr.String(), want)
			}
			if !tt.fail && !tt.short && file.String() != string(message) {
				t.Errorf("file message missing: %q", file.String())
			}
		})
	}
}

func TestFallbackWriterReportsFallbackFailure(t *testing.T) {
	fail := errors.New("stderr unavailable")
	w := &fallbackWriter{
		primary:  writerFunc(func([]byte) (int, error) { return 0, errors.New("file unavailable") }),
		fallback: writerFunc(func([]byte) (int, error) { return 0, fail }),
	}
	if _, err := w.Write([]byte("error")); !errors.Is(err, fail) {
		t.Errorf("Write error = %v, want stderr failure", err)
	}
	w.fallback = writerFunc(func([]byte) (int, error) { return 0, nil })
	if _, err := w.Write([]byte("error")); !errors.Is(err, io.ErrShortWrite) {
		t.Errorf("short fallback Write error = %v, want ErrShortWrite", err)
	}
}

func preserveLogger(t *testing.T) {
	t.Helper()
	oldWriter, oldLevel := std.Writer(), GetLevel()
	t.Cleanup(func() {
		std.SetOutput(oldWriter)
		SetLevel(oldLevel)
	})
}

func TestInitUnusableLogFallsBackToStderr(t *testing.T) {
	for _, mirror := range []bool{false, true} {
		name := "stdio"
		if mirror {
			name = "http"
		}
		t.Run(name, func(t *testing.T) {
			preserveLogger(t)
			t.Setenv("LOG_LEVEL", "warn")
			badDir := filepath.Join(t.TempDir(), "not-a-directory")
			if err := os.WriteFile(badDir, []byte("x"), 0600); err != nil {
				t.Fatal(err)
			}
			r, w, err := os.Pipe()
			if err != nil {
				t.Fatal(err)
			}
			defer r.Close()
			defer w.Close()
			oldStderr := os.Stderr
			os.Stderr = w
			defer func() { os.Stderr = oldStderr }()

			_, cleanup := Init(Options{Service: "test-server", Dir: badDir, Stderr: mirror})
			defer cleanup()
			Errorf("startup error must not disappear")
			w.Close()
			data, err := io.ReadAll(r)
			if err != nil {
				t.Fatal(err)
			}
			if got := strings.Count(string(data), "startup error must not disappear"); got != 1 {
				t.Fatalf("stderr has %d copies, want 1: %q", got, data)
			}
		})
	}
}

func TestInitKeepsLogCreationLazy(t *testing.T) {
	preserveLogger(t)
	t.Setenv("LOG_LEVEL", "warn")
	path, cleanup := Init(Options{Service: "test-server", Dir: t.TempDir()})
	defer cleanup()
	Debugf("filtered out")
	if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("quiet server created a log file: %v", err)
	}
	Warnf("first visible message")
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("visible message did not create a log file: %v", err)
	}
}
