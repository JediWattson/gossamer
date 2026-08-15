package css_test

import (
	"strings"
	"testing"

	"github.com/JediWattson/gossamer/internal/css"
	"github.com/JediWattson/gossamer/internal/dom"
	htmlparser "github.com/JediWattson/gossamer/internal/html"
)

func TestComplexSelectorCombinatorsBacktrack(t *testing.T) {
	t.Parallel()

	direct := complexElement("div", "", "direct")
	outerCandidate := complexElement("section", "", "candidate")
	innerCandidate := complexElement("div", "", "candidate")
	target := complexElement("span", "", "target")
	direct.AppendChild(outerCandidate)
	outerCandidate.AppendChild(innerCandidate)
	innerCandidate.AppendChild(target)

	// The nearest .candidate is not a child of .direct. A descendant match must
	// backtrack to outerCandidate before evaluating the child combinator.
	selector := parseComplexSelector(t, ".direct\n\t> .candidate\t .target")
	if !selector.Matches(target) {
		t.Error("descendant matcher did not backtrack to the matching ancestor")
	}
	if parseComplexSelector(t, ".direct > .candidate > .target").Matches(target) {
		t.Error("child combinator matched across an extra element generation")
	}

	siblings := dom.NewElement("div")
	good := complexElement("div", "", "good")
	firstCandidate := complexElement("div", "", "candidate")
	nearCandidate := complexElement("div", "", "candidate")
	siblingTarget := complexElement("div", "", "target")
	siblings.AppendChild(good)
	siblings.AppendChild(dom.NewText("whitespace does not count as an element sibling"))
	siblings.AppendChild(dom.NewComment("nor does a comment"))
	siblings.AppendChild(firstCandidate)
	siblings.AppendChild(dom.NewComment("between candidates"))
	siblings.AppendChild(nearCandidate)
	siblings.AppendChild(dom.NewText("before target"))
	siblings.AppendChild(siblingTarget)

	// nearCandidate is the first candidate encountered from the right, but it
	// is not adjacent to .good. The general-sibling search must continue to
	// firstCandidate, while + must ignore the intervening non-element nodes.
	selector = parseComplexSelector(t, ".good\n + .candidate ~\n .target")
	if !selector.Matches(siblingTarget) {
		t.Error("general-sibling matcher did not backtrack to the adjacent candidate")
	}

	noMatchParent := dom.NewElement("div")
	noMatchParent.AppendChild(complexElement("div", "", "other"))
	noMatchParent.AppendChild(dom.NewComment("ignored"))
	noMatchParent.AppendChild(complexElement("div", "", "candidate"))
	noMatchTarget := complexElement("div", "", "target")
	noMatchParent.AppendChild(noMatchTarget)
	if selector.Matches(noMatchTarget) {
		t.Error("sibling selector matched without a .good adjacent sibling")
	}
}

func TestComplexSelectorCombinatorWhitespace(t *testing.T) {
	t.Parallel()

	parent := dom.NewElement("div")
	child := dom.NewElement("span")
	parent.AppendChild(child)
	for _, source := range []string{
		"div>span",
		"div >span",
		"div> span",
		"div > span",
		"div\t>\nspan",
	} {
		t.Run(source, func(t *testing.T) {
			t.Parallel()
			if !parseComplexSelector(t, source).Matches(child) {
				t.Errorf("%q did not match a direct child", source)
			}
		})
	}
}

