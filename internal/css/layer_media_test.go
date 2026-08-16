package css_test

import (
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/JediWattson/gossamer/internal/css"
	"github.com/JediWattson/gossamer/internal/dom"
)

func TestParseFlattensNamedLayersAndRetainsLayerOrder(t *testing.T) {
	t.Parallel()

	stylesheet, err := css.Parse(`
@layer reset, theme;
@layer reset {
  p { color: #111111; }
  @media screen and (min-width: 600px) {
    p { font-size: 18px; }
  }
}
p { color: #222222; }
@layer theme { p { color: #333333; } }
@layer utilities { p { line-height: 2; } }
@layer reset { p { background-color: #444444; } }
`)
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}

	if got, want := stylesheet.LayerOrder, []string{"reset", "theme", "utilities"}; !slices.Equal(got, want) {
		t.Fatalf("LayerOrder = %#v, want %#v", got, want)
	}

	want := []struct {
		layer    string
		media    []string
		property string
		value    string
	}{
		{layer: "reset", property: "color", value: "#111111"},
		{layer: "reset", media: []string{"screen and (min-width: 600px)"}, property: "font-size", value: "18px"},
		{property: "color", value: "#222222"},
		{layer: "theme", property: "color", value: "#333333"},
		{layer: "utilities", property: "line-height", value: "2"},
		{layer: "reset", property: "background-color", value: "#444444"},
	}
	if got := len(stylesheet.Rules); got != len(want) {
		t.Fatalf("len(Rules) = %d, want %d: %#v", got, len(want), stylesheet.Rules)
	}
	paragraph := dom.NewElement("p")
	for index, expected := range want {
		rule := stylesheet.Rules[index]
		if rule.Order != index {
			t.Errorf("Rules[%d].Order = %d, want %d", index, rule.Order, index)
		}
		if rule.Layer != expected.layer {
			t.Errorf("Rules[%d].Layer = %q, want %q", index, rule.Layer, expected.layer)
		}
		if !slices.Equal(rule.Media, expected.media) {
			t.Errorf("Rules[%d].Media = %#v, want %#v", index, rule.Media, expected.media)
		}
		if len(rule.Selectors) != 1 || !rule.Selectors[0].Matches(paragraph) {
			t.Errorf("Rules[%d] does not retain its p selector", index)
		}
		if got, want := rule.Declarations, []css.Declaration{{Property: expected.property, Value: expected.value}}; !reflect.DeepEqual(got, want) {
			t.Errorf("Rules[%d].Declarations = %#v, want %#v", index, got, want)
		}
	}
}

func TestParseBoundsNestedGroupRules(t *testing.T) {
	t.Parallel()

	source := strings.Repeat("@media screen {", 140) +
		"p { color: red; }" +
		strings.Repeat("}", 140) +
		"p { color: blue; }"
	stylesheet, err := css.Parse(source)
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	if got, want := len(stylesheet.Rules), 1; got != want {
		t.Fatalf("len(Rules) = %d, want %d", got, want)
	}
	if got, want := stylesheet.Rules[0].Declarations, []css.Declaration{{Property: "color", Value: "blue"}}; !reflect.DeepEqual(got, want) {
		t.Errorf("retained declarations = %#v, want %#v", got, want)
	}
}

func TestParseRetainsNestedMediaStackInsideLayer(t *testing.T) {
	t.Parallel()

	stylesheet, err := css.Parse(`
@layer responsive {
  @media screen and (min-width: 700px) {
    @media (orientation: landscape), (min-height: 900px) {
      #target { color: #abcdef; }
    }
  }
}
`)
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	if got, want := stylesheet.LayerOrder, []string{"responsive"}; !slices.Equal(got, want) {
		t.Fatalf("LayerOrder = %#v, want %#v", got, want)
	}
	if got, want := len(stylesheet.Rules), 1; got != want {
		t.Fatalf("len(Rules) = %d, want %d", got, want)
	}
	rule := stylesheet.Rules[0]
	if got, want := rule.Layer, "responsive"; got != want {
		t.Errorf("Rule.Layer = %q, want %q", got, want)
	}
	wantMedia := []string{
		"screen and (min-width: 700px)",
		"(orientation: landscape), (min-height: 900px)",
	}
	if got := rule.Media; !slices.Equal(got, wantMedia) {
		t.Errorf("Rule.Media = %#v, want %#v", got, wantMedia)
	}
	target := dom.NewElement("p", dom.Attribute{Name: "id", Value: "target"})
	if len(rule.Selectors) != 1 || !rule.Selectors[0].Matches(target) {
		t.Error("flattened nested rule did not retain its selector")
	}
}

func TestParseFlattensNestedDottedAndAnonymousLayerOrder(t *testing.T) {
	t.Parallel()

	stylesheet, err := css.Parse(`
		@layer framework, utilities;
		@layer framework {
			.direct { color: black }
			@layer reset, theme;
			@layer reset { .reset { color: red } }
			@layer theme {
				@layer components { .component { color: blue } }
				.theme { color: green }
			}
			@layer { .anonymous { color: white } }
		}
		@layer framework.theme.components { .reopened { color: gray } }
	`)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := len(stylesheet.LayerOrder), 6; got != want {
		t.Fatalf("layer count = %d, want %d: %q", got, want, stylesheet.LayerOrder)
	}
	anonymous := stylesheet.LayerOrder[3]
	if !strings.HasPrefix(anonymous, "framework.\x00layer-") {
		t.Fatalf("anonymous layer identity = %q", anonymous)
	}
	wantOrder := []string{
		"framework.reset",
		"framework.theme.components",
		"framework.theme",
		anonymous,
		"framework",
		"utilities",
	}
	if !slices.Equal(stylesheet.LayerOrder, wantOrder) {
		t.Fatalf("LayerOrder = %q, want %q", stylesheet.LayerOrder, wantOrder)
	}
	wantRuleLayers := []string{
		"framework",
		"framework.reset",
		"framework.theme.components",
		"framework.theme",
		anonymous,
		"framework.theme.components",
	}
	if got := len(stylesheet.Rules); got != len(wantRuleLayers) {
		t.Fatalf("rule count = %d, want %d", got, len(wantRuleLayers))
	}
	for index, want := range wantRuleLayers {
		if got := stylesheet.Rules[index].Layer; got != want {
			t.Errorf("rule %d layer = %q, want %q", index, got, want)
		}
	}
}

func TestParseLayerNamesUseDecodedComponentsAndRejectWhitespacePaths(t *testing.T) {
	t.Parallel()

	stylesheet, err := css.Parse(`
		@layer fr\61 me.th\65 me { .decoded { color: red } }
		@layer invalid . path { .invalid { color: blue } }
		@layer initial { .reserved { color: green } }
	`)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := stylesheet.LayerOrder, []string{"frame.theme", "frame"}; !slices.Equal(got, want) {
		t.Fatalf("LayerOrder = %q, want %q", got, want)
	}
	if got, want := len(stylesheet.Rules), 1; got != want {
		t.Fatalf("rule count = %d, want %d", got, want)
	}
	if got := stylesheet.Rules[0].Layer; got != "frame.theme" {
		t.Fatalf("decoded rule layer = %q", got)
	}
}
