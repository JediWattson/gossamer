package style

import (
	"image/color"
	"testing"

	"github.com/JediWattson/gossamer/internal/css"
	"github.com/JediWattson/gossamer/internal/dom"
)

func TestPropertyValuesDecodeEscapedIdentifiersAndUnits(t *testing.T) {
	t.Parallel()

	viewport := Viewport{Width: 800, Height: 600, InitialFontSize: 16}
	tests := []struct {
		property string
		value    string
	}{
		{property: "display", value: `bl\6f ck`},
		{property: "color", value: `r\65 d`},
		{property: "font-size", value: `2\65 m`},
		{property: "font-weight", value: `b\6f ld`},
		{property: "line-height", value: `n\6f rmal`},
		{property: "list-style-type", value: `d\65 cimal`},
		{property: "margin-left", value: `12\70 x`},
		{property: "max-width", value: `n\6f ne`},
		{property: "text-align", value: `c\65 nter`},
		{property: "text-decoration-line", value: `underl\69 ne`},
	}
	for _, test := range tests {
		t.Run(test.property, func(t *testing.T) {
			t.Parallel()
			if !validComputedDeclaration(css.Declaration{Property: test.property, Value: test.value}, viewport) {
				t.Fatalf("%s: %s was rejected", test.property, test.value)
			}
		})
	}

	parsed, ok := parseLength(`2\65 m`, 10, 10, viewport)
	if !ok || parsed.unit != lengthPX || parsed.value != 20 {
		t.Fatalf("escaped em length = %#v, %t; want 20px", parsed, ok)
	}
	parsedColor, ok := parseColor(`r\65 d`)
	if !ok || parsedColor != (color.NRGBA{R: 0xff, A: 0xff}) {
		t.Fatalf("escaped red = %#v, %t", parsedColor, ok)
	}
}

func TestPropertyShorthandsConsumeComponentValues(t *testing.T) {
	t.Parallel()

	viewport := Viewport{Width: 800, Height: 600, InitialFontSize: 16}
	tests := []struct {
		property string
		value    string
	}{
		{property: "background", value: `r\65 d ignored`},
		{property: "border", value: `th\69 n s\6f lid r\65 d`},
		{property: "border-width", value: `1px/**/2px 3px 4px`},
		{property: "border-style", value: `s\6f lid none`},
		{property: "border-color", value: `r\65 d currentc\6f lor`},
		{property: "font", value: `b\6f ld 12\70 x/1.5 s\65 rif`},
		{property: "list-style", value: `outside d\65 cimal`},
		{property: "margin", value: `1px/**/2px`},
		{property: "padding", value: `1\70 x 2\70 x`},
		{property: "text-decoration", value: `underl\69 ne`},
	}
	for _, test := range tests {
		t.Run(test.property, func(t *testing.T) {
			t.Parallel()
			if !validComputedDeclaration(css.Declaration{Property: test.property, Value: test.value}, viewport) {
				t.Fatalf("%s: %s was rejected", test.property, test.value)
			}
		})
	}
}

func TestPropertyValuesPreserveTokenBoundariesAndRejectUnsupportedForms(t *testing.T) {
	t.Parallel()

	viewport := Viewport{Width: 800, Height: 600, InitialFontSize: 16}
	tests := []css.Declaration{
		{Property: "background-color", Value: `red blue`},
		{Property: "width", Value: `1/**/px`},
		{Property: "width", Value: `1 px`},
		{Property: "width", Value: `calc(1px + 1px)`},
		{Property: "color", Value: `rgb(255 0 0)`},
		{Property: "display", Value: "block\u00a0"},
		{Property: "font-weight", Value: "400.0"},
		{Property: "border", Value: `solid/**/red/**/extra`},
	}
	for _, declaration := range tests {
		if validComputedDeclaration(declaration, viewport) {
			t.Errorf("%s: %s unexpectedly accepted", declaration.Property, declaration.Value)
		}
	}
}

func FuzzPropertyValueParsingDoesNotPanic(f *testing.F) {
	f.Add("width", "10px")
	f.Add("color", `r\65 d`)
	f.Add("border", "1px solid currentcolor")
	f.Add("font", "bold 12px/1.5 serif")
	f.Add("margin", "1px 2% auto")

	f.Fuzz(func(t *testing.T, property, source string) {
		viewport := Viewport{Width: 800, Height: 600, InitialFontSize: 16}
		declaration := css.Declaration{Property: property, Value: source}
		_ = validComputedDeclaration(declaration, viewport)
		computed := cssInitialStyle(viewport)
		applyDeclaration(&computed, property, source, propertyApplyContext{
			parentFontSize:   16,
			parentFontWeight: 400,
			viewport:         viewport,
		})
	})
}

func TestComponentValueApplicationMatchesValidation(t *testing.T) {
	t.Parallel()

	viewport := Viewport{Width: 800, Height: 600, InitialFontSize: 16}
	computed := cssInitialStyle(viewport)
	context := propertyApplyContext{parentFontSize: 16, parentFontWeight: 400, viewport: viewport}
	applyDeclaration(&computed, "display", `bl\6f ck`, context)
	applyDeclaration(&computed, "color", `r\65 d`, context)
	applyDeclaration(&computed, "margin", `1px/**/2px`, context)
	applyDeclaration(&computed, "opacity", `.25`, context)

	if computed.display != displayBlock {
		t.Fatalf("display = %v, want block", computed.display)
	}
	if computed.color != (color.NRGBA{R: 0xff, A: 0xff}) {
		t.Fatalf("color = %#v, want red", computed.color)
	}
	if computed.marginTop.value != 1 || computed.marginRight.value != 2 || computed.marginBottom.value != 1 || computed.marginLeft.value != 2 {
		t.Fatalf("margins = %#v %#v %#v %#v", computed.marginTop, computed.marginRight, computed.marginBottom, computed.marginLeft)
	}
	if computed.opacity != .25 {
		t.Fatalf("opacity = %v, want .25", computed.opacity)
	}
}

func TestEscapedPropertyValuesFlowFromStylesheetToSnapshot(t *testing.T) {
	t.Parallel()

	document := dom.NewDocument()
	html := dom.NewElement("html")
	head := dom.NewElement("head")
	styleElement := dom.NewElement("style")
	styleElement.AppendChild(dom.NewText(`#target {
		display: bl\6f ck;
		color: r\65 d;
		width: 25%;
		border: 2\70 x s\6f lid currentc\6f lor;
	}`))
	head.AppendChild(styleElement)
	body := dom.NewElement("body")
	target := dom.NewElement("div", dom.Attribute{Name: "id", Value: "target"})
	body.AppendChild(target)
	html.AppendChild(head)
	html.AppendChild(body)
	document.AppendChild(html)

	snapshot := Compute(document, Input{Environment: Environment{Width: 800, Height: 600, InitialFontSize: 16}})
	computed, ok := snapshot.Lookup(target)
	if !ok {
		t.Fatal("snapshot did not contain target")
	}
	if computed.display != displayBlock || computed.color != (color.NRGBA{R: 0xff, A: 0xff}) {
		t.Fatalf("display/color = %v/%#v", computed.display, computed.color)
	}
	if computed.width.unit != lengthPercent || computed.width.value != 25 {
		t.Fatalf("width = %#v, want 25%%", computed.width)
	}
	if computed.borderTop.width != px(2) || computed.borderTop.style != borderStyleSolid || computed.borderTop.hasColor {
		t.Fatalf("border top = %#v", computed.borderTop)
	}
}
