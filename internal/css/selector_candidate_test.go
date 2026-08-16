package css

import (
	"testing"

	"github.com/JediWattson/gossamer/internal/dom"
)

func TestSelectorCandidateKeyUsesRequiredRightmostCompound(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		selector string
		want     SelectorCandidateKey
	}{
		{name: "id beats class and type", selector: `main.card#target`, want: SelectorCandidateKey{Kind: SelectorCandidateID, Value: "target"}},
		{name: "class beats type", selector: `article.card`, want: SelectorCandidateKey{Kind: SelectorCandidateClass, Value: "card"}},
		{name: "rightmost compound", selector: `.ancestor > SECTION`, want: SelectorCandidateKey{Kind: SelectorCandidateType, Value: "section"}},
		{name: "escaped class", selector: `.\63 ard`, want: SelectorCandidateKey{Kind: SelectorCandidateClass, Value: "card"}},
		{name: "pseudo element keeps originating subject", selector: `p.note::before`, want: SelectorCandidateKey{Kind: SelectorCandidateClass, Value: "note"}},
		{name: "attribute is conservative universal", selector: `[data-state=open]`, want: SelectorCandidateKey{Kind: SelectorCandidateUniversal}},
		{name: "logical branches are conservative universal", selector: `:is(.one, #two)`, want: SelectorCandidateKey{Kind: SelectorCandidateUniversal}},
		{name: "structural pseudo is conservative universal", selector: `:first-child`, want: SelectorCandidateKey{Kind: SelectorCandidateUniversal}},
		{name: "universal", selector: `*`, want: SelectorCandidateKey{Kind: SelectorCandidateUniversal}},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			selectors, err := ParseSelectorList(test.selector)
			if err != nil {
				t.Fatal(err)
			}
			if len(selectors) != 1 {
				t.Fatalf("ParseSelectorList(%q) returned %d selectors", test.selector, len(selectors))
			}
			if got := selectors[0].CandidateKey(); got != test.want {
				t.Fatalf("CandidateKey(%q) = %#v, want %#v", test.selector, got, test.want)
			}
		})
	}
}

func TestSelectorCandidateKeyKeepsCaseSensitiveIdentityValues(t *testing.T) {
	t.Parallel()

	for _, source := range []string{"#Target", ".Card"} {
		selectors, err := ParseSelectorList(source)
		if err != nil {
			t.Fatal(err)
		}
		if got := selectors[0].CandidateKey().Value; got != source[1:] {
			t.Fatalf("CandidateKey(%q).Value = %q, want %q", source, got, source[1:])
		}
	}
}

func TestStylesheetSelectorIndexRebuildsAfterRuleComposition(t *testing.T) {
	stylesheet, err := Parse(`.old { color:red }`)
	if err != nil {
		t.Fatal(err)
	}
	newSelectors, err := ParseSelectorList(`.new`)
	if err != nil {
		t.Fatal(err)
	}
	stylesheet.Rules[0].Selectors = newSelectors
	stylesheet = stylesheet.RebuildSelectorIndex()

	oldNode := dom.NewElement("div", dom.Attribute{Name: "class", Value: "old"})
	if got := stylesheet.CandidateRuleIndexes(oldNode, nil); len(got) != 0 {
		t.Fatalf("old selector candidates after rebuild = %v, want none", got)
	}
	newNode := dom.NewElement("div", dom.Attribute{Name: "class", Value: "new"})
	if got := stylesheet.CandidateRuleIndexes(newNode, nil); len(got) != 1 || got[0] != 0 {
		t.Fatalf("new selector candidates after rebuild = %v, want [0]", got)
	}
}
