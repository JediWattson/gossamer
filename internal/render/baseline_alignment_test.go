package render_test

import (
	"math"
	"strings"
	"testing"

	htmlparser "github.com/JediWattson/gossamer/internal/html"
	"github.com/JediWattson/gossamer/internal/render"
)

func TestFlexFirstAndLastBaselineAlignmentShareLineBaselines(t *testing.T) {
	t.Parallel()

	document, err := htmlparser.Parse(strings.NewReader(`<!doctype html><html><body style="margin:0">
		<section id=first style="display:flex;width:200px;align-items:baseline">
			<div id=first-a style="font-size:32px;line-height:32px;padding-bottom:20px">flex-first-a</div>
			<div id=first-b style="font-size:16px;line-height:16px;padding-top:30px">flex-first-b</div>
		</section>
		<section id=last style="display:flex;width:200px;height:100px;align-items:last baseline">
			<div style="font-size:32px;line-height:32px">flex-last-a</div>
			<div style="font-size:16px;line-height:16px">flex-last-b</div>
		</section>
	</body></html>`))
	if err != nil {
		t.Fatal(err)
	}
	frame, err := render.Render(document, render.Viewport{Width: 260, Height: 240})
	if err != nil {
		t.Fatal(err)
	}
	fragments := collectTextFragments(frame.Root)
	firstA := findTextFragment(fragments, "flex-first-a")
	firstB := findTextFragment(fragments, "flex-first-b")
	lastA := findTextFragment(fragments, "flex-last-a")
	lastB := findTextFragment(fragments, "flex-last-b")
	if firstA == nil || firstB == nil || lastA == nil || lastB == nil {
		t.Fatalf("missing baseline fragments: first=%#v/%#v last=%#v/%#v", firstA, firstB, lastA, lastB)
	}
	assertNear(t, "flex first shared baseline", firstA.BaselineY, firstB.BaselineY)
	assertNear(t, "flex last shared baseline", lastA.BaselineY, lastB.BaselineY)

	firstContainer, _ := frame.Layout.Geometry(findStaticPageElementByID(document, "first"))
	lastContainer, _ := frame.Layout.Geometry(findStaticPageElementByID(document, "last"))
	firstAGeometry, _ := frame.Layout.Geometry(findStaticPageElementByID(document, "first-a"))
	firstBGeometry, _ := frame.Layout.Geometry(findStaticPageElementByID(document, "first-b"))
	if firstContainer.ContentBounds.Height <= math.Max(firstAGeometry.Bounds.Height, firstBGeometry.Bounds.Height) {
		t.Fatalf("baseline shims did not expand flex line: container=%#v first=%#v second=%#v", firstContainer.ContentBounds, firstAGeometry.Bounds, firstBGeometry.Bounds)
	}
	if lastA.BaselineY-lastContainer.ContentBounds.Y <= firstA.BaselineY-firstContainer.ContentBounds.Y {
		t.Fatalf("last baseline did not align at cross-end: first=%v last=%v", firstA.BaselineY-firstContainer.ContentBounds.Y, lastA.BaselineY-lastContainer.ContentBounds.Y)
	}
}

