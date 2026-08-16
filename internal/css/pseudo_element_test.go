package css_test

import (
	"testing"

	"github.com/JediWattson/gossamer/internal/css"
	"github.com/JediWattson/gossamer/internal/dom"
)

func TestPseudoElementSelectorsTargetGeneratedSubjectOnly(t *testing.T) {
	t.Parallel()

	element := dom.NewElement("div", dom.Attribute{Name: "class", Value: "card"})
	for _, test := range []struct {
		source      string
		pseudo      css.PseudoElement
		specificity css.Specificity
	}{
		{source: `.card::before`, pseudo: css.PseudoElementBefore, specificity: css.Specificity{Classes: 1, Types: 1}},
		{source: `div:after`, pseudo: css.PseudoElementAfter, specificity: css.Specificity{Types: 2}},
		{source: `::before`, pseudo: css.PseudoElementBefore, specificity: css.Specificity{Types: 1}},
	} {
		selector := parseOneSelector(t, test.source)
		if selector.PseudoElement() != test.pseudo {
			t.Errorf("%s pseudo = %s, want %s", test.source, selector.PseudoElement(), test.pseudo)
		}
		if selector.Specificity() != test.specificity {
			t.Errorf("%s specificity = %#v, want %#v", test.source, selector.Specificity(), test.specificity)
		}
		if selector.Matches(element) {
			t.Errorf("%s matched the originating element", test.source)
		}
		if !selector.MatchesPseudoWithContext(element, test.pseudo, css.MatchContext{}) {
			t.Errorf("%s did not match its generated subject", test.source)
		}
	}
}

func TestPseudoElementRuleListsKeepElementAndPseudoSubjectsSeparate(t *testing.T) {
	t.Parallel()

	stylesheet, err := css.Parse(`div, span::before, #target::before { color:red }`)
	if err != nil || len(stylesheet.Rules) != 1 {
		t.Fatalf("Parse() = %#v, %v", stylesheet, err)
	}
	rule := stylesheet.Rules[0]
	div := dom.NewElement("div")
	span := dom.NewElement("span", dom.Attribute{Name: "id", Value: "target"})
	if got, ok := rule.Match(div); !ok || got != (css.Specificity{Types: 1}) {
		t.Fatalf("element match = %#v, %t", got, ok)
	}
	if _, ok := rule.Match(span); ok {
		t.Fatal("pseudo selectors matched the originating span")
	}
	if got, ok := rule.MatchPseudoWithContext(span, css.PseudoElementBefore, css.MatchContext{}); !ok || got != (css.Specificity{IDs: 1, Types: 1}) {
		t.Fatalf("pseudo match = %#v, %t", got, ok)
	}
	if _, ok := rule.MatchPseudoWithContext(span, css.PseudoElementAfter, css.MatchContext{}); ok {
		t.Fatal("::before rule matched ::after")
	}
}

func TestPseudoElementsAreRejectedInsideLogicalRelativeAndNonfinalPositions(t *testing.T) {
	t.Parallel()

	for _, source := range []string{
		`:not(div::before)`,
		`:nth-child(1 of div::before)`,
		`div:has(::before)`,
		`div::before:hover`,
		`div::before span`,
		`div::before::after`,
		`div::marker`,
	} {
		if _, err := css.ParseSelectorList(source); err == nil {
			t.Errorf("ParseSelectorList(%q) succeeded", source)
		}
	}
	forgiving := parseOneSelector(t, `:is(::before)`)
	if forgiving.Matches(dom.NewElement("div")) {
		t.Fatal("forgiving :is() retained an unsupported pseudo-element branch")
	}
}

func TestNestedPseudoElementSuffixTargetsParentSelector(t *testing.T) {
	t.Parallel()

	stylesheet, err := css.Parse(`.card { &::before { color:red } }`)
	if err != nil || len(stylesheet.Rules) != 1 {
		t.Fatalf("Parse() = %#v, %v", stylesheet, err)
	}
	element := dom.NewElement("div", dom.Attribute{Name: "class", Value: "card"})
	if _, ok := stylesheet.Rules[0].MatchPseudoWithContext(element, css.PseudoElementBefore, css.MatchContext{}); !ok {
		t.Fatal("nested &::before selector did not target parent pseudo-element")
	}
}