func TestComplexSelectorAttributeOperatorsAndFlags(t *testing.T) {
	t.Parallel()

	element := dom.NewElement("input",
		dom.Attribute{Name: "data-exists", Value: ""},
		dom.Attribute{Name: "data-eq", Value: "Exact"},
		dom.Attribute{Name: "data-list", Value: "alpha\tbeta gamma"},
		dom.Attribute{Name: "lang", Value: "en-US"},
		dom.Attribute{Name: "data-prefix", Value: "prefix-value"},
		dom.Attribute{Name: "data-suffix", Value: "value-suffix"},
		dom.Attribute{Name: "data-contains", Value: "before-middle-after"},
		dom.Attribute{Name: "data-case", Value: "MiXeD"},
		dom.Attribute{Name: "data-unicode", Value: "É"},
		dom.Attribute{Name: "data-escaped", Value: "ab"},
		dom.Attribute{Name: "data-quote", Value: `a"b`},
		dom.Attribute{Name: "data-lines", Value: "ab"},
		dom.Attribute{Name: "data-comment", Value: "BAR"},
	)

	tests := []struct {
		selector string
		want     bool
	}{
		{selector: "[data-exists]", want: true},
		{selector: "[DATA-EXISTS]", want: true},
		{selector: "[data-missing]", want: false},
		{selector: `[data-eq="Exact"]`, want: true},
		{selector: `[data-eq="exact"]`, want: false},
		{selector: "[data-list~=beta]", want: true},
		{selector: "[data-list~=bet]", want: false},
		{selector: "[lang|=en]", want: true},
		{selector: "[lang|=e]", want: false},
		{selector: "[data-prefix^=prefix]", want: true},
		{selector: "[data-prefix^=value]", want: false},
		{selector: "[data-suffix$=suffix]", want: true},
		{selector: "[data-suffix$=value]", want: false},
		{selector: "[data-contains*=middle]", want: true},
		{selector: "[data-contains*=missing]", want: false},
		{selector: `[data-case="mixed" i]`, want: true},
		{selector: `[data-case="mixed" s]`, want: false},
		{selector: `[data-case="MiXeD" s]`, want: true},
		{selector: `[data-unicode="é" i]`, want: false},
		{selector: `[data-escaped="a\62"]`, want: true},
		{selector: `[data-quote="a\"b"]`, want: true},
		{selector: `[data-lines="a\
b"]`, want: true},
		{selector: `[data-comment=bar/**/i]`, want: true},
		{selector: `[data-exists=""]`, want: true},
		{selector: `[data-list~=""]`, want: false},
		{selector: `[data-prefix^=""]`, want: false},
		{selector: `[data-suffix$=""]`, want: false},
		{selector: `[data-contains*=""]`, want: false},
	}

	for _, test := range tests {
		t.Run(test.selector, func(t *testing.T) {
			t.Parallel()
			if got := parseComplexSelector(t, test.selector).Matches(element); got != test.want {
				t.Errorf("Matches() = %t, want %t", got, test.want)
			}
		})
	}
}

func TestComplexSelectorHTMLAttributeCaseSemantics(t *testing.T) {
	t.Parallel()

	document, err := htmlparser.Parse(strings.NewReader(`<input id="Case" TYPE="TeXt" data-mode="TeXt" class="Hero" rel="NoFollow NEXT" disabled><ol id="list" type="A"></ol>`))
	if err != nil {
		t.Fatalf("html.Parse() error = %v", err)
	}
	element := findElementByID(document, "Case")
	if element == nil {
		t.Fatal("parsed input element not found")
	}

	tests := []struct {
		selector string
		want     bool
	}{
		{selector: "INPUT[TYPE=text]", want: true},
		{selector: "[type=text s]", want: false},
		{selector: "[type=TeXt s]", want: true},
		{selector: "[data-mode=text]", want: false},
		{selector: "[data-mode=text i]", want: true},
		{selector: "[rel~=nofollow]", want: true},
		{selector: "[rel~=next]", want: true},
		{selector: ".hero", want: false},
		{selector: "#case", want: false},
		{selector: "[disabled]", want: true},
		{selector: `[disabled=""]`, want: true},
	}
	for _, test := range tests {
		t.Run(test.selector, func(t *testing.T) {
			t.Parallel()
			if got := parseComplexSelector(t, test.selector).Matches(element); got != test.want {
				t.Errorf("Matches() = %t, want %t", got, test.want)
			}
		})
	}

	list := findElementByID(document, "list")
	if list == nil {
		t.Fatal("parsed list element not found")
	}
	if !parseComplexSelector(t, `ol[type="a"]`).Matches(list) {
		t.Error("default HTML type matching was not ASCII-case-insensitive")
	}
	if parseComplexSelector(t, `ol[type="a" s]`).Matches(list) {
		t.Error("explicit case-sensitive type selector matched a differently cased value")
	}
	if !parseComplexSelector(t, `ol[type="A" s]`).Matches(list) {
		t.Error("explicit case-sensitive type selector did not match the exact value")
	}
}

