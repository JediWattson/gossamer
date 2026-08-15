package render_test

import (
	"testing"

	"github.com/JediWattson/gossamer/internal/dom"
	"github.com/JediWattson/gossamer/internal/render"
)

func TestRenderNeverSSLStyleFontShorthand(t *testing.T) {
	t.Parallel()

	fragment := renderFontShorthandFragment(t, "font: 1.2em/1.62 sans-serif", "", "NeverSSL style")
	assertNear(t, "font shorthand size", fragment.FontSize, 19.2)
	assertNear(t, "font shorthand line height", fragment.Height, 19.2*1.62)
	if fragment.FontWeight != render.FontWeightNormal {
		t.Errorf("font shorthand weight = %v, want normal", fragment.FontWeight)
	}
}

func TestRenderBoldFontShorthand(t *testing.T) {
	t.Parallel()

	fragment := renderFontShorthandFragment(t, "font: bold 20px/1.5 sans-serif", "", "Bold shorthand")
	assertNear(t, "bold shorthand size", fragment.FontSize, 20)
	assertNear(t, "bold shorthand line height", fragment.Height, 30)
	if fragment.FontWeight != render.FontWeightBold {
		t.Errorf("font shorthand weight = %v, want bold", fragment.FontWeight)
	}
}

func TestRenderNumericNormalWeightInFontShorthandResetsInheritedBold(t *testing.T) {
	t.Parallel()

	fragment := renderFontShorthandFragment(
		t,
		"font-weight: bold",
		"font: 400 18px sans-serif",
		"Numeric normal weight",
	)
	if fragment.FontWeight != render.FontWeightNormal {
		t.Errorf("numeric shorthand weight = %v, want normal", fragment.FontWeight)
	}
}

func TestRenderInvalidFontShorthandIsRejectedAtomically(t *testing.T) {
	t.Parallel()

	fragment := renderFontShorthandFragment(
		t,
		"font-size: 18px; line-height: 2; font-weight: bold",
		"font: 30px",
		"Invalid shorthand",
	)
	assertNear(t, "font size after invalid shorthand", fragment.FontSize, 18)
	assertNear(t, "line height after invalid shorthand", fragment.Height, 36)
	if fragment.FontWeight != render.FontWeightBold {
		t.Errorf("font weight after invalid shorthand = %v, want bold", fragment.FontWeight)
	}
}

func TestRenderFontShorthandAcceptsWhitespaceAroundSlash(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		font       string
		wantHeight float64
	}{
		{name: "compact slash", font: "18px/1.75 sans-serif", wantHeight: 31.5},
		{name: "spaced slash", font: "18px / 30px sans-serif", wantHeight: 30},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			fragment := renderFontShorthandFragment(t, "font: "+test.font, "", "Slash spacing")
			assertNear(t, "slash shorthand size", fragment.FontSize, 18)
			assertNear(t, "slash shorthand line height", fragment.Height, test.wantHeight)
		})
	}
}

func TestRenderFontShorthandResetsAndYieldsToLaterLonghands(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		style      string
		wantSize   float64
		wantHeight float64
		wantWeight render.FontWeight
	}{
		{
			name:       "later shorthand resets omitted longhands",
			style:      "font-weight: bold; line-height: 2; font: 20px sans-serif",
			wantSize:   20,
			wantWeight: render.FontWeightNormal,
		},
		{
			name:       "later longhands override shorthand",
			style:      "font: 20px sans-serif; font-weight: bold; line-height: 2",
			wantSize:   20,
			wantHeight: 40,
			wantWeight: render.FontWeightBold,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			fragment := renderFontShorthandFragment(t, test.style, "", "Cascade")
			assertNear(t, "cascade font size", fragment.FontSize, test.wantSize)
			if test.wantHeight > 0 {
				assertNear(t, "cascade line height", fragment.Height, test.wantHeight)
			} else {
				control := renderFontShorthandFragment(t, "font-size: 20px", "", "Initial line height")
				assertNear(t, "reset line height", fragment.Height, control.Height)
			}
			if fragment.FontWeight != test.wantWeight {
				t.Errorf("cascade font weight = %v, want %v", fragment.FontWeight, test.wantWeight)
			}
		})
	}
}

