package nativeengine_test

import (
	"context"
	"errors"
	"net/url"
	"testing"

	"github.com/JediWattson/gossamer/internal/browser"
	"github.com/JediWattson/gossamer/internal/dom"
	"github.com/JediWattson/gossamer/internal/nativeengine"
	"github.com/JediWattson/gossamer/internal/runtime/memory"
)

func TestBrowserTasksExecuteNativeScriptsAcrossRealmCheckpoints(t *testing.T) {
	t.Parallel()

	scriptEngine := nativeengine.New(nativeengine.Config{})
	browserRuntime, err := browser.NewWithEngine(scriptEngine)
	if err != nil {
		t.Fatal(err)
	}
	defer browserRuntime.Close()
	location, _ := url.Parse("https://gossamer.test/native/")
	page, err := browserRuntime.NewPage(dom.NewDocument(), location)
	if err != nil {
		t.Fatal(err)
	}
	scriptRealm, ok := scriptEngine.LatestRealm()
	if !ok {
		t.Fatal("native engine did not create a JSRealm")
	}

	run := func(label, source string) {
		t.Helper()
		if _, err := page.QueueScript(browser.ScriptSource{URL: location.ResolveReference(&url.URL{Path: label + ".js"}).String(), Source: source}); err != nil {
			t.Fatalf("QueueScript(%s): %v", label, err)
		}
		if err := page.Realm.RunOne(context.Background()); err != nil {
			t.Fatalf("RunOne(%s): %v", label, err)
		}
	}

	run("first", `
let counter = 40;
function add(value) { counter = counter + value; }
queueMicrotask(function () { add(2); });
`)
	firstProfile := scriptRealm.Profile()
	firstStats := page.Realm.Store().Stats()
	run("second", `
if (counter !== 42) { throw new Error("global or microtask state was lost"); }
Promise.resolve(5).then(add);
`)
	secondProfile := scriptRealm.Profile()
	secondStats := page.Realm.Store().Stats()
	run("third", `
if (counter !== 47) { throw new Error("Promise checkpoint was not retained"); }
counter;
`)
	thirdStats := page.Realm.Store().Stats()
	if firstProfile.PersistentRegion == 0 || secondProfile.PersistentRegion == firstProfile.PersistentRegion {
		t.Fatalf("persistent region did not rotate: first=%#v second=%#v", firstProfile, secondProfile)
	}
	released, err := page.Realm.Store().Region(firstProfile.PersistentRegion)
	if err != nil {
		t.Fatal(err)
	}
	if released.State != memory.RegionDestroyed {
		t.Fatalf("replaced persistent region state = %v, want destroyed", released.State)
	}

	profile := scriptRealm.Profile()
	if profile.Evaluations != 3 || profile.Checkpoints != 3 || profile.ActiveTask != 0 || profile.PersistentRegion == 0 {
		t.Fatalf("native Realm profile = %#v", profile)
	}
	if firstStats.LiveRegions != secondStats.LiveRegions || secondStats.LiveRegions != thirdStats.LiveRegions {
		t.Fatalf("task checkpoints retained regions: first=%#v second=%#v third=%#v", firstStats, secondStats, thirdStats)
	}
	if secondStats.LiveSlots != thirdStats.LiveSlots {
		t.Fatalf("unreachable script graph survived checkpoint: second=%#v third=%#v", secondStats, thirdStats)
	}
	if err := page.Realm.Store().CheckInvariants(); err != nil {
		t.Fatal(err)
	}

	persistentRegion := profile.PersistentRegion
	store := page.Realm.Store()
	if err := page.Close(); err != nil {
		t.Fatal(err)
	}
	region, err := store.Region(persistentRegion)
	if err != nil {
		t.Fatal(err)
	}
	if region.State != memory.RegionDestroyed {
		t.Fatalf("persistent region state after Page.Close = %v, want destroyed", region.State)
	}
	engineProfile := scriptEngine.Profile()
	if engineProfile.RealmsCreated != 1 || engineProfile.RealmsClosed != 1 || engineProfile.LiveRealms != 0 || engineProfile.Evaluations != 3 || engineProfile.Checkpoints != 3 {
		t.Fatalf("native Engine profile = %#v", engineProfile)
	}
}

func TestNativeRealmRejectsCallsWithoutRuntimeTaskHost(t *testing.T) {
	t.Parallel()

	engine := nativeengine.New(nativeengine.Config{})
	defer engine.Close()
	created, err := engine.NewRealm()
	if err != nil {
		t.Fatal(err)
	}
	realm := created.(*nativeengine.Realm)
	err = realm.Evaluate(nil, browser.ScriptSource{URL: "missing-host.js", Source: "1 + 1;"})
	if !errors.Is(err, nativeengine.ErrNativeTaskHost) {
		t.Fatalf("Evaluate error = %v, want ErrNativeTaskHost", err)
	}
}
