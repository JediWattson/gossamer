package style_test

import (
	"testing"

	"github.com/JediWattson/gossamer/internal/css"
	"github.com/JediWattson/gossamer/internal/dom"
	"github.com/JediWattson/gossamer/internal/style"
)

func TestCachedInlineDeclarationsAreCascadeAndProvenanceEquivalent(t *testing.T) {
	t.Parallel()

	source := `--tone:#123456;color:red;color:var(--tone)!important;margin:1px 2px;vertical-align:super`
	document, target := inlineDeclarationCacheDocument(source)
	environment := style.Environment{Width: 320, Height: 200, InitialFontSize: 16}
	fallback := style.Compute(document, style.Input{Environment: environment})
	declarations, err := css.ParseRawDeclarationListWithSources(source)
	if err != nil {
		t.Fatal(err)
	}
	cached := style.Compute(document, style.Input{
		Environment: environment,
		InlineDeclarations: map[*dom.Node][]css.SourcedDeclaration{
			target: declarations,
		},
	})
	if got, want := cached.Dump(target), fallback.Dump(target); got != want {
		t.Fatalf("cached inline cascade differs from parser fallback\n--- cached ---\n%s\n--- fallback ---\n%s", got, want)
	}
}

func TestPresentEmptyInlineDeclarationCacheEntryIsAuthoritative(t *testing.T) {
	t.Parallel()

	document, target := inlineDeclarationCacheDocument("color:#ff0000")
	snapshot := style.Compute(document, style.Input{
		Environment: style.Environment{Width: 320, Height: 200, InitialFontSize: 16},
		InlineDeclarations: map[*dom.Node][]css.SourcedDeclaration{
			target: {},
		},
	})
	computed, ok := snapshot.Lookup(target)
	if !ok {
		t.Fatal("cached inline target missing")
	}
	if got, _ := style.ComputedPropertyValue(computed, "color"); got != "rgb(0, 0, 0)" {
		t.Fatalf("authoritative empty cached declaration color = %q, want initial black", got)
	}
}

func FuzzCachedInlineDeclarationsMatchFallback(f *testing.F) {
	for _, source := range []string{
		"color:red;color:blue",
		"--x:10px;width:var(--x);margin:1px 2px!important",
		`font:italic 20px/1.5 monospace;vertical-align:calc(10% + 2px)`,
		"broken:);color:#123456;opacity:.5",
	} {
		f.Add([]byte(source))
	}
	f.Fuzz(func(t *testing.T, raw []byte) {
		if len(raw) > 512 {
			raw = raw[:512]
		}
		source := string(raw)
		document, target := inlineDeclarationCacheDocument(source)
		environment := style.Environment{Width: 320, Height: 200, InitialFontSize: 16}
		fallback := style.Compute(document, style.Input{Environment: environment})
		declarations, _ := css.ParseRawDeclarationListWithSources(source)
		cached := style.Compute(document, style.Input{
			Environment: environment,
			InlineDeclarations: map[*dom.Node][]css.SourcedDeclaration{
				target: declarations,
			},
		})
		if got, want := cached.Dump(target), fallback.Dump(target); got != want {
			t.Fatalf("cached inline declarations diverged for %q\n--- cached ---\n%s\n--- fallback ---\n%s", source, got, want)
		}
	})
}

func inlineDeclarationCacheDocument(source string) (*dom.Node, *dom.Node) {
	document := dom.NewDocument()
	html := dom.NewElement("html")
	body := dom.NewElement("body")
	target := dom.NewElement("span", dom.Attribute{Name: "style", Value: source})
	target.AppendChild(dom.NewText("target"))
	body.AppendChild(target)
	html.AppendChild(dom.NewElement("head"))
	html.AppendChild(body)
	document.AppendChild(html)
	return document, target
}
