package render_test

import (
	"fmt"
	"math"
	"testing"

	"github.com/JediWattson/gossamer/internal/dom"
	"github.com/JediWattson/gossamer/internal/render"
)

func FuzzVerticalAlignLineLayoutDoesNotPanic(f *testing.F) {
	f.Add(uint8(0), int16(0), uint8(20), uint8(40), uint8(12))
	f.Add(uint8(2), int16(10), uint8(16), uint8(20), uint8(40))
	f.Add(uint8(9), int16(-200), uint8(1), uint8(1), uint8(1))
	f.Add(uint8(10), int16(500), uint8(64), uint8(128), uint8(128))

	f.Fuzz(func(t *testing.T, rawMode uint8, rawOffset int16, rawFontSize, rawLineHeight, rawAtomicHeight uint8) {
		fontSize := int(rawFontSize%64) + 1
		lineHeight := int(rawLineHeight%128) + 1
		atomicHeight := int(rawAtomicHeight%128) + 1
		offset := int(rawOffset % 513)
		alignments := []string{
			"baseline", "sub", "super", "text-top", "text-bottom", "middle", "top", "bottom",
			fmt.Sprintf("%dpx", offset),
			fmt.Sprintf("%d%%", offset),
			fmt.Sprintf("calc(%d%% + %dpx)", offset, int(rawMode%31)-15),
		}
		alignment := alignments[int(rawMode)%len(alignments)]

		document := dom.NewDocument()
		html := dom.NewElement("html")
		body := dom.NewElement("body", dom.Attribute{Name: "style", Value: "margin:0"})
		container := dom.NewElement("div", dom.Attribute{Name: "style", Value: fmt.Sprintf("font:%dpx/%dpx monospace", fontSize, lineHeight)})
		container.AppendChild(dom.NewText("base "))
		span := dom.NewElement("span", dom.Attribute{Name: "style", Value: "vertical-align:" + alignment})
		text := dom.NewText("shift")
		span.AppendChild(text)
		container.AppendChild(span)
		atomic := dom.NewElement("span", dom.Attribute{Name: "style", Value: fmt.Sprintf("display:inline-block;width:8px;height:%dpx;vertical-align:%s", atomicHeight, alignment)})
		container.AppendChild(atomic)
		body.AppendChild(container)
		html.AppendChild(body)
		document.AppendChild(html)

		frame, err := render.Render(document, render.Viewport{Width: 800, Height: 512})
		if err != nil {
			return
		}
		for _, fragment := range collectTextFragments(frame.Root) {
			for name, value := range map[string]float64{
				"x": fragment.X, "baseline": fragment.BaselineY, "width": fragment.Width,
				"height": fragment.Height, "baseline offset": fragment.BaselineOffset,
			} {
				if math.IsNaN(value) || math.IsInf(value, 0) {
					t.Fatalf("%s = %v for vertical-align %q", name, value, alignment)
				}
			}
			if fragment.Height < 0 || fragment.BaselineOffset < 0 || fragment.BaselineOffset > fragment.Height {
				t.Fatalf("invalid fragment metrics %#v for vertical-align %q", fragment, alignment)
			}
		}
		for _, node := range []*dom.Node{container, atomic} {
			geometry, ok := frame.Layout.Geometry(node)
			if !ok {
				t.Fatalf("vertical-align layout omitted %s geometry", node.Data)
			}
			for name, value := range map[string]float64{
				"x": geometry.Bounds.X, "y": geometry.Bounds.Y,
				"width": geometry.Bounds.Width, "height": geometry.Bounds.Height,
			} {
				if math.IsNaN(value) || math.IsInf(value, 0) || (name == "width" || name == "height") && value < 0 {
					t.Fatalf("%s = %v for vertical-align %q", name, value, alignment)
				}
			}
		}
	})
}
