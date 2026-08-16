package style_test

import (
	"testing"

	"github.com/JediWattson/gossamer/internal/dom"
	"github.com/JediWattson/gossamer/internal/style"
)

func TestDirectionInitialInheritanceHTMLMappingAndAuthorOverride(t *testing.T) {
	t.Parallel()

	document := dom.NewDocument()
	html := dom.NewElement("html", dom.Attribute{Name: "dir", Value: "rtl"})
	head := dom.NewElement("head")
	styleNode := dom.NewElement("style")
	styleNode.AppendChild(dom.NewText(`#author { direction:ltr }`))
	head.AppendChild(styleNode)
	body := dom.NewElement("body")
	inherited := dom.NewElement("section", dom.Attribute{Name: "id", Value: "inherited"})
	author := dom.NewElement("section", dom.Attribute{Name: "id", Value: "author"}, dom.Attribute{Name: "dir", Value: "rtl"})
	auto := dom.NewElement("section", dom.Attribute{Name: "id", Value: "auto"}, dom.Attribute{Name: "dir", Value: "auto"})
	auto.AppendChild(dom.NewText("مرحبا"))
	telephone := dom.NewElement("input", dom.Attribute{Name: "type", Value: "tel"})
	body.AppendChild(inherited)
	body.AppendChild(author)
	body.AppendChild(auto)
	body.AppendChild(telephone)
	html.AppendChild(head)
	html.AppendChild(body)
	document.AppendChild(html)

	snapshot := style.Compute(document, style.Input{})
	assertDirection := func(node *dom.Node, want string) {
		t.Helper()
		computed, ok := snapshot.Lookup(node)
		if !ok {
			t.Fatalf("computed style missing for %s", node.Data)
		}
		got, ok := style.ComputedPropertyValue(computed, "direction")
		if !ok || got != want {
			t.Fatalf("direction for %s = %q, %t, want %q, true", node.Data, got, ok, want)
		}
	}
	assertDirection(html, "rtl")
	assertDirection(inherited, "rtl")
	assertDirection(author, "ltr")
	assertDirection(auto, "rtl")
	assertDirection(telephone, "ltr")
}

func TestCSSDirectionDoesNotChangeDirPseudoClassState(t *testing.T) {
	t.Parallel()

	document := dom.NewDocument()
	html := dom.NewElement("html")
	head := dom.NewElement("head")
	styleNode := dom.NewElement("style")
	styleNode.AppendChild(dom.NewText(`
		#subject:dir(ltr) { color:#123456 }
		#subject:dir(rtl) { background-color:#abcdef }
	`))
	head.AppendChild(styleNode)
	body := dom.NewElement("body")
	subject := dom.NewElement("div",
		dom.Attribute{Name: "id", Value: "subject"},
		dom.Attribute{Name: "style", Value: "direction:rtl"},
	)
	body.AppendChild(subject)
	html.AppendChild(head)
	html.AppendChild(body)
	document.AppendChild(html)

	computed, ok := style.Compute(document, style.Input{}).Lookup(subject)
	if !ok {
		t.Fatal("computed subject style missing")
	}
	assertComputed := func(property, want string) {
		t.Helper()
		got, supported := style.ComputedPropertyValue(computed, property)
		if !supported || got != want {
			t.Fatalf("%s = %q, %t, want %q, true", property, got, supported, want)
		}
	}
	assertComputed("direction", "rtl")
	assertComputed("color", "rgb(18, 52, 86)")
	assertComputed("background-color", "rgba(0, 0, 0, 0)")
}
