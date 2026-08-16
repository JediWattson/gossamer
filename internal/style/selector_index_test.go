package style

import (
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/JediWattson/gossamer/internal/css"
	"github.com/JediWattson/gossamer/internal/dom"
)

func TestSelectorRuleIndexReturnsSortedUniqueConservativeCandidates(t *testing.T) {
	t.Parallel()

	stylesheet := mustParseSelectorIndexStylesheet(t, `
		* { color: black }
		div { color: red }
		.foo { color: green }
		#target { color: blue }
		:is(.logical, #logical) { color: teal }
		section.foo, #other { color: navy }
		.foo.foo { color: olive }
		SECTION { color: purple }
	`)
	index := buildSelectorRuleIndex(stylesheet, 11)
	node := dom.NewElement("SeCtIoN",
		dom.Attribute{Name: "id", Value: "target"},
		dom.Attribute{Name: "class", Value: "foo\tsecond\fthird"},
	)
	stylesheet = stylesheet.WithSelectorIndex()
	if got, want := index.candidates(stylesheet, node, false, nil), []int{0, 2, 3, 4, 5, 6, 7}; !slices.Equal(got, want) {
		t.Fatalf("indexed candidates = %v, want %v", got, want)
	}
	if got, want := index.candidates(stylesheet, node, true, nil), []int{0, 1, 2, 3, 4, 5, 6, 7}; !slices.Equal(got, want) {
		t.Fatalf("full-scan candidates = %v, want %v", got, want)
	}
	for ruleIndex, want := range []int{11, 12, 13, 14, 15, 16, 17, 18} {
		if got := index.sourceOrder[ruleIndex]; got != want {
			t.Fatalf("rule %d source-order base = %d, want %d", ruleIndex, got, want)
		}
	}
}

func TestSelectorRuleIndexPreservesCascadeAndProvenance(t *testing.T) {
	t.Parallel()

	documentRoot, styleOwner := selectorIndexDocument()
	document, err := dom.IndexDocument(documentRoot)
	if err != nil {
		t.Fatal(err)
	}
	author := mustParseSelectorIndexStylesheet(t, `
		@layer low, high;
		* { color:#010101; margin-left:1px }
		article { color:#020202 }
		.unused { color:#030303 }
		@layer low { .card { color:#040404; --tone:#111111 } }
		@layer high { body > img#target.card[data-state=open] { color:var(--tone); width:revert-layer } }
		:is(.card, #other) { margin-left:7px }
		#target::before { content:"indexed"; color:inherit }
		@media (min-width: 300px) { img { height:22px } }
		@supports (display:grid) { [data-state] { opacity:.75 } }
	`)
	user := mustParseSelectorIndexStylesheet(t, `
		#target { color:#121212!important }
		.missing { width:99px }
	`)
	userAgent := mustParseSelectorIndexStylesheet(t, `
		img { width:auto }
		* { visibility:visible }
	`)
	base := Input{
		Environment:          Environment{Width: 640, Height: 480, MediaType: "screen", InitialFontSize: 16},
		Stylesheets:          map[*dom.Node]css.Stylesheet{styleOwner: author},
		UserStylesheets:      []css.Stylesheet{user},
		UserAgentStylesheets: []css.Stylesheet{userAgent},
	}
	indexed := computeSelectorIndexSnapshot(t, document, base)
	fullInput := base
	fullInput.disableRuleIndex = true
	full := computeSelectorIndexSnapshot(t, document, fullInput)
	if got, want := indexed.DumpExplanations(), full.DumpExplanations(); got != want {
		t.Fatalf("indexed cascade differs from full scan\n--- indexed ---\n%s\n--- full ---\n%s", got, want)
	}
}

func TestSelectorRuleIndexKeepsSourceOrderStableAcrossDifferentSubjects(t *testing.T) {
	t.Parallel()

	stylesheet := mustParseSelectorIndexStylesheet(t, `
		.only-first { color:not-a-color }
		.only-second { color:#010101 }
		* { background-color:#020202 }
	`)
	context := buildOriginStyleContext([]stylesheetSource{{stylesheet: stylesheet, kind: SourceAuthorStylesheet}}, css.MediaEnvironment{})
	if got := context.declarationCount; got != 3 {
		t.Fatalf("declaration count = %d, want 3", got)
	}
	for _, ruleIndex := range context.sheets[0].ruleIndex.candidates(context.sheets[0].stylesheet, dom.NewElement("div", dom.Attribute{Name: "class", Value: "only-second"}), false, nil) {
		if ruleIndex == 1 && context.sheets[0].ruleIndex.sourceOrder[ruleIndex] != 1 {
			t.Fatalf("second rule source order = %d, want 1", context.sheets[0].ruleIndex.sourceOrder[ruleIndex])
		}
	}
}

