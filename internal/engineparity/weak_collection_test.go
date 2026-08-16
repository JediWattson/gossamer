package engineparity

import (
	"context"
	"net/url"
	"testing"

	"github.com/JediWattson/gossamer/internal/browser"
	"github.com/JediWattson/gossamer/internal/dom"
	"github.com/JediWattson/gossamer/internal/nativeengine"
)

func TestStrandWeakCollectionParity(t *testing.T) {
	runWeakCollectionParity(t, nativeengine.New(nativeengine.Config{}))
}

func runWeakCollectionParity(t *testing.T, engine browser.Engine) {
	t.Helper()
	browserRuntime, err := browser.NewWithEngine(engine)
	if err != nil {
		t.Fatal(err)
	}
	location, _ := url.Parse("https://parity.gossamer.test/weak-collections.html")
	page, err := browserRuntime.NewPage(dom.NewDocument(), location)
	if err != nil {
		t.Fatal(err)
	}
	run := func(label, source string) {
		t.Helper()
		if _, err := page.QueueScript(browser.ScriptSource{URL: label + ".js", Source: source}); err != nil {
			t.Fatal(err)
		}
		if err := page.Realm.RunOne(context.Background()); err != nil {
			t.Fatal(err)
		}
	}
	run("create-weak", `
let parityWeakKey = {};
let parityWeakOther = {};
let parityWeakMap = new WeakMap();
let parityWeakSet = new WeakSet();
if (parityWeakMap.set(parityWeakKey, 7) !== parityWeakMap ||
    parityWeakSet.add(parityWeakKey) !== parityWeakSet) {
  throw new Error("weak collection fluent methods diverged");
}
`)
	run("verify-weak", `
if (parityWeakMap.get(parityWeakKey) !== 7 || !parityWeakMap.has(parityWeakKey) ||
    parityWeakMap.has(parityWeakOther) || parityWeakMap.get(1) !== undefined ||
    parityWeakMap.has(1) || !parityWeakSet.has(parityWeakKey) ||
    parityWeakSet.has(parityWeakOther) || parityWeakSet.has(1) ||
    !parityWeakMap.delete(parityWeakKey) || parityWeakMap.has(parityWeakKey) ||
    !parityWeakSet.delete(parityWeakKey) || parityWeakSet.has(parityWeakKey)) {
  throw new Error("weak collection identity parity failed");
}
`)
	if err := page.Close(); err != nil {
		t.Fatalf("close page: %v", err)
	}
	if stats := browserRuntime.Ledger().Stats(); stats.LiveObjects != 0 || stats.PersistentObjects != 0 {
		t.Fatalf("weak collection teardown ownership = %#v", stats)
	}
	if err := browserRuntime.Close(); err != nil {
		t.Fatalf("close browser: %v", err)
	}
}
