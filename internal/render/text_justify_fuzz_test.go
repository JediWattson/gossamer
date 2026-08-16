package render_test

import (
	"math"
	"strconv"
	"strings"
	"testing"

	"github.com/JediWattson/gossamer/internal/dom"
	"github.com/JediWattson/gossamer/internal/render"
)

func FuzzJustifiedLineLayoutDoesNotPanic(f *testing.F) {
	f.Add(uint16(80), []byte{3, 3, 5, 4, 4})
	f.Add(uint16(1), []byte{1, 10, 2})
	f.Add(uint16(320), []byte{8, 1, 8, 1, 8})

	f.Fuzz(func(t *testing.T, rawWidth uint16, rawWords []byte) {
		if len(rawWords) == 0 {
			rawWords = []byte{1}
		}
		if len(rawWords) > 32 {
			rawWords = rawWords[:32]
		}
		words := make([]string, len(rawWords))
		for index, raw := range rawWords {
			words[index] = strings.Repeat(string(rune('a'+index%26)), int(raw%10)+1)
		}
		width := int(rawWidth%512) + 1

		document := dom.NewDocument()
		html := dom.NewElement("html")
		body := dom.NewElement("body", dom.Attribute{Name: "style", Value: "margin:0"})
		container := dom.NewElement("div", dom.Attribute{Name: "style", Value: "text-align:justify;font:10px monospace;width:" + strconv.Itoa(width) + "px"})
		textNode := dom.NewText(strings.Join(words, " "))
		container.AppendChild(textNode)
		body.AppendChild(container)
		html.AppendChild(body)
		document.AppendChild(html)

		frame, err := render.Render(document, render.Viewport{Width: 640, Height: 1024})
		if err != nil {
			return
		}
		box := findBox(frame.Root, container)
		lines := textFragmentLinesForNode(collectTextFragments(frame.Root), textNode)
		if box == nil || len(lines) == 0 {
			t.Fatalf("justification fuzz omitted layout: box:%#v lines:%#v", box, lines)
		}
		rightEdge := box.ContentBounds.X + box.ContentBounds.Width
		for lineIndex, line := range lines {
			previousRight := math.Inf(-1)
			for _, fragment := range line {
				for name, value := range map[string]float64{"x": fragment.X, "width": fragment.Width, "baseline": fragment.BaselineY} {
					if math.IsNaN(value) || math.IsInf(value, 0) {
						t.Fatalf("line %d %s = %v, want finite", lineIndex, name, value)
					}
				}
				if fragment.X < previousRight {
					t.Fatalf("line %d fragments overlap or reverse: %#v", lineIndex, line)
				}
				previousRight = fragment.X + fragment.Width
			}
			if lineIndex != len(lines)-1 && len(line) > 1 && math.Abs(lineRight(line)-rightEdge) > 0.001 {
				t.Fatalf("soft line %d right = %v, want %v: %#v", lineIndex, lineRight(line), rightEdge, line)
			}
		}
		if len(lines[len(lines)-1]) != 1 {
			t.Fatalf("final line was justified into %d fragments: %#v", len(lines[len(lines)-1]), lines[len(lines)-1])
		}
	})
}