func FuzzSelectorRuleIndexMatchesFullCascade(fuzz *testing.F) {
	for _, source := range []string{
		`.match { color:red } #target { width:10px }`,
		`:is(.match,#other), [data-state=open] { --x:7px; margin-left:var(--x) }`,
		`section > .match::before { content:"x" } :has(> .child) { opacity:.5 }`,
		`@layer a,b; @layer a { * { color:blue } } @layer b { .match { color:revert-layer } }`,
	} {
		fuzz.Add([]byte(source))
	}
	fuzz.Fuzz(func(t *testing.T, raw []byte) {
		if len(raw) > 2048 {
			raw = raw[:2048]
		}
		stylesheet, _ := css.Parse(string(raw))
		root, owner := selectorIndexDocument()
		document, err := dom.IndexDocument(root)
		if err != nil {
			t.Fatal(err)
		}
		base := Input{
			Environment: Environment{Width: 640, Height: 480, MediaType: "screen", InitialFontSize: 16},
			Stylesheets: map[*dom.Node]css.Stylesheet{owner: stylesheet},
		}
		indexed := computeSelectorIndexSnapshot(t, document, base)
		base.disableRuleIndex = true
		full := computeSelectorIndexSnapshot(t, document, base)
		if got, want := indexed.DumpExplanations(), full.DumpExplanations(); got != want {
			t.Fatalf("indexed cascade differs from full scan for %q\n--- indexed ---\n%s\n--- full ---\n%s", string(raw), got, want)
		}
	})
}

var selectorIndexBenchmarkSnapshot *Snapshot

func BenchmarkSelectorRuleIndex(b *testing.B) {
	var source strings.Builder
	for index := 0; index < 800; index++ {
		fmt.Fprintf(&source, ".rule-%d { color:rgb(%d 0 0); margin-left:%dpx }\n", index, index%256, index%31)
	}
	stylesheet, err := css.Parse(source.String())
	if err != nil {
		b.Fatal(err)
	}
	root := dom.NewDocument()
	html := dom.NewElement("html")
	head := dom.NewElement("head")
	owner := dom.NewElement("style")
	head.AppendChild(owner)
	body := dom.NewElement("body")
	for index := 0; index < 400; index++ {
		body.AppendChild(dom.NewElement("div", dom.Attribute{Name: "class", Value: fmt.Sprintf("rule-%d", index*2)}))
	}
	html.AppendChild(head)
	html.AppendChild(body)
	root.AppendChild(html)
	base := Input{
		Environment: Environment{Width: 1024, Height: 768, MediaType: "screen", InitialFontSize: 16},
		Stylesheets: map[*dom.Node]css.Stylesheet{owner: stylesheet},
	}
	for _, benchmark := range []struct {
		name     string
		disabled bool
	}{{name: "indexed"}, {name: "full-scan", disabled: true}} {
		b.Run(benchmark.name, func(b *testing.B) {
			input := base
			input.disableRuleIndex = benchmark.disabled
			b.ReportAllocs()
			b.ResetTimer()
			for iteration := 0; iteration < b.N; iteration++ {
				selectorIndexBenchmarkSnapshot = Compute(root, input)
			}
		})
	}
}

func computeSelectorIndexSnapshot(t *testing.T, document *dom.Document, input Input) *Snapshot {
	t.Helper()
	var snapshot *Snapshot
	if err := document.WithReadView(func(view dom.ReadView) error {
		var computeErr error
		snapshot, computeErr = ComputeReadView(view, input)
		return computeErr
	}); err != nil {
		t.Fatal(err)
	}
	return snapshot
}

func selectorIndexDocument() (*dom.Node, *dom.Node) {
	document := dom.NewDocument()
	html := dom.NewElement("html")
	head := dom.NewElement("head")
	owner := dom.NewElement("style")
	head.AppendChild(owner)
	body := dom.NewElement("body")
	target := dom.NewElement("img",
		dom.Attribute{Name: "id", Value: "target"},
		dom.Attribute{Name: "class", Value: "card active"},
		dom.Attribute{Name: "data-state", Value: "open"},
		dom.Attribute{Name: "width", Value: "40"},
		dom.Attribute{Name: "style", Value: "--tone:#343434;margin-left:9px"},
	)
	body.AppendChild(target)
	body.AppendChild(dom.NewElement("section", dom.Attribute{Name: "class", Value: "other"}))
	html.AppendChild(head)
	html.AppendChild(body)
	document.AppendChild(html)
	return document, owner
}

func mustParseSelectorIndexStylesheet(t *testing.T, source string) css.Stylesheet {
	t.Helper()
	stylesheet, err := css.Parse(source)
	if err != nil {
		t.Fatal(err)
	}
	return stylesheet
}
