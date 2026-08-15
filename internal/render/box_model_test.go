package render_test

import (
	"image/color"
	"testing"

	"github.com/JediWattson/gossamer/internal/dom"
	"github.com/JediWattson/gossamer/internal/render"
)

func TestRenderMaxWidthAutoMarginsAndPaddingPositionContent(t *testing.T) {
	t.Parallel()

	document, body := boxModelDocument()
	main := dom.NewElement("main", dom.Attribute{
		Name:  "style",
		Value: "max-width: 240px; margin: 0 auto; padding: 10px 20px",
	})
	text := dom.NewText("Padded content")
	main.AppendChild(text)
	body.AppendChild(main)

	frame, err := render.Render(document, render.Viewport{Width: 400, Height: 200})
	if err != nil {
		t.Fatalf("Render() error = %v", err)
	}
	box := findBox(frame.Root, main)
	if box == nil {
		t.Fatal("main layout box not found")
	}
	assertNear(t, "border-box x", box.Bounds.X, 60)
	assertNear(t, "border-box width", box.Bounds.Width, 280)
	assertNear(t, "content x", box.ContentBounds.X, 80)
	assertNear(t, "content y", box.ContentBounds.Y, 10)
	assertNear(t, "content width", box.ContentBounds.Width, 240)

	fragment := findTextFragment(collectTextFragments(frame.Root), "Padded content")
	if fragment == nil {
		t.Fatal("padded text fragment not found")
	}
	assertNear(t, "text x", fragment.X, box.ContentBounds.X)
}

func TestRenderTextAlignPositionsLinesWithinContentBox(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		alignment string
		wantX     func(render.Rect, render.TextFragment) float64
	}{
		{
			name:      "center",
			alignment: "center",
			wantX: func(content render.Rect, fragment render.TextFragment) float64 {
				return content.X + (content.Width-fragment.Width)/2
			},
		},
		{
			name:      "right",
			alignment: "right",
			wantX: func(content render.Rect, fragment render.TextFragment) float64 {
				return content.X + content.Width - fragment.Width
			},
		},
		{
			name:      "end",
			alignment: "end",
			wantX: func(content render.Rect, fragment render.TextFragment) float64 {
				return content.X + content.Width - fragment.Width
			},
		},
		{
			name:      "start",
			alignment: "start",
			wantX: func(content render.Rect, _ render.TextFragment) float64 {
				return content.X
			},
		},
		{
			name:      "justify fallback",
			alignment: "justify",
			wantX: func(content render.Rect, _ render.TextFragment) float64 {
				return content.X
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			document, body := boxModelDocument()
			container := dom.NewElement("div", dom.Attribute{
				Name:  "style",
				Value: "width: 200px; text-align: " + test.alignment,
			})
			container.AppendChild(dom.NewText("Aligned"))
			body.AppendChild(container)

			frame, err := render.Render(document, render.Viewport{Width: 320, Height: 120})
			if err != nil {
				t.Fatalf("Render() error = %v", err)
			}
			box := findBox(frame.Root, container)
			if box == nil {
				t.Fatal("aligned container box not found")
			}
			fragment := findTextFragment(collectTextFragments(frame.Root), "Aligned")
			if fragment == nil {
				t.Fatal("aligned text fragment not found")
			}
			assertNear(t, "aligned text x", fragment.X, test.wantX(box.ContentBounds, *fragment))
		})
	}
}

