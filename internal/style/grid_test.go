package style

import (
	"slices"
	"strconv"
	"strings"
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

func TestGridAutoRepeatRetainsComputedFormAndExpandsNames(t *testing.T) {
	t.Parallel()

	document := dom.NewDocument()
	target := dom.NewElement("div", dom.Attribute{Name: "style", Value: `grid-template-columns:[outer] 20px [before] repeat(auto-fill, [cell] minmax(50px, 1fr) [edge]) [after] 30px [last]`})
	document.AppendChild(target)
	snapshot := Compute(document, Input{Environment: Environment{Width: 800, Height: 600, InitialFontSize: 16}})
	computed, ok := snapshot.Lookup(target)
	if !ok {
		t.Fatal("missing computed style")
	}
	if got, found := ComputedPropertyValue(computed, "grid-template-columns"); !found || got != "[outer] 20px [before] repeat(auto-fill, [cell] minmax(50px, 1fr) [edge]) [after] 30px [last]" {
		t.Fatalf("computed auto-repeat = %q, %t", got, found)
	}
	template := computed.GridTemplateColumns()
	if template.AutoRepeatKind() != GridAutoRepeatFill || template.Len() != 3 {
		t.Fatalf("auto-repeat kind/one-repeat length = %v/%d", template.AutoRepeatKind(), template.Len())
	}
	if start, end, present := template.AutoRepeatRange(); !present || start != 1 || end != 2 {
		t.Fatalf("computed auto-repeat range = %d..%d, %t", start, end, present)
	}
	expanded, ok := template.ExpandAutoRepeat(3)
	if !ok || expanded.Len() != 5 {
		t.Fatalf("expanded auto-repeat length = %d, %t", expanded.Len(), ok)
	}
	wantNames := [][]string{
		{"outer"}, {"before", "cell"}, {"edge", "cell"},
		{"edge", "cell"}, {"edge", "after"}, {"last"},
	}
	for line, want := range wantNames {
		if got := expanded.LineNames(line); !slices.Equal(got, want) {
			t.Fatalf("expanded line %d names = %v, want %v", line, got, want)
		}
	}
	mutated := expanded.LineNames(2)
	mutated[0] = "changed"
	if got := expanded.LineNames(2)[0]; got != "edge" {
		t.Fatalf("auto-repeat line names are mutable: %q", got)
	}
	if _, ok := template.ExpandAutoRepeat(0); ok {
		t.Fatal("accepted zero auto repetitions")
	}
	nameHeavy, ok := parseGridTrackList("repeat(auto-fill,[a b c d e f g h i] 1px)", 16, Viewport{Width: 800, Height: 600})
	if !ok {
		t.Fatal("rejected bounded computed auto-repeat line names")
	}
	if _, ok := nameHeavy.ExpandAutoRepeat(1024); ok {
		t.Fatal("expanded auto-repeat beyond the line-name budget")
	}
	if expanded, ok := nameHeavy.ExpandAutoRepeat(910); !ok || expanded.Len() != 910 {
		t.Fatalf("last bounded name-heavy expansion = %d, %t", expanded.Len(), ok)
	}
}

func TestNamedGridAreasComputeSerializeAndGenerateLines(t *testing.T) {
	t.Parallel()

	document := dom.NewDocument()
	html := dom.NewElement("html")
	body := dom.NewElement("body")
	grid := dom.NewElement("section", dom.Attribute{Name: "style", Value: `grid-template-areas:"head   head" "nav main" "foot ...."`})
	named := dom.NewElement("div", dom.Attribute{Name: "style", Value: "grid-area:main"})
	numeric := dom.NewElement("div", dom.Attribute{Name: "style", Value: `grid-area:\31 st`})
	explicit := dom.NewElement("div", dom.Attribute{Name: "style", Value: "grid-area:2 / 3 / span 2 / span 3"})
	grid.AppendChild(named)
	grid.AppendChild(numeric)
	grid.AppendChild(explicit)
	body.AppendChild(grid)
	html.AppendChild(body)
	document.AppendChild(html)

	snapshot := Compute(document, Input{Environment: Environment{Width: 800, Height: 600, InitialFontSize: 16}})
	gridStyle, _ := snapshot.Lookup(grid)
	if got, found := ComputedPropertyValue(gridStyle, "grid-template-areas"); !found || got != `"head head" "nav main" "foot ."` {
		t.Fatalf("grid-template-areas = %q, %t", got, found)
	}
	template := gridStyle.GridTemplateAreas()
	if template.Rows() != 3 || template.Columns() != 2 {
		t.Fatalf("template dimensions = %dx%d, want 3x2", template.Rows(), template.Columns())
	}
	main, ok := template.Area("main")
	if !ok || main.RowStart() != 1 || main.RowEnd() != 2 || main.ColumnStart() != 1 || main.ColumnEnd() != 2 {
		t.Fatalf("main area = %#v, %t", main, ok)
	}
	head, ok := template.Area("head")
	if !ok || head.RowStart() != 0 || head.RowEnd() != 1 || head.ColumnStart() != 0 || head.ColumnEnd() != 2 {
		t.Fatalf("head area = %#v, %t", head, ok)
	}
	if got, want := template.ColumnLineNames(0), []string{"head-start", "nav-start", "foot-start"}; !slices.Equal(got, want) {
		t.Fatalf("column line 0 names = %v, want %v", got, want)
	}
	if got, want := template.RowLineNames(1), []string{"head-end", "nav-start", "main-start"}; !slices.Equal(got, want) {
		t.Fatalf("row line 1 names = %v, want %v", got, want)
	}
	row := template.Row(1)
	row[0] = "mutated"
	if got := template.Row(1)[0]; got != "nav" {
		t.Fatalf("mutating returned area row changed snapshot: %q", got)
	}

	namedStyle, _ := snapshot.Lookup(named)
	for _, property := range []string{"grid-row-start", "grid-column-start", "grid-row-end", "grid-column-end"} {
		if got, _ := ComputedPropertyValue(namedStyle, property); got != "main" {
			t.Errorf("named %s = %q, want main", property, got)
		}
	}
	numericStyle, _ := snapshot.Lookup(numeric)
	for _, property := range []string{"grid-row-start", "grid-column-start", "grid-row-end", "grid-column-end"} {
		if got, _ := ComputedPropertyValue(numericStyle, property); got != `\31 st` {
			t.Errorf("numeric %s = %q, want escaped identifier", property, got)
		}
	}
	explicitStyle, _ := snapshot.Lookup(explicit)
	wantExplicit := map[string]string{
		"grid-row-start": "2", "grid-column-start": "3",
		"grid-row-end": "span 2", "grid-column-end": "span 3",
	}
	for property, want := range wantExplicit {
		if got, _ := ComputedPropertyValue(explicitStyle, property); got != want {
			t.Errorf("explicit %s = %q, want %q", property, got, want)
		}
	}
}

func TestGridTemplateAreasBoundGeneratedLineNames(t *testing.T) {
	t.Parallel()

	const columns = 683
	var source strings.Builder
	for row := range 3 {
		if row != 0 {
			source.WriteByte(' ')
		}
		source.WriteByte('"')
		for column := range columns {
			if column != 0 {
				source.WriteByte(' ')
			}
			source.WriteByte('a')
			source.WriteString(strconv.Itoa(row*columns + column))
		}
		source.WriteByte('"')
	}
	if _, ok := parseGridTemplateAreas(source.String()); ok {
		t.Fatalf("accepted %d unique areas beyond the generated-line-name budget", columns*3)
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
		{Property: "grid-template-columns", Value: "repeat(auto-fill, 100px)"},
		{Property: "grid-template-columns", Value: "20px repeat(auto-fit, [slot] minmax(50px, 1fr) [edge]) 30px"},
		{Property: "grid-template-columns", Value: "repeat(2, 20px) repeat(auto-fill, minmax(auto, 50px)) repeat(2, 30px)"},
		{Property: "grid-template-areas", Value: `"head head" "nav main"`},
		{Property: "grid-template-areas", Value: `"1st 1st"`},
		{Property: "grid-auto-columns", Value: "fit-content(calc(20px + 5%))"},
		{Property: "grid-auto-columns", Value: "10px min-content 2fr"},
		{Property: "grid-auto-flow", Value: "dense row"},
		{Property: "grid-column", Value: "span 2 / -1"},
		{Property: "grid-column", Value: "content 2 / span 3 content"},
		{Property: "grid-area", Value: "main"},
		{Property: "grid-area", Value: "1 / 2 / span 2 / span 3"},
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
		{Property: "grid-template-columns", Value: "repeat(auto-fill, 1fr)"},
		{Property: "grid-template-columns", Value: "repeat(auto-fill, auto)"},
		{Property: "grid-template-columns", Value: "repeat(auto-fill, min-content)"},
		{Property: "grid-template-columns", Value: "repeat(auto-fill, fit-content(20px))"},
		{Property: "grid-template-columns", Value: "repeat(auto-fill, minmax(auto, 1fr))"},
		{Property: "grid-template-columns", Value: "1fr repeat(auto-fill, 100px)"},
		{Property: "grid-template-columns", Value: "repeat(auto-fill, 100px) max-content"},
		{Property: "grid-template-columns", Value: "repeat(auto-fill, 100px) repeat(auto-fit, 100px)"},
		{Property: "grid-template-columns", Value: "repeat(auto-fill, repeat(2, 100px))"},
		{Property: "grid-auto-columns", Value: "repeat(auto-fill, 100px)"},
		{Property: "grid-template-areas", Value: `"a a" "a"`},
		{Property: "grid-template-areas", Value: `"a a" "a ."`},
		{Property: "grid-template-areas", Value: `"a /"`},
		{Property: "grid-template-areas", Value: `"   "`},
		{Property: "grid-template-areas", Value: `none "a"`},
		{Property: "grid-auto-flow", Value: "row column"},
		{Property: "grid-column-start", Value: "0"},
		{Property: "grid-column-end", Value: "span -1"},
		{Property: "grid-column-end", Value: "span"},
		{Property: "grid-column-end", Value: "auto content"},
		{Property: "grid-column-end", Value: "0 content"},
		{Property: "grid-column-end", Value: "span 2 first second"},
		{Property: "grid-row", Value: "1 / 2 / 3"},
		{Property: "grid-area", Value: "1 / 2 / 3 / 4 / 5"},
		{Property: "grid-area", Value: "1 / / 2"},
		{Property: "justify-content", Value: "self-start"},
		{Property: "justify-items", Value: "space-around"},
	}
	for _, declaration := range invalid {
		if SupportsDeclaration(declaration) {
			t.Errorf("SupportsDeclaration(%s:%s) = true", declaration.Property, declaration.Value)
		}
	}
}
