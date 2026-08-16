package css_test

import (
	"testing"

	"github.com/JediWattson/gossamer/internal/css"
	"github.com/JediWattson/gossamer/internal/dom"
)

func TestHasMatchesRelativeSelectorsAcrossCombinators(t *testing.T) {
	t.Parallel()

	root := dom.NewElement("main")
	card := dom.NewElement("article", dom.Attribute{Name: "class", Value: "card"})
	wrapper := dom.NewElement("div", dom.Attribute{Name: "class", Value: "wrapper"})
	hero := dom.NewElement("img", dom.Attribute{Name: "class", Value: "hero"})
	caption := dom.NewElement("span", dom.Attribute{Name: "class", Value: "caption"})
	wrapper.AppendChild(hero)
	wrapper.AppendChild(caption)
	card.AppendChild(wrapper)
	empty := dom.NewElement("article", dom.Attribute{Name: "class", Value: "empty"})
	term := dom.NewElement("dt")
	note := dom.NewElement("dd", dom.Attribute{Name: "class", Value: "note"})
	later := dom.NewElement("dd", dom.Attribute{Name: "class", Value: "later"})
	root.AppendChild(card)
	root.AppendChild(empty)
	root.AppendChild(term)
	root.AppendChild(dom.NewText("ignored"))
	root.AppendChild(note)
	root.AppendChild(later)

	tests := []struct {
		selector string
		node     *dom.Node
		want     bool
	}{
		{selector: "article:has(img.hero)", node: card, want: true},
		{selector: "article:has(> img.hero)", node: card, want: false},
		{selector: "article:has(> .wrapper > img.hero)", node: card, want: true},
		{selector: "article:has(.hero + .caption)", node: card, want: true},
		{selector: "article:has(.caption + .hero)", node: card, want: false},
		{selector: "article:has(> .missing, .wrapper .hero)", node: card, want: true},
		{selector: "article:has(*)", node: empty, want: false},
		{selector: "dt:has(+ dd.note)", node: term, want: true},
		{selector: "dt:has(~ dd.later)", node: term, want: true},
		{selector: "dd.note:has(+ dd.later)", node: note, want: true},
		{selector: "dd.later:has(~ dd)", node: later, want: false},
	}
	for _, test := range tests {
		t.Run(test.selector, func(t *testing.T) {
			got := parseOneSelector(t, test.selector).Matches(test.node)
			if got != test.want {
				t.Errorf("Matches() = %t, want %t", got, test.want)
			}
		})
	}
}

func TestHasBacktracksAcrossRelativeCandidates(t *testing.T) {
	t.Parallel()

	section := dom.NewElement("section")
	firstCandidate := dom.NewElement("div", dom.Attribute{Name: "class", Value: "candidate"})
	firstCandidate.AppendChild(dom.NewElement("span"))
	secondCandidate := dom.NewElement("div", dom.Attribute{Name: "class", Value: "candidate"})
	secondCandidate.AppendChild(dom.NewElement("span", dom.Attribute{Name: "class", Value: "target"}))
	section.AppendChild(firstCandidate)
	section.AppendChild(secondCandidate)
	if !parseOneSelector(t, "section:has(.candidate .target)").Matches(section) {
		t.Fatal(":has() stopped after the first nonmatching relative candidate")
	}

	good := dom.NewElement("i", dom.Attribute{Name: "class", Value: "good"})
	badCandidate := dom.NewElement("i", dom.Attribute{Name: "class", Value: "candidate"})
	otherCandidate := dom.NewElement("i", dom.Attribute{Name: "class", Value: "candidate"})
	target := dom.NewElement("i", dom.Attribute{Name: "class", Value: "target"})
	row := dom.NewElement("div")
	row.AppendChild(good)
	row.AppendChild(badCandidate)
	row.AppendChild(otherCandidate)
	row.AppendChild(target)
	section.AppendChild(row)
	if !parseOneSelector(t, "section:has(.good + .candidate ~ .target)").Matches(section) {
		t.Fatal(":has() failed sibling backtracking across relative candidates")
	}
}

