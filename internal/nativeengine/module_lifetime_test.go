package nativeengine

import (
	"context"
	"errors"
	"net/url"
	"testing"

	"github.com/JediWattson/gossamer/internal/browser"
	browserruntime "github.com/JediWattson/gossamer/internal/runtime"
)

type moduleLifetimeHost struct {
	browser.Host
	browser.DOMDocumentHost
	browser.DOMElementHost
	browser.SessionHistoryHost
	task *browserruntime.TaskContext
}

func (host moduleLifetimeHost) RuntimeTaskContext() *browserruntime.TaskContext {
	return host.task
}

func (moduleLifetimeHost) DocumentMetadata() (browser.DocumentMetadata, error) {
	return browser.DocumentMetadata{
		Root:    browser.NodeHandle{Document: 1, Node: 1},
		BaseURI: "https://gossamer.test/",
	}, nil
}

func (moduleLifetimeHost) NodeMetadata(browser.NodeHandle) (browser.NodeMetadata, error) {
	return browser.NodeMetadata{Type: browser.DOMDocumentNode, NodeName: "#document", Connected: true}, nil
}

func (moduleLifetimeHost) SessionHistorySnapshot() (browser.SessionHistorySnapshot, error) {
	location, _ := url.Parse("https://gossamer.test/")
	return browser.SessionHistorySnapshot{Length: 1, Index: 0, StateJSON: "null", URL: location}, nil
}

func TestEvaluatedModulesReleasePortableProgramsAndCompiledSource(t *testing.T) {
	t.Parallel()

	engine := New(Config{})
	realmInterface, err := engine.NewRealm()
	if err != nil {
		t.Fatal(err)
	}
	realm := realmInterface.(*Realm)
	defer engine.Close()
	runtimeRealm, err := browserruntime.NewRealm(910, nil)
	if err != nil {
		t.Fatal(err)
	}

	const rootURL = "https://gossamer.test/root.js"
	const staticURL = "https://gossamer.test/static.js"
	const lazyURL = "https://gossamer.test/lazy.js"
	const rootSource = `
import {value} from "./static.js";
export const result = value;
export function load() { return import("./lazy.js"); }
`
	const staticSource = `export const value = 42;`
	const lazySource = `export const value = 99;`
	graph := browser.ModuleGraph{
		RootURL: rootURL,
		Sources: []browser.ScriptSource{
			{URL: rootURL, Source: rootSource},
			{URL: staticURL, Source: staticSource},
			{URL: lazyURL, Source: lazySource},
		},
		Resolutions: []browser.ModuleResolution{
			{Referrer: rootURL, Specifier: "./static.js", URL: staticURL},
			{Referrer: rootURL, Specifier: "./lazy.js", URL: lazyURL},
		},
	}
	runModuleGraph(t, realm, runtimeRealm, graph)

	profile, err := realm.ScriptMemoryProfile()
	if err != nil {
		t.Fatal(err)
	}
	if profile.Modules != 3 || profile.ModuleSourceBytes != uint64(len(lazySource)) {
		t.Fatalf("lazy module profile = %#v", profile)
	}
	assertNoPortableModulePrograms(t, profile)

	runModuleGraph(t, realm, runtimeRealm, browser.ModuleGraph{
		RootURL: lazyURL,
		Sources: []browser.ScriptSource{{URL: lazyURL, Source: lazySource}},
	})
	profile, err = realm.ScriptMemoryProfile()
	if err != nil {
		t.Fatal(err)
	}
	if profile.ModuleSourceBytes != 0 {
		t.Fatalf("compiled module source bytes = %d, want zero", profile.ModuleSourceBytes)
	}
	assertNoPortableModulePrograms(t, profile)
	if err := realm.runtime.Store().CheckInvariants(); err != nil {
		t.Fatal(err)
	}
}

