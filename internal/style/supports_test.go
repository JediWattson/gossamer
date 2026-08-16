package style_test

import (
	"image/color"
	"testing"

	"github.com/JediWattson/gossamer/internal/dom"
	"github.com/JediWattson/gossamer/internal/style"
)

func TestComputeAppliesSupportedGroupRules(t *testing.T) {
	t.Parallel()

	document := dom.NewDocument()
	html := dom.NewElement("html")
	head := dom.NewElement("head")
	styleElement := dom.NewElement("style")
	styleElement.AppendChild(dom.NewText(`
		#target { color: black; background-color: black }
		@supports (display: block) and selector(#target) {
			#target { color: red }
		}
		@supports (display: grid) {
			#target { color: blue }
		}
		@supports not (display: grid) {
			#target { background-color: green }
		}
	`))
	head.AppendChild(styleElement)
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
	if got, want := computed.Color(), (color.NRGBA{B: 0xff, A: 0xff}); got != want {
		t.Fatalf("color = %#v, want %#v", got, want)
	}
	got, _ := computed.Background()
	if want := (color.NRGBA{A: 0xff}); got != want {
		t.Fatalf("background = %#v, want %#v", got, want)
	}
}
