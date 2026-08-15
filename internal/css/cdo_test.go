package css_test

import (
	"reflect"
	"testing"

	"github.com/JediWattson/gossamer/internal/css"
	"github.com/JediWattson/gossamer/internal/dom"
)

func TestParseIgnoresTopLevelCDOAndCDC(t *testing.T) {
	t.Parallel()

	stylesheet, err := css.Parse(`
<!--
html, body { text-align: left; text-decoration: none; color: #222; }
-->
.next { display: block; }
<!-- -->
p { color: blue; }
-->
`)
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	if got, want := len(stylesheet.Rules), 3; got != want {
		t.Fatalf("len(Rules) = %d, want %d: %#v", got, want, stylesheet.Rules)
	}
	for index, rule := range stylesheet.Rules {
		if rule.Order != index {
			t.Errorf("Rules[%d].Order = %d, want %d", index, rule.Order, index)
		}
	}

	gnuRule := stylesheet.Rules[0]
	if got, want := len(gnuRule.Selectors), 2; got != want {
		t.Fatalf("GNU selector count = %d, want %d", got, want)
	}
	if !gnuRule.Selectors[0].Matches(dom.NewElement("html")) {
		t.Error("first GNU selector did not match html")
	}
	if !gnuRule.Selectors[1].Matches(dom.NewElement("body")) {
		t.Error("second GNU selector did not match body")
	}
	wantDeclarations := []css.Declaration{
		{Property: "text-align", Value: "left"},
		{Property: "text-decoration", Value: "none"},
		{Property: "color", Value: "#222"},
	}
	if got := gnuRule.Declarations; !reflect.DeepEqual(got, wantDeclarations) {
		t.Errorf("GNU declarations = %#v, want %#v", got, wantDeclarations)
	}
	if !stylesheet.Rules[1].Selectors[0].Matches(dom.NewElement("div", dom.Attribute{Name: "class", Value: "next"})) {
		t.Error("rule following CDC did not match .next")
	}
	if !stylesheet.Rules[2].Selectors[0].Matches(dom.NewElement("p")) {
		t.Error("rule following adjacent CDO/CDC did not match p")
	}
}

func TestParsePreservesCDOAndCDCSequencesInsideDeclarationValues(t *testing.T) {
	t.Parallel()

	stylesheet, err := css.Parse(`p {
	content: "keep <!-- and --> in the string";
	open-token: <!--;
	close-token: -->;
	paired-token: before<!--middle-->after;
	--open-custom: <!--;
	--paired-custom: before<!--middle-->after;
}`)
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	if got, want := len(stylesheet.Rules), 1; got != want {
		t.Fatalf("len(Rules) = %d, want %d", got, want)
	}
	want := []css.Declaration{
		{Property: "content", Value: `"keep <!-- and --> in the string"`},
		{Property: "open-token", Value: "<!--"},
		{Property: "close-token", Value: "-->"},
		{Property: "paired-token", Value: "before<!--middle-->after"},
		{Property: "--open-custom", Value: "<!--"},
		{Property: "--paired-custom", Value: "before<!--middle-->after"},
	}
	if got := stylesheet.Rules[0].Declarations; !reflect.DeepEqual(got, want) {
		t.Errorf("Declarations = %#v, want %#v", got, want)
	}
}

func TestParseCDOAndCDCDoNotChangeMalformedRuleRecovery(t *testing.T) {
	t.Parallel()

	stylesheet, err := css.Parse(`
<!--
> broken { ignored: true; }
-->
div { color red; good: yes; }
<!-- -->
p { broken; color: blue; }
`)
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	if got, want := len(stylesheet.Rules), 2; got != want {
		t.Fatalf("len(recovered Rules) = %d, want %d: %#v", got, want, stylesheet.Rules)
	}
	for index, rule := range stylesheet.Rules {
		if rule.Order != index {
			t.Errorf("Rules[%d].Order = %d, want %d", index, rule.Order, index)
		}
	}
	if !stylesheet.Rules[0].Selectors[0].Matches(dom.NewElement("div")) {
		t.Error("recovered first selector did not match div")
	}
	if got, want := stylesheet.Rules[0].Declarations, []css.Declaration{{Property: "good", Value: "yes"}}; !reflect.DeepEqual(got, want) {
		t.Errorf("recovered div declarations = %#v, want %#v", got, want)
	}
	if !stylesheet.Rules[1].Selectors[0].Matches(dom.NewElement("p")) {
		t.Error("recovered second selector did not match p")
	}
	if got, want := stylesheet.Rules[1].Declarations, []css.Declaration{{Property: "color", Value: "blue"}}; !reflect.DeepEqual(got, want) {
		t.Errorf("recovered p declarations = %#v, want %#v", got, want)
	}
}
