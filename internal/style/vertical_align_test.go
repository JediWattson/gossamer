package style_test

import (
	"testing"

	"github.com/JediWattson/gossamer/internal/dom"
	"github.com/JediWattson/gossamer/internal/style"
)

func TestVerticalAlignComputedValuesAndGrammar(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		source string
		want   string
	}{
		{name: "initial", want: "baseline"},
		{name: "baseline", source: "vertical-align: baseline", want: "baseline"},
		{name: "sub", source: "vertical-align: sub", want: "sub"},
		{name: "super", source: "vertical-align: super", want: "super"},
		{name: "text top", source: "vertical-align: text-top", want: "text-top"},
		{name: "text bottom", source: "vertical-align: text-bottom", want: "text-bottom"},
		{name: "middle", source: "vertical-align: middle", want: "middle"},
		{name: "line top", source: "vertical-align: top", want: "top"},
		{name: "line bottom", source: "vertical-align: bottom", want: "bottom"},
		{name: "percentage", source: "vertical-align: 25%", want: "25%"},
		{name: "negative length", source: "vertical-align: -3px", want: "-3px"},
		{name: "unitless zero", source: "vertical-align: 0", want: "0px"},
		{name: "font relative length", source: "font-size: 20px; vertical-align: 1em", want: "20px"},
		{name: "escaped keyword", source: `vertical-align: s\75 per`, want: "super"},
		{name: "invalid loser", source: "vertical-align: super; vertical-align: auto", want: "super"},
		{name: "invalid nonzero number", source: "vertical-align: sub; vertical-align: 1", want: "sub"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			computed := computeVerticalAlignStyle(t, "", test.source)
			got, ok := style.ComputedPropertyValue(computed, "vertical-align")
			if !ok || got != test.want {
				t.Fatalf("vertical-align = %q, %t; want %q, true", got, ok, test.want)
			}
		})
	}
}

func TestVerticalAlignDoesNotInheritUnlessExplicitlyRequested(t *testing.T) {
	t.Parallel()

	initial := computeVerticalAlignStyle(t, "vertical-align: super", "")
	if got, _ := style.ComputedPropertyValue(initial, "vertical-align"); got != "baseline" {
		t.Fatalf("ordinary child vertical-align = %q, want baseline", got)
	}
	inherited := computeVerticalAlignStyle(t, "vertical-align: super", "vertical-align: inherit")
	if got, _ := style.ComputedPropertyValue(inherited, "vertical-align"); got != "super" {
		t.Fatalf("explicitly inherited vertical-align = %q, want super", got)
	}
}

func computeVerticalAlignStyle(t *testing.T, parentStyle, childStyle string) style.ComputedStyle {
	t.Helper()
	document := dom.NewDocument()
	html := dom.NewElement("html")
	body := dom.NewElement("body")
	parent := dom.NewElement("div", dom.Attribute{Name: "style", Value: parentStyle})
	child := dom.NewElement("span", dom.Attribute{Name: "style", Value: childStyle})
	child.AppendChild(dom.NewText("target"))
	parent.AppendChild(child)
	body.AppendChild(parent)
	html.AppendChild(dom.NewElement("head"))
	html.AppendChild(body)
	document.AppendChild(html)

	snapshot := style.Compute(document, style.Input{Environment: style.Environment{
		Width: 320, Height: 200, InitialFontSize: 16,
	}})
	computed, ok := snapshot.Lookup(child)
	if !ok {
		t.Fatal("computed snapshot does not contain vertical-align target")
	}
	return computed
}