func TestGridBaselineAlignmentAddsIntrinsicRowShimsAndAlignsItems(t *testing.T) {
	t.Parallel()

	document, err := htmlparser.Parse(strings.NewReader(`<!doctype html><html><body style="margin:0">
		<section id=grid style="display:grid;width:200px;grid-template-columns:100px 100px;grid-template-rows:auto;align-items:baseline">
			<div id=grid-a style="font-size:32px;line-height:32px;padding-bottom:20px">grid-first-a</div>
			<div id=grid-b style="font-size:16px;line-height:16px;padding-top:30px">grid-first-b</div>
		</section>
		<section id=last style="display:grid;width:200px;height:100px;grid-template-columns:100px 100px;grid-template-rows:100px;align-items:last baseline">
			<div style="font-size:32px;line-height:32px">grid-last-a</div>
			<div style="font-size:16px;line-height:16px">grid-last-b</div>
		</section>
	</body></html>`))
	if err != nil {
		t.Fatal(err)
	}
	frame, err := render.Render(document, render.Viewport{Width: 260, Height: 240})
	if err != nil {
		t.Fatal(err)
	}
	fragments := collectTextFragments(frame.Root)
	firstA := findTextFragment(fragments, "grid-first-a")
	firstB := findTextFragment(fragments, "grid-first-b")
	lastA := findTextFragment(fragments, "grid-last-a")
	lastB := findTextFragment(fragments, "grid-last-b")
	if firstA == nil || firstB == nil || lastA == nil || lastB == nil {
		t.Fatalf("missing grid baseline fragments: first=%#v/%#v last=%#v/%#v", firstA, firstB, lastA, lastB)
	}
	assertNear(t, "grid first shared baseline", firstA.BaselineY, firstB.BaselineY)
	assertNear(t, "grid last shared baseline", lastA.BaselineY, lastB.BaselineY)

	grid, _ := frame.Layout.Geometry(findStaticPageElementByID(document, "grid"))
	gridA, _ := frame.Layout.Geometry(findStaticPageElementByID(document, "grid-a"))
	gridB, _ := frame.Layout.Geometry(findStaticPageElementByID(document, "grid-b"))
	rows := grid.GridRowSizes()
	if len(rows) != 1 || rows[0] <= math.Max(gridA.Bounds.Height, gridB.Bounds.Height) {
		t.Fatalf("grid baseline row did not include shims: rows=%v first=%#v second=%#v", rows, gridA.Bounds, gridB.Bounds)
	}
}

func TestGridBaselineFallbacksUseStartAndEndAlignment(t *testing.T) {
	t.Parallel()

	document, err := htmlparser.Parse(strings.NewReader(`<!doctype html><html><body style="margin:0">
		<section id=content-first style="display:grid;width:100px;height:100px;grid-template-columns:20px;grid-template-rows:20px;align-content:baseline"><div id=content-first-child></div></section>
		<section id=content-last style="display:grid;width:100px;height:100px;grid-template-columns:20px;grid-template-rows:20px;align-content:last baseline"><div id=content-last-child></div></section>
		<section id=self style="display:grid;width:100px;height:40px;grid-template-columns:100px;grid-template-rows:20px"><div id=self-first style="grid-column:1;grid-row:1;width:20px;justify-self:baseline"></div><div id=self-last style="grid-column:1;grid-row:1;width:20px;justify-self:last baseline"></div></section>
	</body></html>`))
	if err != nil {
		t.Fatal(err)
	}
	frame, err := render.Render(document, render.Viewport{Width: 220, Height: 280})
	if err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		container, child string
		axis             string
		want             float64
	}{
		{container: "content-first", child: "content-first-child", axis: "y", want: 0},
		{container: "content-last", child: "content-last-child", axis: "y", want: 80},
		{container: "self", child: "self-first", axis: "x", want: 0},
		{container: "self", child: "self-last", axis: "x", want: 80},
	} {
		container, _ := frame.Layout.Geometry(findStaticPageElementByID(document, test.container))
		child, _ := frame.Layout.Geometry(findStaticPageElementByID(document, test.child))
		got := child.Bounds.X - container.ContentBounds.X
		if test.axis == "y" {
			got = child.Bounds.Y - container.ContentBounds.Y
		}
		assertNear(t, test.child+" baseline fallback", got, test.want)
	}
}

func TestGridAlignSelfBaselineOverridesContainerAlignment(t *testing.T) {
	t.Parallel()

	document, err := htmlparser.Parse(strings.NewReader(`<!doctype html><html><body style="margin:0">
		<section style="display:grid;width:200px;grid-template-columns:100px 100px;grid-template-rows:auto;align-items:start">
			<div style="align-self:baseline;font-size:32px;line-height:32px;padding-bottom:20px">override-a</div>
			<div style="align-self:baseline;font-size:16px;line-height:16px;padding-top:30px">override-b</div>
		</section>
	</body></html>`))
	if err != nil {
		t.Fatal(err)
	}
	frame, err := render.Render(document, render.Viewport{Width: 260, Height: 160})
	if err != nil {
		t.Fatal(err)
	}
	fragments := collectTextFragments(frame.Root)
	first := findTextFragment(fragments, "override-a")
	second := findTextFragment(fragments, "override-b")
	if first == nil || second == nil {
		t.Fatalf("missing align-self baseline fragments: %#v/%#v", first, second)
	}
	assertNear(t, "grid align-self shared baseline", first.BaselineY, second.BaselineY)
}
