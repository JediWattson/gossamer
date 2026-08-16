package style_test

import (
	"image/color"
	"testing"

	"github.com/JediWattson/gossamer/internal/css"
	"github.com/JediWattson/gossamer/internal/dom"
	"github.com/JediWattson/gossamer/internal/style"
)

func TestPseudoStyleCascadeInheritsAndComputesGeneratedContent(t *testing.T) {
	t.Parallel()

	document, target := pseudoStyleDocument(`
		#target { color:#0000ff; font-size:20px; --label:"world" }
		#target::before {
			content:"hello " var(--label) " " attr(data-suffix);
			display:block;
			color:#ff0000;
		}
	`)
	snapshot := style.Compute(document, style.Input{Environment: style.Environment{Width: 320, Height: 200, InitialFontSize: 16}})

	origin, ok := snapshot.Lookup(target)
	if !ok {
		t.Fatal("origin style missing")
	}
	if value, _ := style.ComputedPropertyValue(origin, "content"); value != "normal" {
		t.Fatalf("origin content = %q, want normal", value)
	}
	before, ok := snapshot.LookupPseudo(target, css.PseudoElementBefore)
	if !ok {
		t.Fatal("::before style missing")
	}
	if before.Display() != style.DisplayBlock || before.FontSize() != 20 {
		t.Fatalf("::before display/font = %v/%v", before.Display(), before.FontSize())
	}
	if before.Color() != (color.NRGBA{R: 0xff, A: 0xff}) {
		t.Fatalf("::before color = %#v", before.Color())
	}
	if value, _ := style.ComputedPropertyValue(before, "content"); value != `"hello " "world" " " attr(data-suffix)` {
		t.Fatalf("::before content = %q", value)
	}
	if text, generated := before.Content().GeneratedText(target); !generated || text != "hello world !" {
		t.Fatalf("GeneratedText() = %q, %t", text, generated)
	}
	if custom, ok := before.CustomProperties().Value("--label"); !ok || custom != `"world"` {
		t.Fatalf("inherited custom property = %q, %t", custom, ok)
	}

	after, ok := snapshot.LookupPseudo(target, css.PseudoElementAfter)
	if !ok {
		t.Fatal("synthesized ::after style missing")
	}
	if value, _ := style.ComputedPropertyValue(after, "content"); value != "none" {
		t.Fatalf("default ::after content = %q, want none", value)
	}
	if after.Color() != origin.Color() {
		t.Fatalf("default ::after did not inherit color: %#v", after.Color())
	}
}

func TestPseudoStyleUsesCascadeOriginsAndInvalidAtComputedValueRules(t *testing.T) {
	t.Parallel()

	document, target := pseudoStyleDocument(`
		#target::before { content:"author"; color:#ff0000 !important }
		#target::after { content:"lower"; content:var(--missing) }
	`)
	user, err := css.Parse(`#target::before { color:#008000 !important }`)
	if err != nil {
		t.Fatal(err)
	}
	snapshot := style.Compute(document, style.Input{
		Environment:     style.Environment{Width: 320, Height: 200, InitialFontSize: 16},
		UserStylesheets: []css.Stylesheet{user},
	})
	before, _ := snapshot.LookupPseudo(target, css.PseudoElementBefore)
	if before.Color() != (color.NRGBA{G: 0x80, A: 0xff}) {
		t.Fatalf("user-important pseudo color = %#v", before.Color())
	}
	after, _ := snapshot.LookupPseudo(target, css.PseudoElementAfter)
	if value, _ := style.ComputedPropertyValue(after, "content"); value != "none" {
		t.Fatalf("invalid-at-computed content = %q, want none without loser revival", value)
	}
}

func TestStablePseudoStyleSnapshotDoesNotRetainDOMPointers(t *testing.T) {
	t.Parallel()

	root, target := pseudoStyleDocument(`#target::before { content:"stable" }`)
	document, err := dom.IndexDocument(root)
	if err != nil {
		t.Fatal(err)
	}
	targetID, ok := document.ID(target)
	if !ok {
		t.Fatal("target ID missing")
	}
	var snapshot *style.Snapshot
	err = document.WithReadView(func(view dom.ReadView) error {
		var computeErr error
		snapshot, computeErr = style.ComputeReadView(view, style.Input{Environment: style.Environment{Width: 320, Height: 200, InitialFontSize: 16}})
		return computeErr
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := snapshot.LookupPseudo(target, css.PseudoElementBefore); ok {
		t.Fatal("stable snapshot retained pointer pseudo lookup")
	}
	before, ok := snapshot.LookupPseudoID(targetID, css.PseudoElementBefore)
	if !ok {
		t.Fatal("stable pseudo lookup missing")
	}
	if value, _ := style.ComputedPropertyValue(before, "content"); value != `"stable"` {
		t.Fatalf("stable pseudo content = %q", value)
	}
}

func TestContentGrammarRejectsUnsupportedGeneratedContent(t *testing.T) {
	t.Parallel()

	for _, source := range []string{
		`content:"ok" attr(data-value)`,
		`content:none`,
		`content:normal`,
	} {
		if !style.SupportsDeclaration(mustPseudoDeclaration(t, source)) {
			t.Errorf("SupportsDeclaration(%q) = false", source)
		}
	}
	for _, source := range []string{
		`content:open-quote`,
		`content:url(icon.png)`,
		`content:counter(item)`,
		`content:attr(data-value string)`,
	} {
		if style.SupportsDeclaration(mustPseudoDeclaration(t, source)) {
			t.Errorf("SupportsDeclaration(%q) = true", source)
		}
	}
	if style.SupportsDeclaration(css.Declaration{Property: "content", Value: `"unterminated`}) {
		t.Error("SupportsDeclaration accepted an unterminated string")
	}
}

func pseudoStyleDocument(source string) (*dom.Node, *dom.Node) {
	document := dom.NewDocument()
	html := dom.NewElement("html")
	head := dom.NewElement("head")
	styleElement := dom.NewElement("style")
	styleElement.AppendChild(dom.NewText(source))
	head.AppendChild(styleElement)
	body := dom.NewElement("body")
	target := dom.NewElement("div",
		dom.Attribute{Name: "id", Value: "target"},
		dom.Attribute{Name: "data-suffix", Value: "!"},
	)
	target.AppendChild(dom.NewText("body"))
	body.AppendChild(target)
	html.AppendChild(head)
	html.AppendChild(body)
	document.AppendChild(html)
	return document, target
}

func mustPseudoDeclaration(t *testing.T, source string) css.Declaration {
	t.Helper()
	declarations, err := css.ParseRawDeclarationList(source)
	if err != nil || len(declarations) != 1 {
		t.Fatalf("ParseRawDeclarationList(%q) = %#v, %v", source, declarations, err)
	}
	return declarations[0]
}