func TestComplexSelectorLogicalPseudoClasses(t *testing.T) {
	t.Parallel()

	element := complexElement("article", "", "hit x")
	tests := []struct {
		name            string
		selector        string
		wantMatch       bool
		wantSpecificity css.Specificity
	}{
		{
			name:            "is ignores an invalid branch",
			selector:        "article:is(:future, .hit)",
			wantMatch:       true,
			wantSpecificity: css.Specificity{Classes: 1, Types: 1},
		},
		{
			name:            "is uses the static greatest argument specificity",
			selector:        "article:is(.hit, #never)",
			wantMatch:       true,
			wantSpecificity: css.Specificity{IDs: 1, Types: 1},
		},
		{
			name:            "all invalid is arguments match nothing",
			selector:        "article:is(:future)",
			wantMatch:       false,
			wantSpecificity: css.Specificity{Types: 1},
		},
		{
			name:            "empty is argument is valid and matches nothing",
			selector:        "article:is()",
			wantMatch:       false,
			wantSpecificity: css.Specificity{Types: 1},
		},
		{
			name:            "forgiving lists trim only CSS whitespace",
			selector:        "article:is(.hit\u00a0)",
			wantMatch:       false,
			wantSpecificity: css.Specificity{Classes: 1, Types: 1},
		},
		{
			name:            "where ignores invalid branches and contributes zero",
			selector:        "article:where(:future, .hit, #never)",
			wantMatch:       true,
			wantSpecificity: css.Specificity{Types: 1},
		},
		{
			name:            "comment-only where argument is valid and matches nothing",
			selector:        "article:where(/**/)",
			wantMatch:       false,
			wantSpecificity: css.Specificity{Types: 1},
		},
		{
			name:            "not uses the static greatest argument specificity",
			selector:        "article:not(.miss, #never)",
			wantMatch:       true,
			wantSpecificity: css.Specificity{IDs: 1, Types: 1},
		},
		{
			name:            "not can negate a forgiving list that matches nothing",
			selector:        "article:not(:is())",
			wantMatch:       true,
			wantSpecificity: css.Specificity{Types: 1},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			selector := parseComplexSelector(t, test.selector)
			if got := selector.Matches(element); got != test.wantMatch {
				t.Errorf("Matches() = %t, want %t", got, test.wantMatch)
			}
			if got := selector.Specificity(); got != test.wantSpecificity {
				t.Errorf("Specificity() = %#v, want %#v", got, test.wantSpecificity)
			}
		})
	}

	section := dom.NewElement("section")
	section.AppendChild(element)
	if !parseComplexSelector(t, "article:is(section > article)").Matches(element) {
		t.Error(":is() did not match a complex-selector argument")
	}
	if parseComplexSelector(t, "article:not(section > article)").Matches(element) {
		t.Error(":not() did not negate a complex-selector argument")
	}

	for _, source := range []string{
		"article:not(.miss, :future) { color: red }",
		"article:nth-child(2n of .hit, :future) { color: red }",
	} {
		stylesheet, err := css.Parse(source)
		if err != nil {
			t.Fatalf("Parse(%q) error = %v", source, err)
		}
		if len(stylesheet.Rules) != 0 {
			t.Errorf("Parse(%q) produced %d rules, want 0 for an unforgiving list", source, len(stylesheet.Rules))
		}
	}
}

