package render_test

import (
	"fmt"
	"math"
	"testing"

	"github.com/JediWattson/gossamer/internal/dom"
	"github.com/JediWattson/gossamer/internal/render"
)

func FuzzInlineBlockLayoutDoesNotPanic(f *testing.F) {
	f.Add(uint16(120), uint8(0), "short words")
	f.Add(uint16(1), uint8(1), "unbreakable")
	f.Add(uint16(320), uint8(2), "line one\nline two")

	f.Fuzz(func(t *testing.T, rawWidth uint16, mode uint8, text string) {
		if len(text) > 256 {
			text = text[:256]
		}
		viewportWidth := int(rawWidth%512) + 1
		document := dom.NewDocument()
		html := dom.NewElement("html")
		body := dom.NewElement("body", dom.Attribute{Name: "style", Value: "margin:0"})
		container := dom.NewElement("div", dom.Attribute{Name: "style", Value: fmt.Sprintf("width:%dpx", viewportWidth)})
		display := "inline-block"
		if mode%3 == 2 {
			display = "inline-flex"
		}
		atomic := dom.NewElement("span", dom.Attribute{
			Name: "style",
			Value: fmt.Sprintf(
				"display:%s;max-width:%dpx;padding:%dpx;border:%dpx solid;white-space:%s",
				display,
				int(mode%64)+1,
				int(mode%5),
				int(mode%3),
				[]string{"normal", "pre-wrap", "nowrap"}[mode%3],
			),
		})
		child := dom.NewElement("span")
		if mode%2 == 1 {
			child.Attributes = append(child.Attributes, dom.Attribute{Name: "style", Value: "display:block"})
		}
		child.AppendChild(dom.NewText(text))
		atomic.AppendChild(child)
		container.AppendChild(atomic)
		container.AppendChild(dom.NewText("tail"))
		body.AppendChild(container)
		html.AppendChild(body)
		document.AppendChild(html)

		frame, err := render.Render(document, render.Viewport{Width: viewportWidth, Height: 256})
		if err != nil {
			return
		}
		geometry, ok := frame.Layout.Geometry(atomic)
		if !ok {
			t.Fatal("inline-block layout omitted principal geometry")
		}
		for name, value := range map[string]float64{
			"x": geometry.Bounds.X, "y": geometry.Bounds.Y,
			"width": geometry.Bounds.Width, "height": geometry.Bounds.Height,
		} {
			if math.IsNaN(value) || math.IsInf(value, 0) || value < 0 {
				t.Fatalf("%s = %v, want finite non-negative geometry", name, value)
			}
		}
	})
}
