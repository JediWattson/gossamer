package browser

import (
	"math"
	"slices"
	"testing"
)

func TestComputedTextOrientationStaysLiveWithoutPublishingFrame(t *testing.T) {
	t.Parallel()

	engine, page, targetID := computedStyleTestPage(t, `<!doctype html><html><body><span id="target" style="display:inline-block;writing-mode:vertical-rl;text-orientation:mixed;font-size:20px;line-height:24px">A漢</span></body></html>`)
	defer engine.Close()
	handle := NodeHandle{Document: page.DocumentGeneration(), Node: targetID}
	host := &taskHost{page: page, generation: page.DocumentGeneration()}

	assertResolvedProperty(t, page, handle, "text-orientation", "mixed")
	initialGeometry, err := page.ElementGeometry(handle)
	if err != nil {
		t.Fatal(err)
	}
	names, err := host.ComputedStylePropertyNames(handle, "")
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Contains(names, "text-orientation") {
		t.Fatalf("computed properties omit text-orientation: %q", names)
	}
	if err := host.SetStyleProperty(handle, "text-orientation", "upright", ""); err != nil {
		t.Fatal(err)
	}
	assertResolvedProperty(t, page, handle, "text-orientation", "upright")
	uprightGeometry, err := page.ElementGeometry(handle)
	if err != nil {
		t.Fatal(err)
	}
	if math.Abs(uprightGeometry.Rect.Height-40) > 0.01 {
		t.Fatalf("live upright geometry height = %g, want 40", uprightGeometry.Rect.Height)
	}
	if err := host.SetStyleProperty(handle, "text-orientation", "sideways", ""); err != nil {
		t.Fatal(err)
	}
	assertResolvedProperty(t, page, handle, "text-orientation", "sideways")
	sidewaysGeometry, err := page.ElementGeometry(handle)
	if err != nil {
		t.Fatal(err)
	}
	if sidewaysGeometry.Rect.Height >= uprightGeometry.Rect.Height {
		t.Fatalf("live sideways/upright heights = %g/%g", sidewaysGeometry.Rect.Height, uprightGeometry.Rect.Height)
	}
	if err := host.SetStyleProperty(handle, "text-orientation", "rotate-left", ""); err != nil {
		t.Fatal(err)
	}
	assertResolvedProperty(t, page, handle, "text-orientation", "mixed")
	resetGeometry, err := page.ElementGeometry(handle)
	if err != nil {
		t.Fatal(err)
	}
	if math.Abs(resetGeometry.Rect.Height-initialGeometry.Rect.Height) > 0.01 {
		t.Fatalf("invalid value reset geometry = %g, want initial mixed %g", resetGeometry.Rect.Height, initialGeometry.Rect.Height)
	}
	if page.Frame() != nil || !page.Dirty() {
		t.Fatal("computed text-orientation read published a frame or cleared dirtiness")
	}
}