func TestComplexSelectorNthPseudoClasses(t *testing.T) {
	t.Parallel()

	parent := dom.NewElement("ul")
	a := complexElement("li", "a", "item")
	x := complexElement("div", "x", "item")
	b := complexElement("li", "b", "other")
	c := complexElement("li", "c", "item featured")
	y := complexElement("span", "y", "featured")
	d := complexElement("li", "d", "item")
	parent.AppendChild(dom.NewText("before"))
	parent.AppendChild(a)
	parent.AppendChild(dom.NewComment("between element siblings"))
	parent.AppendChild(x)
	parent.AppendChild(b)
	parent.AppendChild(c)
	parent.AppendChild(y)
	parent.AppendChild(d)
	parent.AppendChild(dom.NewText("after"))

	tests := []struct {
		name     string
		selector string
		node     *dom.Node
		want     bool
	}{
		{name: "odd keyword", selector: "li:nth-child(odd)", node: a, want: true},
		{name: "comments around odd keyword", selector: "li:nth-child(/**/odd/**/)", node: b, want: true},
		{name: "even keyword", selector: "li:nth-child(even)", node: c, want: true},
		{name: "odd formula", selector: "li:nth-child(2n+1)", node: b, want: true},
		{name: "comment before formula offset", selector: "li:nth-child(2n/**/+1)", node: b, want: true},
		{name: "comment after formula offset sign", selector: "li:nth-child(2n+/**/1)", node: b, want: true},
		{name: "comment after leading plus token", selector: "li:nth-child(+/**/n)", node: d, want: true},
		{name: "even formula with whitespace", selector: "li:nth-child(2n + 2)", node: c, want: true},
		{name: "negative coefficient includes prefix", selector: "li:nth-child(-n+4)", node: c, want: true},
		{name: "negative coefficient excludes suffix", selector: "li:nth-child(-n+4)", node: d, want: false},
		{name: "positive offset excludes prefix", selector: "li:nth-child(n+4)", node: b, want: false},
		{name: "positive offset includes suffix", selector: "li:nth-child(n+4)", node: c, want: true},
		{name: "zero coefficient", selector: "li:nth-child(0n+3)", node: b, want: true},
		{name: "negative offset", selector: "li:nth-child(2n-2)", node: c, want: true},
		{name: "last child", selector: "li:nth-last-child(1)", node: d, want: true},
		{name: "last child formula", selector: "li:nth-last-child(3)", node: c, want: true},
		{name: "of type", selector: "li:nth-of-type(2)", node: b, want: true},
		{name: "later of type", selector: "li:nth-of-type(3)", node: c, want: true},
		{name: "last of type", selector: "li:nth-last-of-type(1)", node: d, want: true},
		{name: "filtered child", selector: "li:nth-child(3 of .item)", node: c, want: true},
		{name: "comments around of keyword", selector: "li:nth-child(3/**/of/**/.item)", node: c, want: true},
		{name: "filtered last child", selector: "li:nth-last-child(1 of .item)", node: d, want: true},
		{name: "filtered selector list", selector: "span:nth-child(4 of .item, .featured)", node: y, want: true},
		{name: "filtered list counts nonmatching type", selector: "li:nth-child(4 of .item)", node: d, want: true},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			if got := parseComplexSelector(t, test.selector).Matches(test.node); got != test.want {
				t.Errorf("%s Matches() = %t, want %t", test.selector, got, test.want)
			}
		})
	}
}

