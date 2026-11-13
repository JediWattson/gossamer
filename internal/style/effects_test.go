package style

import (
	"image/color"
	"testing"

	"github.com/JediWattson/gossamer/internal/dom"
)

func TestPaintEffectsComputeThroughVariablesAndSerialize(t *testing.T) {
	t.Parallel()

	document := dom.NewDocument()
	html := dom.NewElement("html")
	body := dom.NewElement("body")
	target := dom.NewElement("div", dom.Attribute{Name: "style", Value: `
		--ring: 0 0 0 3px #ff00ff;
		color: #123456;
		box-shadow: var(--ring), inset 0 1px 2px currentcolor;
		background-image: linear-gradient(to right, red 0%, blue 100%), radial-gradient(circle at center, transparent, #00ff00);
		translate: 5px 25%;
		scale: 125% .5;
		rotate: .25turn;
		transform: translate(25%, 4px) scale(2, .5) rotate(90deg);
		transform-origin: left bottom;
	`})
	body.AppendChild(target)
	html.AppendChild(body)
	document.AppendChild(html)

	snapshot := Compute(document, Input{Environment: Environment{Width: 200, Height: 100, InitialFontSize: 16}})
	computed, ok := snapshot.Lookup(target)
	if !ok {
		t.Fatal("snapshot does not contain effects target")
	}
	if computed.BoxShadows().Len() != 2 {
		t.Fatalf("box shadow count = %d, want 2", computed.BoxShadows().Len())
	}
	second, _ := computed.BoxShadows().At(1)
	if !second.Inset() || second.Color(computed.Color()) != (color.NRGBA{R: 0x12, G: 0x34, B: 0x56, A: 0xff}) {
		t.Fatalf("inset currentcolor shadow = %#v", second)
	}
	if computed.BackgroundImages().Len() != 2 {
		t.Fatalf("background image count = %d, want 2", computed.BackgroundImages().Len())
	}
	if computed.Transforms().Len() != 3 {
		t.Fatalf("transform operation count = %d, want 3", computed.Transforms().Len())
	}
	if computed.Translate().IsNone() || computed.Scale().IsNone() || computed.Rotate().IsNone() {
		t.Fatal("individual transform properties were not retained")
	}
	wants := map[string]string{
		"box-shadow":       "0px 0px 0px 3px rgb(255, 0, 255), 0px 1px 2px currentcolor inset",
		"background-image": "linear-gradient(90deg, rgb(255, 0, 0) 0%, rgb(0, 0, 255) 100%), radial-gradient(circle at center, rgba(0, 0, 0, 0), rgb(0, 255, 0))",
		"transform":        "translate(25%, 4px) scale(2, 0.5) rotate(90deg)",
		"transform-origin": "0px 100%",
		"translate":        "5px 25%",
		"scale":            "1.25 0.5",
		"rotate":           "90deg",
	}
	for property, want := range wants {
		got, found := ComputedPropertyValue(computed, property)
		if !found || got != want {
			t.Errorf("ComputedPropertyValue(%q) = %q, %t; want %q, true", property, got, found, want)
		}
	}
}

func TestModernColorsFlowThroughTailwindShapedPaintValues(t *testing.T) {
	t.Parallel()

	document := dom.NewDocument()
	html := dom.NewElement("html")
	body := dom.NewElement("body")
	styleNode := dom.NewElement("style")
	styleNode.AppendChild(dom.NewText(`
		@supports (color:color-mix(in lab,red,red)) {
			#target {
				--accent:oklch(62.3% .214 259.815);
				--accent-faded:color-mix(in oklab,var(--accent) 40%,transparent);
				color:red;
				background-color:var(--accent-faded);
				border:1px solid color-mix(in srgb,currentcolor 25%,blue);
				box-shadow:0 0 2px color-mix(in oklab,currentcolor 50%,transparent);
				background-image:linear-gradient(color-mix(in oklab,currentcolor 50%,transparent),var(--accent));
			}
		}
	`))
	target := dom.NewElement("div", dom.Attribute{Name: "id", Value: "target"})
	body.AppendChild(styleNode)
	body.AppendChild(target)
	html.AppendChild(body)
	document.AppendChild(html)

	snapshot := Compute(document, Input{Environment: Environment{Width: 200, Height: 100, InitialFontSize: 16}})
	computed, ok := snapshot.Lookup(target)
	if !ok {
		t.Fatal("snapshot does not contain modern-color target")
	}
	if got, want := computed.Color(), (color.NRGBA{R: 255, A: 255}); got != want {
		t.Fatalf("color = %#v, want %#v", got, want)
	}
	if got, painted := computed.Background(); !painted || got != (color.NRGBA{R: 43, G: 127, B: 255, A: 102}) {
		t.Fatalf("mixed background = %#v, %t", got, painted)
	}
	if got := computed.BorderTop().ResolveColor(computed.Color()); got != (color.NRGBA{R: 64, B: 191, A: 255}) {
		t.Fatalf("mixed border = %#v", got)
	}
	shadow, ok := computed.BoxShadows().At(0)
	if !ok || shadow.Color(computed.Color()) != (color.NRGBA{R: 255, A: 128}) {
		t.Fatalf("mixed shadow = %#v, %t", shadow, ok)
	}
	background, ok := computed.BackgroundImages().At(0)
	if !ok || background.StopCount() != 2 {
		t.Fatalf("gradient = %#v, %t", background, ok)
	}
	first, _ := background.Stop(0)
	second, _ := background.Stop(1)
	if got := first.Color(computed.Color()); got != (color.NRGBA{R: 255, A: 128}) {
		t.Fatalf("first gradient stop = %#v", got)
	}
	if got := second.Color(computed.Color()); got != (color.NRGBA{R: 43, G: 127, B: 255, A: 255}) {
		t.Fatalf("second gradient stop = %#v", got)
	}

	wants := map[string]string{
		"background-color": "rgba(43, 127, 255, 0.4)",
		"border-top-color": "rgb(64, 0, 191)",
		"box-shadow":       "0px 0px 2px rgba(255, 0, 0, 0.5)",
	}
	for property, want := range wants {
		got, found := ComputedPropertyValue(computed, property)
		if !found || got != want {
			t.Errorf("ComputedPropertyValue(%q) = %q, %t; want %q, true", property, got, found, want)
		}
	}
}

