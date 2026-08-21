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

func TestStrandTextCodecParity(t *testing.T) {
	runTextCodecParity(t, nativeengine.New(nativeengine.Config{}))
}

func runTextCodecParity(t *testing.T, engine browser.Engine) {
	t.Helper()
	document, err := htmlparser.Parse(strings.NewReader(`<!doctype html><html><body></body></html>`))
	if err != nil {
		t.Fatal(err)
	}
	browserRuntime, err := browser.NewWithEngine(engine)
	if err != nil {
		t.Fatal(err)
	}
	location, _ := url.Parse("https://parity.gossamer.test/codec")
	page, err := browserRuntime.NewPage(document, location)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := page.QueueScript(browser.ScriptSource{URL: location.String(), Source: `
const encoder = new TextEncoder();
const bytes = encoder.encode("A🙂");
if (!(bytes instanceof Uint8Array) || encoder.encoding !== "utf-8" || bytes.length !== 5 || bytes[0] !== 65 || bytes[4] !== 130) {
  throw new Error("TextEncoder parity failed");
}
const array = Uint8Array.from([1, 2, 3], value => value + 1);
array.set([9], 1);
const slice = array.slice(1);
const view = array.subarray(0, 2);
view.fill(7, 1);
if (!(array instanceof Uint8Array) || array.length !== 3 || array[0] !== 2 || array[1] !== 7 ||
    slice[0] !== 9 || slice[1] !== 4 || view.buffer !== array.buffer) {
  throw new Error("Uint8Array parity failed");
}
const destination = new Uint8Array(4);
const progress = encoder.encodeInto("é🙂", destination);
if (progress.read !== 1 || progress.written !== 2 || destination[0] !== 195 || destination[1] !== 169) {
  throw new Error("TextEncoder.encodeInto parity failed");
}
const decoder = new TextDecoder("UTF8");
if (decoder.encoding !== "utf-8" || decoder.fatal || decoder.ignoreBOM || decoder.decode(bytes) !== "A🙂") {
  throw new Error("TextDecoder parity failed");
}
let rejected = false;
try { new TextDecoder("utf-8", {fatal: true}).decode(new Uint8Array([255])); }
catch (error) { rejected = error instanceof TypeError; }
if (!rejected) throw new Error("fatal TextDecoder accepted invalid UTF-8");
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
