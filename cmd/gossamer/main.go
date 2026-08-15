package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/signal"
	"strings"

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

	response, err := client.Load(ctx, options.url)
	if err != nil {
		fmt.Fprintf(stderr, "gossamer: %v\n", err)
		if errors.Is(err, loader.ErrInvalidURL) {
			return 2
		}
		return 1
	}
	defer response.Body.Close()

	if options.dumpDOM || options.screenshotPath != "" {
		document, err := htmlparser.Parse(response.Body)
		if err != nil {
			fmt.Fprintf(stderr, "gossamer: parse document: %v\n", err)
			return 1
		}

		if options.screenshotPath != "" {
			return writeScreenshot(stderr, options.screenshotPath, document)
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

func writeScreenshot(stderr io.Writer, path string, document *dom.Node) int {
	file, err := os.Create(path)
	if err != nil {
		fmt.Fprintf(stderr, "gossamer: create screenshot: %v\n", err)
		return 1
	}

	renderErr := render.RenderPNG(file, document, render.DefaultViewport)
	closeErr := file.Close()
	if renderErr != nil {
		_ = os.Remove(path)
		fmt.Fprintf(stderr, "gossamer: render screenshot: %v\n", renderErr)
		return 1
	}
	if closeErr != nil {
		_ = os.Remove(path)
		fmt.Fprintf(stderr, "gossamer: close screenshot: %v\n", closeErr)
		return 1
	}

	return 0
}
