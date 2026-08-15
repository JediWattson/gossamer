package css_test

import (
	"strings"
	"testing"

	"github.com/JediWattson/gossamer/internal/css"
	"github.com/JediWattson/gossamer/internal/dom"
	htmlparser "github.com/JediWattson/gossamer/internal/html"
)

func TestSelectorMatchesCompoundSelectors(t *testing.T) {
	t.Parallel()

	element := dom.NewElement("DIV",
		dom.Attribute{Name: "ID", Value: "main"},
		dom.Attribute{Name: "CLASS", Value: "card\tfeatured\nwide\r\fselected"},
	)
	tests := []struct {
		selector string
		want     bool
	}{
		{selector: "*", want: true},
		{selector: "div", want: true},
		{selector: "DIV", want: true},
		{selector: "#main", want: true},
		{selector: ".card", want: true},
		{selector: ".wide", want: true},
		{selector: ".selected", want: true},
		{selector: "div#main.card.featured", want: true},
		{selector: "span", want: false},
		{selector: "#Main", want: false},
		{selector: ".Card", want: false},
		{selector: ".car", want: false},
		{selector: "div#main.missing", want: false},
		{selector: ":hover", want: false},
	}

	for _, test := range tests {
		t.Run(test.selector, func(t *testing.T) {
			t.Parallel()
			selector := parseOneSelector(t, test.selector)
			if got := selector.Matches(element); got != test.want {
				t.Errorf("Matches() = %t, want %t", got, test.want)
			}
		})
	}
}

func TestUnsupportedPseudoClassInvalidatesSelectorList(t *testing.T) {
	t.Parallel()

	stylesheet, err := css.Parse(`div, :not-yet-supported { color: red } p { color: blue }`)
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	if got, want := len(stylesheet.Rules), 1; got != want {
		t.Fatalf("len(Rules) = %d, want %d", got, want)
	}
	remaining := stylesheet.Rules[0].Selectors[0]
	if !remaining.Matches(dom.NewElement("p")) {
		t.Error("remaining p selector did not match p")
	}
	if remaining.Matches(dom.NewElement("div")) {
		t.Error("remaining p selector matched div")
	}
}

func TestSelectorOnlyMatchesElements(t *testing.T) {
	t.Parallel()

	selector := parseOneSelector(t, "*")
	if (css.Selector{}).Matches(dom.NewElement("div")) {
		t.Error("zero-value selector matched an element")
	}
	for name, node := range map[string]*dom.Node{
		"nil":                    nil,
		"document":               dom.NewDocument(),
		"doctype":                dom.NewDoctype("html"),
		"text":                   dom.NewText("hello"),
		"comment":                dom.NewComment("note"),
		"processing instruction": dom.NewProcessingInstruction("build", "debug"),
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			if selector.Matches(node) {
				t.Error("Matches() = true, want false")
			}
		})
	}
}

func TestSelectorMatchesLinkStateWithoutHistory(t *testing.T) {
	t.Parallel()

	link := dom.NewElement("a", dom.Attribute{Name: "href", Value: ""})
	area := dom.NewElement("area", dom.Attribute{Name: "href", Value: "/map"})
	stylesheetLink := dom.NewElement("link", dom.Attribute{Name: "href", Value: "/site.css"})
	plainAnchor := dom.NewElement("a")
	other := dom.NewElement("div", dom.Attribute{Name: "href", Value: "/not-a-link"})

	tests := []struct {
		name     string
		selector string
		node     *dom.Node
		want     bool
	}{
		{name: "link", selector: "a:link", node: link, want: true},
		{name: "any link", selector: ":any-link", node: area, want: true},
		{name: "stylesheet link", selector: "link:any-link", node: stylesheetLink, want: true},
		{name: "visited has no history state", selector: "a:visited", node: link, want: false},
		{name: "anchor without href", selector: "a:link", node: plainAnchor, want: false},
		{name: "other element with href", selector: ":link", node: other, want: false},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			if got := parseOneSelector(t, test.selector).Matches(test.node); got != test.want {
				t.Errorf("Matches() = %t, want %t", got, test.want)
			}
		})
	}
}

