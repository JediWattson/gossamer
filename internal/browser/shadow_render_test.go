package browser

import (
	"net/url"
	"strings"
	"testing"

	"github.com/JediWattson/gossamer/internal/dom"
	htmlparser "github.com/JediWattson/gossamer/internal/html"
)

func TestClosedShadowTreeReplacesLightChildrenInRenderedComposition(t *testing.T) {
	t.Parallel()

	browserRuntime, err := New()
	if err != nil {
		t.Fatal(err)
	}
	defer browserRuntime.Close()
	root, err := htmlparser.Parse(strings.NewReader(`<!doctype html><html><body><div id="host">light fallback</div></body></html>`))
	if err != nil {
		t.Fatal(err)
	}
	location, _ := url.Parse("https://shadow.test/")
	page, err := browserRuntime.NewPage(root, location)
	if err != nil {
		t.Fatal(err)
	}
	hostID, _ := page.Document().ElementByID("host")
	shadowID, err := page.Document().AttachShadow(hostID, dom.ShadowRootClosed, false)
	if err != nil {
		t.Fatal(err)
	}
	shadow := dom.NewElement("span", dom.Attribute{Name: "style", Value: "color:#123456"})
	shadow.AppendChild(dom.NewText("shadow content"))
	shadowChildID, err := page.Document().AppendChild(shadowID, shadow)
	if err != nil {
		t.Fatal(err)
	}
	if err := page.Render(); err != nil {
		t.Fatal(err)
	}
	if !frameHasText(page.Frame(), "shadow content") {
		_, geometry := page.Frame().Layout.GeometryID(shadowChildID)
		t.Fatalf("render omitted closed shadow content; text=%q geometry=%v", frameTextCommands(page.Frame()), geometry)
	}
	if frameHasText(page.Frame(), "light fallback") {
		t.Fatal("render retained light children replaced by a shadow tree")
	}
}
