package engineparity

import (
	"context"
	"net/url"
	"testing"

	"github.com/JediWattson/gossamer/internal/browser"
	"github.com/JediWattson/gossamer/internal/dom"
	"github.com/JediWattson/gossamer/internal/nativeengine"
)

func TestStrandObjectBindingParity(t *testing.T) {
	runObjectBindingParity(t, nativeengine.New(nativeengine.Config{}))
}

func runObjectBindingParity(t *testing.T, engine browser.Engine) {
	t.Helper()
	browserRuntime, err := browser.NewWithEngine(engine)
	if err != nil {
		t.Fatal(err)
	}
	location, _ := url.Parse("https://parity.gossamer.test/object-binding.html")
	page, err := browserRuntime.NewPage(dom.NewDocument(), location)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := page.QueueScript(browser.ScriptSource{URL: location.String(), Source: `
let reads = 0;
let missing;
let source = {first: 4, get second() { reads++; return missing; }, nullable: null};
let {first: renamed, second = renamed + 1, third = 9, nullable = 10} = source;
if (renamed !== 4 || second !== 5 || third !== 9 || nullable !== null || reads !== 1) {
  throw new Error("object binding parity failed");
}
`}); err != nil {
		t.Fatal(err)
	}
	for page.Realm.Tasks.Len() != 0 {
		if err := page.Realm.RunOne(context.Background()); err != nil {
			t.Fatal(err)
		}
	}
	if err := page.Close(); err != nil {
		t.Fatal(err)
	}
	if stats := browserRuntime.Ledger().Stats(); stats.LiveObjects != 0 || stats.PersistentObjects != 0 {
		t.Fatalf("object binding teardown ownership = %#v", stats)
	}
	if err := browserRuntime.Close(); err != nil {
		t.Fatal(err)
	}
}
