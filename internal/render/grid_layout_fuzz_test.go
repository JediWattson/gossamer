package render_test

import (
	"fmt"
	"math"
	"strings"
	"testing"

	"github.com/JediWattson/gossamer/internal/dom"
	"github.com/JediWattson/gossamer/internal/render"
)

func FuzzGridLayoutPlacementStaysFinite(f *testing.F) {
	f.Add(byte(3), byte(2), byte(8), byte(0), byte(1))
	f.Add(byte(4), byte(4), byte(16), byte(0xff), byte(3))
	f.Add(byte(1), byte(8), byte(24), byte(0x55), byte(7))
	f.Fuzz(func(t *testing.T, rawColumns, rawRows, rawItems, rawModes, rawSpan byte) {
		columns := int(rawColumns%8) + 1
		rows := int(rawRows%8) + 1
		itemCount := int(rawItems%32) + 1
		span := int(rawSpan%4) + 1
		track := "1fr"
		switch (rawModes >> 2) % 8 {
		case 1:
			track = fmt.Sprintf("%dpx", 10+rawModes%20)
		case 2:
			track = "auto"
		case 3:
			track = fmt.Sprintf("%d%%", 5+rawModes%20)
		case 4:
			track = "min-content"
		case 5:
			track = "max-content"
		case 6:
			track = fmt.Sprintf("minmax(%dpx, %dfr)", rawModes%11, rawSpan%3)
		case 7:
			track = fmt.Sprintf("fit-content(%d%%)", 5+rawSpan%90)
		}
		flow := "row"
		if rawModes&1 != 0 {
			flow = "column"
		}
		if rawModes&2 != 0 {
			flow += " dense"
		}
		contentAlignment := []string{"normal", "start", "end", "center", "space-between", "space-around", "space-evenly"}[int(rawModes)%7]
		selfAlignment := []string{"normal", "stretch", "start", "end", "center"}[int(rawSpan)%5]
		areaRow := strings.TrimSpace(strings.Repeat("zone ", columns))
		templateColumns := fmt.Sprintf("repeat(%d,[slot] %s [edge])", columns, track)
		if rawModes&0x80 != 0 {
			automatic := "auto-fill"
			if rawModes&0x40 != 0 {
				automatic = "auto-fit"
			}
			templateColumns = fmt.Sprintf("repeat(%s,[slot] minmax(%dpx,1fr) [edge])", automatic, 1+span)
		}

		document := dom.NewDocument()
		html := dom.NewElement("html")
		body := dom.NewElement("body", dom.Attribute{Name: "style", Value: "margin:0"})
		grid := dom.NewElement("section", dom.Attribute{Name: "style", Value: fmt.Sprintf(
			"display:grid;width:%dpx;height:%dpx;grid-template-areas:\"%s\";grid-template-columns:%s;grid-template-rows:repeat(%d,[row] auto [row-end]);grid-auto-columns:%s auto;grid-auto-rows:%dpx auto;grid-auto-flow:%s;gap:%dpx;justify-content:%s;align-content:%s;justify-items:%s;align-items:%s",
			100+int(rawColumns)*2, 80+int(rawRows)*2, areaRow, templateColumns, rows, track, 8+rawModes%12, flow, rawModes%7, contentAlignment, contentAlignment, selfAlignment, selfAlignment,
		)})
		for index := range itemCount {
			placement := ""
			switch (int(rawModes) + index) % 4 {
			case 0:
				placement = fmt.Sprintf("grid-column:%d slot / span %d slot;", index%columns+1, min(span, columns))
			case 1:
				placement = fmt.Sprintf("grid-row:%d row;grid-column-end:span %d edge;", index%rows+1, min(span, columns))
			case 2:
				placement = fmt.Sprintf("grid-row-end:span %d row;", min(span, rows))
			case 3:
				placement = "grid-area:zone;"
			}
			item := dom.NewElement("div", dom.Attribute{Name: "style", Value: placement + fmt.Sprintf("min-width:%dpx;min-height:%dpx;background:#123456;justify-self:%s;align-self:%s", index%13, index%11, selfAlignment, selfAlignment)})
			item.AppendChild(dom.NewText(strings.Repeat("x", index%9)))
			grid.AppendChild(item)
		}
		body.AppendChild(grid)
		html.AppendChild(body)
		document.AppendChild(html)

		frame, err := render.Render(document, render.Viewport{Width: 400, Height: 300})
		if err != nil {
			t.Fatal(err)
		}
		geometry, ok := frame.Layout.Geometry(grid)
		if !ok || geometry.Bounds.Width < 0 || geometry.Bounds.Height < 0 {
			t.Fatalf("grid geometry = %#v, %t", geometry, ok)
		}
		values := []float64{geometry.Bounds.X, geometry.Bounds.Y, geometry.Bounds.Width, geometry.Bounds.Height}
		for _, command := range frame.DisplayList.Commands {
			values = append(values, command.Rect.X, command.Rect.Y, command.Rect.Width, command.Rect.Height, command.X, command.BaselineY)
		}
		for _, value := range values {
			if math.IsNaN(value) || math.IsInf(value, 0) {
				t.Fatalf("non-finite grid result %v", value)
			}
		}
	})
}
