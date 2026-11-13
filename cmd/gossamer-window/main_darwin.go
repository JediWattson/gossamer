//go:build cgo && darwin && arm64

package main

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"math"
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
	frameProfile := flag.String("frame-profile", "", "optional JSONL native frame timing path")
	profileScrollCount := flag.Int("profile-scroll-count", 0, "run this many deterministic scroll inputs, then close (requires --frame-profile)")
	profileScrollDeltaY := flag.Float64("profile-scroll-delta-y", 48, "CSS-pixel vertical delta for deterministic profile scrolls")
	profileScrollWarmup := flag.Duration("profile-scroll-warmup", 3*time.Second, "warmup after navigation completes before deterministic profile scrolls")
	profileScrollInterval := flag.Duration("profile-scroll-interval", 100*time.Millisecond, "interval between deterministic profile scrolls")
	profileDeviceScale := flag.Float64("profile-device-scale", 2, "device scale used by deterministic scroll profiling")
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
	if *profileScrollCount < 0 || *profileScrollWarmup < 0 || *profileScrollInterval < 0 {
		fatalf("profile scroll count, warmup, and interval must be non-negative")
	}
	if *profileScrollCount > 0 && strings.TrimSpace(*frameProfile) == "" {
		fatalf("--profile-scroll-count requires --frame-profile")
	}
	if *profileScrollCount > 0 && (!finitePositive(*profileDeviceScale) || !finiteNumber(*profileScrollDeltaY)) {
		fatalf("profile device scale and scroll delta must be finite, with a positive scale")
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
	var frameFile *os.File
	var frameBuffer *bufio.Writer
	var frameEncoder *json.Encoder
	if path := strings.TrimSpace(*frameProfile); path != "" {
		frameFile, err = os.Create(path)
		if err != nil {
			_ = recorder.Close()
			_ = browserRuntime.Close()
			fatalf("create frame profile: %v", err)
		}
		frameBuffer = bufio.NewWriterSize(frameFile, 1024*1024)
		frameEncoder = json.NewEncoder(frameBuffer)
	}
	var frameObserver window.FrameTimingObserver
	if frameEncoder != nil {
		frameObserver = func(observation window.FrameTiming) error {
			return frameEncoder.Encode(observation)
		}
	}
	var backend window.Backend = window.NewNativeBackend()
	if *profileScrollCount > 0 {
		memoryBackend := window.NewMemoryBackend()
		memoryBackend.SetDeviceScaleFactor(*profileDeviceScale)
		backend = &scrollProfileBackend{
			MemoryBackend: memoryBackend, page: page,
			remaining: *profileScrollCount, deltaY: *profileScrollDeltaY,
			warmup: *profileScrollWarmup, interval: *profileScrollInterval,
		}
	}
	runErr := window.RunBrowser(ctx, page, backend, window.ShellConfig{
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
		FrameTiming: frameObserver,
	})
	var frameProfileErr error
	if frameBuffer != nil {
		frameProfileErr = frameBuffer.Flush()
	}
	if frameFile != nil {
		frameProfileErr = errors.Join(frameProfileErr, frameFile.Sync(), frameFile.Close())
	}
	finalProfileErr := recorder.Record("window-close", page, true)
	heapProfileErr := recorder.WriteHeapProfile()
	recorderCloseErr := recorder.Close()
	browserCloseErr := browserRuntime.Close()
	if err := errors.Join(runErr, frameProfileErr, finalProfileErr, heapProfileErr, recorderCloseErr, browserCloseErr); err != nil {
		fatalf("run interactive window: %v", err)
	}
}

// scrollProfileBackend keeps the complete browser/window/compositor path but
// replaces human input and an accumulating native front buffer with a bounded,
// repeatable sequence. EventNone polls let asynchronous loader and Realm work
// reach their ordinary checkpoints during navigation and warmup.
type scrollProfileBackend struct {
	*window.MemoryBackend
	page      *browser.Page
	remaining int
	deltaY    float64
	warmup    time.Duration
	interval  time.Duration
	width     int
	height    int
	readyAt   time.Time
	nextAt    time.Time
}

func (backend *scrollProfileBackend) Open(config window.Config) error {
	backend.width, backend.height = config.Width, config.Height
	return backend.MemoryBackend.Open(config)
}

func (backend *scrollProfileBackend) NextEvent(ctx context.Context) (window.Event, error) {
	if err := ctx.Err(); err != nil {
		return window.Event{}, err
	}
	if backend.remaining == 0 {
		return window.Event{Kind: window.EventClose}, nil
	}
	now := time.Now()
	if backend.readyAt.IsZero() {
		if backend.page == nil || backend.page.Navigation().State != browser.NavigationComplete {
			return profilePoll(ctx)
		}
		backend.readyAt = now.Add(backend.warmup)
	}
	if now.Before(backend.readyAt) {
		return profilePoll(ctx)
	}
	if !backend.nextAt.IsZero() && now.Before(backend.nextAt) {
		return profilePoll(ctx)
	}
	backend.remaining--
	backend.nextAt = now.Add(backend.interval)
	return window.Event{
		Kind: window.EventScroll,
		X:    float64(backend.width) / 2, Y: float64(backend.height) / 2,
		DeltaY: backend.deltaY,
	}, nil
}

func profilePoll(ctx context.Context) (window.Event, error) {
	timer := time.NewTimer(10 * time.Millisecond)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return window.Event{}, ctx.Err()
	case <-timer.C:
		return window.Event{Kind: window.EventNone}, nil
	}
}

func finiteNumber(value float64) bool {
	return !math.IsNaN(value) && !math.IsInf(value, 0)
}

func finitePositive(value float64) bool { return value > 0 && finiteNumber(value) }

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
