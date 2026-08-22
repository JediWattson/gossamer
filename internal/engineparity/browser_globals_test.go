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

func TestStrandBrowserGlobalsParity(t *testing.T) {
	runBrowserGlobalsParity(t, nativeengine.New(nativeengine.Config{}))
}

func runBrowserGlobalsParity(t *testing.T, engine browser.Engine) {
	t.Helper()
	document, err := htmlparser.Parse(strings.NewReader(`<!doctype html><html><body></body></html>`))
	if err != nil {
		t.Fatal(err)
	}
	browserRuntime, err := browser.NewWithEngine(engine)
	if err != nil {
		t.Fatal(err)
	}
	location, _ := url.Parse("https://parity.gossamer.test/globals")
	page, err := browserRuntime.NewPage(document, location)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := page.QueueScript(browser.ScriptSource{URL: location.String(), Source: `
if (navigator !== window.navigator || !navigator.userAgent.includes("Gossamer") ||
    navigator.language !== "en-US" || navigator.languages[0] !== "en-US" ||
    navigator.hardwareConcurrency !== 4 || navigator.maxTouchPoints !== 0 || !navigator.onLine) {
  throw new Error("navigator parity failed");
}
const wide = matchMedia("screen and (min-width: 800px) and (orientation: landscape)");
if (!wide.matches || wide.media !== "screen and (min-width: 800px) and (orientation: landscape)" ||
    matchMedia("(display-mode: standalone)").matches || !matchMedia("(prefers-color-scheme: light)").matches ||
    typeof wide.addEventListener !== "function" || wide.dispatchEvent(new Event("change")) !== false) {
  throw new Error("matchMedia parity failed");
}
const script = document.createElement("script");
const video = document.createElement("video");
const image = new Image();
if (!(document.head instanceof HTMLHeadElement) || !(script instanceof HTMLScriptElement) ||
    !(video instanceof HTMLMediaElement) || !(image instanceof Image) || !(image instanceof HTMLImageElement) ||
    image.localName !== "img" || HTMLMediaElement.HAVE_METADATA !== 1 || video.HAVE_ENOUGH_DATA !== 4) {
  throw new Error("specialized HTML interface parity failed");
}
const interval = setInterval(() => { throw new Error("cleared interval fired"); }, 60000);
clearInterval(interval);
if (typeof interval !== "number") throw new Error("interval timer parity failed");
if (console !== window.console || typeof console.log !== "function" ||
    console.log("strand", { owned: true }) !== undefined || console.warn("warning") !== undefined) {
  throw new Error("console parity failed");
}
const keyboard = new KeyboardEvent("keydown", { key: "Enter", code: "Enter", altKey: true, cancelable: true });
if (!(keyboard instanceof KeyboardEvent) || !(keyboard instanceof Event) || keyboard.key !== "Enter" ||
    keyboard.code !== "Enter" || !keyboard.altKey || !keyboard.getModifierState("Alt") ||
    keyboard.getModifierState("Shift")) {
  throw new Error("KeyboardEvent parity failed");
}
const intersection = new IntersectionObserver(() => {});
intersection.observe(document.body);
intersection.unobserve(document.body);
if (!(intersection instanceof IntersectionObserver) || intersection.takeRecords().length !== 0 ||
    intersection.disconnect() !== undefined) {
  throw new Error("IntersectionObserver surface failed");
}
const firstUUID = crypto.randomUUID();
const secondUUID = crypto.randomUUID();
if (crypto !== window.crypto || firstUUID === secondUUID ||
    !/^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$/.test(firstUUID)) {
  throw new Error("crypto.randomUUID parity failed");
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
