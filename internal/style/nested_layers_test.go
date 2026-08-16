package style_test

import (
	"testing"

	"github.com/JediWattson/gossamer/internal/dom"
	"github.com/JediWattson/gossamer/internal/style"
)

func TestNestedLayerDirectAndImportantOrdering(t *testing.T) {
	t.Parallel()

	computed := computeLayerFixture(t, []string{`
		@layer outer {
			@layer inner {
				#target { background-color: red; color: red !important }
			}
			#target { background-color: green; color: green !important }
		}
	`})
	assertComputedLayerValue(t, computed, "background-color", "rgb(0, 128, 0)")
	assertComputedLayerValue(t, computed, "color", "rgb(255, 0, 0)")
}

func TestAnonymousLayersRemainDistinctAcrossStylesheets(t *testing.T) {
	t.Parallel()

	computed := computeLayerFixture(t, []string{
		`@layer { #target { color: red !important } }`,
		`@layer { #target { color: blue !important } }`,
	})
	// Important layer order is reversed. Distinct anonymous layers therefore
	// expose the first sheet; accidentally merging their local names yields blue.
	assertComputedLayerValue(t, computed, "color", "rgb(255, 0, 0)")

	computed = computeLayerFixture(t, []string{
		`@layer shared { #target { color: red !important } }`,
		`@layer shared { #target { color: blue !important } }`,
	})
	// Named layers with the same path intentionally merge across sheets.
	assertComputedLayerValue(t, computed, "color", "rgb(0, 0, 255)")
}

func TestNestedRevertLayerExposesPriorSibling(t *testing.T) {
	t.Parallel()

	computed := computeLayerFixture(t, []string{`
		@layer outer {
			@layer first { #target { color: red } }
			@layer second { #target { color: blue; color: revert-layer } }
		}
	`})
	assertComputedLayerValue(t, computed, "color", "rgb(255, 0, 0)")
}

func computeLayerFixture(t *testing.T, sheets []string) style.ComputedStyle {
	t.Helper()
	document := dom.NewDocument()
	html := dom.NewElement("html")
	head := dom.NewElement("head")
	for _, source := range sheets {
		owner := dom.NewElement("style")
		owner.AppendChild(dom.NewText(source))
		head.AppendChild(owner)
	}
	body := dom.NewElement("body")
	target := dom.NewElement("div", dom.Attribute{Name: "id", Value: "target"})
	body.AppendChild(target)
	html.AppendChild(head)
	html.AppendChild(body)
	document.AppendChild(html)
	snapshot := style.Compute(document, style.Input{Environment: style.Environment{Width: 800, Height: 600, MediaType: "screen", InitialFontSize: 16}})
	computed, ok := snapshot.Lookup(target)
	if !ok {
		t.Fatal("target has no computed style")
	}
	return computed
}

func assertComputedLayerValue(t *testing.T, computed style.ComputedStyle, property, want string) {
	t.Helper()
	got, ok := style.ComputedPropertyValue(computed, property)
	if !ok {
		t.Fatalf("property %q is unsupported", property)
	}
	if got != want {
		t.Fatalf("%s = %q, want %q", property, got, want)
	}
}
