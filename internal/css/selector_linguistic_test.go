package css_test

import (
	"strings"
	"testing"

	"github.com/JediWattson/gossamer/internal/css"
	"github.com/JediWattson/gossamer/internal/dom"
)

func TestLinguisticPseudoClassGrammarAndSpecificity(t *testing.T) {
	t.Parallel()

	valid := []string{
		`:lang(en)`,
		`:lang(en, "fr-CA", \*-Latn)`,
		`:lang("")`,
		`:dir(ltr)`,
		`:dir( RTL )`,
		`:dir(sideways)`,
	}
	for _, source := range valid {
		selector := parseOneSelector(t, source)
		if got := selector.Specificity(); got != (css.Specificity{Classes: 1}) {
			t.Errorf("%s specificity = %#v, want one class component", source, got)
		}
	}

	invalid := []string{
		`:lang()`,
		`:lang(,)`,
		`:lang(en,)`,
		`:lang(,en)`,
		`:lang(en fr)`,
		`:lang(*)`,
		`:lang(*-Latn)`,
		`:lang(url(en))`,
		`:dir()`,
		`:dir(ltr, rtl)`,
		`:dir("ltr")`,
		`:dir(ltr rtl)`,
		`:dir(*)`,
	}
	for _, source := range invalid {
		if _, err := css.ParseSelectorList(source); err == nil {
			t.Errorf("ParseSelectorList(%q) succeeded, want invalid selector", source)
		}
	}

	if parseOneSelector(t, `:dir(sideways)`).Matches(dom.NewElement("div")) {
		t.Fatal("unknown but syntactically valid :dir() argument matched")
	}
	if !css.SupportsConditionMatches(`selector(:lang(en, "fr"))`, nil) {
		t.Fatal("@supports selector() did not advertise :lang() support")
	}
	if css.SupportsConditionMatches(`selector(:dir("rtl"))`, nil) {
		t.Fatal("@supports selector() accepted invalid :dir() grammar")
	}
}

func TestLangMatchesInheritedExtendedLanguageRanges(t *testing.T) {
	t.Parallel()

	document := dom.NewDocument()
	html := dom.NewElement("html", dom.Attribute{Name: "lang", Value: "fr-BE"})
	body := dom.NewElement("body")
	paragraph := dom.NewElement("p")
	body.AppendChild(paragraph)
	html.AppendChild(body)
	document.AppendChild(html)

	assertLanguageMatch(t, paragraph, css.MatchContext{}, `:lang(fr)`, true)
	assertLanguageMatch(t, paragraph, css.MatchContext{}, `:lang(FR-be)`, true)
	assertLanguageMatch(t, paragraph, css.MatchContext{}, `:lang(de, "fr-BE")`, true)
	assertLanguageMatch(t, paragraph, css.MatchContext{}, `:lang(de)`, false)

	body.Attributes = append(body.Attributes, dom.Attribute{Name: "lang", Value: "de-Latn-DE-1996"})
	assertLanguageMatch(t, paragraph, css.MatchContext{}, `:lang(de-DE)`, true)
	assertLanguageMatch(t, paragraph, css.MatchContext{}, `:lang("*-DE")`, true)
	assertLanguageMatch(t, paragraph, css.MatchContext{}, `:lang("de-*-1996")`, true)

	paragraph.Attributes = append(paragraph.Attributes, dom.Attribute{Name: "lang", Value: ""})
	assertLanguageMatch(t, paragraph, css.MatchContext{}, `:lang("")`, true)
	assertLanguageMatch(t, paragraph, css.MatchContext{}, `:lang("*")`, false)
	paragraph.Attributes[0].Value = "und"
	assertLanguageMatch(t, paragraph, css.MatchContext{}, `:lang("*")`, true)
	assertLanguageMatch(t, paragraph, css.MatchContext{}, `:lang("")`, false)

	paragraph.Attributes[0].Value = "xyzzy"
	assertLanguageMatch(t, paragraph, css.MatchContext{}, `:lang(xyzzy)`, true)
	assertLanguageMatch(t, paragraph, css.MatchContext{}, `:lang(abcde)`, false)
}

