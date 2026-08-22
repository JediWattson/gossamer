//go:build cgo && darwin && arm64

package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"runtime"
	"strings"
	"syscall"
	"time"

	"github.com/JediWattson/gossamer/internal/browser"
	"github.com/JediWattson/gossamer/internal/loader"
	"github.com/JediWattson/gossamer/internal/memoryprofile"
	"github.com/JediWattson/gossamer/internal/nativeengine"
	"github.com/JediWattson/gossamer/internal/window"
)

func main() {
	// AppKit must be initialized and pumped from the process main thread.
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	icuData := flag.String("icu-data", os.Getenv("GOSSAMER_V8_ICU_DATA"), "path to the pinned V8 build's icudtl.dat")
	engineName := flag.String("engine", "v8", "JavaScript engine: strand or v8")
	title := flag.String("title", "", "native window title")
	sessionFile := flag.String("session-file", "", "optional path for Graphite tab-session restore")
	memoryProfile := flag.String("memory-profile", "", "optional JSONL browser memory timeline path")
	heapProfile := flag.String("heap-profile", "", "optional Go heap profile path written when the window closes")
	memoryProfileInterval := flag.Duration("memory-profile-interval", time.Second, "minimum interval between memory timeline checkpoints")
	flag.Parse()
	if flag.NArg() != 1 {
		fatalf("usage: gossamer-window [flags] <absolute-http-or-https-url>")
	}
	rawURL := flag.Arg(0)

	engine, err := selectEngine(*engineName, *icuData)
	if err != nil {
		fatalf("initialize %s engine: %v", strings.TrimSpace(*engineName), err)
	}
	browserRuntime, err := browser.NewWithEngine(engine)
	if err != nil {
		_ = engine.Close()
		fatalf("initialize browser runtime: %v", err)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	documentLoader := loader.New(nil)
	page, err := browserRuntime.LoadPage(ctx, rawURL, documentLoader)
	if err != nil {
		_ = browserRuntime.Close()
		fatalf("load page: %v", err)
	}
	recorder, err := memoryprofile.New(strings.TrimSpace(*memoryProfile), strings.TrimSpace(*heapProfile), *memoryProfileInterval)
	if err != nil {
		_ = browserRuntime.Close()
		fatalf("initialize memory profile: %v", err)
	}
	if err := recorder.Record("loaded", page, true); err != nil {
		_ = recorder.Close()
		_ = browserRuntime.Close()
		fatalf("record loaded memory profile: %v", err)
	}
	page.SetFormNavigationLoader(documentLoader)
	windowTitle := strings.TrimSpace(*title)
	if windowTitle == "" {
		windowTitle = "Gossamer — " + rawURL
	}
	var sessionStore window.SessionStore
	if strings.TrimSpace(*sessionFile) != "" {
		sessionStore = window.FileSessionStore{Path: *sessionFile}
	}
	runErr := window.RunBrowser(ctx, page, window.NewNativeBackend(), window.ShellConfig{
		Title:   windowTitle,
		Loader:  documentLoader,
		OpenTab: browserRuntime.NewBlankPage,
		Session: sessionStore,
		Checkpoint: func(current *browser.Page) error {
			return recorder.Record("task-checkpoint", current, false)
		},
	})
	finalProfileErr := recorder.Record("window-close", page, true)
	heapProfileErr := recorder.WriteHeapProfile()
	recorderCloseErr := recorder.Close()
	browserCloseErr := browserRuntime.Close()
	if err := errors.Join(runErr, finalProfileErr, heapProfileErr, recorderCloseErr, browserCloseErr); err != nil {
		fatalf("run interactive window: %v", err)
	}
}

func selectEngine(name, icuData string) (browser.Engine, error) {
	switch strings.ToLower(strings.TrimSpace(name)) {
	case "strand":
		return nativeengine.New(nativeengine.Config{}), nil
	case "v8":
		return newStockV8Engine(icuData)
	default:
		return nil, fmt.Errorf("unknown engine %q (want strand or v8)", name)
	}
}

func fatalf(format string, arguments ...any) {
	_, _ = fmt.Fprintf(os.Stderr, "gossamer-window: "+format+"\n", arguments...)
	os.Exit(1)
}
