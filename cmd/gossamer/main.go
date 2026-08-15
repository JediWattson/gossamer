package main

import (
	"context"
	"errors"
	"fmt"
	"image/png"
	"io"
	"os"
	"os/signal"
	"path/filepath"
	"strings"

	"github.com/JediWattson/gossamer/internal/browser"
	"github.com/JediWattson/gossamer/internal/dom"
	htmlparser "github.com/JediWattson/gossamer/internal/html"
	"github.com/JediWattson/gossamer/internal/loader"
	"github.com/JediWattson/gossamer/internal/render"
)

const usage = "usage: gossamer [--dump-dom | --screenshot <file>] <url>"

type documentLoader interface {
	Load(context.Context, string) (*loader.Response, error)
}

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()

	os.Exit(run(
		ctx,
		os.Args[1:],
		os.Stdout,
		os.Stderr,
		loader.New(nil),
	))
}

func run(
	ctx context.Context,
	args []string,
	stdout io.Writer,
	stderr io.Writer,
	client documentLoader,
) int {
	options, ok := parseArguments(args)
	if !ok {
		fmt.Fprintln(stderr, usage)
		return 2
	}
	if options.screenshotPath != "" {
		return runScreenshot(ctx, stderr, options.screenshotPath, options.url, client)
	}

	response, err := client.Load(ctx, options.url)
	if err != nil {
		fmt.Fprintf(stderr, "gossamer: %v\n", err)
		if errors.Is(err, loader.ErrInvalidURL) {
			return 2
		}
		return 1
	}
	defer response.Body.Close()

	if options.dumpDOM {
		document, err := htmlparser.Parse(response.Body)
		if err != nil {
			fmt.Fprintf(stderr, "gossamer: parse document: %v\n", err)
			return 1
		}

		if err := dom.Dump(stdout, document); err != nil {
			fmt.Fprintf(stderr, "gossamer: dump document: %v\n", err)
			return 1
		}
		return 0
	}

	if _, err := io.Copy(stdout, response.Body); err != nil {
		fmt.Fprintf(stderr, "gossamer: copy response: %v\n", err)
		return 1
	}

	return 0
}

func runScreenshot(
	ctx context.Context,
	stderr io.Writer,
	path string,
	rawURL string,
	client documentLoader,
) int {
	engine, err := browser.New()
	if err != nil {
		fmt.Fprintf(stderr, "gossamer: create browser: %v\n", err)
		return 1
	}
	defer engine.Close()
	page, err := engine.LoadPage(ctx, rawURL, client)
	if err != nil {
		fmt.Fprintf(stderr, "gossamer: %v\n", err)
		if errors.Is(err, loader.ErrInvalidURL) {
			return 2
		}
		return 1
	}
	return writeScreenshot(ctx, stderr, path, page.Frame())
}

type commandOptions struct {
	url            string
	dumpDOM        bool
	screenshotPath string
}

func parseArguments(args []string) (commandOptions, bool) {
	switch {
	case len(args) == 1 && args[0] != "" && !strings.HasPrefix(args[0], "-"):
		return commandOptions{url: args[0]}, true
	case len(args) == 2 && args[0] == "--dump-dom" && args[1] != "":
		return commandOptions{url: args[1], dumpDOM: true}, true
	case len(args) == 3 && args[0] == "--screenshot" && args[1] != "" && args[2] != "":
		return commandOptions{url: args[2], screenshotPath: args[1]}, true
	default:
		return commandOptions{}, false
	}
}

func writeScreenshot(ctx context.Context, stderr io.Writer, path string, frame *render.Frame) int {
	if err := ctx.Err(); err != nil {
		fmt.Fprintf(stderr, "gossamer: render screenshot: %v\n", err)
		return 1
	}
	targetMode := os.FileMode(0o644)
	if info, err := os.Stat(path); err == nil {
		if info.IsDir() {
			fmt.Fprintf(stderr, "gossamer: create screenshot: %s is a directory\n", path)
			return 1
		}
		targetMode = info.Mode().Perm()
	}

	directory := filepath.Dir(path)
	file, err := os.CreateTemp(directory, "."+filepath.Base(path)+"-*")
	if err != nil {
		fmt.Fprintf(stderr, "gossamer: create screenshot: %v\n", err)
		return 1
	}
	if err := file.Chmod(targetMode); err != nil {
		_ = file.Close()
		_ = os.Remove(file.Name())
		fmt.Fprintf(stderr, "gossamer: create screenshot: %v\n", err)
		return 1
	}
	temporaryPath := file.Name()
	committed := false
	defer func() {
		if !committed {
			_ = os.Remove(temporaryPath)
		}
	}()

	canvas, renderErr := render.Rasterize(frame)
	if renderErr == nil {
		renderErr = png.Encode(file, canvas)
	}
	closeErr := file.Close()
	if renderErr != nil {
		fmt.Fprintf(stderr, "gossamer: render screenshot: %v\n", renderErr)
		return 1
	}
	if closeErr != nil {
		fmt.Fprintf(stderr, "gossamer: close screenshot: %v\n", closeErr)
		return 1
	}
	if err := ctx.Err(); err != nil {
		fmt.Fprintf(stderr, "gossamer: render screenshot: %v\n", err)
		return 1
	}
	if err := os.Rename(temporaryPath, path); err != nil {
		fmt.Fprintf(stderr, "gossamer: replace screenshot: %v\n", err)
		return 1
	}
	committed = true

	return 0
}