func TestRenderInlineBlockUsesCurrentInlineFlow(t *testing.T) {
	t.Parallel()

	document, body := boxModelDocument()
	container := dom.NewElement("div", dom.Attribute{Name: "style", Value: "width: 200px"})
	inlineBlock := dom.NewElement("span", dom.Attribute{Name: "style", Value: "display: inline-block"})
	inlineBlock.AppendChild(dom.NewText("First "))
	inline := dom.NewElement("span")
	inline.AppendChild(dom.NewText("Second"))
	container.AppendChild(inlineBlock)
	container.AppendChild(inline)
	body.AppendChild(container)

	frame, err := render.Render(document, render.Viewport{Width: 320, Height: 120})
	if err != nil {
		t.Fatal(err)
	}
	first := findTextFragment(collectTextFragments(frame.Root), "First")
	second := findTextFragment(collectTextFragments(frame.Root), "Second")
	if first == nil || second == nil {
		t.Fatalf("inline fragments = First:%v Second:%v", first, second)
	}
	assertNear(t, "inline-block baseline", first.BaselineY, second.BaselineY)
	if second.X <= first.X {
		t.Errorf("second inline x = %.2f, want after inline-block x %.2f", second.X, first.X)
	}
}

func TestRenderListItemsUseSeparateBlockFlow(t *testing.T) {
	t.Parallel()

	document, body := boxModelDocument()
	list := dom.NewElement("ul", dom.Attribute{Name: "style", Value: "margin: 0; padding: 0"})
	first := dom.NewElement("li")
	first.AppendChild(dom.NewText("First item"))
	second := dom.NewElement("li")
	second.AppendChild(dom.NewText("Second item"))
	list.AppendChild(first)
	list.AppendChild(second)
	body.AppendChild(list)

	frame, err := render.Render(document, render.Viewport{Width: 320, Height: 200})
	if err != nil {
		t.Fatalf("Render() error = %v", err)
	}
	firstBox := findBox(frame.Root, first)
	if firstBox == nil {
		t.Fatal("first list-item box not found")
	}
	secondBox := findBox(frame.Root, second)
	if secondBox == nil {
		t.Fatal("second list-item box not found")
	}
	firstBottom := firstBox.Bounds.Y + firstBox.Bounds.Height
	if secondBox.Bounds.Y < firstBottom {
		t.Errorf("second list-item y = %.2f, want at or below first bottom %.2f", secondBox.Bounds.Y, firstBottom)
	}
	if secondBox.Bounds.Y <= firstBox.Bounds.Y {
		t.Errorf("list-item y positions = %.2f and %.2f, want separate vertical flow", firstBox.Bounds.Y, secondBox.Bounds.Y)
	}

	fragments := collectTextFragments(frame.Root)
	firstText := findTextFragment(fragments, "First item")
	secondText := findTextFragment(fragments, "Second item")
	if firstText == nil || secondText == nil {
		t.Fatalf("list text fragments = first:%#v second:%#v, want both", firstText, secondText)
	}
	if secondText.BaselineY <= firstText.BaselineY {
		t.Errorf("list text baselines = %.2f and %.2f, want increasing vertical order", firstText.BaselineY, secondText.BaselineY)
	}
}

func TestRenderLengthValuedLineHeightPositionsLines(t *testing.T) {
	t.Parallel()

	document, body := boxModelDocument()
	container := dom.NewElement("div", dom.Attribute{
		Name:  "style",
		Value: "font-size: 20px; line-height: 32px",
	})
	container.AppendChild(dom.NewText("First line"))
	container.AppendChild(dom.NewElement("br"))
	container.AppendChild(dom.NewText("Second line"))
	body.AppendChild(container)

	frame, err := render.Render(document, render.Viewport{Width: 320, Height: 160})
	if err != nil {
		t.Fatalf("Render() error = %v", err)
	}
	box := findBox(frame.Root, container)
	if box == nil {
		t.Fatal("line-height container box not found")
	}
	fragments := collectTextFragments(frame.Root)
	first := findTextFragment(fragments, "First line")
	second := findTextFragment(fragments, "Second line")
	if first == nil || second == nil {
		t.Fatalf("line fragments = first:%#v second:%#v, want both", first, second)
	}
	assertNear(t, "first line box height", first.Height, 32)
	assertNear(t, "second line box height", second.Height, 32)
	assertNear(t, "line baseline separation", second.BaselineY-first.BaselineY, 32)
	assertNear(t, "content height", box.ContentBounds.Height, 64)
}

