//go:build v8 && cgo && darwin && arm64

package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"runtime"
	"strings"
	"syscall"

	"github.com/JediWattson/gossamer/internal/browser"
	"github.com/JediWattson/gossamer/internal/loader"
	"github.com/JediWattson/gossamer/internal/v8engine"
	"github.com/JediWattson/gossamer/internal/window"
)

func main() {
	// AppKit must be initialized and pumped from the process main thread.
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	icuData := flag.String("icu-data", os.Getenv("GOSSAMER_V8_ICU_DATA"), "path to the pinned V8 build's icudtl.dat")
	title := flag.String("title", "", "native window title")
	flag.Parse()
	if flag.NArg() != 1 {
		fatalf("usage: gossamer-window [flags] <absolute-http-or-https-url>")
	}
	rawURL := flag.Arg(0)

	engine, err := v8engine.New(v8engine.Config{ICUDataPath: *icuData})
	if err != nil {
		fatalf("initialize stock V8: %v", err)
	}
	browserRuntime, err := browser.NewWithEngine(engine)
	if err != nil {
		_ = engine.Close()
		fatalf("initialize browser runtime: %v", err)
	}
	defer browserRuntime.Close()

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	documentLoader := loader.New(nil)
	page, err := browserRuntime.LoadPage(ctx, rawURL, documentLoader)
	if err != nil {
		fatalf("load page: %v", err)
	}
	page.SetFormNavigationLoader(documentLoader)
	windowTitle := strings.TrimSpace(*title)
	if windowTitle == "" {
		windowTitle = "Gossamer — " + rawURL
	}
	if err := window.RunBrowser(ctx, page, window.NewNativeBackend(), window.ShellConfig{
		Title:  windowTitle,
		Loader: documentLoader,
	}); err != nil {
		fatalf("run interactive window: %v", err)
	}
}

func fatalf(format string, arguments ...any) {
	_, _ = fmt.Fprintf(os.Stderr, "gossamer-window: "+format+"\n", arguments...)
	os.Exit(1)
}
