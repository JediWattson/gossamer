//go:build v8 && cgo && darwin && arm64

package main

import (
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"time"

	"github.com/JediWattson/gossamer/internal/browser"
	"github.com/JediWattson/gossamer/internal/loader"
	browserruntime "github.com/JediWattson/gossamer/internal/runtime"
	"github.com/JediWattson/gossamer/internal/runtime/memory"
	"github.com/JediWattson/gossamer/internal/v8engine"
)

const profileDocument = `<!doctype html><html><body><main id="mount"></main></body></html>`

const defaultProfileScript = `
(() => {
  const mount = document.getElementById("mount");
  globalThis.__gossamerProfile = Array.from(
    { length: 25000 },
    (_, index) => ({ index, payload: "gossamer".repeat(16) }),
  );
  for (let index = 0; index < 256; index++) {
    const row = document.createElement("div");
    row.setAttribute("data-index", String(index));
    row.textContent = "row-" + index;
    mount.appendChild(row);
  }
  Promise.resolve().then(() => { globalThis.__gossamerMicrotaskRan = true; });
})();
`

const releaseProfileScript = `
globalThis.__gossamerProfile = undefined;
document.getElementById("mount").replaceChildren();
`

type checkpoint struct {
	Sequence      uint64                      `json:"sequence"`
	Name          string                      `json:"name"`
	V8            v8engine.RealmProfile       `json:"v8"`
	Go            browserruntime.RealmProfile `json:"go"`
	DocumentNodes int                         `json:"documentNodes"`
	LiveNodes     int                         `json:"liveNodes"`
}

type report struct {
	V8Version  string                 `json:"v8Version"`
	Iterations int                    `json:"iterations"`
	Timeline   []checkpoint           `json:"timeline"`
	Closed     v8engine.EngineProfile `json:"closed"`
}

func main() {
	icuData := flag.String("icu-data", os.Getenv("GOSSAMER_V8_ICU_DATA"), "path to the pinned V8 build's icudtl.dat")
	scriptPath := flag.String("script", "", "optional browser JavaScript file to profile")
	iterations := flag.Int("iterations", 1, "number of browser workload iterations")
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
	browserRuntime, err := browser.NewWithEngine(engine)
	if err != nil {
		fatalf("initialize browser runtime: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	page, err := browserRuntime.LoadPage(ctx, "https://profile.gossamer.test/", staticLoader{})
	if err != nil {
		fatalf("load profile page: %v", err)
	}
	realm, ok := engine.LatestRealm()
	if !ok {
		fatalf("profile page has no stock V8 realm")
	}

	timeline := make([]checkpoint, 0, *iterations+3)
	sequence := uint64(0)
	takeCheckpoint := func(name string) {
		profile, profileErr := realm.Profile()
		if profileErr != nil {
			fatalf("%s V8 profile: %v", name, profileErr)
		}
		sequence++
		document := page.Document()
		timeline = append(timeline, checkpoint{
			Sequence:      sequence,
			Name:          name,
			V8:            profile,
			Go:            page.Realm.Profile(),
			DocumentNodes: document.Store().Len(),
			LiveNodes:     document.Store().LiveLen(),
		})
	}
	takeCheckpoint("baseline")

	for iteration := 0; iteration < *iterations; iteration++ {
		if err := queueNativeProfileGraph(page.Realm); err != nil {
			fatalf("queue native graph %d: %v", iteration+1, err)
		}
		if _, err := page.QueueScript(browser.ScriptSource{
			URL:    fmt.Sprintf("https://profile.gossamer.test/workload-%d.js", iteration+1),
			Source: source,
		}); err != nil {
			fatalf("queue browser workload %d: %v", iteration+1, err)
		}
		if err := page.Realm.RunOne(ctx); err != nil {
			fatalf("run native producer %d: %v", iteration+1, err)
		}
		if err := page.Realm.RunOne(ctx); err != nil {
			fatalf("run browser workload %d: %v", iteration+1, err)
		}
		takeCheckpoint(fmt.Sprintf("iteration-%d-live", iteration+1))
		if err := runUntilIdle(ctx, page.Realm); err != nil {
			fatalf("settle browser workload %d: %v", iteration+1, err)
		}
	}

	if _, err := page.QueueScript(browser.ScriptSource{
		URL:    "https://profile.gossamer.test/release.js",
		Source: releaseProfileScript,
	}); err != nil {
		fatalf("queue release workload: %v", err)
	}
	if err := runUntilIdle(ctx, page.Realm); err != nil {
		fatalf("run release workload: %v", err)
	}
	if err := realm.CollectGarbage(page); err != nil {
		fatalf("collect stock V8 and wrapper garbage: %v", err)
	}
	if err := page.Realm.Store().CheckInvariants(); err != nil {
		fatalf("Go heap invariants after GC: %v", err)
	}
	takeCheckpoint("after-gc")

	if err := page.Close(); err != nil {
		fatalf("close profile page: %v", err)
	}
	closed := engine.Profile()
	if err := browserRuntime.Close(); err != nil {
		fatalf("close browser runtime: %v", err)
	}

	encoder := json.NewEncoder(os.Stdout)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(report{
		V8Version:  engine.Version(),
		Iterations: *iterations,
		Timeline:   timeline,
		Closed:     closed,
	}); err != nil {
		fatalf("encode profile: %v", err)
	}
}

func queueNativeProfileGraph(realm *browserruntime.Realm) error {
	_, err := realm.EnqueueTask(func(context *browserruntime.TaskContext) error {
		name, err := context.NewString("profile-root")
		if err != nil {
			return err
		}
		object, err := context.NewHeapObject()
		if err != nil {
			return err
		}
		array, err := context.NewArray(1)
		if err != nil {
			return err
		}
		if err := context.SetArrayElement(array, 0, memory.RefValue(object)); err != nil {
			return err
		}
		buffer, err := context.NewArrayBuffer([]byte("gossamer-profile"))
		if err != nil {
			return err
		}
		view, err := context.NewTypedArray(buffer, memory.ElementUint8, 0, uint64(len("gossamer-profile")))
		if err != nil {
			return err
		}
		nativeMap, err := context.NewMap()
		if err != nil {
			return err
		}
		if err := context.MapSet(nativeMap, memory.RefValue(name), memory.RefValue(view)); err != nil {
			return err
		}
		root, err := context.NewCell()
		if err != nil {
			return err
		}
		for field, ref := range []memory.Ref{name, object, array, buffer, view, nativeMap} {
			if err := context.Set(root, field, memory.RefValue(ref)); err != nil {
				return err
			}
		}
		_, err = context.Transfer(context.Realm, func(*browserruntime.TaskContext) error { return nil }, root)
		return err
	})
	return err
}

func runUntilIdle(ctx context.Context, realm *browserruntime.Realm) error {
	for realm.Tasks.Len() != 0 {
		if err := realm.RunOne(ctx); err != nil {
			return err
		}
	}
	return nil
}

type staticLoader struct{}

func (staticLoader) Load(_ context.Context, rawURL string) (*loader.Response, error) {
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return nil, err
	}
	return &loader.Response{
		URL:        parsed,
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"text/html; charset=utf-8"}},
		Body:       io.NopCloser(bytes.NewBufferString(profileDocument)),
	}, nil
}

func fatalf(format string, arguments ...any) {
	fmt.Fprintf(os.Stderr, "gossamer-v8-profile: "+format+"\n", arguments...)
	os.Exit(1)
}
