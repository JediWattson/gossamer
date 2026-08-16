package style

import (
	"slices"
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
		align-content:space-around;
		align-items:end;
		align-self:self-start;
		justify-content:center;
		justify-items:stretch;
		justify-self:flex-end;
		grid-template-columns:repeat(2, 40px minmax(min-content, 1fr)) max-content fit-content(75px);
		grid-template-rows:auto min-content 25%;
		grid-auto-columns:minmax(20px, max-content) 1fr fit-content(30px);
		grid-auto-rows:18px auto;
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
		"align-content":         "space-around",
		"align-items":           "end",
		"align-self":            "self-start",
		"display":               "grid",
		"grid-template-columns": "repeat(2, 40px minmax(min-content, 1fr)) max-content fit-content(75px)",
		"grid-template-rows":    "auto min-content 25%",
		"grid-auto-columns":     "minmax(20px, max-content) 1fr fit-content(30px)",
		"grid-auto-rows":        "18px auto",
		"grid-auto-flow":        "column dense",
		"grid-column-start":     "2",
		"grid-column-end":       "span 3",
		"grid-row-start":        "-2",
		"grid-row-end":          "4",
		"justify-content":       "center",
		"justify-items":         "stretch",
		"justify-self":          "flex-end",
	}
	for property, want := range properties {
		if got, found := ComputedPropertyValue(computed, property); !found || got != want {
			t.Errorf("%s = %q, %t, want %q, true", property, got, found, want)
		}
	}

	tracks := computed.GridTemplateColumns().Tracks()
	if len(tracks) != 6 || !tracks[1].IsMinMax() || tracks[1].MinKind() != GridTrackMinContent || tracks[1].MaxKind() != GridTrackFraction || tracks[1].MaxFraction() != 1 || tracks[4].Kind() != GridTrackMaxContent || !tracks[5].IsFitContent() || tracks[5].FitContentLimit().Value() != 75 {
		t.Fatalf("computed grid tracks = %#v", tracks)
	}
	tracks[0] = GridTrackSize{}
	again, _ := snapshot.Lookup(target)
	first, _ := again.GridTemplateColumns().At(0)
	if first.Kind() != GridTrackLength || first.Length().Value() != 40 {
		t.Fatalf("mutating returned tracks changed snapshot: %#v", first)
	}
	automatic := computed.GridAutoColumns().Tracks()
	if len(automatic) != 3 || !automatic[0].IsMinMax() || automatic[1].Kind() != GridTrackFraction || !automatic[2].IsFitContent() {
		t.Fatalf("computed automatic track pattern = %#v", automatic)
	}
	automatic[0] = GridTrackSize{}
	again, _ = snapshot.Lookup(target)
	if preserved, _ := again.GridAutoColumns().At(0); !preserved.IsMinMax() {
		t.Fatalf("mutating returned automatic tracks changed snapshot: %#v", preserved)
	}
}

func TestNamedGridLinesComputeExpandRepeatAndStayImmutable(t *testing.T) {
	t.Parallel()

	document := dom.NewDocument()
	html := dom.NewElement("html")
	body := dom.NewElement("body")
	target := dom.NewElement("div", dom.Attribute{Name: "style", Value: `
		display:grid;
		grid-template-columns:[first nav-start] 40px [middle] repeat(2, [col] 1fr [edge]) [last];
		grid-template-rows:[\66 oo] 20px [Bar];
		grid-column:content-start / span 2 content-end;
		grid-row:Bar 2 / span foo;
	`})
	body.AppendChild(target)
	html.AppendChild(body)
	document.AppendChild(html)

	snapshot := Compute(document, Input{Environment: Environment{Width: 800, Height: 600, InitialFontSize: 16}})
	computed, ok := snapshot.Lookup(target)
	if !ok {
		t.Fatal("target has no computed style")
	}
	wantProperties := map[string]string{
		"grid-template-columns": "[first nav-start] 40px [middle] repeat(2, [col] 1fr [edge]) [last]",
		"grid-template-rows":    "[foo] 20px [Bar]",
		"grid-column-start":     "content-start",
		"grid-column-end":       "span 2 content-end",
		"grid-row-start":        "2 Bar",
		"grid-row-end":          "span foo",
	}
	for property, want := range wantProperties {
		if got, found := ComputedPropertyValue(computed, property); !found || got != want {
			t.Errorf("%s = %q, %t, want %q, true", property, got, found, want)
		}
	}

	template := computed.GridTemplateColumns()
	wantLines := [][]string{
		{"first", "nav-start"},
		{"middle", "col"},
		{"edge", "col"},
		{"edge", "last"},
	}
	if template.Len() != 3 {
		t.Fatalf("expanded tracks = %d, want 3", template.Len())
	}
	for index, want := range wantLines {
		got := template.LineNames(index)
		if !slices.Equal(got, want) {
			t.Errorf("line %d names = %v, want %v", index, got, want)
		}
		if len(got) != 0 {
			got[0] = "mutated"
		}
		if preserved := template.LineNames(index); len(preserved) != 0 && preserved[0] == "mutated" {
			t.Fatalf("mutating line %d names changed snapshot", index)
		}
	}
	if computed.GridColumnStart().Name() != "content-start" || computed.GridColumnStart().Number() != 1 || computed.GridColumnStart().NumberExplicit() {
		t.Fatalf("named start = %#v", computed.GridColumnStart())
	}
	if computed.GridColumnEnd().Name() != "content-end" || computed.GridColumnEnd().Number() != 2 || !computed.GridColumnEnd().NumberExplicit() {
		t.Fatalf("named span = %#v", computed.GridColumnEnd())
	}
}

