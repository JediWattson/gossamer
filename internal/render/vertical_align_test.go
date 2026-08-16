package render_test

import (
	"testing"

	"github.com/JediWattson/gossamer/internal/dom"
	"github.com/JediWattson/gossamer/internal/render"
)

func TestVerticalAlignShiftsInlineTextFromTheParentBaseline(t *testing.T) {
	t.Parallel()

	document, body := boxModelDocument()
	container := dom.NewElement("div", dom.Attribute{Name: "style", Value: "margin:0;font:20px/40px monospace"})
	fragments := make(map[string]*dom.Node)
	for _, item := range []struct {
		text  string
		align string
	}{
		{text: "base", align: "baseline"},
		{text: "super", align: "super"},
		{text: "sub", align: "sub"},
		{text: "raise", align: "10px"},
		{text: "lower", align: "-10px"},
		{text: "percent", align: "50%"},
		{text: "texttop", align: "text-top"},
		{text: "textbottom", align: "text-bottom"},
	} {
		span := dom.NewElement("span", dom.Attribute{Name: "style", Value: "vertical-align:" + item.align})
		text := dom.NewText(item.text)
		span.AppendChild(text)
		container.AppendChild(span)
		container.AppendChild(dom.NewText(" "))
		fragments[item.text] = text
	}
	body.AppendChild(container)

	frame, err := render.Render(document, render.Viewport{Width: 800, Height: 160})
	if err != nil {
		t.Fatal(err)
	}
	positioned := make(map[string]*render.TextFragment)
	all := collectTextFragments(frame.Root)
	for text := range fragments {
		positioned[text] = findTextFragment(all, text)
		if positioned[text] == nil {
			t.Fatalf("vertical-align fragment %q missing", text)
		}
	}
	base := positioned["base"].BaselineY
	assertNear(t, "10px raise", base-positioned["raise"].BaselineY, 10)
	assertNear(t, "10px lower", positioned["lower"].BaselineY-base, 10)
	assertNear(t, "50 percent line-height raise", base-positioned["percent"].BaselineY, 20)
	assertNear(t, "super fallback raise", base-positioned["super"].BaselineY, 20.0/3)
	assertNear(t, "sub fallback lower", positioned["sub"].BaselineY-base, 4)
	if positioned["texttop"].BaselineY <= base {
		t.Errorf("text-top baseline = %.2f, want below baseline-aligned %.2f after matching parent content top", positioned["texttop"].BaselineY, base)
	}
	if positioned["textbottom"].BaselineY >= base {
		t.Errorf("text-bottom baseline = %.2f, want above baseline-aligned %.2f after matching parent content bottom", positioned["textbottom"].BaselineY, base)
	}
}

func TestVerticalAlignTopAndBottomPositionAtomicInlineBoxesAgainstLine(t *testing.T) {
	t.Parallel()

	document, body := boxModelDocument()
	container := dom.NewElement("div", dom.Attribute{Name: "style", Value: "margin:0;font:20px/40px monospace"})
	container.AppendChild(dom.NewText("strut"))
	top := dom.NewElement("span", dom.Attribute{Name: "style", Value: "display:inline-block;width:12px;height:12px;vertical-align:top;background:#123456"})
	bottom := dom.NewElement("span", dom.Attribute{Name: "style", Value: "display:inline-block;width:12px;height:12px;vertical-align:bottom;background:#654321"})
	container.AppendChild(top)
	container.AppendChild(bottom)
	body.AppendChild(container)

	frame, err := render.Render(document, render.Viewport{Width: 320, Height: 120})
	if err != nil {
		t.Fatal(err)
	}
	containerBox := findBox(frame.Root, container)
	topBox := findBox(frame.Root, top)
	bottomBox := findBox(frame.Root, bottom)
	if containerBox == nil || topBox == nil || bottomBox == nil {
		t.Fatalf("vertical-align boxes = container:%#v top:%#v bottom:%#v", containerBox, topBox, bottomBox)
	}
	assertNear(t, "top-aligned border edge", topBox.Bounds.Y, containerBox.ContentBounds.Y)
	assertNear(t, "bottom-aligned border edge", bottomBox.Bounds.Y+bottomBox.Bounds.Height, containerBox.ContentBounds.Y+containerBox.ContentBounds.Height)
	if hit := render.HitTest(frame, topBox.Bounds.X+1, topBox.Bounds.Y+1); hit != top {
		t.Fatalf("top-aligned hit = %p, want %p", hit, top)
	}
	if hit := render.HitTest(frame, bottomBox.Bounds.X+1, bottomBox.Bounds.Y+1); hit != bottom {
		t.Fatalf("bottom-aligned hit = %p, want %p", hit, bottom)
	}
}

func TestVerticalAlignTextFragmentBoundsFollowTheShiftedLineBox(t *testing.T) {
	t.Parallel()

	document, body := boxModelDocument()
	container := dom.NewElement("div", dom.Attribute{Name: "style", Value: "margin:0;font:20px/40px monospace"})
	span := dom.NewElement("span", dom.Attribute{Name: "style", Value: "vertical-align:10px"})
	text := dom.NewText("shifted")
	span.AppendChild(text)
	container.AppendChild(span)
	body.AppendChild(container)

	frame, err := render.Render(document, render.Viewport{Width: 240, Height: 100})
	if err != nil {
		t.Fatal(err)
	}
	fragment := findTextFragment(collectTextFragments(frame.Root), "shifted")
	if fragment == nil || fragment.BaselineOffset <= 0 || fragment.BaselineOffset > fragment.Height {
		t.Fatalf("shifted fragment metrics = %#v", fragment)
	}
	top := fragment.BaselineY - fragment.BaselineOffset
	if hit := render.HitTest(frame, fragment.X+1, top+1); hit != text {
		t.Fatalf("shifted text hit = %p, want text node %p", hit, text)
	}
	if hit := render.HitTest(frame, fragment.X+1, top+fragment.Height+1); hit == text {
		t.Fatalf("hit below shifted fragment unexpectedly returned text node %p", hit)
	}
}
