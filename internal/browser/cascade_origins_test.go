package browser

import (
	"image/color"
	"testing"

	"github.com/JediWattson/gossamer/internal/css"
	"github.com/JediWattson/gossamer/internal/render"
	"github.com/JediWattson/gossamer/internal/style"
)

func TestPageResourcesCarryUserAndUserAgentCascadeOrigins(t *testing.T) {
	t.Parallel()

	engine, page, target := computedStyleTestPage(t, `
		<html><body><div id="target" style="color:#aa0000!important"></div></body></html>
	`)
	defer engine.Close()

	user, err := css.Parse(`#target { color:#00aa00!important }`)
	if err != nil {
		t.Fatal(err)
	}
	userAgent, err := css.Parse(`#target { color:#0000aa!important }`)
	if err != nil {
		t.Fatal(err)
	}
	if err := page.SetResources(render.Resources{
		UserStylesheets:      []css.Stylesheet{user},
		UserAgentStylesheets: []css.Stylesheet{userAgent},
	}); err != nil {
		t.Fatal(err)
	}

	handle := NodeHandle{Document: page.DocumentGeneration(), Node: target}
	computed, err := page.ComputedStyle(handle)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := computed.Color(), (color.NRGBA{B: 0xaa, A: 0xff}); got != want {
		t.Fatalf("color = %#v, want UA-important %#v", got, want)
	}
	explanation, ok := page.computedStyle.snapshot.ExplainID(target, "color")
	if !ok {
		t.Fatal("computed snapshot has no color explanation")
	}
	if explanation.Controller.Origin != style.CascadeOriginUserAgent {
		t.Fatalf("controller origin = %v, want user-agent", explanation.Controller.Origin)
	}
	if explanation.Controller.Kind != style.SourceUserAgentRule {
		t.Fatalf("controller kind = %v, want user-agent-rule", explanation.Controller.Kind)
	}
	if page.Frame() != nil || !page.Dirty() {
		t.Fatal("origin-aware style-only read changed frame publication state")
	}
}
