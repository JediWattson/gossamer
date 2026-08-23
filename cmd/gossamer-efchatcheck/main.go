package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/JediWattson/gossamer/internal/efchatcompat"
	"github.com/JediWattson/gossamer/internal/memoryprofile"
)

func main() {
	if err := run(os.Args[1:], os.Stdout); err != nil {
		_, _ = fmt.Fprintf(os.Stderr, "gossamer-efchatcheck: %v\n", err)
		os.Exit(1)
	}
}

func run(arguments []string, output io.Writer) error {
	flags := flag.NewFlagSet("gossamer-efchatcheck", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	dist := flags.String("dist", "", "efchat production distribution directory")
	message := flags.String("message", "hello from Strand", "local message content to submit")
	place := flags.String("place", "global", "efchat place slug")
	timeout := flags.Duration("timeout", 30*time.Second, "anonymous session gate timeout")
	memoryProfile := flags.String("memory-profile", "", "optional JSONL browser memory timeline path")
	heapProfile := flags.String("heap-profile", "", "optional Go heap profile captured after the message attempt")
	memoryProfileInterval := flags.Duration("memory-profile-interval", 100*time.Millisecond, "minimum interval between active-execution samples")
	executionSampleInterval := flags.Uint64("strand-execution-sample-interval", 1_000_000, "raw Strand instructions between active-render memory samples (zero disables)")
	if err := flags.Parse(arguments); err != nil {
		return err
	}
	if flags.NArg() != 0 || strings.TrimSpace(*dist) == "" {
		return fmt.Errorf("usage: gossamer-efchatcheck --dist /absolute/path/to/efchat/web/dist [--message text]")
	}
	ctx, cancel := context.WithTimeout(context.Background(), *timeout)
	defer cancel()
	recorder, err := memoryprofile.New(strings.TrimSpace(*memoryProfile), strings.TrimSpace(*heapProfile), *memoryProfileInterval)
	if err != nil {
		return fmt.Errorf("initialize memory profile: %w", err)
	}
	var profiler efchatcompat.MemoryProfiler
	var sampleInterval uint64
	if strings.TrimSpace(*memoryProfile) != "" || strings.TrimSpace(*heapProfile) != "" {
		profiler = recorder
	}
	if strings.TrimSpace(*memoryProfile) != "" {
		sampleInterval = *executionSampleInterval
	}
	report, runErr := efchatcompat.Run(ctx, efchatcompat.Options{
		Dist: *dist, Message: *message, Place: *place,
		MemoryProfiler: profiler, ExecutionSampleInterval: sampleInterval,
	})
	closeErr := recorder.Close()
	encoder := json.NewEncoder(output)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(report); err != nil {
		return errors.Join(runErr, closeErr, err)
	}
	return errors.Join(runErr, closeErr)
}