func TestLangCanonicalizesBCP47AndHonorsNamespaceSemantics(t *testing.T) {
	t.Parallel()

	html := dom.NewElement("html", dom.Attribute{Name: "lang", Value: "fr"})
	legacy := dom.NewElement("p", dom.Attribute{Name: "lang", Value: "iw"})
	extlang := dom.NewElement("p", dom.Attribute{Name: "lang", Value: "zh-hak-CN"})
	htmlLiteralXML := dom.NewElement("p", dom.Attribute{Name: "xml:lang", Value: "de"})
	svg := dom.NewElement("svg",
		dom.Attribute{Name: "lang", Value: "de"},
		dom.Attribute{Name: "xml:lang", Value: "es-MX"},
	)
	svg.NamespaceURI = dom.SVGNamespace
	html.AppendChild(legacy)
	html.AppendChild(extlang)
	html.AppendChild(htmlLiteralXML)
	html.AppendChild(svg)

	assertLanguageMatch(t, legacy, css.MatchContext{}, `:lang(he)`, true)
	assertLanguageMatch(t, extlang, css.MatchContext{}, `:lang(hak)`, true)
	assertLanguageMatch(t, extlang, css.MatchContext{}, `:lang(zh)`, true)
	assertLanguageMatch(t, extlang, css.MatchContext{}, `:lang("zh-*-CN")`, true)
	wrongPrefix := dom.NewElement("p", dom.Attribute{Name: "lang", Value: "ar-hak"})
	assertLanguageMatch(t, wrongPrefix, css.MatchContext{}, `:lang("ar-hak")`, true)
	assertLanguageMatch(t, wrongPrefix, css.MatchContext{}, `:lang(hak)`, false)
	assertLanguageMatch(t, htmlLiteralXML, css.MatchContext{}, `:lang(fr)`, true)
	assertLanguageMatch(t, htmlLiteralXML, css.MatchContext{}, `:lang(de)`, false)
	assertLanguageMatch(t, svg, css.MatchContext{}, `:lang(es)`, true)
	assertLanguageMatch(t, svg, css.MatchContext{}, `:lang(de)`, false)

	detached := dom.NewElement("p")
	assertLanguageMatch(t, detached, css.MatchContext{DefaultLanguage: "pt-BR"}, `:lang(pt)`, true)
	assertLanguageMatch(t, detached, css.MatchContext{}, `:lang("")`, true)
}

func TestDirFollowsHTMLInheritanceAndSpecialDefaults(t *testing.T) {
	t.Parallel()

	rtl := dom.NewElement("section", dom.Attribute{Name: "dir", Value: "RTL"})
	inherited := dom.NewElement("p")
	invalid := dom.NewElement("p", dom.Attribute{Name: "dir", Value: "sideways"})
	tel := dom.NewElement("input", dom.Attribute{Name: "type", Value: "tel"})
	foreign := dom.NewElement("g", dom.Attribute{Name: "dir", Value: "ltr"})
	foreign.NamespaceURI = dom.SVGNamespace
	rtl.AppendChild(inherited)
	rtl.AppendChild(invalid)
	rtl.AppendChild(tel)
	rtl.AppendChild(foreign)

	assertDirectionMatch(t, inherited, `:dir(rtl)`, true)
	assertDirectionMatch(t, invalid, `:dir(rtl)`, true)
	assertDirectionMatch(t, tel, `:dir(ltr)`, true)
	assertDirectionMatch(t, tel, `:dir(rtl)`, false)
	assertDirectionMatch(t, foreign, `:dir(rtl)`, true)

	ltr := dom.NewElement("div", dom.Attribute{Name: "dir", Value: "ltr"})
	rtl.AppendChild(ltr)
	assertDirectionMatch(t, ltr, `:dir(ltr)`, true)
	assertDirectionMatch(t, ltr, `:dir(rtl)`, false)
	assertDirectionMatch(t, dom.NewElement("div"), `:dir(ltr)`, true)
}

