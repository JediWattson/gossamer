package css_test

import (
	"testing"

	"github.com/JediWattson/gossamer/internal/css"
	"github.com/JediWattson/gossamer/internal/dom"
)

func TestSelectorMatchContextDrivesDynamicPseudoClasses(t *testing.T) {
	t.Parallel()

	root := dom.NewElement("main")
	section := dom.NewElement("section")
	button := dom.NewElement("button")
	other := dom.NewElement("button")
	link := dom.NewElement("a", dom.Attribute{Name: "href", Value: "/visited"})
	section.AppendChild(button)
	section.AppendChild(other)
	section.AppendChild(link)
	root.AppendChild(section)

	context := css.MatchContext{
		Hovered:      button,
		Active:       button,
		Focused:      button,
		FocusVisible: true,
		Target:       section,
		Visited:      func(node *dom.Node) bool { return node == link },
	}
	tests := []struct {
		selector string
		node     *dom.Node
		want     bool
	}{
		{selector: "button:hover:active:focus:focus-visible", node: button, want: true},
		{selector: "main:hover:active:focus-within", node: root, want: true},
		{selector: "section:target", node: section, want: true},
		{selector: "section:is(:hover, .missing)", node: section, want: true},
		{selector: "button:hover", node: other, want: false},
		{selector: "button:focus", node: other, want: false},
		{selector: "section:focus", node: section, want: false},
		{selector: "a:visited:any-link", node: link, want: true},
		{selector: "a:link", node: link, want: false},
	}
	for _, test := range tests {
		t.Run(test.selector, func(t *testing.T) {
			t.Parallel()
			got := parseOneSelector(t, test.selector).MatchesWithContext(test.node, context)
			if got != test.want {
				t.Errorf("MatchesWithContext() = %t, want %t", got, test.want)
			}
		})
	}
	if parseOneSelector(t, "button:focus-visible").MatchesWithContext(button, css.MatchContext{Focused: button}) {
		t.Fatal(":focus-visible matched without focus-visible state")
	}
}

func TestSelectorMatchContextFlowsIntoNthOfFilter(t *testing.T) {
	t.Parallel()

	list := dom.NewElement("ul")
	first := dom.NewElement("li")
	second := dom.NewElement("li")
	list.AppendChild(first)
	list.AppendChild(second)
	selector := parseOneSelector(t, "li:nth-child(1 of :hover)")
	context := css.MatchContext{Hovered: second}
	if selector.MatchesWithContext(first, context) {
		t.Fatal("unhovered sibling matched stateful nth filter")
	}
	if !selector.MatchesWithContext(second, context) {
		t.Fatal("hovered sibling did not match as first filtered child")
	}
}
