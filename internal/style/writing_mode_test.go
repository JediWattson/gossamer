package style_test

import (
	"testing"

	"github.com/JediWattson/gossamer/internal/dom"
	"github.com/JediWattson/gossamer/internal/style"
)

func TestWritingModeInitialInheritanceCascadeAndAll(t *testing.T) {
	t.Parallel()

	document := dom.NewDocument()
	html := dom.NewElement("html")
	head := dom.NewElement("head")
	styleNode := dom.NewElement("style")
	styleNode.AppendChild(dom.NewText(`
		#parent { writing-mode:vertical-rl }
		#override { writing-mode:vertical-lr }
		#invalid { writing-mode:sideways-rl }
		#reset { all:initial }
		#all-inherit { all:inherit }
	`))
	head.AppendChild(styleNode)
	body := dom.NewElement("body")
	parent := dom.NewElement("section", dom.Attribute{Name: "id", Value: "parent"})
	inherited := dom.NewElement("div", dom.Attribute{Name: "id", Value: "inherited"})
	override := dom.NewElement("div", dom.Attribute{Name: "id", Value: "override"})
	invalid := dom.NewElement("div", dom.Attribute{Name: "id", Value: "invalid"})
	reset := dom.NewElement("div", dom.Attribute{Name: "id", Value: "reset"})
	allInherit := dom.NewElement("div", dom.Attribute{Name: "id", Value: "all-inherit"})
	parent.AppendChild(inherited)
	parent.AppendChild(override)
	parent.AppendChild(invalid)
	parent.AppendChild(reset)
	parent.AppendChild(allInherit)
	body.AppendChild(parent)
	html.AppendChild(head)
	html.AppendChild(body)
	document.AppendChild(html)

	snapshot := style.Compute(document, style.Input{})
	assert := func(node *dom.Node, want string, wantMode style.WritingMode) {
		t.Helper()
		computed, ok := snapshot.Lookup(node)
		if !ok {
			t.Fatalf("computed style missing for %s", node.Data)
		}
		got, supported := style.ComputedPropertyValue(computed, "writing-mode")
		if !supported || got != want || computed.WritingMode() != wantMode {
			t.Fatalf("writing-mode for %s = %q/%d, %t, want %q/%d, true", node.Data, got, computed.WritingMode(), supported, want, wantMode)
		}
	}
	assert(html, "horizontal-tb", style.WritingModeHorizontalTB)
	assert(parent, "vertical-rl", style.WritingModeVerticalRL)
	assert(inherited, "vertical-rl", style.WritingModeVerticalRL)
	assert(override, "vertical-lr", style.WritingModeVerticalLR)
	assert(invalid, "vertical-rl", style.WritingModeVerticalRL)
	assert(reset, "horizontal-tb", style.WritingModeHorizontalTB)
	assert(allInherit, "vertical-rl", style.WritingModeVerticalRL)

	computed, _ := snapshot.Lookup(parent)
	found := false
	for _, name := range style.ComputedPropertyNames(computed) {
		if name == "writing-mode" {
			found = true
			break
		}
	}
	if !found {
		t.Fatal("computed property enumeration omitted writing-mode")
	}
}