func TestSelectorMatchesStructuralPseudoClasses(t *testing.T) {
	t.Parallel()

	document := dom.NewDocument()
	html := dom.NewElement("html")
	document.AppendChild(dom.NewComment("before root"))
	document.AppendChild(html)
	parent := dom.NewElement("main")
	html.AppendChild(parent)
	first := dom.NewElement("p")
	middle := dom.NewElement("span")
	last := dom.NewElement("p")
	parent.AppendChild(dom.NewText("before"))
	parent.AppendChild(first)
	parent.AppendChild(dom.NewComment("between"))
	parent.AppendChild(middle)
	parent.AppendChild(last)
	parent.AppendChild(dom.NewText("after"))

	tests := []struct {
		name     string
		selector string
		node     *dom.Node
		want     bool
	}{
		{name: "root", selector: ":root", node: html, want: true},
		{name: "non-root", selector: ":root", node: parent, want: false},
		{name: "first child", selector: "p:first-child", node: first, want: true},
		{name: "not first child", selector: "p:first-child", node: last, want: false},
		{name: "last child", selector: "p:last-child", node: last, want: true},
		{name: "not last child", selector: "p:last-child", node: first, want: false},
		{name: "first of type", selector: "p:first-of-type", node: first, want: true},
		{name: "not first of type", selector: "p:first-of-type", node: last, want: false},
		{name: "last of type", selector: "p:last-of-type", node: last, want: true},
		{name: "not last of type", selector: "p:last-of-type", node: first, want: false},
		{name: "not only of type", selector: "p:only-of-type", node: first, want: false},
		{name: "only of type", selector: "span:only-of-type", node: middle, want: true},
		{name: "not only child", selector: "span:only-child", node: middle, want: false},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			if got := parseOneSelector(t, test.selector).Matches(test.node); got != test.want {
				t.Errorf("Matches() = %t, want %t", got, test.want)
			}
		})
	}

	onlyParent := dom.NewElement("section")
	only := dom.NewElement("strong")
	onlyParent.AppendChild(dom.NewText("before"))
	onlyParent.AppendChild(only)
	onlyParent.AppendChild(dom.NewComment("after"))
	if !parseOneSelector(t, ":only-child").Matches(only) {
		t.Error(":only-child did not match the sole element child")
	}
}

func TestSelectorMatchesEmptyPseudoClass(t *testing.T) {
	t.Parallel()

	selector := parseOneSelector(t, ":empty")
	empty := dom.NewElement("div")
	empty.AppendChild(dom.NewComment("ignored"))
	empty.AppendChild(dom.NewText(""))
	if !selector.Matches(empty) {
		t.Error(":empty did not match an element with only comments and empty text")
	}

	withWhitespace := dom.NewElement("div")
	withWhitespace.AppendChild(dom.NewText(" "))
	if selector.Matches(withWhitespace) {
		t.Error(":empty matched an element with a whitespace text node")
	}

	withElement := dom.NewElement("div")
	withElement.AppendChild(dom.NewElement("span"))
	if selector.Matches(withElement) {
		t.Error(":empty matched an element child")
	}
}

func TestRuleMatchUsesGreatestMatchingSpecificity(t *testing.T) {
	t.Parallel()

	stylesheet, err := css.Parse(`div.card, #main, #other.notice { color: red }`)
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	rule := stylesheet.Rules[0]
	element := dom.NewElement("div",
		dom.Attribute{Name: "id", Value: "main"},
		dom.Attribute{Name: "class", Value: "card"},
	)

	got, matched := rule.Match(element)
	if !matched {
		t.Fatal("Match() matched = false, want true")
	}
	want := css.Specificity{IDs: 1}
	if got != want {
		t.Errorf("Match() specificity = %#v, want %#v", got, want)
	}

	if got, matched := rule.Match(dom.NewElement("section")); matched || got != (css.Specificity{}) {
		t.Errorf("non-match = (%#v, %t), want (zero, false)", got, matched)
	}

	compoundSheet, err := css.Parse(`div#main.card { color: blue }`)
	if err != nil {
		t.Fatalf("Parse(compound) error = %v", err)
	}
	compoundRule := compoundSheet.Rules[0]
	got, matched = compoundRule.Match(element)
	if want := (css.Specificity{IDs: 1, Classes: 1, Types: 1}); !matched || got != want {
		t.Errorf("compound Match() = (%#v, %t), want (%#v, true)", got, matched, want)
	}
}

func TestSelectorMatchesHTMLParserElementWithNonASCIICase(t *testing.T) {
	t.Parallel()

	document, err := htmlparser.Parse(strings.NewReader(`<x-É id=target></x-É>`))
	if err != nil {
		t.Fatalf("html.Parse() error = %v", err)
	}
	element := findElementByID(document, "target")
	if element == nil {
		t.Fatal("parsed element not found")
	}
	if !parseOneSelector(t, `x-É`).Matches(element) {
		t.Error(`x-É did not match the identically cased parsed element`)
	}
	if parseOneSelector(t, `x-é`).Matches(element) {
		t.Error(`x-é matched x-É; only ASCII case should fold`)
	}
}

func findElementByID(node *dom.Node, id string) *dom.Node {
	if node == nil {
		return nil
	}
	if node.Type == dom.ElementNode {
		for _, attribute := range node.Attributes {
			if attribute.Name == "id" && attribute.Value == id {
				return node
			}
		}
	}
	for _, child := range node.Children {
		if match := findElementByID(child, id); match != nil {
			return match
		}
	}
	return nil
}

func parseOneSelector(t *testing.T, source string) css.Selector {
	t.Helper()
	stylesheet, err := css.Parse(source + " { test-property: value }")
	if err != nil {
		t.Fatalf("Parse(%q) error = %v", source, err)
	}
	if len(stylesheet.Rules) != 1 || len(stylesheet.Rules[0].Selectors) != 1 {
		t.Fatalf("Parse(%q) produced %#v, want one selector", source, stylesheet)
	}
	return stylesheet.Rules[0].Selectors[0]
}
