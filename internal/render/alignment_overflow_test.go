package render_test

import (
	"strings"
	"testing"

	htmlparser "github.com/JediWattson/gossamer/internal/html"
	"github.com/JediWattson/gossamer/internal/render"
)

func TestGridContentOverflowAlignmentDistinguishesSafeUnsafeAndDefault(t *testing.T) {
	t.Parallel()

	document, err := htmlparser.Parse(strings.NewReader(`<!doctype html><html><body style="margin:0">
		<section id=safe style="display:grid;width:100px;height:100px;grid-template-columns:150px;grid-template-rows:150px;justify-content:safe center;align-content:safe center"><div id=safe-child></div></section>
		<section id=unsafe style="display:grid;width:100px;height:100px;grid-template-columns:150px;grid-template-rows:150px;justify-content:unsafe center;align-content:unsafe center"><div id=unsafe-child></div></section>
		<section id=default style="display:grid;width:100px;height:100px;grid-template-columns:150px;grid-template-rows:150px;justify-content:center;align-content:center"><div id=default-child></div></section>
		<section id=end style="display:grid;width:100px;height:100px;grid-template-columns:150px;grid-template-rows:150px;justify-content:unsafe end;align-content:unsafe end"><div id=end-child></div></section>
	</body></html>`))
	if err != nil {
		t.Fatal(err)
	}
	frame, err := render.Render(document, render.Viewport{Width: 240, Height: 500})
	if err != nil {
		t.Fatal(err)
	}

	for _, test := range []struct {
		id, child string
		want      float64
	}{
		{id: "safe", child: "safe-child", want: 0},
		{id: "unsafe", child: "unsafe-child", want: -25},
		{id: "default", child: "default-child", want: -25},
		{id: "end", child: "end-child", want: -50},
	} {
		container, ok := frame.Layout.Geometry(findStaticPageElementByID(document, test.id))
		if !ok {
			t.Fatalf("%s has no geometry", test.id)
		}
		child, ok := frame.Layout.Geometry(findStaticPageElementByID(document, test.child))
		if !ok {
			t.Fatalf("%s has no geometry", test.child)
		}
		assertNear(t, test.id+" content x", child.Bounds.X-container.ContentBounds.X, test.want)
		assertNear(t, test.id+" content y", child.Bounds.Y-container.ContentBounds.Y, test.want)
	}
}

func TestGridSelfOverflowAlignmentUsesItemAreaOverflow(t *testing.T) {
	t.Parallel()

	document, err := htmlparser.Parse(strings.NewReader(`<!doctype html><html><body style="margin:0">
		<section id=safe style="display:grid;width:100px;height:100px;grid-template-columns:100px;grid-template-rows:100px"><div id=safe-child style="width:150px;height:150px;justify-self:safe center;align-self:safe center"></div></section>
		<section id=unsafe style="display:grid;width:100px;height:100px;grid-template-columns:100px;grid-template-rows:100px"><div id=unsafe-child style="width:150px;height:150px;justify-self:unsafe center;align-self:unsafe center"></div></section>
		<section id=default style="display:grid;width:100px;height:100px;grid-template-columns:100px;grid-template-rows:100px"><div id=default-child style="width:150px;height:150px;justify-self:center;align-self:center"></div></section>
	</body></html>`))
	if err != nil {
		t.Fatal(err)
	}
	frame, err := render.Render(document, render.Viewport{Width: 240, Height: 400})
	if err != nil {
		t.Fatal(err)
	}

	for _, test := range []struct {
		id, child string
		want      float64
	}{
		{id: "safe", child: "safe-child", want: 0},
		{id: "unsafe", child: "unsafe-child", want: -25},
		{id: "default", child: "default-child", want: -25},
	} {
		container, _ := frame.Layout.Geometry(findStaticPageElementByID(document, test.id))
		child, _ := frame.Layout.Geometry(findStaticPageElementByID(document, test.child))
		assertNear(t, test.id+" self x", child.Bounds.X-container.ContentBounds.X, test.want)
		assertNear(t, test.id+" self y", child.Bounds.Y-container.ContentBounds.Y, test.want)
	}
}

func TestSafeAndUnsafeSelfAlignmentUseTheMarginBox(t *testing.T) {
	t.Parallel()

	document, err := htmlparser.Parse(strings.NewReader(`<!doctype html><html><body style="margin:0">
		<section id=grid-safe style="display:grid;width:100px;height:100px;grid-template-columns:100px;grid-template-rows:100px"><div id=grid-safe-child style="width:100px;height:100px;margin:10px;justify-self:safe center;align-self:safe center"></div></section>
		<section id=grid-unsafe style="display:grid;width:100px;height:100px;grid-template-columns:100px;grid-template-rows:100px"><div id=grid-unsafe-child style="width:100px;height:100px;margin:10px;justify-self:unsafe center;align-self:unsafe center"></div></section>
		<section id=flex-safe style="display:flex;width:100px;height:30px;justify-content:safe center"><div id=flex-safe-child style="width:100px;height:20px;margin-left:10px;margin-right:10px;flex-shrink:0"></div></section>
		<section id=flex-unsafe style="display:flex;width:100px;height:30px;justify-content:unsafe center"><div id=flex-unsafe-child style="width:100px;height:20px;margin-left:10px;margin-right:10px;flex-shrink:0"></div></section>
	</body></html>`))
	if err != nil {
		t.Fatal(err)
	}
	frame, err := render.Render(document, render.Viewport{Width: 240, Height: 300})
	if err != nil {
		t.Fatal(err)
	}

	for _, test := range []struct {
		id, child string
		wantX     float64
		wantY     float64
	}{
		{id: "grid-safe", child: "grid-safe-child", wantX: 10, wantY: 10},
		{id: "grid-unsafe", child: "grid-unsafe-child", wantX: 0, wantY: 0},
		{id: "flex-safe", child: "flex-safe-child", wantX: 10},
		{id: "flex-unsafe", child: "flex-unsafe-child", wantX: 0},
	} {
		container, _ := frame.Layout.Geometry(findStaticPageElementByID(document, test.id))
		child, _ := frame.Layout.Geometry(findStaticPageElementByID(document, test.child))
		assertNear(t, test.id+" margin-box x", child.Bounds.X-container.ContentBounds.X, test.wantX)
		if strings.HasPrefix(test.id, "grid-") {
			assertNear(t, test.id+" margin-box y", child.Bounds.Y-container.ContentBounds.Y, test.wantY)
		}
	}
}

