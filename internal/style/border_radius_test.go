package style

import (
	"testing"

	"github.com/JediWattson/gossamer/internal/css"
	"github.com/JediWattson/gossamer/internal/dom"
)

func TestBorderRadiusShorthandExpandsEllipticalCorners(t *testing.T) {
	t.Parallel()

	viewport := Viewport{Width: 800, Height: 600, InitialFontSize: 16}
	parsed, ok := parseBorderRadiusShorthand("10px 20% 30px / 4px 5px", 16, viewport)
	if !ok {
		t.Fatal("border-radius shorthand was rejected")
	}
	wantHorizontal := []struct {
		unit  LengthUnit
		value float64
	}{{LengthPX, 10}, {LengthPercent, 20}, {LengthPX, 30}, {LengthPercent, 20}}
	wantVertical := []float64{4, 5, 4, 5}
	for index, radius := range parsed {
		if radius.horizontal.unit != wantHorizontal[index].unit || radius.horizontal.value != wantHorizontal[index].value {
			t.Errorf("corner %d horizontal = %#v, want %v %v", index, radius.horizontal, wantHorizontal[index].value, wantHorizontal[index].unit)
		}
		if radius.vertical.unit != LengthPX || radius.vertical.value != wantVertical[index] {
			t.Errorf("corner %d vertical = %#v, want %vpx", index, radius.vertical, wantVertical[index])
		}
	}
}

func TestBorderRadiusComputesAndSerializesLonghands(t *testing.T) {
	t.Parallel()

	document := dom.NewDocument()
	html := dom.NewElement("html")
	body := dom.NewElement("body")
	target := dom.NewElement("div", dom.Attribute{Name: "style", Value: "border-radius:10px 20% 30px / 4px 5px"})
	body.AppendChild(target)
	html.AppendChild(body)
	document.AppendChild(html)

	snapshot := Compute(document, Input{Environment: Environment{Width: 800, Height: 600, InitialFontSize: 16}})
	computed, ok := snapshot.Lookup(target)
	if !ok {
		t.Fatal("snapshot does not contain target")
	}
	wants := map[string]string{
		"border-top-left-radius":     "10px 4px",
		"border-top-right-radius":    "20% 5px",
		"border-bottom-right-radius": "30px 4px",
		"border-bottom-left-radius":  "20% 5px",
	}
	for property, want := range wants {
		got, found := ComputedPropertyValue(computed, property)
		if !found || got != want {
			t.Errorf("ComputedPropertyValue(%q) = %q, %t, want %q, true", property, got, found, want)
		}
	}
}

func TestBorderRadiusRejectsNegativeAutoAndMalformedSlash(t *testing.T) {
	t.Parallel()

	viewport := Viewport{Width: 800, Height: 600, InitialFontSize: 16}
	for _, declaration := range []css.Declaration{
		{Property: "border-top-left-radius", Value: "-1px"},
		{Property: "border-top-left-radius", Value: "auto"},
		{Property: "border-radius", Value: "1px /"},
		{Property: "border-radius", Value: "/ 1px"},
		{Property: "border-radius", Value: "1px / 2px / 3px"},
	} {
		if validComputedDeclaration(declaration, viewport) {
			t.Errorf("invalid %s:%s was accepted", declaration.Property, declaration.Value)
		}
	}
}