func TestHasSpecificityUsesGreatestRelativeSelector(t *testing.T) {
	t.Parallel()

	selector := parseOneSelector(t, "article:has(.hit, #winner > span)")
	want := css.Specificity{IDs: 1, Types: 2}
	if got := selector.Specificity(); got != want {
		t.Fatalf("Specificity() = %#v, want %#v", got, want)
	}
	where := parseOneSelector(t, ":where(:has(#winner))")
	if got := where.Specificity(); got != (css.Specificity{}) {
		t.Fatalf(":where(:has()) specificity = %#v, want zero", got)
	}
}

func TestHasUsesUnforgivingRelativeListAndRejectsNesting(t *testing.T) {
	t.Parallel()

	invalid := []string{
		"div:has()",
		"div:has(>)",
		"div:has(, .ok)",
		"div:has(.ok, :future)",
		"div:has(:has(.nested))",
		"div:has(:not(:has(.nested)))",
		"div:has(::before)",
	}
	for _, source := range invalid {
		if _, err := css.ParseSelectorList(source); err == nil {
			t.Errorf("ParseSelectorList(%q) succeeded, want invalid selector", source)
		}
	}

	root := dom.NewElement("div")
	root.AppendChild(dom.NewElement("span", dom.Attribute{Name: "class", Value: "ok"}))
	selector := parseOneSelector(t, "div:has(:is(.ok, :future))")
	if !selector.Matches(root) {
		t.Fatal("forgiving :is() branch inside :has() did not retain its valid selector")
	}
	selector = parseOneSelector(t, "div:has(:is(.ok, :has(.nested)))")
	if !selector.Matches(root) {
		t.Fatal("forgiving :is() did not discard a contextually invalid nested :has() branch")
	}
}

func TestSupportsSelectorUsesHasCapabilityBoundary(t *testing.T) {
	t.Parallel()

	if !css.SupportsConditionMatches(`selector(section:has(> .item))`, nil) {
		t.Fatal("@supports selector() did not advertise valid :has() support")
	}
	if css.SupportsConditionMatches(`selector(section:has(.item, :future))`, nil) {
		t.Fatal("@supports selector() accepted an invalid :has() relative list")
	}
}

func TestSelectorOperationLimitBoundsHasTraversal(t *testing.T) {
	t.Parallel()

	root := dom.NewElement("div")
	for index := 0; index < 200; index++ {
		attributes := []dom.Attribute(nil)
		if index == 199 {
			attributes = append(attributes, dom.Attribute{Name: "class", Value: "target"})
		}
		root.AppendChild(dom.NewElement("span", attributes...))
	}
	selector := parseOneSelector(t, "div:has(> .target)")
	if selector.MatchesWithContext(root, css.MatchContext{OperationLimit: 16}) {
		t.Fatal("bounded selector evaluation matched after exhausting its operation limit")
	}
	if !selector.MatchesWithContext(root, css.MatchContext{OperationLimit: 2_000}) {
		t.Fatal("selector did not match with a sufficient operation limit")
	}
	negated := parseOneSelector(t, "div:not(:has(.missing))")
	if negated.MatchesWithContext(root, css.MatchContext{OperationLimit: 16}) {
		t.Fatal("operation exhaustion became a false :has() result that :not() inverted into a match")
	}
	if !negated.MatchesWithContext(root, css.MatchContext{OperationLimit: 2_000}) {
		t.Fatal("negated relational selector did not match with a sufficient operation limit")
	}
	selectors, err := css.ParseSelectorList("div, div:not(:has(.missing))")
	if err != nil {
		t.Fatal(err)
	}
	rule := css.Rule{Selectors: selectors}
	if _, matched := rule.MatchWithContext(root, css.MatchContext{OperationLimit: 16}); matched {
		t.Fatal("rule retained an earlier selector-list match after a later branch exhausted the shared budget")
	}
}
