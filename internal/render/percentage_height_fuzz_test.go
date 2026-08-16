package render_test

import (
	"fmt"
	"math"
	"testing"

	"github.com/JediWattson/gossamer/internal/dom"
	"github.com/JediWattson/gossamer/internal/render"
	computed "github.com/JediWattson/gossamer/internal/style"
)

func FuzzPercentageHeightLayoutDoesNotPanic(f *testing.F) {
	f.Add(uint16(200), uint16(50), uint8(0))
	f.Add(uint16(1), uint16(500), uint8(1))
	f.Add(uint16(65535), uint16(100), uint8(4))
	f.Add(uint16(65518), uint16(0), uint8(12))

	f.Fuzz(func(t *testing.T, rawParentHeight, rawPercentage uint16, mode uint8) {
		parentHeight := int(rawParentHeight%1024) + 1
		percentage := int(rawPercentage % 1001)
		parentHeightDeclaration := "height:auto"
		definite := mode%2 == 0
		if definite {
			parentHeightDeclaration = fmt.Sprintf("height:%dpx", parentHeight)
		}
		display := []string{"block", "inline-block", "flex"}[(mode/2)%3]
		height := fmt.Sprintf("%d%%", percentage)
		if mode&8 != 0 {
			height = fmt.Sprintf("calc(%d%% - %dpx)", percentage, int(mode%17))
		}

		document := dom.NewDocument()
		html := dom.NewElement("html")
		body := dom.NewElement("body", dom.Attribute{Name: "style", Value: "margin:0"})
		parent := dom.NewElement("div", dom.Attribute{Name: "style", Value: parentHeightDeclaration})
		child := dom.NewElement("div", dom.Attribute{
			Name: "style",
			Value: fmt.Sprintf(
				"display:%s;height:%s;min-height:%d%%;max-height:%d%%;padding:%dpx;border:%dpx solid;box-sizing:%s",
				display,
				height,
				int(mode%80),
				int(mode%80)+20,
				int(mode%7),
				int(mode%5),
				[]string{"content-box", "border-box"}[mode%2],
			),
		})
		grandchild := dom.NewElement("div", dom.Attribute{Name: "style", Value: "height:50%"})
		child.AppendChild(grandchild)
		parent.AppendChild(child)
		body.AppendChild(parent)
		html.AppendChild(body)
		document.AppendChild(html)

		frame, err := render.Render(document, render.Viewport{Width: 640, Height: 480})
		if err != nil {
			return
		}
		for _, node := range []*dom.Node{parent, child, grandchild} {
			geometry, ok := frame.Layout.Geometry(node)
			if !ok {
				t.Fatalf("layout omitted geometry for %s", node.Data)
			}
			for name, value := range map[string]float64{
				"x": geometry.Bounds.X, "y": geometry.Bounds.Y,
				"width": geometry.Bounds.Width, "height": geometry.Bounds.Height,
			} {
				if math.IsNaN(value) || math.IsInf(value, 0) || value < 0 {
					t.Fatalf("%s = %v, want finite non-negative geometry", name, value)
				}
			}
		}
		childStyle, ok := frame.ComputedStyles.Lookup(child)
		if !ok {
			t.Fatal("computed style omitted child")
		}
		childGeometry, _ := frame.Layout.Geometry(child)
		wantChildResolved := definite && childStyle.Height().DependsOnPercent()
		if childGeometry.PercentHeightResolved != wantChildResolved {
			t.Fatalf("child resolved = %t, want %t", childGeometry.PercentHeightResolved, wantChildResolved)
		}
		grandchildGeometry, _ := frame.Layout.Geometry(grandchild)
		childHeightDefinite := childStyle.Height().Unit() != computed.LengthAuto &&
			(!childStyle.Height().DependsOnPercent() || definite)
		if grandchildGeometry.PercentHeightResolved != childHeightDefinite {
			t.Fatalf("grandchild resolved = %t, want %t", grandchildGeometry.PercentHeightResolved, childHeightDefinite)
		}
	})
}
