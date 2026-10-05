package logging

import (
	"bytes"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

func capture(t *testing.T, lvl Level, fn func()) (*bytes.Buffer, func()) {
	t.Helper()
	var buf bytes.Buffer
	old := std
	std = log.New(&buf, "", 0)
	oldLevel := level.Load()
	SetLevel(lvl)
	fn()
	return &buf, func() {
		std = old
		level.Store(oldLevel)
	}
}

func TestLevelFiltering(t *testing.T) {
	buf, restore := capture(t, LevelWarn, func() {
		Debugf("debug %d", 1)
		Infof("info %d", 2)
		Warnf("warn %d", 3)
		Errorf("error %d", 4)
	})
	defer restore()

	for _, hidden := range []string{"debug 1", "info 2"} {
		if strings.Contains(buf.String(), hidden) {
			t.Errorf("warn level should hide %q, got: %s", hidden, buf)
		}
	}
	for _, shown := range []string{"warn 3", "error 4"} {
		if !strings.Contains(buf.String(), shown) {
			t.Errorf("warn level should show %q, got: %s", shown, buf)
		}
	}
}

func TestDebugLevelShowsAll(t *testing.T) {
	buf, restore := capture(t, LevelDebug, func() {
		Debugf("visible")
	})
	defer restore()
	if !strings.Contains(buf.String(), "debug visible") {
		t.Errorf("debug level should show message, got: %s", buf)
	}
}

func TestParseLevel(t *testing.T) {
	for in, want := range map[string]Level{
		"debug": LevelDebug,
		"info":  LevelInfo,
		"warn":  LevelWarn,
		"":      LevelWarn,
		"error": LevelError,
	} {
		got, err := ParseLevel(in)
		if err != nil || got != want {
			t.Errorf("ParseLevel(%q) = %v, %v; want %v", in, got, err, want)
		}
	}
	if _, err := ParseLevel("loud"); err == nil {
		t.Error("expected error for invalid level")
	}
}

func TestInitCreatesRotatedLog(t *testing.T) {
	dir := t.TempDir()
	path, cleanup := Init(Options{Service: "test-server", Dir: dir, Verbose: true})
	defer cleanup()

	if want := filepath.Join(dir, "test-server.log"); path != want {
		t.Fatalf("log path = %q, want %q", path, want)
	}
	Infof("hello rotation")

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "info hello rotation") {
		t.Errorf("log file missing message, got: %s", data)
	}
}

func TestConcurrentLogging(t *testing.T) {
	buf, restore := capture(t, LevelError, func() {
		var wg sync.WaitGroup
		for i := 0; i < 50; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				Errorf("concurrent %d", i)
			}()
		}
		wg.Wait()
	})
	defer restore()
	if strings.Count(buf.String(), "concurrent") != 50 {
		t.Errorf("lost messages under concurrency: %s", buf)
	}
}
