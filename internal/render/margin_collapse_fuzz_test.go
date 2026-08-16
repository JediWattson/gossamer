package render_test

import (
	"fmt"
	"math"
	"testing"

	"github.com/JediWattson/gossamer/internal/dom"
	"github.com/JediWattson/gossamer/internal/render"
)

func FuzzCollapsedMarginStrutMatchesSignedExtremes(f *testing.F) {
	f.Add(int16(5), int16(20), int16(30), int16(15))
	f.Add(int16(20), int16(-30), int16(-10), int16(5))
	f.Add(int16(-5), int16(-20), int16(-30), int16(-15))

	f.Fuzz(func(t *testing.T, firstBottom, emptyTop, emptyBottom, secondTop int16) {
		clampMargin := func(value int16) int {
			return int(value % 257)
		}
		margins := []int{
			clampMargin(firstBottom),
			clampMargin(emptyTop),
			clampMargin(emptyBottom),
			clampMargin(secondTop),
		}

		document := dom.NewDocument()
		html := dom.NewElement("html")
		body := dom.NewElement("body", dom.Attribute{Name: "style", Value: "margin:0;padding:1px 0"})
		first := dom.NewElement("div", dom.Attribute{Name: "style", Value: fmt.Sprintf("height:10px;margin-bottom:%dpx", margins[0])})
		empty := dom.NewElement("div", dom.Attribute{Name: "style", Value: fmt.Sprintf("margin-top:%dpx;margin-bottom:%dpx", margins[1], margins[2])})
		second := dom.NewElement("div", dom.Attribute{Name: "style", Value: fmt.Sprintf("height:10px;margin-top:%dpx", margins[3])})
		body.AppendChild(first)
		body.AppendChild(empty)
		body.AppendChild(second)
		html.AppendChild(body)
		document.AppendChild(html)

		frame, err := render.Render(document, render.Viewport{Width: 320, Height: 240})
		if err != nil {
			return
		}
		firstBox := findBox(frame.Root, first)
		emptyBox := findBox(frame.Root, empty)
		secondBox := findBox(frame.Root, second)
		if firstBox == nil || emptyBox == nil || secondBox == nil {
			t.Fatalf("margin fuzz omitted boxes: first:%#v empty:%#v second:%#v", firstBox, emptyBox, secondBox)
		}
		largestPositive := 0.0
		mostNegative := 0.0
		for _, margin := range margins {
			value := float64(margin)
			if value >= 0 {
				largestPositive = math.Max(largestPositive, value)
			} else {
				mostNegative = math.Min(mostNegative, value)
			}
		}
		wantY := firstBox.Bounds.Y + firstBox.Bounds.Height + largestPositive + mostNegative
		if secondBox.Bounds.Y != wantY || emptyBox.Bounds.Y != wantY {
			t.Fatalf("collapsed y = second:%v empty:%v, want %v for margins %v", secondBox.Bounds.Y, emptyBox.Bounds.Y, wantY, margins)
		}
		for name, value := range map[string]float64{
			"first y":     firstBox.Bounds.Y,
			"empty y":     emptyBox.Bounds.Y,
			"second y":    secondBox.Bounds.Y,
			"body height": findBox(frame.Root, body).Bounds.Height,
		} {
			if math.IsNaN(value) || math.IsInf(value, 0) {
				t.Fatalf("%s = %v, want finite geometry", name, value)
			}
		}
	})
}
