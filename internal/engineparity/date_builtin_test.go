package engineparity

import (
	"context"
	"net/url"
	"testing"

	"github.com/JediWattson/gossamer/internal/browser"
	"github.com/JediWattson/gossamer/internal/dom"
	"github.com/JediWattson/gossamer/internal/nativeengine"
)

func TestStrandDateBuiltinParity(t *testing.T) {
	runDateBuiltinParity(t, nativeengine.New(nativeengine.Config{}))
}

func runDateBuiltinParity(t *testing.T, engine browser.Engine) {
	t.Helper()
	browserRuntime, err := browser.NewWithEngine(engine)
	if err != nil {
		t.Fatal(err)
	}
	location, _ := url.Parse("https://parity.gossamer.test/date-builtins.html")
	page, err := browserRuntime.NewPage(dom.NewDocument(), location)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := page.QueueScript(browser.ScriptSource{URL: location.String(), Source: `
const epoch = new Date(0);
if (epoch.getTime() !== 0 || epoch.valueOf() !== 0 || epoch.toISOString() !== "1970-01-01T00:00:00.000Z" ||
    epoch.getUTCMonth() !== 0 || epoch.getUTCDate() !== 1 || typeof epoch.toLocaleTimeString() !== "string" ||
    JSON.stringify({at: epoch}) !== '{"at":"1970-01-01T00:00:00.000Z"}') {
  throw new Error("Date builtin parity failed");
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
	if err := browserRuntime.Close(); err != nil {
		t.Fatal(err)
	}
}
