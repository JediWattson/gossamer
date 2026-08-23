package engineparity

import (
	"context"
	"net/url"
	"strings"
	"testing"

	"github.com/JediWattson/gossamer/internal/browser"
	htmlparser "github.com/JediWattson/gossamer/internal/html"
	"github.com/JediWattson/gossamer/internal/nativeengine"
	"github.com/JediWattson/gossamer/internal/render"
)

func TestStrandScrollIntoViewParity(t *testing.T) {
	runScrollIntoViewParity(t, nativeengine.New(nativeengine.Config{}))
}

func TestStrandScrollIntoViewHonorsNearestOptions(t *testing.T) {
	root, err := htmlparser.Parse(strings.NewReader(`<!doctype html><html><body style="margin:0">
		<div id="scroller" style="width:100px;height:100px;overflow:hidden">
			<div style="height:40px"></div>
			<button id="target" style="display:block;width:20px;height:20px">target</button>
			<div style="height:80px"></div>
		</div>
	</body></html>`))
	if err != nil {
		t.Fatal(err)
	}
	browserRuntime, err := browser.NewWithEngine(nativeengine.New(nativeengine.Config{}))
	if err != nil {
		t.Fatal(err)
	}
	defer browserRuntime.Close()
	page, err := browserRuntime.NewPage(root, &url.URL{Scheme: "https", Host: "parity.gossamer.test"})
	if err != nil {
		t.Fatal(err)
	}
	defer page.Close()
	if err := page.SetViewport(render.Viewport{Width: 100, Height: 100}); err != nil {
		t.Fatal(err)
	}

	if _, err := page.QueueScript(browser.ScriptSource{URL: "scroll-into-view-nearest.js", Source: `
const nearestScroller = document.getElementById("scroller");
const nearestTarget = document.getElementById("target");
nearestTarget.scrollIntoView({ block: "nearest", inline: "nearest" });
if (nearestScroller.scrollTop !== 0 || nearestScroller.scrollLeft !== 0) {
  throw new Error("nearest moved a visible target: " + nearestScroller.scrollLeft + "/" + nearestScroller.scrollTop);
}
nearestTarget.scrollIntoView({ block: "start", inline: "nearest" });
if (nearestScroller.scrollTop !== 40 || nearestScroller.scrollLeft !== 0) {
  throw new Error("start alignment mismatch: " + nearestScroller.scrollLeft + "/" + nearestScroller.scrollTop);
}
`}); err != nil {
		t.Fatal(err)
	}
	for page.Realm.Tasks.Len() != 0 {
		if err := page.Realm.RunOne(context.Background()); err != nil {
			t.Fatal(err)
		}
	}
	if err := page.Realm.Store().CheckInvariants(); err != nil {
		t.Fatal(err)
	}
}

func runScrollIntoViewParity(t *testing.T, engine browser.Engine) {
	t.Helper()
	root, err := htmlparser.Parse(strings.NewReader(`
<!doctype html><html><body style="margin:0">
<div style="height:700px"></div>
<button id="target" style="display:block;height:40px">target</button>
<div style="height:200px"></div>
</body></html>`))
	if err != nil {
		t.Fatal(err)
	}
	browserRuntime, err := browser.NewWithEngine(engine)
	if err != nil {
		t.Fatal(err)
	}
	defer browserRuntime.Close()
	page, err := browserRuntime.NewPage(root, &url.URL{Scheme: "https", Host: "parity.gossamer.test"})
	if err != nil {
		t.Fatal(err)
	}
	defer page.Close()
	if err := page.SetViewport(render.Viewport{Width: 320, Height: 120}); err != nil {
		t.Fatal(err)
	}

	if _, err := page.QueueScript(browser.ScriptSource{URL: "scroll-into-view.js", Source: `
const scrollTarget = document.getElementById("target");
if (typeof scrollTarget.scrollIntoView !== "function" || scrollTarget.scrollIntoView.length !== 0) {
  throw new Error("scrollIntoView shape mismatch");
}
if (scrollTarget.scrollIntoView({ block: "start" }) !== undefined) {
  throw new Error("scrollIntoView return mismatch");
}
if (window.scrollY <= 0 || scrollTarget.getBoundingClientRect().y !== 0) {
  throw new Error("scrollIntoView did not align target: " + window.scrollY + "/" + scrollTarget.getBoundingClientRect().y);
}
`}); err != nil {
		t.Fatal(err)
	}
	for page.Realm.Tasks.Len() != 0 {
		if err := page.Realm.RunOne(context.Background()); err != nil {
			t.Fatal(err)
		}
	}
	if err := page.Realm.Store().CheckInvariants(); err != nil {
		t.Fatal(err)
	}
}
