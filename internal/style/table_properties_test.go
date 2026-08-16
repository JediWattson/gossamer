package style_test

import (
	"testing"

	"github.com/JediWattson/gossamer/internal/dom"
	"github.com/JediWattson/gossamer/internal/style"
)

func TestTablePropertiesComputeSerializeAndInherit(t *testing.T) {
	t.Parallel()

	document := dom.NewDocument()
	html := dom.NewElement("html")
	body := dom.NewElement("body")
	table := dom.NewElement("table", dom.Attribute{Name: "style", Value: `
		font-size:10px;
		border-collapse:collapse;
		border-spacing:1em 5vh;
		caption-side:bottom;
		empty-cells:hide;
		table-layout:fixed
	`})
	row := dom.NewElement("tr")
	cell := dom.NewElement("td")
	row.AppendChild(cell)
	table.AppendChild(row)
	body.AppendChild(table)
	html.AppendChild(dom.NewElement("head"))
	html.AppendChild(body)
	document.AppendChild(html)

	snapshot := style.Compute(document, style.Input{Environment: style.Environment{Width: 200, Height: 100, InitialFontSize: 16}})
	computed, ok := snapshot.Lookup(table)
	if !ok {
		t.Fatal("table style missing")
	}
	want := map[string]string{
		"border-collapse": "collapse",
		"border-spacing":  "10px 5px",
		"caption-side":    "bottom",
		"empty-cells":     "hide",
		"table-layout":    "fixed",
	}
	for property, expected := range want {
		if got, found := style.ComputedPropertyValue(computed, property); !found || got != expected {
			t.Errorf("%s = %q, %t, want %q, true", property, got, found, expected)
		}
	}

	child, ok := snapshot.Lookup(cell)
	if !ok {
		t.Fatal("cell style missing")
	}
	for property, expected := range map[string]string{
		"border-collapse": "collapse",
		"border-spacing":  "10px 5px",
		"caption-side":    "bottom",
		"empty-cells":     "hide",
		"table-layout":    "auto",
	} {
		if got, _ := style.ComputedPropertyValue(child, property); got != expected {
			t.Errorf("inherited cell %s = %q, want %q", property, got, expected)
		}
	}
}

func TestTablePropertyGrammarRejectsInvalidValuesWithoutMaskingLosers(t *testing.T) {
	t.Parallel()

	document := dom.NewDocument()
	html := dom.NewElement("html")
	body := dom.NewElement("body")
	table := dom.NewElement("table", dom.Attribute{Name: "style", Value: `
		border-collapse:collapse; border-collapse:merge;
		border-spacing:3px 4px; border-spacing:1%;
		caption-side:bottom; caption-side:left;
		empty-cells:hide; empty-cells:none;
		table-layout:fixed; table-layout:fast
	`})
	body.AppendChild(table)
	html.AppendChild(dom.NewElement("head"))
	html.AppendChild(body)
	document.AppendChild(html)

	computed, ok := style.Compute(document, style.Input{}).Lookup(table)
	if !ok {
		t.Fatal("table style missing")
	}
	want := map[string]string{
		"border-collapse": "collapse",
		"border-spacing":  "3px 4px",
		"caption-side":    "bottom",
		"empty-cells":     "hide",
		"table-layout":    "fixed",
	}
	for property, expected := range want {
		if got, _ := style.ComputedPropertyValue(computed, property); got != expected {
			t.Errorf("%s after invalid winner = %q, want %q", property, got, expected)
		}
	}
}

func TestHTMLTableUserAgentDefaultsIncludeSeparatedSpacing(t *testing.T) {
	t.Parallel()

	document := dom.NewDocument()
	html := dom.NewElement("html")
	body := dom.NewElement("body")
	table := dom.NewElement("table")
	rowGroup := dom.NewElement("tbody")
	row := dom.NewElement("tr")
	cell := dom.NewElement("td")
	row.AppendChild(cell)
	rowGroup.AppendChild(row)
	table.AppendChild(rowGroup)
	body.AppendChild(table)
	html.AppendChild(dom.NewElement("head"))
	html.AppendChild(body)
	document.AppendChild(html)

	snapshot := style.Compute(document, style.Input{})
	tableStyle, _ := snapshot.Lookup(table)
	cellStyle, _ := snapshot.Lookup(cell)
	if got, _ := style.ComputedPropertyValue(tableStyle, "border-spacing"); got != "2px" {
		t.Errorf("UA table border-spacing = %q, want 2px", got)
	}
	if got, _ := style.ComputedPropertyValue(cellStyle, "vertical-align"); got != "middle" {
		t.Errorf("UA cell vertical-align = %q, want middle", got)
	}
}
