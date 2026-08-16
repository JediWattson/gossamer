package style

import (
	"testing"

	"github.com/JediWattson/gossamer/internal/dom"
)

func TestOverflowAlignmentComputesAndSerializesSpecifiedKeywords(t *testing.T) {
	t.Parallel()

	document := dom.NewDocument()
	html := dom.NewElement("html")
	body := dom.NewElement("body")
	target := dom.NewElement("div", dom.Attribute{Name: "style", Value: `
		align-content:safe end;
		align-items:unsafe center;
		align-self:safe normal;
		justify-content:unsafe center;
		justify-items:safe self-end;
		justify-self:unsafe flex-end;
	`})
	body.AppendChild(target)
	html.AppendChild(body)
	document.AppendChild(html)

	snapshot := Compute(document, Input{Environment: Environment{Width: 320, Height: 240, InitialFontSize: 16}})
	computed, ok := snapshot.Lookup(target)
	if !ok {
		t.Fatal("target has no computed style")
	}
	for property, want := range map[string]string{
		"align-content":   "safe end",
		"align-items":     "unsafe center",
		"align-self":      "safe normal",
		"justify-content": "unsafe center",
		"justify-items":   "safe self-end",
		"justify-self":    "unsafe flex-end",
	} {
		if got, found := ComputedPropertyValue(computed, property); !found || got != want {
			t.Errorf("%s = %q, %t, want %q, true", property, got, found, want)
		}
	}
	if computed.AlignContentOverflow() != OverflowAlignmentSafe ||
		computed.AlignItemsOverflow() != OverflowAlignmentUnsafe ||
		computed.AlignSelfOverflow() != OverflowAlignmentSafe ||
		computed.JustifyContentOverflow() != OverflowAlignmentUnsafe ||
		computed.JustifyItemsOverflow() != OverflowAlignmentSafe ||
		computed.JustifySelfOverflow() != OverflowAlignmentUnsafe {
		t.Fatalf("computed overflow alignment modes were not retained: %#v", computed)
	}
}

func TestOverflowAlignmentGrammarRejectsInvalidCombinationsAtomically(t *testing.T) {
	t.Parallel()

	document := dom.NewDocument()
	html := dom.NewElement("html")
	body := dom.NewElement("body")
	target := dom.NewElement("div", dom.Attribute{Name: "style", Value: `
		align-content:end; align-content:safe space-between;
		align-items:end; align-items:safe normal;
		align-self:end; align-self:safe auto;
		justify-content:end; justify-content:center safe;
		justify-items:end; justify-items:safe stretch;
		justify-self:end; justify-self:safe;
	`})
	body.AppendChild(target)
	html.AppendChild(body)
	document.AppendChild(html)

	computed, ok := Compute(document, Input{Environment: Environment{Width: 320, Height: 240, InitialFontSize: 16}}).Lookup(target)
	if !ok {
		t.Fatal("target has no computed style")
	}
	for _, property := range []string{
		"align-content", "align-items", "align-self",
		"justify-content", "justify-items", "justify-self",
	} {
		if got, found := ComputedPropertyValue(computed, property); !found || got != "end" {
			t.Errorf("%s = %q, %t, want lower valid declaration end", property, got, found)
		}
	}
	if computed.AlignContentOverflow() != OverflowAlignmentDefault ||
		computed.AlignItemsOverflow() != OverflowAlignmentDefault ||
		computed.AlignSelfOverflow() != OverflowAlignmentDefault ||
		computed.JustifyContentOverflow() != OverflowAlignmentDefault ||
		computed.JustifyItemsOverflow() != OverflowAlignmentDefault ||
		computed.JustifySelfOverflow() != OverflowAlignmentDefault {
		t.Fatal("invalid overflow-position declaration changed computed overflow state")
	}
}

func TestOverflowAlignmentAcceptsEscapedKeywordTokens(t *testing.T) {
	t.Parallel()

	parsed, ok := parseContentAlignment(`s\61 fe c\65 nter`)
	if !ok || parsed.position != JustifyCenter || parsed.overflow != OverflowAlignmentSafe {
		t.Fatalf("escaped safe center = %#v, %t", parsed, ok)
	}
	self, ok := parseSelfAlignment(`uns\61 fe self-end`, false)
	if !ok || self.position != AlignSelfEnd || self.overflow != OverflowAlignmentUnsafe {
		t.Fatalf("escaped unsafe self-end = %#v, %t", self, ok)
	}
}

func TestOverflowAlignmentSurvivesVariablesAndCSSWideInheritance(t *testing.T) {
	t.Parallel()

	document := dom.NewDocument()
	html := dom.NewElement("html")
	body := dom.NewElement("body")
	parent := dom.NewElement("section", dom.Attribute{Name: "style", Value: "--mode:safe center;justify-content:var(--mode);align-items:unsafe end"})
	child := dom.NewElement("div", dom.Attribute{Name: "style", Value: "justify-content:inherit;align-items:inherit"})
	parent.AppendChild(child)
	body.AppendChild(parent)
	html.AppendChild(body)
	document.AppendChild(html)

	snapshot := Compute(document, Input{Environment: Environment{Width: 320, Height: 240, InitialFontSize: 16}})
	parentStyle, ok := snapshot.Lookup(parent)
	if !ok {
		t.Fatal("parent has no computed style")
	}
	childStyle, ok := snapshot.Lookup(child)
	if !ok {
		t.Fatal("child has no computed style")
	}
	for _, computed := range []ComputedStyle{parentStyle, childStyle} {
		if got, _ := ComputedPropertyValue(computed, "justify-content"); got != "safe center" {
			t.Errorf("justify-content = %q, want safe center", got)
		}
		if got, _ := ComputedPropertyValue(computed, "align-items"); got != "unsafe end" {
			t.Errorf("align-items = %q, want unsafe end", got)
		}
	}
}