func TestComplexSelectorSpecificityAndRepeatedSimpleSelectors(t *testing.T) {
	t.Parallel()

	element := dom.NewElement("div",
		dom.Attribute{Name: "id", Value: "x"},
		dom.Attribute{Name: "class", Value: "a"},
		dom.Attribute{Name: "data-v", Value: "present"},
	)
	repeated := parseComplexSelector(t, "#x#x.a.a[data-v][data-v]")
	if !repeated.Matches(element) {
		t.Error("repeated ID, class, and attribute selectors did not match")
	}
	if got, want := repeated.Specificity(), (css.Specificity{IDs: 2, Classes: 4}); got != want {
		t.Errorf("repeated selector Specificity() = %#v, want %#v", got, want)
	}
	if parseComplexSelector(t, "#x#different").Matches(element) {
		t.Error("two different ID selectors matched a single id attribute")
	}

	tests := []struct {
		selector string
		want     css.Specificity
	}{
		{
			selector: "main > section.card article[data-x]",
			want:     css.Specificity{Classes: 2, Types: 3},
		},
		{
			selector: "article:is(.hit, #never)",
			want:     css.Specificity{IDs: 1, Types: 1},
		},
		{
			selector: ".x:where(#never, article.hit)",
			want:     css.Specificity{Classes: 1},
		},
		{
			selector: "li:nth-child(2n of .item, #never)",
			want:     css.Specificity{IDs: 1, Classes: 1, Types: 1},
		},
	}

	for _, test := range tests {
		t.Run(test.selector, func(t *testing.T) {
			t.Parallel()
			if got := parseComplexSelector(t, test.selector).Specificity(); got != test.want {
				t.Errorf("Specificity() = %#v, want %#v", got, test.want)
			}
		})
	}
}

func TestComplexSelectorRejectsMalformedGrammar(t *testing.T) {
	t.Parallel()

	invalid := []string{
		"> div",
		"div >",
		"div + ~ span",
		"div,,span",
		"div,",
		"[data-x",
		"[data-x=]",
		"[data-x value]",
		"[data-x=value q]",
		"[data-x i]",
		"[data-x=\"raw\nnewline\"]",
		":not()",
		":not(.ok, :future)",
		":nth-child()",
		":nth-child(2n+-1)",
		":nth-child(2n + -1)",
		":nth-child(2.5n)",
		":nth-child(+/**/2)",
		":nth-child(-/**/n)",
		":nth-child(n+)",
		":nth-child(odd+1)",
		":nth-child(999999999999999999999999n)",
		":nth-child(2 of .ok, :future)",
		":nth-of-type(2 of .ok)",
		"div::before",
		"div/**/span",
		":is/**/()",
		":where/**/()",
		":nth-child/**/(2)",
		"svg|circle",
		`div.escaped\ name`,
		"-",
		"-5",
	}
	for _, selector := range invalid {
		t.Run(selector, func(t *testing.T) {
			t.Parallel()
			stylesheet, err := css.Parse(selector + " { test-property: value }")
			if err != nil {
				t.Fatalf("Parse() error = %v", err)
			}
			if len(stylesheet.Rules) != 0 {
				t.Errorf("Parse(%q) produced %d rules, want none", selector, len(stylesheet.Rules))
			}
		})
	}
}

func TestComplexSelectorRejectsPathologicalFunctionalNesting(t *testing.T) {
	t.Parallel()

	selector := strings.Repeat(":not(", 130) + "*" + strings.Repeat(")", 130)
	stylesheet, err := css.Parse(selector + " { test-property: value }")
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	if len(stylesheet.Rules) != 0 {
		t.Fatalf("Parse() produced %d rules beyond the nesting guard, want none", len(stylesheet.Rules))
	}
}

func parseComplexSelector(t *testing.T, source string) css.Selector {
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

func complexElement(name, id, classes string) *dom.Node {
	attributes := make([]dom.Attribute, 0, 2)
	if id != "" {
		attributes = append(attributes, dom.Attribute{Name: "id", Value: id})
	}
	if classes != "" {
		attributes = append(attributes, dom.Attribute{Name: "class", Value: classes})
	}
	return dom.NewElement(name, attributes...)
}
