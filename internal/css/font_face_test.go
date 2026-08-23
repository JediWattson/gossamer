package css_test

import (
	"reflect"
	"testing"

	"github.com/JediWattson/gossamer/internal/css"
)

func TestParseFontFaceRetainsDescriptorsSourcesAndConditions(t *testing.T) {
	stylesheet, err := css.Parse(`
		@font-face {
			font-family: "News Sans";
			src: local("News Sans"), url(../fonts/news.woff2) format("woff2"), url("../fonts/news.otf") format(opentype);
			font-style: italic;
			font-weight: 300 700;
			font-display: swap;
			unicode-range: U+0020-007E, U+4??;
		}
		@media screen { @supports (display: grid) {
			@font-face { font-family: Icons; src: url(icons.ttf); font-weight: bold; }
		} }
	`)
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	if len(stylesheet.FontFaces) != 2 {
		t.Fatalf("font faces = %d, want 2", len(stylesheet.FontFaces))
	}
	first := stylesheet.FontFaces[0]
	if first.Family != "News Sans" || first.Style != "italic" || first.Display != "swap" || first.Weight != (css.FontFaceWeight{Min: 300, Max: 700}) {
		t.Fatalf("first font face = %#v", first)
	}
	wantRanges := []css.UnicodeRange{{Start: 0x20, End: 0x7e}, {Start: 0x400, End: 0x4ff}}
	if !reflect.DeepEqual(first.UnicodeRanges, wantRanges) {
		t.Fatalf("unicode ranges = %#v, want %#v", first.UnicodeRanges, wantRanges)
	}
	wantSources := []css.FontSource{{URL: "../fonts/news.woff2", Format: "woff2"}, {URL: "../fonts/news.otf", Format: "opentype"}}
	if !reflect.DeepEqual(first.Sources, wantSources) {
		t.Fatalf("sources = %#v, want %#v", first.Sources, wantSources)
	}
	second := stylesheet.FontFaces[1]
	if second.Family != "Icons" || second.Weight != (css.FontFaceWeight{Min: 700, Max: 700}) || !reflect.DeepEqual(second.Media, []string{"screen"}) || !reflect.DeepEqual(second.Supports, []string{"(display: grid)"}) {
		t.Fatalf("conditional font face = %#v", second)
	}
}

func TestParseFontFaceRecoversFromInvalidDescriptorsAndRules(t *testing.T) {
	stylesheet, err := css.Parse(`
		@font-face { font-family: Broken; src: local(Broken); }
		@font-face { font-family: Working Face; src: url(ok.ttf); src: local(Working); font-style: sideways; font-weight: 0; font-display: soon; }
		p { color: red }
	`)
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	if len(stylesheet.FontFaces) != 1 {
		t.Fatalf("font faces = %#v, want one usable face", stylesheet.FontFaces)
	}
	face := stylesheet.FontFaces[0]
	if face.Style != "normal" || face.Weight != (css.FontFaceWeight{Min: 400, Max: 400}) || face.Display != "auto" {
		t.Fatalf("invalid optional descriptors did not recover to initials: %#v", face)
	}
	if len(stylesheet.Rules) != 1 {
		t.Fatalf("rules = %d, want trailing qualified rule", len(stylesheet.Rules))
	}
}