func TestFlexOverflowAlignmentUsesSignedMainAndCrossFreeSpace(t *testing.T) {
	t.Parallel()

	document, err := htmlparser.Parse(strings.NewReader(`<!doctype html><html><body style="margin:0">
		<section id=main-safe style="display:flex;width:100px;height:30px;justify-content:safe center"><div id=main-safe-child style="width:150px;height:20px;flex-shrink:0"></div></section>
		<section id=main-unsafe style="display:flex;width:100px;height:30px;justify-content:unsafe center"><div id=main-unsafe-child style="width:150px;height:20px;flex-shrink:0"></div></section>
		<section id=main-default style="display:flex;width:100px;height:30px;justify-content:center"><div id=main-default-child style="width:150px;height:20px;flex-shrink:0"></div></section>
		<section id=reverse-safe style="display:flex;flex-direction:row-reverse;width:100px;height:30px;justify-content:safe center"><div id=reverse-safe-child style="width:150px;height:20px;flex-shrink:0"></div></section>
		<section id=reverse-unsafe style="display:flex;flex-direction:row-reverse;width:100px;height:30px;justify-content:unsafe center"><div id=reverse-unsafe-child style="width:150px;height:20px;flex-shrink:0"></div></section>
		<section id=reverse-flex-start style="display:flex;flex-direction:row-reverse;width:100px;height:30px;justify-content:flex-start"><div id=reverse-flex-start-child style="width:20px;height:20px;flex-shrink:0"></div></section>
		<section id=reverse-start style="display:flex;flex-direction:row-reverse;width:100px;height:30px;justify-content:start"><div id=reverse-start-child style="width:20px;height:20px;flex-shrink:0"></div></section>
		<section id=reverse-space-between style="display:flex;flex-direction:row-reverse;width:100px;height:30px;justify-content:space-between"><div id=reverse-space-between-child style="width:20px;height:20px;flex-shrink:0"></div></section>
		<section id=overflow-space-around style="display:flex;width:100px;height:30px;justify-content:space-around"><div id=overflow-space-around-child style="width:150px;height:20px;flex-shrink:0"></div></section>
		<section id=column-reverse-safe style="display:flex;flex-direction:column-reverse;width:30px;height:100px;justify-content:safe center"><div id=column-reverse-safe-child style="width:20px;height:150px;flex-shrink:0"></div></section>
		<section id=cross-safe style="display:flex;width:100px;height:100px;align-items:safe center"><div id=cross-safe-child style="width:20px;height:150px"></div></section>
		<section id=cross-unsafe style="display:flex;width:100px;height:100px;align-items:unsafe center"><div id=cross-unsafe-child style="width:20px;height:150px"></div></section>
		<section id=cross-default style="display:flex;width:100px;height:100px;align-items:center"><div id=cross-default-child style="width:20px;height:150px"></div></section>
	</body></html>`))
	if err != nil {
		t.Fatal(err)
	}
	frame, err := render.Render(document, render.Viewport{Width: 240, Height: 500})
	if err != nil {
		t.Fatal(err)
	}

	for _, test := range []struct {
		id, child string
		axis      string
		want      float64
	}{
		{id: "main-safe", child: "main-safe-child", axis: "x", want: 0},
		{id: "main-unsafe", child: "main-unsafe-child", axis: "x", want: -25},
		{id: "main-default", child: "main-default-child", axis: "x", want: -25},
		{id: "reverse-safe", child: "reverse-safe-child", axis: "x", want: -50},
		{id: "reverse-unsafe", child: "reverse-unsafe-child", axis: "x", want: -25},
		{id: "reverse-flex-start", child: "reverse-flex-start-child", axis: "x", want: 80},
		{id: "reverse-start", child: "reverse-start-child", axis: "x", want: 0},
		{id: "reverse-space-between", child: "reverse-space-between-child", axis: "x", want: 80},
		{id: "overflow-space-around", child: "overflow-space-around-child", axis: "x", want: 0},
		{id: "column-reverse-safe", child: "column-reverse-safe-child", axis: "y", want: -50},
		{id: "cross-safe", child: "cross-safe-child", axis: "y", want: 0},
		{id: "cross-unsafe", child: "cross-unsafe-child", axis: "y", want: -25},
		{id: "cross-default", child: "cross-default-child", axis: "y", want: -25},
	} {
		container, _ := frame.Layout.Geometry(findStaticPageElementByID(document, test.id))
		child, _ := frame.Layout.Geometry(findStaticPageElementByID(document, test.child))
		got := child.Bounds.X - container.ContentBounds.X
		if test.axis == "y" {
			got = child.Bounds.Y - container.ContentBounds.Y
		}
		assertNear(t, test.id+" overflow offset", got, test.want)
	}
}
