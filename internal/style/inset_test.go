package style

import (
	"testing"

	"github.com/JediWattson/gossamer/internal/css"
	"github.com/JediWattson/gossamer/internal/dom"
)

func TestInsetAndLogicalInsetProperties(t *testing.T) {
	t.Parallel()

	for _, declaration := range []css.Declaration{
		{Property: "inset", Value: "0"},
		{Property: "inset", Value: "1px 2px 3px 4px"},
		{Property: "inset", Value: "10px auto"},
		{Property: "inset-block", Value: "4px 6px"},
		{Property: "inset-inline", Value: "5% auto"},
		{Property: "inset-block-start", Value: "calc(2px + 3px)"},
	} {
		if !SupportsDeclaration(declaration) {
			t.Errorf("SupportsDeclaration(%#v) = false", declaration)
		}
	}
	for _, declaration := range []css.Declaration{
		{Property: "inset", Value: "1px 2px 3px 4px 5px"},
		{Property: "inset-block", Value: "1px 2px 3px"},
		{Property: "inset-inline-start", Value: "invalid"},
	} {
		if SupportsDeclaration(declaration) {
			t.Errorf("SupportsDeclaration(%#v) = true", declaration)
		}
	}

	document := dom.NewDocument()
	html := dom.NewElement("html")
	body := dom.NewElement("body")
	target := dom.NewElement("div", dom.Attribute{Name: "style", Value: "inset: 1px 2px 3px; inset-inline: 8px 9px"})
	vertical := dom.NewElement("div", dom.Attribute{Name: "style", Value: "writing-mode: vertical-lr; direction: rtl; inset-block: 5px 6px; inset-inline: 7px 8px"})
	physicalWins := dom.NewElement("div", dom.Attribute{Name: "style", Value: "inset-block-start: 7px; top: 2px"})
	document.AppendChild(html)
	html.AppendChild(body)
	body.AppendChild(target)
	body.AppendChild(vertical)
	body.AppendChild(physicalWins)

	snapshot := Compute(document, Input{})
	computed, ok := snapshot.Lookup(target)
	if !ok {
		t.Fatal("target missing computed style")
	}
	if computed.Top() != px(1) || computed.Right() != px(9) || computed.Bottom() != px(3) || computed.Left() != px(8) {
		t.Fatalf("computed insets = top:%#v right:%#v bottom:%#v left:%#v", computed.Top(), computed.Right(), computed.Bottom(), computed.Left())
	}

	vComputed, ok := snapshot.Lookup(vertical)
	if !ok {
		t.Fatal("vertical element missing computed style")
	}
	if vComputed.Top() != px(8) || vComputed.Right() != px(6) || vComputed.Bottom() != px(7) || vComputed.Left() != px(5) {
		t.Fatalf("vertical-lr rtl computed insets = top:%#v right:%#v bottom:%#v left:%#v", vComputed.Top(), vComputed.Right(), vComputed.Bottom(), vComputed.Left())
	}

	pComputed, ok := snapshot.Lookup(physicalWins)
	if !ok {
		t.Fatal("physicalWins element missing computed style")
	}
	if pComputed.Top() != px(2) {
		t.Fatalf("physicalWins top = %#v, want 2px", pComputed.Top())
	}

	for property, want := range map[string]string{
		"inset-block-start":  "5px",
		"inset-block-end":    "6px",
		"inset-inline-start": "7px",
		"inset-inline-end":   "8px",
	} {
		if got, ok := ComputedPropertyValue(vComputed, property); !ok || got != want {
			t.Errorf("ComputedPropertyValue(%q) = %q, %t, want %q, true", property, got, ok, want)
		}
	}
}
