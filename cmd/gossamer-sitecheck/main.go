package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/JediWattson/gossamer/internal/loader"
	"github.com/JediWattson/gossamer/internal/sitecompat"
)

func main() {
	if err := run(os.Args[1:], os.Stdout); err != nil {
		_, _ = fmt.Fprintf(os.Stderr, "gossamer-sitecheck: %v\n", err)
		os.Exit(1)
	}
}

func run(arguments []string, output io.Writer) error {
	flags := flag.NewFlagSet("gossamer-sitecheck", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	engineName := flags.String("engine", "strand", "JavaScript engine: strand or v8")
	icuData := flags.String("icu-data", os.Getenv("GOSSAMER_V8_ICU_DATA"), "path to V8 icudtl.dat")
	dist := flags.String("dist", "", "serve this production distribution directory")
	domLimit := flags.Int("dom-limit", 200, "maximum DOM dump lines; negative disables the dump")
	timeout := flags.Duration("timeout", 30*time.Second, "site boot timeout")
	screenshotPath := flags.String("screenshot", "", "optional PNG output path")
	if err := flags.Parse(arguments); err != nil {
		return err
	}

	var server *httptest.Server
	var rawURL string
	if strings.TrimSpace(*dist) != "" {
		root, err := filepath.Abs(*dist)
		if err != nil {
			return err
		}
		info, err := os.Stat(root)
		if err != nil {
			return fmt.Errorf("inspect dist: %w", err)
		}
		if !info.IsDir() {
			return fmt.Errorf("dist %q is not a directory", root)
		}
		server = httptest.NewServer(http.FileServer(http.Dir(root)))
		defer server.Close()
		rawURL = server.URL + "/"
		if flags.NArg() != 0 {
			return fmt.Errorf("a URL cannot be combined with --dist")
		}
	} else {
		if flags.NArg() != 1 {
			return fmt.Errorf("usage: gossamer-sitecheck [flags] <absolute-http-or-https-url>")
		}
		rawURL = flags.Arg(0)
	}

	engine, err := selectEngine(*engineName, *icuData)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), *timeout)
	defer cancel()

	var screenshot io.Writer
	var screenshotFile *os.File
	if strings.TrimSpace(*screenshotPath) != "" {
		screenshotFile, err = os.Create(*screenshotPath)
		if err != nil {
			return err
		}
		defer screenshotFile.Close()
		screenshot = screenshotFile
	}
	report, runErr := sitecompat.Run(ctx, engine, rawURL, loader.New(nil), sitecompat.Options{
		EngineName: strings.ToLower(strings.TrimSpace(*engineName)), DOMLimit: *domLimit, Screenshot: screenshot,
	})
	encoder := json.NewEncoder(output)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(report); err != nil {
		return errors.Join(runErr, err)
	}
	if runErr != nil {
		return runErr
	}
	if !report.Passed {
		return fmt.Errorf("compatibility gate failed")
	}
	return nil
}
