package style

import (
	"testing"

	"github.com/JediWattson/gossamer/internal/css"
	"github.com/JediWattson/gossamer/internal/dom"
)

func TestLogicalPaddingMapsThroughWritingModeAndDirection(t *testing.T) {
	t.Parallel()

	for _, test := range []struct {
		name  string
		style string
		want  [4]float64
	}{
		{
			name:  "horizontal ltr",
			style: "padding-block:4px 6px;padding-inline:8px 10px",
			want:  [4]float64{4, 10, 6, 8},
		},
		{
			name:  "vertical rl rtl",
			style: "writing-mode:vertical-rl;direction:rtl;padding-block:1px 2px;padding-inline:3px 4px",
			want:  [4]float64{4, 1, 3, 2},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			document := dom.NewDocument()
			html := dom.NewElement("html")
			body := dom.NewElement("body")
			target := dom.NewElement("div", dom.Attribute{Name: "style", Value: test.style})
			body.AppendChild(target)
			html.AppendChild(body)
			document.AppendChild(html)

			snapshot := Compute(document, Input{Environment: Environment{Width: 320, Height: 200, InitialFontSize: 16}})
			computed, ok := snapshot.Lookup(target)
			if !ok {
				t.Fatal("snapshot does not contain target")
			}
			got := [4]float64{
				computed.PaddingTop().Value(), computed.PaddingRight().Value(),
				computed.PaddingBottom().Value(), computed.PaddingLeft().Value(),
			}
			if got != test.want {
				t.Fatalf("physical padding = %v, want %v", got, test.want)
			}
		})
	}
}

func TestLogicalPaddingAndPhysicalPaddingShareCascadeOrder(t *testing.T) {
	t.Parallel()

	document := dom.NewDocument()
	html := dom.NewElement("html")
	body := dom.NewElement("body")
	logicalWins := dom.NewElement("div", dom.Attribute{Name: "style", Value: "padding-top:1px;padding-block-start:7px"})
	physicalWins := dom.NewElement("div", dom.Attribute{Name: "style", Value: "padding-block-start:7px;padding-top:1px"})
	body.AppendChild(logicalWins)
	body.AppendChild(physicalWins)
	html.AppendChild(body)
	document.AppendChild(html)

	snapshot := Compute(document, Input{Environment: Environment{Width: 320, Height: 200, InitialFontSize: 16}})
	first, _ := snapshot.Lookup(logicalWins)
	second, _ := snapshot.Lookup(physicalWins)
	if got := first.PaddingTop().Value(); got != 7 {
		t.Errorf("later logical padding top = %v, want 7", got)
	}
	if got := second.PaddingTop().Value(); got != 1 {
		t.Errorf("later physical padding top = %v, want 1", got)
	}
}

func TestLogicalPaddingInheritanceMapsBetweenWritingModes(t *testing.T) {
	t.Parallel()

	document := dom.NewDocument()
	html := dom.NewElement("html")
	body := dom.NewElement("body")
	parent := dom.NewElement("div", dom.Attribute{Name: "style", Value: "padding-block-start:11px"})
	child := dom.NewElement("div", dom.Attribute{Name: "style", Value: "writing-mode:vertical-rl;padding-block-start:inherit"})
	parent.AppendChild(child)
	body.AppendChild(parent)
	html.AppendChild(body)
	document.AppendChild(html)

	snapshot := Compute(document, Input{Environment: Environment{Width: 320, Height: 200, InitialFontSize: 16}})
	computed, _ := snapshot.Lookup(child)
	if got := computed.PaddingRight().Value(); got != 11 {
		t.Fatalf("inherited vertical block-start padding = %v, want 11 on the right edge", got)
	}
	if got := computed.PaddingTop().Value(); got != 0 {
		t.Fatalf("inherited vertical physical top padding = %v, want 0", got)
	}
}

func TestLogicalPaddingSupportAndComputedSerialization(t *testing.T) {
	t.Parallel()

	for _, declaration := range []css.Declaration{
		{Property: "padding-block", Value: "4px 6px"},
		{Property: "padding-inline", Value: "5%"},
		{Property: "padding-block-start", Value: "calc(2px + 3px)"},
	} {
		if !SupportsDeclaration(declaration) {
			t.Errorf("SupportsDeclaration(%#v) = false", declaration)
		}
	}
	for _, declaration := range []css.Declaration{
		{Property: "padding-block", Value: "1px 2px 3px"},
		{Property: "padding-inline-start", Value: "-1px"},
	} {
		if SupportsDeclaration(declaration) {
			t.Errorf("SupportsDeclaration(%#v) = true", declaration)
		}
	}

	computed := cssInitialStyle(Viewport{})
	computed.writingMode = WritingModeVerticalLR
	computed.direction = DirectionRTL
	computed.paddingTop = px(4)
	computed.paddingRight = px(3)
	computed.paddingBottom = px(2)
	computed.paddingLeft = px(1)
	for property, want := range map[string]string{
		"padding-block-start":  "1px",
		"padding-block-end":    "3px",
		"padding-inline-start": "2px",
		"padding-inline-end":   "4px",
	} {
		if got, ok := ComputedPropertyValue(computed, property); !ok || got != want {
			t.Errorf("ComputedPropertyValue(%q) = %q, %t, want %q, true", property, got, ok, want)
		}
	}
}