func TestPaintEffectGrammarsRejectUnboundedOrInvalidValues(t *testing.T) {
	t.Parallel()

	viewport := Viewport{Width: 800, Height: 600, InitialFontSize: 16}
	invalid := []struct{ property, value string }{
		{"box-shadow", "0 0 -1px red"},
		{"box-shadow", "0 0 calc(-1px) red"},
		{"box-shadow", "0 0 1% red"},
		{"background-image", "linear-gradient(red)"},
		{"background-image", "paint(example)"},
		{"background-position", "left right"},
		{"background-repeat", "round space"},
		{"background-size", "-10px 10px"},
		{"transform", "perspective(10px)"},
		{"transform", "scale()"},
		{"transform", "scale(1,,2)"},
		{"background-image", "linear-gradient(to left left, red, blue)"},
		{"transform-origin", "left right"},
		{"object-fit", "stretch"},
		{"object-fit", "cover contain"},
		{"object-position", "left right"},
		{"translate", "1px 2px 3px"},
		{"scale", "1 2 3"},
		{"rotate", "1px"},
	}
	for _, test := range invalid {
		definition, ok := lookupPropertyDefinition(test.property)
		if !ok {
			t.Fatalf("missing property definition %q", test.property)
		}
		if definition.valid(test.value, viewport) {
			t.Errorf("invalid %s:%s was accepted", test.property, test.value)
		}
	}
}

func TestTailwindRingFallbackFeatureQueryProducesCountedShadows(t *testing.T) {
	t.Parallel()

	document := dom.NewDocument()
	html := dom.NewElement("html")
	body := dom.NewElement("body")
	target := dom.NewElement("div", dom.Attribute{Name: "class", Value: "ring"})
	styleNode := dom.NewElement("style")
	styleNode.AppendChild(dom.NewText(`
		@layer properties {
			@supports (((-webkit-hyphens:none)) and (not (margin-trim:inline))) or ((-moz-orient:inline) and (not (color:rgb(from red r g b)))) {
				* { --tw-inset-shadow:0 0 #0000; --tw-inset-ring-shadow:0 0 #0000; --tw-ring-offset-width:0px; --tw-ring-offset-shadow:0 0 #0000; --tw-ring-shadow:0 0 #0000; --tw-shadow:0 0 #0000 }
			}
		}
		.ring {
			--tw-ring-shadow:var(--tw-ring-inset,) 0 0 0 calc(2px + var(--tw-ring-offset-width)) var(--tw-ring-color,currentcolor);
			box-shadow:var(--tw-inset-shadow),var(--tw-inset-ring-shadow),var(--tw-ring-offset-shadow),var(--tw-ring-shadow),var(--tw-shadow)
		}
	`))
	body.AppendChild(styleNode)
	body.AppendChild(target)
	html.AppendChild(body)
	document.AppendChild(html)

	snapshot := Compute(document, Input{Environment: Environment{Width: 200, Height: 100, InitialFontSize: 16}})
	computed, ok := snapshot.Lookup(target)
	if !ok {
		t.Fatal("snapshot does not contain ring target")
	}
	if computed.BoxShadows().Len() != 5 {
		t.Fatalf("Tailwind ring shadow count = %d, want 5", computed.BoxShadows().Len())
	}
	ring, ok := computed.BoxShadows().At(3)
	if !ok {
		t.Fatal("Tailwind ring layer is missing")
	}
	spread, ok := ring.SpreadRadius().Resolve(0, 200, 100)
	if !ok || spread != 2 {
		t.Fatalf("Tailwind ring spread = %v, %t; want 2px", spread, ok)
	}
}
