package style_test

import (
	"image/color"
	"testing"

	"github.com/JediWattson/gossamer/internal/dom"
	"github.com/JediWattson/gossamer/internal/style"
)

func TestCurrentColorResolvesAgainstFinalAndInheritedColor(t *testing.T) {
	t.Parallel()

	document := dom.NewDocument()
	html := dom.NewElement("html")
	body := dom.NewElement("body", dom.Attribute{Name: "style", Value: "color:#123456"})
	target := dom.NewElement("div", dom.Attribute{Name: "style", Value: `
		background-color:currentcolor;
		border:2px solid currentcolor;
		color:#c86432;
	`})
	child := dom.NewElement("span", dom.Attribute{Name: "style", Value: `
		color:red;
		color:currentcolor;
		background:currentcolor;
	`})
	grandchild := dom.NewElement("em", dom.Attribute{Name: "style", Value: `
		color:#0000ff;
		background-color:inherit;
	`})
	child.AppendChild(grandchild)
	target.AppendChild(child)
	body.AppendChild(target)
	html.AppendChild(body)
	document.AppendChild(html)

	snapshot := style.Compute(document, style.Input{Environment: style.Environment{Width: 320, Height: 200}})
	targetStyle, ok := snapshot.Lookup(target)
	if !ok {
		t.Fatal("target style is missing")
	}
	targetColor := color.NRGBA{R: 0xc8, G: 0x64, B: 0x32, A: 0xff}
	if got := targetStyle.Color(); got != targetColor {
		t.Fatalf("target color = %#v, want %#v", got, targetColor)
	}
	if got, painted := targetStyle.Background(); got != targetColor || !painted {
		t.Fatalf("target background = %#v, %t, want current color %#v, true", got, painted, targetColor)
	}
	if value, ok := style.ComputedPropertyValue(targetStyle, "background-color"); !ok || value != "rgb(200, 100, 50)" {
		t.Fatalf("computed background-color = %q, %t", value, ok)
	}
	for _, property := range []string{"border-top-color", "border-right-color", "border-bottom-color", "border-left-color"} {
		if value, ok := style.ComputedPropertyValue(targetStyle, property); !ok || value != "rgb(200, 100, 50)" {
			t.Errorf("computed %s = %q, %t", property, value, ok)
		}
	}

	childStyle, ok := snapshot.Lookup(child)
	if !ok {
		t.Fatal("child style is missing")
	}
	if got := childStyle.Color(); got != targetColor {
		t.Fatalf("color:currentcolor = %#v, want inherited %#v", got, targetColor)
	}
	if got, painted := childStyle.Background(); got != targetColor || !painted {
		t.Fatalf("child background = %#v, %t, want %#v, true", got, painted, targetColor)
	}

	grandchildStyle, ok := snapshot.Lookup(grandchild)
	if !ok {
		t.Fatal("grandchild style is missing")
	}
	blue := color.NRGBA{B: 0xff, A: 0xff}
	if got, painted := grandchildStyle.Background(); got != blue || !painted {
		t.Fatalf("inherited currentcolor background = %#v, %t, want child color %#v, true", got, painted, blue)
	}
}

func TestCurrentColorOnRootColorUsesInitialColor(t *testing.T) {
	t.Parallel()

	document := dom.NewDocument()
	html := dom.NewElement("html", dom.Attribute{Name: "style", Value: "color:currentcolor"})
	document.AppendChild(html)

	snapshot := style.Compute(document, style.Input{})
	computed, ok := snapshot.Lookup(html)
	if !ok {
		t.Fatal("root style is missing")
	}
	if got, want := computed.Color(), (color.NRGBA{A: 0xff}); got != want {
		t.Fatalf("root currentcolor = %#v, want initial %#v", got, want)
	}
}