func TestRenderFontShorthandParticipatesInImportantCascade(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		style      string
		wantSize   float64
		wantHeight float64
		wantWeight render.FontWeight
	}{
		{
			name:       "important shorthand beats later normal longhands",
			style:      "font: 18px/2 sans-serif !important; font-size: 30px; font-weight: bold; line-height: 3",
			wantSize:   18,
			wantHeight: 36,
			wantWeight: render.FontWeightNormal,
		},
		{
			name:       "important longhands beat later normal shorthand",
			style:      "font-size: 22px !important; font-weight: bold !important; line-height: 2 !important; font: 30px/1 sans-serif",
			wantSize:   22,
			wantHeight: 44,
			wantWeight: render.FontWeightBold,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			fragment := renderFontShorthandFragment(t, test.style, "", "Important cascade")
			assertNear(t, "important cascade font size", fragment.FontSize, test.wantSize)
			assertNear(t, "important cascade line height", fragment.Height, test.wantHeight)
			if fragment.FontWeight != test.wantWeight {
				t.Errorf("important cascade font weight = %v, want %v", fragment.FontWeight, test.wantWeight)
			}
		})
	}
}

func TestRenderFontShorthandExpandsBeforeSpecificityCascade(t *testing.T) {
	t.Parallel()

	document, body := boxModelDocument()
	style := dom.NewElement("style")
	style.AppendChild(dom.NewText(`
#target { font-size: 18px; line-height: 2; font-weight: bold; }
.target { font: 30px/1 sans-serif; }
`))
	document.Children[0].Children[0].AppendChild(style)
	container := dom.NewElement("div",
		dom.Attribute{Name: "id", Value: "target"},
		dom.Attribute{Name: "class", Value: "target"},
	)
	container.AppendChild(dom.NewText("Specific shorthand"))
	body.AppendChild(container)

	frame, err := render.Render(document, render.Viewport{Width: 480, Height: 160})
	if err != nil {
		t.Fatalf("Render() error = %v", err)
	}
	fragment := findTextFragment(collectTextFragments(frame.Root), "Specific shorthand")
	if fragment == nil {
		t.Fatal("specific shorthand fragment not found")
	}
	assertNear(t, "specific cascade font size", fragment.FontSize, 18)
	assertNear(t, "specific cascade line height", fragment.Height, 36)
	if fragment.FontWeight != render.FontWeightBold {
		t.Errorf("specific cascade font weight = %v, want bold", fragment.FontWeight)
	}
}

func TestRenderFontShorthandPercentageLineHeightComputesBeforeInheritance(t *testing.T) {
	t.Parallel()

	fragment := renderFontShorthandFragment(
		t,
		"font: 20px/150% \"Open Sans\", sans-serif",
		"font-size: 10px",
		"Percentage line height",
	)
	assertNear(t, "percentage shorthand child size", fragment.FontSize, 10)
	assertNear(t, "percentage shorthand inherited line height", fragment.Height, 30)
}

func TestRenderFontShorthandInheritanceUsesComputedSizeAndLineHeightKind(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		parent     string
		child      string
		wantSize   float64
		wantHeight float64
	}{
		{
			name:       "relative size inherits as computed pixels",
			parent:     "font: 1.5em/2 sans-serif",
			wantSize:   24,
			wantHeight: 48,
		},
		{
			name:       "absolute line height remains absolute",
			parent:     "font: 20px/32px sans-serif",
			child:      "font-size: 10px",
			wantSize:   10,
			wantHeight: 32,
		},
		{
			name:       "unitless line height remains a factor",
			parent:     "font: 20px/2 sans-serif",
			child:      "font-size: 10px",
			wantSize:   10,
			wantHeight: 20,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			fragment := renderFontShorthandFragment(t, test.parent, test.child, "Inherited shorthand")
			assertNear(t, "inherited font size", fragment.FontSize, test.wantSize)
			assertNear(t, "inherited line height", fragment.Height, test.wantHeight)
		})
	}
}

func renderFontShorthandFragment(t *testing.T, parentStyle, childStyle, text string) *render.TextFragment {
	t.Helper()

	document, body := boxModelDocument()
	parent := dom.NewElement("div", dom.Attribute{Name: "style", Value: parentStyle})
	child := dom.NewElement("span", dom.Attribute{Name: "style", Value: childStyle})
	child.AppendChild(dom.NewText(text))
	parent.AppendChild(child)
	body.AppendChild(parent)

	frame, err := render.Render(document, render.Viewport{Width: 480, Height: 160})
	if err != nil {
		t.Fatalf("Render() error = %v", err)
	}
	fragment := findTextFragment(collectTextFragments(frame.Root), text)
	if fragment == nil {
		t.Fatalf("text fragment %q not found", text)
	}
	return fragment
}
