//go:build v8 && cgo && darwin && arm64

package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"

	"github.com/JediWattson/gossamer/internal/browser"
	"github.com/JediWattson/gossamer/internal/v8engine"
)

const defaultProfileScript = `
globalThis.__gossamerProfile = Array.from(
  { length: 100000 },
  (_, index) => ({ index, payload: "gossamer".repeat(32) }),
);
Promise.resolve().then(() => { globalThis.__gossamerMicrotaskRan = true; });
`

type report struct {
	V8Version string                 `json:"v8Version"`
	Baseline  v8engine.RealmProfile  `json:"baseline"`
	AfterRun  v8engine.RealmProfile  `json:"afterRun"`
	AfterGC   v8engine.RealmProfile  `json:"afterGC"`
	Closed    v8engine.EngineProfile `json:"closed"`
}

func main() {
	icuData := flag.String("icu-data", os.Getenv("GOSSAMER_V8_ICU_DATA"), "path to the pinned V8 build's icudtl.dat")
	scriptPath := flag.String("script", "", "optional JavaScript file to profile")
	iterations := flag.Int("iterations", 1, "number of source evaluations")
	samplingInterval := flag.Uint64("sampling-interval", v8engine.DefaultSamplingInterval, "mean bytes between V8 allocation samples")
	flag.Parse()

	if *iterations < 1 {
		fatalf("iterations must be positive")
	}
	source := defaultProfileScript
	if *scriptPath != "" {
		contents, err := os.ReadFile(*scriptPath)
		if err != nil {
			fatalf("read script: %v", err)
		}
		source = string(contents)
	}

	engine, err := v8engine.New(v8engine.Config{
		ICUDataPath:      *icuData,
		SamplingInterval: *samplingInterval,
	})
	if err != nil {
		fatalf("initialize stock V8: %v", err)
	}
	realmValue, err := engine.NewRealm()
	if err != nil {
		fatalf("create V8 realm: %v", err)
	}
	realm := realmValue.(*v8engine.Realm)

	baseline, err := realm.Profile()
	if err != nil {
		fatalf("baseline profile: %v", err)
	}
	for iteration := 0; iteration < *iterations; iteration++ {
		if err := realm.Evaluate(nil, browser.ScriptSource{
			URL:    fmt.Sprintf("gossamer-profile-%d.js", iteration+1),
			Source: source,
		}); err != nil {
			fatalf("evaluate: %v", err)
		}
		if err := realm.DrainMicrotasks(nil); err != nil {
			fatalf("drain microtasks: %v", err)
		}
	}
	afterRun, err := realm.Profile()
	if err != nil {
		fatalf("post-run profile: %v", err)
	}
	if err := realm.Evaluate(nil, browser.ScriptSource{
		URL:    "gossamer-profile-release.js",
		Source: "globalThis.__gossamerProfile = undefined;",
	}); err != nil {
		fatalf("release profile objects: %v", err)
	}
	if err := realm.CollectGarbage(); err != nil {
		fatalf("collect garbage: %v", err)
	}
	afterGC, err := realm.Profile()
	if err != nil {
		fatalf("post-GC profile: %v", err)
	}
	if err := realm.Close(); err != nil {
		fatalf("close realm: %v", err)
	}
	closed := engine.Profile()
	if err := engine.Close(); err != nil {
		fatalf("close engine: %v", err)
	}

	encoder := json.NewEncoder(os.Stdout)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(report{
		V8Version: engine.Version(),
		Baseline:  baseline,
		AfterRun:  afterRun,
		AfterGC:   afterGC,
		Closed:    closed,
	}); err != nil {
		fatalf("encode profile: %v", err)
	}
}

func fatalf(format string, arguments ...any) {
	fmt.Fprintf(os.Stderr, "gossamer-v8-profile: "+format+"\n", arguments...)
	os.Exit(1)
}
