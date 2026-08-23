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
