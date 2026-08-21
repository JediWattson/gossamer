package engineparity

import (
	"context"
	"net/url"
	"strings"
	"testing"

	"github.com/JediWattson/gossamer/internal/browser"
	htmlparser "github.com/JediWattson/gossamer/internal/html"
	"github.com/JediWattson/gossamer/internal/nativeengine"
)

func TestStrandURLParity(t *testing.T) {
	runURLParity(t, nativeengine.New(nativeengine.Config{}))
}

func runURLParity(t *testing.T, engine browser.Engine) {
	t.Helper()
	document, err := htmlparser.Parse(strings.NewReader(`<!doctype html><html><body></body></html>`))
	if err != nil {
		t.Fatal(err)
	}
	browserRuntime, err := browser.NewWithEngine(engine)
	if err != nil {
		t.Fatal(err)
	}
	location, _ := url.Parse("https://parity.gossamer.test/url")
	page, err := browserRuntime.NewPage(document, location)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := page.QueueScript(browser.ScriptSource{URL: location.String(), Source: `
const target = new URL("../items?q=one#old", "https://user:pass@example.test:8443/a/b/");
if (!(target instanceof URL) || target.href !== "https://user:pass@example.test:8443/a/items?q=one#old" ||
    target.origin !== "https://example.test:8443" || target.protocol !== "https:" ||
    target.username !== "user" || target.password !== "pass" || target.host !== "example.test:8443" ||
    target.hostname !== "example.test" || target.port !== "8443" || target.pathname !== "/a/items" ||
    target.search !== "?q=one" || target.hash !== "#old") {
  throw new Error("URL read parity failed: " + target.href);
}
const params = target.searchParams;
if (params !== target.searchParams || params.get("q") !== "one") throw new Error("URL searchParams identity failed");
params.append("tag", "two words");
if (target.search !== "?q=one&tag=two+words" || !target.href.includes("tag=two+words")) {
  throw new Error("URL searchParams live update failed: " + target.href);
}
target.search = "?fresh=yes";
if (target.searchParams !== params || params.get("q") !== null || params.get("fresh") !== "yes") {
  throw new Error("URL search setter sync failed");
}
target.href = "https://next.test/path?last=1";
if (target.searchParams !== params || params.get("fresh") !== null || params.get("last") !== "1") {
  throw new Error("URL href setter sync failed");
}
target.pathname = "/changed";
target.hash = "done";
if (String(target) !== "https://next.test/changed?last=1#done" || target.toJSON() !== target.href ||
    !URL.canParse("child", "https://base.test/root/") || URL.canParse("child")) {
  throw new Error("URL mutation parity failed: " + target.href);
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
	if err := browserRuntime.Close(); err != nil {
		t.Fatal(err)
	}
}
