package style_test

import (
	"testing"

	"github.com/JediWattson/gossamer/internal/dom"
	"github.com/JediWattson/gossamer/internal/style"
)

func TestAnonymousTableStyleKeepsOnlyInheritedValues(t *testing.T) {
	t.Parallel()

	document := dom.NewDocument()
	html := dom.NewElement("html")
	body := dom.NewElement("body")
	row := dom.NewElement("tr", dom.Attribute{Name: "style", Value: "background:#ff0000;padding:10px;border:5px solid #00ff00;color:#0000ff;visibility:hidden;empty-cells:hide"})
	body.AppendChild(row)
	html.AppendChild(dom.NewElement("head"))
	html.AppendChild(body)
	document.AppendChild(html)
	computed, ok := style.Compute(document, style.Input{}).Lookup(row)
	if !ok {
		t.Fatal("row computed style missing")
	}
	anonymous := computed.WithAnonymousDisplay(style.DisplayTableCell)
	wants := map[string]string{
		"display":           "table-cell",
		"background-color":  "rgba(0, 0, 0, 0)",
		"padding-left":      "0px",
		"border-left-style": "none",
		"color":             "rgb(0, 0, 255)",
		"visibility":        "hidden",
		"empty-cells":       "hide",
	}
	for property, want := range wants {
		got, found := style.ComputedPropertyValue(anonymous, property)
		if !found || got != want {
			t.Errorf("anonymous %s = %q, %t; want %q", property, got, found, want)
		}
	}
}