func runModuleGraph(t *testing.T, realm *Realm, runtimeRealm *browserruntime.Realm, graph browser.ModuleGraph) {
	t.Helper()
	_, err := runtimeRealm.EnqueueTask(func(task *browserruntime.TaskContext) error {
		host := moduleLifetimeHost{task: task}
		return errors.Join(realm.EvaluateModule(host, graph), realm.DrainMicrotasks(host))
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := runtimeRealm.RunOne(context.Background()); err != nil {
		t.Fatal(err)
	}
}

func assertNoPortableModulePrograms(t *testing.T, profile browser.ScriptMemoryProfile) {
	t.Helper()
	if profile.PortableFunctions != 0 || profile.PortableFunctionBytes != 0 || profile.PortableCodeBytes != 0 ||
		profile.PortableConstantBytes != 0 || profile.PortableLocationBytes != 0 {
		t.Fatalf("portable module storage survived materialization: %#v", profile)
	}
}

func TestDynamicImportSurvivesSiblingModuleErrorAndCachesErrorAcrossTasks(t *testing.T) {
	t.Parallel()

	engine := New(Config{})
	realmInterface, err := engine.NewRealm()
	if err != nil {
		t.Fatal(err)
	}
	realm := realmInterface.(*Realm)
	defer engine.Close()
	runtimeRealm, err := browserruntime.NewRealm(911, nil)
	if err != nil {
		t.Fatal(err)
	}

	const (
		rootURL       = "https://gossamer.test/root-err.js"
		failURL       = "https://gossamer.test/fail.js"
		siblingURL    = "https://gossamer.test/sibling.js"
		task2URL      = "https://gossamer.test/task2.js"
		task3URL      = "https://gossamer.test/task3.js"
		failSource    = `export const broken = true; throw new Error("module-boom");`
		siblingSource = `export const okValue = 123;`
	)
	graph1 := browser.ModuleGraph{
		RootURL: rootURL,
		Sources: []browser.ScriptSource{
			{URL: rootURL, Source: `import "./fail.js"; import "./sibling.js";`},
			{URL: failURL, Source: failSource},
			{URL: siblingURL, Source: siblingSource},
		},
		Resolutions: []browser.ModuleResolution{
			{Referrer: rootURL, Specifier: "./fail.js", URL: failURL},
			{Referrer: rootURL, Specifier: "./sibling.js", URL: siblingURL},
		},
	}

	// Task 1: fail.js throws before sibling.js evaluates, leaving sibling.js in
	// moduleLinked across the task boundary and caching fail.js's thrown Error.
	_, err = runtimeRealm.EnqueueTask(func(task *browserruntime.TaskContext) error {
		host := moduleLifetimeHost{task: task}
		evalErr := realm.EvaluateModule(host, graph1)
		if evalErr == nil {
			t.Fatal("expected initial graph evaluation to fail")
		}
		return realm.DrainMicrotasks(host)
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := runtimeRealm.RunOne(context.Background()); err != nil {
		t.Fatal(err)
	}

	// Task 2: evaluate the un-executed sibling module and capture the first
	// re-import rejection across the task boundary.
	runModuleGraph(t, realm, runtimeRealm, browser.ModuleGraph{
		RootURL: task2URL,
		Sources: []browser.ScriptSource{
			{URL: task2URL, Source: `
import { okValue } from "./sibling.js";
globalThis.__siblingValue = okValue;
import("./fail.js").catch((err) => {
	globalThis.__failErr1 = err;
});
`},
			{URL: failURL, Source: failSource},
			{URL: siblingURL, Source: siblingSource},
		},
		Resolutions: []browser.ModuleResolution{
			{Referrer: task2URL, Specifier: "./sibling.js", URL: siblingURL},
			{Referrer: task2URL, Specifier: "./fail.js", URL: failURL},
		},
	})

	// Task 3: re-import the failed module in yet another task and verify the
	// cached Error identity matches without triggering stale memory references.
	runModuleGraph(t, realm, runtimeRealm, browser.ModuleGraph{
		RootURL: task3URL,
		Sources: []browser.ScriptSource{
			{URL: task3URL, Source: `
import("./fail.js").catch((err) => {
	if (globalThis.__siblingValue !== 123) {
		throw new Error("unexpected sibling export: " + String(globalThis.__siblingValue));
	}
	if (err !== globalThis.__failErr1) {
		throw new Error("cached module error identity mismatch across tasks");
	}
	if (!(err instanceof Error) || err.message !== "module-boom") {
		throw new Error("unexpected cached module error: " + String(err));
	}
	globalThis.__crossTaskChecked = true;
});
`},
			{URL: failURL, Source: failSource},
		},
		Resolutions: []browser.ModuleResolution{
			{Referrer: task3URL, Specifier: "./fail.js", URL: failURL},
		},
	})

	_, err = runtimeRealm.EnqueueTask(func(task *browserruntime.TaskContext) error {
		host := moduleLifetimeHost{task: task}
		return errors.Join(realm.Evaluate(host, browser.ScriptSource{
			URL:    "https://gossamer.test/verify.js",
			Source: `if (globalThis.__crossTaskChecked !== true) { throw new Error("cross-task module verification failed"); }`,
		}), realm.DrainMicrotasks(host))
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := runtimeRealm.RunOne(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := realm.runtime.Store().CheckInvariants(); err != nil {
		t.Fatal(err)
	}
}
