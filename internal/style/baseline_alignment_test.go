package style

import (
	"testing"

	"github.com/JediWattson/gossamer/internal/dom"
)

func TestBaselineAlignmentComputesCanonicalFirstAndLastValues(t *testing.T) {
	t.Parallel()

	document := dom.NewDocument()
	html := dom.NewElement("html")
	body := dom.NewElement("body")
	target := dom.NewElement("div", dom.Attribute{Name: "style", Value: `
		align-content:first baseline;
		align-items:baseline;
		align-self:last baseline;
		justify-content:end; justify-content:baseline;
		justify-items:baseline first;
		justify-self:baseline last;
	`})
	body.AppendChild(target)
	html.AppendChild(body)
	document.AppendChild(html)

	computed, ok := Compute(document, Input{Environment: Environment{Width: 320, Height: 240, InitialFontSize: 16}}).Lookup(target)
	if !ok {
		t.Fatal("target has no computed style")
	}
	for property, want := range map[string]string{
		"align-content":   "baseline",
		"align-items":     "baseline",
		"align-self":      "last baseline",
		"justify-content": "end",
		"justify-items":   "baseline",
		"justify-self":    "last baseline",
	} {
		if got, found := ComputedPropertyValue(computed, property); !found || got != want {
			t.Errorf("%s = %q, %t, want %q, true", property, got, found, want)
		}
	}
	if computed.AlignContent() != JustifyBaseline || computed.AlignItems() != AlignBaseline ||
		computed.AlignSelf() != AlignLastBaseline || computed.JustifyItems() != AlignBaseline ||
		computed.JustifySelf() != AlignLastBaseline {
		t.Fatalf("typed baseline alignment was not retained: %#v", computed)
	}
}

func TestBaselineAlignmentRejectsOverflowPrefixesAndMalformedPairs(t *testing.T) {
	t.Parallel()

	document := dom.NewDocument()
	html := dom.NewElement("html")
	body := dom.NewElement("body")
	target := dom.NewElement("div", dom.Attribute{Name: "style", Value: `
		align-content:end; align-content:safe baseline;
		align-items:end; align-items:baseline baseline;
		align-self:end; align-self:first last;
		justify-items:end; justify-items:last;
		justify-self:end; justify-self:unsafe last baseline;
	`})
	body.AppendChild(target)
	html.AppendChild(body)
	document.AppendChild(html)

	computed, ok := Compute(document, Input{Environment: Environment{Width: 320, Height: 240, InitialFontSize: 16}}).Lookup(target)
	if !ok {
		t.Fatal("target has no computed style")
	}
	for _, property := range []string{"align-content", "align-items", "align-self", "justify-items", "justify-self"} {
		if got, found := ComputedPropertyValue(computed, property); !found || got != "end" {
			t.Errorf("%s = %q, %t, want lower valid end", property, got, found)
		}
	}
}

func TestBaselineAlignmentResolvesVariablesAndExplicitInheritance(t *testing.T) {
	t.Parallel()

	document := dom.NewDocument()
	html := dom.NewElement("html")
	body := dom.NewElement("body")
	parent := dom.NewElement("section", dom.Attribute{Name: "style", Value: `
		--alignment-mode:last baseline;
		align-self:first baseline;
	`})
	target := dom.NewElement("div", dom.Attribute{Name: "style", Value: `
		align-items:var(--alignment-mode);
		align-self:inherit;
		justify-self:var(--missing, baseline);
	`})
	parent.AppendChild(target)
	body.AppendChild(parent)
	html.AppendChild(body)
	document.AppendChild(html)

	computed, ok := Compute(document, Input{Environment: Environment{Width: 320, Height: 240, InitialFontSize: 16}}).Lookup(target)
	if !ok {
		t.Fatal("target has no computed style")
	}
	for property, want := range map[string]string{
		"align-items":  "last baseline",
		"align-self":   "baseline",
		"justify-self": "baseline",
	} {
		if got, found := ComputedPropertyValue(computed, property); !found || got != want {
			t.Errorf("%s = %q, %t, want %q, true", property, got, found, want)
		}
	}
}
