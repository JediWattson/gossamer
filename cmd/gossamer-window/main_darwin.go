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
	browserruntime "github.com/JediWattson/gossamer/internal/runtime"
	"github.com/JediWattson/gossamer/internal/window"
)

func main() {
	// AppKit must be initialized and pumped from the process main thread.
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	launchStarted := time.Now()

	icuData := flag.String("icu-data", os.Getenv("GOSSAMER_V8_ICU_DATA"), "path to the pinned V8 build's icudtl.dat")
	engineName := flag.String("engine", "v8", "JavaScript engine: strand or v8")
	title := flag.String("title", "", "native window title")
	sessionFile := flag.String("session-file", "", "optional path for Graphite tab-session restore")
	memoryProfile := flag.String("memory-profile", "", "optional JSONL browser memory timeline path")
	heapProfile := flag.String("heap-profile", "", "optional Go heap profile path written when the window closes")
	memoryProfileInterval := flag.Duration("memory-profile-interval", time.Second, "minimum interval between memory timeline checkpoints")
	maxInstructions := flag.Uint64("max-instructions", 0, "optional Strand control-flow budget per top-level call (zero uses the runtime default)")
	maxCallDepth := flag.Uint("max-call-depth", 0, "optional Strand synchronous call-depth budget (zero uses the runtime default)")
	profileInstructions := flag.Bool("strand-instruction-profile", false, "include Strand bytecode hotspots when an instruction budget is exhausted")
	executionSampleInterval := flag.Uint64("strand-execution-sample-interval", 1_000_000, "raw Strand instructions between active-render memory samples (zero disables)")
	startupTimings := flag.Bool("startup-timings", false, "print initial browser startup milestones to stderr")
	flag.Parse()
	if flag.NArg() != 1 {
		fatalf("usage: gossamer-window [flags] <absolute-http-or-https-url>")
	}
	rawURL := flag.Arg(0)

	recorder, err := memoryprofile.New(strings.TrimSpace(*memoryProfile), strings.TrimSpace(*heapProfile), *memoryProfileInterval)
	if err != nil {
		fatalf("initialize memory profile: %v", err)
	}
	var page *browser.Page
	interpreter := browserruntime.InterpreterConfig{
		MaxInstructions:     *maxInstructions,
		MaxCallDepth:        uint32(*maxCallDepth),
		ProfileInstructions: *profileInstructions,
	}
	if strings.EqualFold(strings.TrimSpace(*engineName), "strand") && strings.TrimSpace(*memoryProfile) != "" && *executionSampleInterval != 0 {
		interpreter.ExecutionSampleInterval = *executionSampleInterval
		interpreter.ExecutionSample = func(context *browserruntime.TaskContext, sample browserruntime.ExecutionSample) error {
			if page == nil {
				return nil
			}
			return recorder.RecordExecution(page, context, sample)
		}
	}
	engine, err := selectEngineWithInterpreter(*engineName, *icuData, interpreter)
	if err != nil {
		_ = recorder.Close()
		fatalf("initialize %s engine: %v", strings.TrimSpace(*engineName), err)
	}
	browserRuntime, err := browser.NewWithEngine(engine)
	if err != nil {
		_ = recorder.Close()
		_ = engine.Close()
		fatalf("initialize browser runtime: %v", err)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	documentLoader := loader.New(nil)
	page, err = browserRuntime.NewBlankPage(ctx)
	if err != nil {
		_ = recorder.Close()
		_ = browserRuntime.Close()
		fatalf("create initial page: %v", err)
	}
	page.SetNavigationLoader(documentLoader)
	page.SetFormNavigationLoader(documentLoader)
	if err := recorder.Record("page-created", page, true); err != nil {
		_ = recorder.Close()
		_ = browserRuntime.Close()
		fatalf("record initial memory profile: %v", err)
	}
	if _, err := page.Navigate(ctx, rawURL, documentLoader); err != nil {
		_ = recorder.Close()
		_ = browserRuntime.Close()
		fatalf("begin navigation: %v", err)
	}
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
		Startup: func(current *browser.Page, observation window.StartupObservation) error {
			if *startupTimings {
				_, _ = fmt.Fprintf(os.Stderr, "gossamer-window startup: %-23s %s\n", observation.Milestone, time.Since(launchStarted).Round(time.Millisecond))
			}
			return recorder.Record(string(observation.Milestone), current, true)
		},
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
	return selectEngineWithInterpreter(name, icuData, browserruntime.InterpreterConfig{})
}

func selectEngineWithInterpreter(name, icuData string, interpreter browserruntime.InterpreterConfig) (browser.Engine, error) {
	switch strings.ToLower(strings.TrimSpace(name)) {
	case "strand":
		return nativeengine.New(nativeengine.Config{
			Interpreter: interpreter,
			ConsoleSink: func(message nativeengine.ConsoleMessage) {
				if message.Method == "error" {
					_, _ = fmt.Fprintf(os.Stderr, "strand console.error: %s\n", strings.Join(message.Arguments, " "))
				}
			},
		}), nil
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
