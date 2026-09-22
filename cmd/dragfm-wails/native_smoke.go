package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	goruntime "runtime"
	"runtime/pprof"
	"strings"
	"sync"
	"time"

	"github.com/wailsapp/wails/v2/pkg/runtime"
)

// Native acceptance runs the same production binary, frontend and RPC bridge.
// It is opt-in, uses an isolated caller-created vault and never opens a network
// test-control server or enables test hooks during ordinary application startup.
type nativeSmoke struct {
	directory string
	script    string
	once      sync.Once
	mu        sync.Mutex
	passed    bool
}

func readNativeSmoke(args []string) (*nativeSmoke, error) {
	if len(args) == 0 || args[0] != "--native-smoke-dir" {
		return nil, nil
	}
	if len(args) != 2 || !filepath.IsAbs(args[1]) {
		return nil, errors.New("--native-smoke-dir requires one absolute isolated directory")
	}
	directory := filepath.Clean(args[1])
	if !strings.HasPrefix(filepath.Base(directory), ".dragfm-native-") {
		return nil, errors.New("native smoke directory must start with .dragfm-native-")
	}
	info, err := os.Lstat(directory)
	if err != nil {
		return nil, err
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 || (goruntime.GOOS != "windows" && info.Mode().Perm()&0077 != 0) {
		return nil, errors.New("native smoke directory must be a private real directory")
	}
	marker, err := os.ReadFile(filepath.Join(directory, "SMOKE_ONLY"))
	if err != nil || string(marker) != "dragfm-native-smoke-v1\n" {
		return nil, errors.New("isolated native smoke marker is missing")
	}
	path := filepath.Join(directory, "smoke.js")
	info, err = os.Lstat(path)
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() || info.Size() > 1<<20 {
		return nil, errors.New("native smoke script must be a regular file smaller than 1 MiB")
	}
	script, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return &nativeSmoke{directory: directory, script: string(script)}, nil
}

func (s *nativeSmoke) domReady(ctx context.Context) {
	done := make(chan struct{})
	finish := func(report map[string]any) {
		s.once.Do(func() {
			close(done)
			if report["success"] != true {
				// Only explicit isolated smoke runs collect stacks, before shutdown
				// erases the blocked state. There is no profiling HTTP endpoint.
				var trace bytes.Buffer
				_ = pprof.Lookup("goroutine").WriteTo(&trace, 2)
				report["goroutines"] = trace.String()
			}
			data, err := json.MarshalIndent(report, "", "  ")
			if err == nil {
				temporary := filepath.Join(s.directory, "report.json.tmp")
				err = os.WriteFile(temporary, append(data, '\n'), 0600)
				if err == nil {
					err = os.Rename(temporary, filepath.Join(s.directory, "report.json"))
				}
			}
			s.mu.Lock()
			s.passed = report["success"] == true && err == nil
			s.mu.Unlock()
			if err != nil {
				_, _ = fmt.Fprintln(os.Stderr, "native smoke report:", err)
			}
			go func() { time.Sleep(3 * time.Second); runtime.Quit(ctx) }()
		})
	}
	runtime.EventsOn(ctx, "__dragfm_native_smoke_result__", func(values ...interface{}) {
		if len(values) != 1 {
			finish(map[string]any{"success": false, "error": "invalid smoke result"})
			return
		}
		value, ok := values[0].(string)
		if !ok || len(value) > 128<<10 {
			finish(map[string]any{"success": false, "error": "invalid smoke result size"})
			return
		}
		var report map[string]any
		if err := json.Unmarshal([]byte(value), &report); err != nil {
			finish(map[string]any{"success": false, "error": "invalid smoke result JSON"})
			return
		}
		finish(report)
	})
	go func() {
		timer := time.NewTimer(240 * time.Second)
		defer timer.Stop()
		select {
		case <-done:
		case <-timer.C:
			finish(map[string]any{"success": false, "error": "native renderer acceptance timed out"})
		}
	}()
	runtime.WindowExecJS(ctx, s.script)
}

func (s *nativeSmoke) succeeded() bool { s.mu.Lock(); defer s.mu.Unlock(); return s.passed }
