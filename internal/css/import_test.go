package css_test

import (
	"reflect"
	"testing"

	"github.com/JediWattson/gossamer/internal/css"
)

func TestParseImportRulesFromComponentValues(t *testing.T) {
	t.Parallel()

	stylesheet, err := css.Parse(`
		@import "base.css";
		@import url(theme.css) layer(theme) supports(display: block) screen and (min-width: 40em);
		@import url("print.css") layer print;
		@import "nested.css" layer(fr\61 me.theme);
		body { color: red }
	`)
	if err != nil {
		t.Fatal(err)
	}
	want := []css.ImportRule{
		{URL: "base.css", Order: 0},
		{URL: "theme.css", Layer: "theme", Layered: true, Supports: "display: block", Media: "screen and (min-width: 40em)", Order: 1},
		{URL: "print.css", Layered: true, Media: "print", Order: 2},
		{URL: "nested.css", Layer: "frame.theme", Layered: true, Order: 3},
	}
	if !reflect.DeepEqual(stylesheet.Imports, want) {
		t.Fatalf("imports = %#v, want %#v", stylesheet.Imports, want)
	}
}

func TestParseImportDecodesEscapesAndRejectsLateOrMalformedRules(t *testing.T) {
	t.Parallel()

	stylesheet, err := css.Parse(`
		@import "escaped\2e css" l\61 yer(base) scr\65 en;
		div { color: red }
		@import "late.css";
		@import layer(no-url);
	`)
	if err != nil {
		t.Fatal(err)
	}
	want := []css.ImportRule{{URL: "escaped.css", Layer: "base", Layered: true, Media: `scr\65 en`, Order: 0}}
	if !reflect.DeepEqual(stylesheet.Imports, want) {
		t.Fatalf("imports = %#v, want %#v", stylesheet.Imports, want)
	}
}

func TestParseImportIsRejectedAfterAnyGroupOrQualifiedRule(t *testing.T) {
	t.Parallel()

	stylesheet, err := css.Parse(`
		@layer reset;
		@import "allowed-after-layer-statement.css";
		@layer theme;
		@import "late-after-post-import-layer.css";
		@supports (display: block) {}
		@import "late-after-empty-group.css";
		.invalid
		@import "late-after-malformed-qualified.css";
	`)
	if err != nil {
		t.Fatal(err)
	}
	want := []css.ImportRule{{URL: "allowed-after-layer-statement.css", Order: 0}}
	if !reflect.DeepEqual(stylesheet.Imports, want) {
		t.Fatalf("imports = %#v, want %#v", stylesheet.Imports, want)
	}
}