func TestDirAutoUsesFirstStrongTextAndExcludesIsolatedSubtrees(t *testing.T) {
	t.Parallel()

	rtl := dom.NewElement("div", dom.Attribute{Name: "dir", Value: "auto"})
	rtl.AppendChild(dom.NewText("123 שלום hello"))
	assertDirectionMatch(t, rtl, `:dir(rtl)`, true)

	ltr := dom.NewElement("div", dom.Attribute{Name: "dir", Value: "auto"})
	ltr.AppendChild(dom.NewText("123 hello שלום"))
	assertDirectionMatch(t, ltr, `:dir(ltr)`, true)

	excluded := dom.NewElement("div", dom.Attribute{Name: "dir", Value: "auto"})
	bdiNode := dom.NewElement("bdi")
	bdiNode.AppendChild(dom.NewText("שלום"))
	script := dom.NewElement("script")
	script.AppendChild(dom.NewText("مرحبا"))
	explicit := dom.NewElement("span", dom.Attribute{Name: "dir", Value: "rtl"})
	explicit.AppendChild(dom.NewText("עברית"))
	excluded.AppendChild(bdiNode)
	excluded.AppendChild(script)
	excluded.AppendChild(explicit)
	excluded.AppendChild(dom.NewText("english"))
	assertDirectionMatch(t, excluded, `:dir(ltr)`, true)

	includedInvalid := dom.NewElement("div", dom.Attribute{Name: "dir", Value: "auto"})
	invalid := dom.NewElement("span", dom.Attribute{Name: "dir", Value: "bogus"})
	invalid.AppendChild(dom.NewText("مرحبا"))
	includedInvalid.AppendChild(invalid)
	includedInvalid.AppendChild(dom.NewText("english"))
	assertDirectionMatch(t, includedInvalid, `:dir(rtl)`, true)

	bdiAuto := dom.NewElement("bdi")
	bdiAuto.AppendChild(dom.NewText("שלום"))
	assertDirectionMatch(t, bdiAuto, `:dir(rtl)`, true)
	assertDirectionMatch(t, dom.NewElement("bdi"), `:dir(ltr)`, true)
}

func TestDirAutoUsesLiveFormValues(t *testing.T) {
	t.Parallel()

	input := dom.NewElement("input",
		dom.Attribute{Name: "dir", Value: "auto"},
		dom.Attribute{Name: "value", Value: "hello"},
	)
	assertDirectionMatch(t, input, `:dir(ltr)`, true)
	input.Control.ValueDirty = true
	input.Control.Value = "123 שלום"
	assertDirectionMatch(t, input, `:dir(rtl)`, true)
	input.Control.Value = "12345"
	assertDirectionMatch(t, input, `:dir(ltr)`, true)

	textarea := dom.NewElement("textarea", dom.Attribute{Name: "dir", Value: "auto"})
	textarea.AppendChild(dom.NewText("مرحبا"))
	assertDirectionMatch(t, textarea, `:dir(rtl)`, true)
	textarea.Control.ValueDirty = true
	textarea.Control.Value = "hello"
	assertDirectionMatch(t, textarea, `:dir(ltr)`, true)
}

func TestLinguisticMatchingFailsClosedAtOperationLimit(t *testing.T) {
	t.Parallel()

	auto := dom.NewElement("div", dom.Attribute{Name: "dir", Value: "auto"})
	auto.AppendChild(dom.NewText(strings.Repeat("1", 200) + " שלום"))
	selector := parseOneSelector(t, `:dir(rtl)`)
	if selector.MatchesWithContext(auto, css.MatchContext{OperationLimit: 32}) {
		t.Fatal(":dir(auto) matched after exhausting the selector operation budget")
	}
	if !selector.MatchesWithContext(auto, css.MatchContext{OperationLimit: 1_000}) {
		t.Fatal(":dir(auto) did not match with a sufficient operation budget")
	}

	ancestor := dom.NewElement("div", dom.Attribute{Name: "lang", Value: "en"})
	current := ancestor
	for range 80 {
		child := dom.NewElement("div")
		current.AppendChild(child)
		current = child
	}
	langSelector := parseOneSelector(t, `:lang(en)`)
	if langSelector.MatchesWithContext(current, css.MatchContext{OperationLimit: 32}) {
		t.Fatal(":lang() matched after exhausting ancestor traversal budget")
	}
	if !langSelector.MatchesWithContext(current, css.MatchContext{OperationLimit: 1_000}) {
		t.Fatal(":lang() did not match with a sufficient operation budget")
	}
}

func assertLanguageMatch(t *testing.T, node *dom.Node, context css.MatchContext, selector string, want bool) {
	t.Helper()
	if got := parseOneSelector(t, selector).MatchesWithContext(node, context); got != want {
		t.Errorf("%s MatchesWithContext() = %t, want %t", selector, got, want)
	}
}

func assertDirectionMatch(t *testing.T, node *dom.Node, selector string, want bool) {
	t.Helper()
	if got := parseOneSelector(t, selector).Matches(node); got != want {
		t.Errorf("%s Matches() = %t, want %t", selector, got, want)
	}
}
