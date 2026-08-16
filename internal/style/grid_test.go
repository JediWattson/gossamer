package style

import (
	"testing"

	"github.com/JediWattson/gossamer/internal/css"
	"github.com/JediWattson/gossamer/internal/dom"
)

func TestGridPropertiesComputeSerializeAndStayImmutable(t *testing.T) {
	t.Parallel()

	document := dom.NewDocument()
	html := dom.NewElement("html")
	body := dom.NewElement("body")
	target := dom.NewElement("div", dom.Attribute{Name: "style", Value: `
		display:grid;
		grid-template-columns:repeat(2, 40px 1fr);
		grid-template-rows:auto 25%;
		grid-auto-columns:3fr;
		grid-auto-rows:18px;
		grid-auto-flow:column dense;
		grid-column:2 / span 3;
		grid-row:-2 / 4;
	`})
	body.AppendChild(target)
	html.AppendChild(body)
	document.AppendChild(html)

	snapshot := Compute(document, Input{Environment: Environment{Width: 800, Height: 600, InitialFontSize: 16}})
	computed, ok := snapshot.Lookup(target)
	if !ok {
		t.Fatal("target has no computed style")
	}
	properties := map[string]string{
		"display":               "grid",
		"grid-template-columns": "repeat(2, 40px 1fr)",
		"grid-template-rows":    "auto 25%",
		"grid-auto-columns":     "3fr",
		"grid-auto-rows":        "18px",
		"grid-auto-flow":        "column dense",
		"grid-column-start":     "2",
		"grid-column-end":       "span 3",
		"grid-row-start":        "-2",
		"grid-row-end":          "4",
	}
	for property, want := range properties {
		if got, found := ComputedPropertyValue(computed, property); !found || got != want {
			t.Errorf("%s = %q, %t, want %q, true", property, got, found, want)
		}
	}

	tracks := computed.GridTemplateColumns().Tracks()
	if len(tracks) != 4 || tracks[1].Kind() != GridTrackFraction || tracks[1].Fraction() != 1 {
		t.Fatalf("computed grid tracks = %#v", tracks)
	}
	tracks[0] = GridTrackSize{kind: GridTrackAuto}
	again, _ := snapshot.Lookup(target)
	first, _ := again.GridTemplateColumns().At(0)
	if first.Kind() != GridTrackLength || first.Length().Value() != 40 {
		t.Fatalf("mutating returned tracks changed snapshot: %#v", first)
	}
}

func TestGridPropertyGrammarRejectsUnboundedOrUnsupportedTracks(t *testing.T) {
	t.Parallel()

	valid := []css.Declaration{
		{Property: "display", Value: "inline-grid"},
		{Property: "grid-template-columns", Value: "repeat(4, min(10px, 2vw) 1fr)"},
		{Property: "grid-auto-columns", Value: "0fr"},
		{Property: "grid-auto-flow", Value: "dense row"},
		{Property: "grid-column", Value: "span 2 / -1"},
	}
	for _, declaration := range valid {
		if !SupportsDeclaration(declaration) {
			t.Errorf("SupportsDeclaration(%s:%s) = false", declaration.Property, declaration.Value)
		}
	}

	invalid := []css.Declaration{
		{Property: "grid-template-columns", Value: "repeat(0, 1fr)"},
		{Property: "grid-template-columns", Value: "repeat(1025, 1fr)"},
		{Property: "grid-template-columns", Value: "minmax(10px, 1fr)"},
		{Property: "grid-auto-columns", Value: "1fr 2fr"},
		{Property: "grid-auto-flow", Value: "row column"},
		{Property: "grid-column-start", Value: "0"},
		{Property: "grid-column-end", Value: "span -1"},
		{Property: "grid-row", Value: "1 / 2 / 3"},
	}
	for _, declaration := range invalid {
		if SupportsDeclaration(declaration) {
			t.Errorf("SupportsDeclaration(%s:%s) = true", declaration.Property, declaration.Value)
		}
	}
}
