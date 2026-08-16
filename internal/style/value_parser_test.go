package style

import (
	"image/color"
	"strings"
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

func TestLengthMathParsesResolvesAndSerializesTypedExpressions(t *testing.T) {
	t.Parallel()

	viewport := Viewport{Width: 800, Height: 600, InitialFontSize: 16}
	tests := []struct {
		source      string
		emBase      float64
		percentBase float64
		wantValue   float64
		wantCSS     string
	}{
		{source: "calc(100% - 2em + 10vw)", emBase: 10, percentBase: 200, wantValue: 260, wantCSS: "calc(100% + 10vw - 20px)"},
		{source: "min(50%, 120px)", percentBase: 300, wantValue: 120, wantCSS: "min(50%, 120px)"},
		{source: "max(10px, 2vw)", percentBase: 300, wantValue: 16, wantCSS: "max(10px, 2vw)"},
		{source: "clamp(100px, 50%, 400px)", percentBase: 600, wantValue: 300, wantCSS: "clamp(100px, 50%, 400px)"},
		{source: "calc(2 * (10px + 5%))", percentBase: 200, wantValue: 40, wantCSS: "calc(10% + 20px)"},
		{source: "calc(min(50%, 400px) - 2em)", emBase: 10, percentBase: 600, wantValue: 280, wantCSS: "calc(min(50%, 400px) - 20px)"},
	}
	for _, test := range tests {
		t.Run(test.source, func(t *testing.T) {
			parsed, ok := parseLength(test.source, test.emBase, test.emBase, viewport)
			if !ok {
				t.Fatalf("parseLength(%q) rejected", test.source)
			}
			resolved, ok := parsed.Resolve(test.percentBase, float64(viewport.Width), float64(viewport.Height))
			if !ok || resolved != test.wantValue {
				t.Fatalf("Resolve(%q) = %v, %t, want %v, true", test.source, resolved, ok, test.wantValue)
			}
			if got := serializeComputedLength(parsed); got != test.wantCSS {
				t.Fatalf("serialize(%q) = %q, want %q", test.source, got, test.wantCSS)
			}
		})
	}
}

func TestLengthMathRejectsInvalidOrUnboundedExpressions(t *testing.T) {
	t.Parallel()

	viewport := Viewport{Width: 800, Height: 600, InitialFontSize: 16}
	invalid := []string{
		"calc()",
		"calc(1px+ 2px)",
		"calc(1px +2px)",
		"calc(1px + 2)",
		"calc(1px * 2px)",
		"calc(1px / 0)",
		"calc(1e308px * 1e308)",
		"calc(auto + 1px)",
		"min()",
		"min(1px,)",
		"min(1px, 2)",
		"clamp(1px, 2px)",
		"clamp(1px, 2px, 3px, 4px)",
	}
	for _, source := range invalid {
		if parsed, ok := parseLength(source, 16, 16, viewport); ok {
			t.Errorf("parseLength(%q) = %#v, true; want rejection", source, parsed)
		}
	}

	var expression strings.Builder
	expression.WriteString("calc(1px")
	for index := 0; index < maxLengthMathNodes; index++ {
		expression.WriteString(" + 1px")
	}
	expression.WriteByte(')')
	if _, ok := parseLength(expression.String(), 16, 16, viewport); ok {
		t.Fatal("expression beyond the CSS math node budget was accepted")
	}
}

func TestLengthMathFlowsThroughPropertyValidation(t *testing.T) {
	t.Parallel()

	viewport := Viewport{Width: 800, Height: 600, InitialFontSize: 16}
	for _, declaration := range []css.Declaration{
		{Property: "width", Value: "calc(100% - 20px)"},
		{Property: "height", Value: "min(50vh, 400px)"},
		{Property: "margin", Value: "calc(1em - 20px) auto"},
		{Property: "padding-left", Value: "max(0px, 2vw)"},
		{Property: "border-width", Value: "clamp(1px, 0.5vw, 5px)"},
	} {
		if !validComputedDeclaration(declaration, viewport) {
			t.Errorf("%s: %s was rejected", declaration.Property, declaration.Value)
		}
	}
	if validComputedDeclaration(css.Declaration{Property: "border-width", Value: "calc(1px + 2%)"}, viewport) {
		t.Fatal("percentage border width was accepted")
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
