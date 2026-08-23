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

	"github.com/JediWattson/gossamer/internal/browser"
	"github.com/JediWattson/gossamer/internal/memoryprofile"
	"github.com/JediWattson/gossamer/internal/nativeengine"
	browserruntime "github.com/JediWattson/gossamer/internal/runtime"
	"github.com/JediWattson/gossamer/internal/sitecompat"
)

func main() {
	if err := run(os.Args[1:], os.Stdout); err != nil {
		_, _ = fmt.Fprintf(os.Stderr, "gossamer-scenariocheck: %v\n", err)
		os.Exit(1)
	}
}

func run(arguments []string, output io.Writer) error {
	flags := flag.NewFlagSet("gossamer-scenariocheck", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	dist := flags.String("dist", "", "production distribution directory")
	scenarioPath := flags.String("scenario", "", "interaction scenario JSON file")
	timeout := flags.Duration("timeout", 30*time.Second, "interaction timeout")
	memoryProfile := flags.String("memory-profile", "", "optional JSONL browser memory timeline path")
	heapProfile := flags.String("heap-profile", "", "optional Go heap profile captured after the scenario")
	memoryProfileInterval := flags.Duration("memory-profile-interval", 100*time.Millisecond, "minimum interval between active-execution samples")
	executionSampleInterval := flags.Uint64("strand-execution-sample-interval", 1_000_000, "raw Strand instructions between active-execution samples (zero disables)")
	variables := make(variableFlags)
	flags.Var(variables, "var", "scenario variable in name=value form; may be repeated")
	if err := flags.Parse(arguments); err != nil {
		return err
	}
	if flags.NArg() != 0 || strings.TrimSpace(*dist) == "" || strings.TrimSpace(*scenarioPath) == "" {
		return fmt.Errorf("usage: gossamer-scenariocheck --dist /absolute/path/to/dist --scenario /absolute/path/to/scenario.json [--var name=value]")
	}
	definition, err := loadDefinition(*scenarioPath)
	if err != nil {
		return err
	}
	recorder, err := memoryprofile.New(strings.TrimSpace(*memoryProfile), strings.TrimSpace(*heapProfile), *memoryProfileInterval)
	if err != nil {
		return fmt.Errorf("initialize memory profile: %w", err)
	}
	var profiler sitecompat.InteractionMemoryProfiler
	var page *browser.Page
	interpreter := browserruntime.InterpreterConfig{}
	if strings.TrimSpace(*memoryProfile) != "" || strings.TrimSpace(*heapProfile) != "" {
		profiler = recorder
	}
	if strings.TrimSpace(*memoryProfile) != "" && *executionSampleInterval != 0 {
		interpreter.ExecutionSampleInterval = *executionSampleInterval
		interpreter.ExecutionSample = func(task *browserruntime.TaskContext, sample browserruntime.ExecutionSample) error {
			if page == nil {
				return nil
			}
			return recorder.RecordExecution(page, task, sample)
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), *timeout)
	defer cancel()
	report, runErr := sitecompat.RunInteraction(ctx, nativeengine.New(nativeengine.Config{Interpreter: interpreter}), sitecompat.InteractionOptions{
		Dist: *dist, Definition: definition, Variables: variables, MemoryProfiler: profiler,
		PageObserver: func(current *browser.Page) { page = current },
	})
	closeErr := recorder.Close()
	encoder := json.NewEncoder(output)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(report); err != nil {
		return errors.Join(runErr, closeErr, err)
	}
	return errors.Join(runErr, closeErr)
}

func loadDefinition(path string) (sitecompat.InteractionDefinition, error) {
	var definition sitecompat.InteractionDefinition
	file, err := os.Open(strings.TrimSpace(path))
	if err != nil {
		return definition, fmt.Errorf("open interaction scenario: %w", err)
	}
	defer file.Close()
	decoder := json.NewDecoder(file)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&definition); err != nil {
		return definition, fmt.Errorf("decode interaction scenario: %w", err)
	}
	return definition, nil
}

type variableFlags map[string]string

func (variables variableFlags) String() string { return "" }

func (variables variableFlags) Set(value string) error {
	name, content, found := strings.Cut(value, "=")
	name = strings.TrimSpace(name)
	if !found || name == "" {
		return fmt.Errorf("scenario variable must use name=value")
	}
	variables[name] = content
	return nil
}
