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
	if err := flags.Parse(arguments); err != nil {
		return err
	}
	if flags.NArg() != 0 || strings.TrimSpace(*dist) == "" {
		return fmt.Errorf("usage: gossamer-efchatcheck --dist /absolute/path/to/efchat/web/dist [--message text]")
	}
	ctx, cancel := context.WithTimeout(context.Background(), *timeout)
	defer cancel()
	report, runErr := efchatcompat.Run(ctx, efchatcompat.Options{Dist: *dist, Message: *message, Place: *place})
	encoder := json.NewEncoder(output)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(report); err != nil {
		return errors.Join(runErr, err)
	}
	return runErr
}