func TestGridPropertyGrammarRejectsUnboundedOrUnsupportedTracks(t *testing.T) {
	t.Parallel()

	valid := []css.Declaration{
		{Property: "align-content", Value: "space-evenly"},
		{Property: "align-items", Value: "self-end"},
		{Property: "align-self", Value: "auto"},
		{Property: "display", Value: "inline-grid"},
		{Property: "grid-template-columns", Value: "repeat(4, min(10px, 2vw) 1fr)"},
		{Property: "grid-auto-columns", Value: "0fr"},
		{Property: "grid-auto-rows", Value: "minmax(min-content, max-content)"},
		{Property: "grid-template-rows", Value: "min-content max-content minmax(auto, 2fr)"},
		{Property: "grid-template-columns", Value: "repeat(2, fit-content(10vw))"},
		{Property: "grid-template-columns", Value: "[first] 20px [middle] repeat(2, [track] 1fr [edge]) [last]"},
		{Property: "grid-template-columns", Value: "[] 10px []"},
		{Property: "grid-auto-columns", Value: "fit-content(calc(20px + 5%))"},
		{Property: "grid-auto-columns", Value: "10px min-content 2fr"},
		{Property: "grid-auto-flow", Value: "dense row"},
		{Property: "grid-column", Value: "span 2 / -1"},
		{Property: "grid-column", Value: "content 2 / span 3 content"},
		{Property: "grid-row-start", Value: "span row"},
		{Property: "justify-content", Value: "stretch"},
		{Property: "justify-items", Value: "start"},
		{Property: "justify-self", Value: "center"},
	}
	for _, declaration := range valid {
		if !SupportsDeclaration(declaration) {
			t.Errorf("SupportsDeclaration(%s:%s) = false", declaration.Property, declaration.Value)
		}
	}

	invalid := []css.Declaration{
		{Property: "align-content", Value: "auto"},
		{Property: "align-items", Value: "auto"},
		{Property: "align-self", Value: "space-between"},
		{Property: "grid-template-columns", Value: "repeat(0, 1fr)"},
		{Property: "grid-template-columns", Value: "repeat(1025, 1fr)"},
		{Property: "grid-template-columns", Value: "repeat(1024, [a b c d e f g h i] 1px)"},
		{Property: "grid-template-columns", Value: "minmax(1fr, 20px)"},
		{Property: "grid-template-columns", Value: "minmax(10px)"},
		{Property: "grid-template-columns", Value: "minmax(10px, 20px, 30px)"},
		{Property: "grid-template-columns", Value: "fit-content(-20px)"},
		{Property: "grid-template-columns", Value: "fit-content(1fr)"},
		{Property: "grid-template-columns", Value: "fit-content(20px 30px)"},
		{Property: "grid-auto-columns", Value: "repeat(2, 10px)"},
		{Property: "grid-auto-columns", Value: "[named] 10px"},
		{Property: "grid-template-columns", Value: "[span] 10px"},
		{Property: "grid-template-columns", Value: "[auto] 10px"},
		{Property: "grid-template-columns", Value: "[initial] 10px"},
		{Property: "grid-template-columns", Value: "[a][b] 10px"},
		{Property: "grid-template-columns", Value: "[a]"},
		{Property: "grid-auto-flow", Value: "row column"},
		{Property: "grid-column-start", Value: "0"},
		{Property: "grid-column-end", Value: "span -1"},
		{Property: "grid-column-end", Value: "span"},
		{Property: "grid-column-end", Value: "auto content"},
		{Property: "grid-column-end", Value: "0 content"},
		{Property: "grid-column-end", Value: "span 2 first second"},
		{Property: "grid-row", Value: "1 / 2 / 3"},
		{Property: "justify-content", Value: "self-start"},
		{Property: "justify-items", Value: "space-around"},
	}
	for _, declaration := range invalid {
		if SupportsDeclaration(declaration) {
			t.Errorf("SupportsDeclaration(%s:%s) = true", declaration.Property, declaration.Value)
		}
	}
}
