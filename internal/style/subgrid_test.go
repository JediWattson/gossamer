package style

import (
	"reflect"
	"strings"
	"testing"

	"github.com/JediWattson/gossamer/internal/dom"
)

func TestSubgridTrackListsRetainCanonicalComputedValues(t *testing.T) {
	t.Parallel()

	document := dom.NewDocument()
	html := dom.NewElement("html")
	body := dom.NewElement("body")
	target := dom.NewElement("div", dom.Attribute{Name: "style", Value: `
		display:grid;
		grid-template-columns:subgrid [outer] repeat(2,[slot] [edge]);
		grid-template-rows:subgrid repeat(auto-fill,[row]) [last];
	`})
	body.AppendChild(target)
	html.AppendChild(body)
	document.AppendChild(html)

	computed, ok := Compute(document, Input{Environment: Environment{Width: 320, Height: 240, InitialFontSize: 16}}).Lookup(target)
	if !ok {
		t.Fatal("target has no computed style")
	}
	if !computed.GridTemplateColumns().IsSubgrid() || !computed.GridTemplateRows().IsSubgrid() {
		t.Fatalf("subgrid axes were not retained: %#v", computed)
	}
	for property, want := range map[string]string{
		"grid-template-columns": "subgrid [outer] repeat(2, [slot] [edge])",
		"grid-template-rows":    "subgrid repeat(auto-fill, [row]) [last]",
	} {
		if got, found := ComputedPropertyValue(computed, property); !found || got != want {
			t.Errorf("%s = %q, %t, want %q, true", property, got, found, want)
		}
	}
	if got, want := computed.GridTemplateColumns().ResolvedSubgridLineNames(5), [][]string{{"outer"}, {"slot"}, {"edge"}, {"slot"}, {"edge"}}; !reflect.DeepEqual(got, want) {
		t.Fatalf("fixed subgrid names = %#v, want %#v", got, want)
	}
	if got, want := computed.GridTemplateRows().ResolvedSubgridLineNames(5), [][]string{{"row"}, {"row"}, {"row"}, {"row"}, {"last"}}; !reflect.DeepEqual(got, want) {
		t.Fatalf("auto-fill subgrid names = %#v, want %#v", got, want)
	}
	resolved := computed.GridTemplateRows().ResolvedSubgridLineNames(3)
	resolved[0][0] = "mutated"
	if again := computed.GridTemplateRows().ResolvedSubgridLineNames(3); again[0][0] != "row" {
		t.Fatalf("resolved subgrid names leaked mutation: %#v", again)
	}
}

func TestSubgridTrackListGrammarRejectsInvalidMixesAndStaysBounded(t *testing.T) {
	t.Parallel()

	for _, source := range []string{
		"subgrid 10px",
		"subgrid repeat(auto-fit,[line])",
		"subgrid repeat(auto-fill,[a]) repeat(auto-fill,[b])",
		"subgrid repeat(2,10px)",
		"subgrid repeat(0,[line])",
		"subgrid none",
	} {
		if _, ok := parseGridTrackList(source, 16, Viewport{Width: 320, Height: 240}); ok {
			t.Errorf("parseGridTrackList(%q) unexpectedly succeeded", source)
		}
	}
	tooMany := "subgrid " + strings.Repeat("[] ", maxGridTrackListEntries+2)
	if _, ok := parseGridTrackList(tooMany, 16, Viewport{Width: 320, Height: 240}); ok {
		t.Fatal("over-budget subgrid line list unexpectedly succeeded")
	}
}

func TestBareSubgridPadsResolvedLocalLineSets(t *testing.T) {
	t.Parallel()

	list, ok := parseGridTrackList("subgrid", 16, Viewport{Width: 320, Height: 240})
	if !ok || !list.IsSubgrid() {
		t.Fatalf("bare subgrid = %#v, %t", list, ok)
	}
	if got, want := list.ResolvedSubgridLineNames(4), [][]string{nil, nil, nil, nil}; !reflect.DeepEqual(got, want) {
		t.Fatalf("bare resolved subgrid names = %#v, want %#v", got, want)
	}
}

func TestGridGapRetainsNormalSeparatelyFromExplicitZero(t *testing.T) {
	t.Parallel()

	document := dom.NewDocument()
	html := dom.NewElement("html")
	body := dom.NewElement("body")
	target := dom.NewElement("div", dom.Attribute{Name: "style", Value: "row-gap:normal;column-gap:0"})
	body.AppendChild(target)
	html.AppendChild(body)
	document.AppendChild(html)

	computed, ok := Compute(document, Input{Environment: Environment{Width: 320, Height: 240, InitialFontSize: 16}}).Lookup(target)
	if !ok {
		t.Fatal("target has no computed style")
	}
	if !computed.RowGapNormal() || computed.ColumnGapNormal() {
		t.Fatalf("normal gap flags = row:%t column:%t", computed.RowGapNormal(), computed.ColumnGapNormal())
	}
	for property, want := range map[string]string{"row-gap": "normal", "column-gap": "0px"} {
		if got, found := ComputedPropertyValue(computed, property); !found || got != want {
			t.Errorf("%s = %q, %t, want %q, true", property, got, found, want)
		}
	}
}
