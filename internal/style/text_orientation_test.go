package style_test

import (
	"testing"

	"github.com/JediWattson/gossamer/internal/dom"
	"github.com/JediWattson/gossamer/internal/style"
)

func TestTextOrientationInitialInheritanceCascadeAndAll(t *testing.T) {
	t.Parallel()

	document := dom.NewDocument()
	html := dom.NewElement("html")
	head := dom.NewElement("head")
	styleNode := dom.NewElement("style")
	styleNode.AppendChild(dom.NewText(`
		#parent { writing-mode:vertical-rl; text-orientation:upright }
		#override { text-orientation:sideways }
		#invalid { text-orientation:rotate-left }
		#reset { all:initial }
		#all-inherit { all:inherit }
		#horizontal { writing-mode:horizontal-tb; text-orientation:upright }
	`))
	head.AppendChild(styleNode)
	body := dom.NewElement("body")
	parent := dom.NewElement("section", dom.Attribute{Name: "id", Value: "parent"})
	inherited := dom.NewElement("div", dom.Attribute{Name: "id", Value: "inherited"})
	override := dom.NewElement("div", dom.Attribute{Name: "id", Value: "override"})
	invalid := dom.NewElement("div", dom.Attribute{Name: "id", Value: "invalid"})
	reset := dom.NewElement("div", dom.Attribute{Name: "id", Value: "reset"})
	allInherit := dom.NewElement("div", dom.Attribute{Name: "id", Value: "all-inherit"})
	horizontal := dom.NewElement("div", dom.Attribute{Name: "id", Value: "horizontal"})
	for _, child := range []*dom.Node{inherited, override, invalid, reset, allInherit, horizontal} {
		parent.AppendChild(child)
	}
	body.AppendChild(parent)
	html.AppendChild(head)
	html.AppendChild(body)
	document.AppendChild(html)

	snapshot := style.Compute(document, style.Input{})
	assert := func(node *dom.Node, want string, wantOrientation style.TextOrientation) {
		t.Helper()
		computed, ok := snapshot.Lookup(node)
		if !ok {
			t.Fatalf("computed style missing for %s", node.Data)
		}
		got, supported := style.ComputedPropertyValue(computed, "text-orientation")
		if !supported || got != want || computed.TextOrientation() != wantOrientation {
			t.Fatalf("text-orientation for %s = %q/%d, %t, want %q/%d, true", node.Data, got, computed.TextOrientation(), supported, want, wantOrientation)
		}
	}
	assert(html, "mixed", style.TextOrientationMixed)
	assert(parent, "upright", style.TextOrientationUpright)
	assert(inherited, "upright", style.TextOrientationUpright)
	assert(override, "sideways", style.TextOrientationSideways)
	assert(invalid, "upright", style.TextOrientationUpright)
	assert(reset, "mixed", style.TextOrientationMixed)
	assert(allInherit, "upright", style.TextOrientationUpright)
	// The computed value is retained in horizontal writing modes even though it
	// has no effect on glyph layout there.
	assert(horizontal, "upright", style.TextOrientationUpright)
}
