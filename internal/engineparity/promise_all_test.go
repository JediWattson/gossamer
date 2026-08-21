package engineparity

import (
	"context"
	"net/url"
	"testing"

	"github.com/JediWattson/gossamer/internal/browser"
	"github.com/JediWattson/gossamer/internal/dom"
	"github.com/JediWattson/gossamer/internal/nativeengine"
)

func TestStrandPromiseAllParity(t *testing.T) {
	runPromiseAllParity(t, nativeengine.New(nativeengine.Config{}))
}

func runPromiseAllParity(t *testing.T, engine browser.Engine) {
	t.Helper()
	browserRuntime, err := browser.NewWithEngine(engine)
	if err != nil {
		t.Fatal(err)
	}
	location, _ := url.Parse("https://parity.gossamer.test/promise-all.html")
	page, err := browserRuntime.NewPage(dom.NewDocument(), location)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := page.QueueScript(browser.ScriptSource{URL: location.String(), Source: `
globalThis.__promiseAll = [];
Promise.all([Promise.resolve("a"), "b", Promise.resolve("c")]).then(values => {
  __promiseAll.push(values.join(":"));
});
Promise.all([]).then(values => __promiseAll.push("empty:" + values.length));
Promise.all([Promise.resolve(1), Promise.reject("no"), Promise.resolve(3)]).then(
  () => __promiseAll.push("unexpected"),
  reason => __promiseAll.push("rejected:" + reason)
);
queueMicrotask(() => __promiseAll.push("microtask"));
`}); err != nil {
		t.Fatal(err)
	}
	if err := page.Realm.RunOne(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, err := page.QueueScript(browser.ScriptSource{URL: location.String() + "assert", Source: `
if (__promiseAll.join("|") !== "empty:0|microtask|a:b:c|rejected:no") {
  throw new Error("Promise.all ordering: " + __promiseAll.join("|"));
}
`}); err != nil {
		t.Fatal(err)
	}
	if err := page.Realm.RunOne(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := page.Close(); err != nil {
		t.Fatal(err)
	}
	if stats := browserRuntime.Ledger().Stats(); stats.LiveObjects != 0 || stats.PersistentObjects != 0 {
		t.Fatalf("Promise.all teardown ownership = %#v", stats)
	}
	if err := browserRuntime.Close(); err != nil {
		t.Fatal(err)
	}
}