func TestRenderLengthValuedLineHeightInheritsAsAbsoluteLength(t *testing.T) {
	t.Parallel()

	document, body := boxModelDocument()
	container := dom.NewElement("div", dom.Attribute{
		Name:  "style",
		Value: "font-size: 20px; line-height: 32px",
	})
	first := dom.NewElement("span", dom.Attribute{Name: "style", Value: "font-size: 10px"})
	first.AppendChild(dom.NewText("Small first line"))
	second := dom.NewElement("span", dom.Attribute{Name: "style", Value: "font-size: 10px"})
	second.AppendChild(dom.NewText("Small second line"))
	container.AppendChild(first)
	container.AppendChild(dom.NewElement("br"))
	container.AppendChild(second)
	body.AppendChild(container)

	frame, err := render.Render(document, render.Viewport{Width: 320, Height: 160})
	if err != nil {
		t.Fatalf("Render() error = %v", err)
	}
	fragments := collectTextFragments(frame.Root)
	firstLine := findTextFragment(fragments, "Small first line")
	secondLine := findTextFragment(fragments, "Small second line")
	if firstLine == nil || secondLine == nil {
		t.Fatalf("line fragments = first:%#v second:%#v, want both", firstLine, secondLine)
	}
	assertNear(t, "inherited first line box height", firstLine.Height, 32)
	assertNear(t, "inherited second line box height", secondLine.Height, 32)
	assertNear(t, "inherited line baseline separation", secondLine.BaselineY-firstLine.BaselineY, 32)
}

func TestRenderBlockHeightSizesContentBoxAndFollowingFlow(t *testing.T) {
	t.Parallel()

	document, body := boxModelDocument()
	fixed := dom.NewElement("div", dom.Attribute{
		Name:  "style",
		Value: "height: 60px; padding: 10px; background: #123456",
	})
	fixed.AppendChild(dom.NewText("Short content"))
	following := dom.NewElement("div")
	following.AppendChild(dom.NewText("Following block"))
	body.AppendChild(fixed)
	body.AppendChild(following)

	frame, err := render.Render(document, render.Viewport{Width: 320, Height: 180})
	if err != nil {
		t.Fatalf("Render() error = %v", err)
	}
	fixedBox := findBox(frame.Root, fixed)
	followingBox := findBox(frame.Root, following)
	if fixedBox == nil || followingBox == nil {
		t.Fatalf("layout boxes = fixed:%#v following:%#v, want both", fixedBox, followingBox)
	}
	assertNear(t, "fixed content height", fixedBox.ContentBounds.Height, 60)
	assertNear(t, "fixed padding-box height", fixedBox.Bounds.Height, 80)
	assertNear(t, "following block y", followingBox.Bounds.Y, fixedBox.Bounds.Y+fixedBox.Bounds.Height)

	wantBackground := color.NRGBA{R: 0x12, G: 0x34, B: 0x56, A: 0xff}
	backgroundIndex := commandIndex(frame.DisplayList.Commands, func(command render.Command) bool {
		return command.Kind == render.FillRectCommand &&
			command.Color == wantBackground &&
			near(command.Rect.X, fixedBox.Bounds.X) &&
			near(command.Rect.Y, fixedBox.Bounds.Y) &&
			near(command.Rect.Width, fixedBox.Bounds.Width) &&
			near(command.Rect.Height, fixedBox.Bounds.Height)
	})
	if backgroundIndex < 0 {
		t.Error("fixed-height background does not cover the padded box")
	}
}

func boxModelDocument() (*dom.Node, *dom.Node) {
	document := dom.NewDocument()
	html := dom.NewElement("html")
	document.AppendChild(html)
	head := dom.NewElement("head")
	html.AppendChild(head)
	body := dom.NewElement("body", dom.Attribute{Name: "style", Value: "margin: 0"})
	html.AppendChild(body)
	return document, body
}
