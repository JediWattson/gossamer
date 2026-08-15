package render_test

import (
	"image/color"
	"testing"

	"github.com/JediWattson/gossamer/internal/css"
	"github.com/JediWattson/gossamer/internal/dom"
	"github.com/JediWattson/gossamer/internal/render"
)

func TestRenderCustomPropertiesInheritAndRemainCaseSensitive(t *testing.T) {
	t.Parallel()

	document, head, body := customPropertyDocument()
	addCustomPropertyStyle(head, `
:root { --Brand: #123456; }
#exact { color: var(--Brand); }
#different-case { color: var(--brand, #abcdef); }
#shadowed { --Brand: var(--missing); color: var(--Brand, #fedcba); }
`)
	exact := customPropertyTextElement(body, "p", "exact", "Exact variable")
	differentCase := customPropertyTextElement(body, "p", "different-case", "Case-sensitive fallback")
	shadowed := customPropertyTextElement(body, "p", "shadowed", "Invalid local shadow")

	frame := renderCustomPropertyFrame(t, document)
	assertCustomPropertyColor(t, frame, exact, "Exact variable", color.NRGBA{R: 0x12, G: 0x34, B: 0x56, A: 0xff})
	assertCustomPropertyColor(t, frame, differentCase, "Case-sensitive fallback", color.NRGBA{R: 0xab, G: 0xcd, B: 0xef, A: 0xff})
	assertCustomPropertyColor(t, frame, shadowed, "Invalid local shadow", color.NRGBA{R: 0xfe, G: 0xdc, B: 0xba, A: 0xff})
}

func TestRenderCustomPropertyCascadeUsesLayersImportanceAndInlineStyle(t *testing.T) {
	t.Parallel()

	document, head, body := customPropertyDocument()
	addCustomPropertyStyle(head, `
@layer early, late;
@layer late { #target { --tone: #222222; } }
@layer early { p { --tone: #111111 !important; } }
#target { color: var(--tone); }
`)
	target := customPropertyTextElement(body, "p", "target", "Cascaded variable")
	target.Attributes = append(target.Attributes, dom.Attribute{
		Name:  "style",
		Value: "--tone: #333333; --unused: ignored !important",
	})

	frame := renderCustomPropertyFrame(t, document)
	assertCustomPropertyColor(t, frame, target, "Cascaded variable", color.NRGBA{R: 0x11, G: 0x11, B: 0x11, A: 0xff})
}

func TestRenderCustomPropertyCascadeAxes(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		stylesheet  string
		inlineStyle string
		want        color.NRGBA
	}{
		{
			name: "normal layer order",
			stylesheet: `
@layer early, late;
@layer early { #target { --tone: #111111; } }
@layer late { #target { --tone: #222222; } }
#target { color: var(--tone); }
`,
			want: color.NRGBA{R: 0x22, G: 0x22, B: 0x22, A: 0xff},
		},
		{
			name:       "specificity",
			stylesheet: `p { --tone: #111111; } #target { --tone: #222222; color: var(--tone); }`,
			want:       color.NRGBA{R: 0x22, G: 0x22, B: 0x22, A: 0xff},
		},
		{
			name:       "source order",
			stylesheet: `#target { --tone: #111111; } #target { --tone: #222222; color: var(--tone); }`,
			want:       color.NRGBA{R: 0x22, G: 0x22, B: 0x22, A: 0xff},
		},
		{
			name:        "inline normal",
			stylesheet:  `#target { --tone: #111111; color: var(--tone); }`,
			inlineStyle: `--tone: #333333`,
			want:        color.NRGBA{R: 0x33, G: 0x33, B: 0x33, A: 0xff},
		},
		{
			name:        "important over inline normal",
			stylesheet:  `#target { --tone: #111111 !important; color: var(--tone); }`,
			inlineStyle: `--tone: #333333`,
			want:        color.NRGBA{R: 0x11, G: 0x11, B: 0x11, A: 0xff},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			got := renderConditionalTextColor(t, test.stylesheet, test.inlineStyle, render.Viewport{Width: 400, Height: 300})
			if got != test.want {
				t.Errorf("text color = %#v, want %#v", got, test.want)
			}
		})
	}
}

func TestRenderCustomPropertyFallbacksAndCycles(t *testing.T) {
	t.Parallel()

	document, head, body := customPropertyDocument()
	addCustomPropertyStyle(head, `
:root {
  --a: var(--b);
  --b: var(--a);
  --accent: var(--missing, var(--backup, #345678));
}
#cycle { color: var(--a, #abcdef); }
#nested { color: var(--accent); }
`)
	cycle := customPropertyTextElement(body, "p", "cycle", "Cycle fallback")
	nested := customPropertyTextElement(body, "p", "nested", "Nested fallback")

	frame := renderCustomPropertyFrame(t, document)
	assertCustomPropertyColor(t, frame, cycle, "Cycle fallback", color.NRGBA{R: 0xab, G: 0xcd, B: 0xef, A: 0xff})
	assertCustomPropertyColor(t, frame, nested, "Nested fallback", color.NRGBA{R: 0x34, G: 0x56, B: 0x78, A: 0xff})
}

func TestRenderVarInvalidAtComputedValueDoesNotReviveLosingDeclaration(t *testing.T) {
	t.Parallel()

	document, head, body := customPropertyDocument()
	addCustomPropertyStyle(head, `
body { color: #123456; }
:root { --empty: ; }
#target {
  color: #abcdef;
  color: var(--missing);
  width: 120px;
  width: var(--missing);
}
#visible {
  display: none;
  display: var(--missing);
}
#empty {
  color: #abcdef;
  color: var(--empty, #fedcba);
}
`)
	target := customPropertyTextElement(body, "div", "target", "Invalid computed value")
	visible := customPropertyTextElement(body, "div", "visible", "Unset display remains visible")
	empty := customPropertyTextElement(body, "p", "empty", "Empty variable")

	frame := renderCustomPropertyFrame(t, document)
	assertCustomPropertyColor(t, frame, target, "Invalid computed value", color.NRGBA{R: 0x12, G: 0x34, B: 0x56, A: 0xff})
	box := findBox(frame.Root, target)
	if box == nil {
		t.Fatal("target box not found")
	}
	if box.ContentBounds.Width <= 120 {
		t.Errorf("invalid winning width left the losing 120px declaration active: width = %.2f", box.ContentBounds.Width)
	}
	if fragment := findTextFragment(collectTextFragments(frame.Root), "Unset display remains visible"); fragment == nil {
		t.Fatalf("invalid winning display revived losing display:none for node %#v", visible)
	}
	assertCustomPropertyColor(t, frame, empty, "Empty variable", color.NRGBA{R: 0x12, G: 0x34, B: 0x56, A: 0xff})
}

func TestRenderVariableValidationHappensAtTheCorrectPhase(t *testing.T) {
	t.Parallel()

	document, head, body := customPropertyDocument()
	addCustomPropertyStyle(head, `
:root { --tone: #123456; }
body { color: #123456; }
#ordinary { color: #abcdef; color: var(color); }
#ordinary-syntax { color: #abcdef; color: ! var(--missing, #fedcba); }
#ordinary-close { color: #abcdef; color: var(--missing, #fedcba)); }
#custom { --tone: var(color); color: var(--tone); }
`)
	ordinary := customPropertyTextElement(body, "p", "ordinary", "Malformed ordinary variable")
	ordinarySyntax := customPropertyTextElement(body, "p", "ordinary-syntax", "Malformed ordinary syntax")
	ordinaryClose := customPropertyTextElement(body, "p", "ordinary-close", "Unmatched ordinary block")
	custom := customPropertyTextElement(body, "p", "custom", "Malformed custom variable")

	frame := renderCustomPropertyFrame(t, document)
	assertCustomPropertyColor(t, frame, ordinary, "Malformed ordinary variable", color.NRGBA{R: 0xab, G: 0xcd, B: 0xef, A: 0xff})
	assertCustomPropertyColor(t, frame, ordinarySyntax, "Malformed ordinary syntax", color.NRGBA{R: 0xab, G: 0xcd, B: 0xef, A: 0xff})
	assertCustomPropertyColor(t, frame, ordinaryClose, "Unmatched ordinary block", color.NRGBA{R: 0xab, G: 0xcd, B: 0xef, A: 0xff})
	assertCustomPropertyColor(t, frame, custom, "Malformed custom variable", color.NRGBA{R: 0x12, G: 0x34, B: 0x56, A: 0xff})
}

func TestRenderInvalidCustomPropertyValueDoesNotShadowInheritance(t *testing.T) {
	t.Parallel()

	document, head, body := customPropertyDocument()
	addCustomPropertyStyle(head, `
body { --tone: #123456; color: #abcdef; }
#target { --tone: !; color: var(--tone, #fedcba); }
`)
	target := customPropertyTextElement(body, "p", "target", "Invalid custom property syntax")

	frame := renderCustomPropertyFrame(t, document)
	assertCustomPropertyColor(t, frame, target, "Invalid custom property syntax", color.NRGBA{R: 0x12, G: 0x34, B: 0x56, A: 0xff})
}

func TestRenderVarSubstitutionPreservesCSSComponentBoundaries(t *testing.T) {
	t.Parallel()

	document, head, body := customPropertyDocument()
	addCustomPropertyStyle(head, `
:root { --number: 1; --hash: #; }
body { color: #123456; }
#target {
  color: #abcdef;
  color: var(--hash)fedcba;
  width: 120px;
  width: var(--number)0px;
}
`)
	target := customPropertyTextElement(body, "div", "target", "Token boundary")

	frame := renderCustomPropertyFrame(t, document)
	assertCustomPropertyColor(t, frame, target, "Token boundary", color.NRGBA{R: 0x12, G: 0x34, B: 0x56, A: 0xff})
	box := findBox(frame.Root, target)
	if box == nil {
		t.Fatal("target box not found")
	}
	if box.ContentBounds.Width <= 120 {
		t.Errorf("token-fused winning width remained active: width = %.2f", box.ContentBounds.Width)
	}
}

func TestRenderVarResolvesBeforeShorthandExpansion(t *testing.T) {
	t.Parallel()

	document, head, body := customPropertyDocument()
	addCustomPropertyStyle(head, `
:root {
  --space: 10px 20px 30px 40px;
  --padding: 1px 2px 3px 4px;
  --border: 5px solid #123456;
  --font: bold 20px/1.5 sans-serif;
}
#target {
  display: block;
  width: 100px;
  margin: var(--space);
  padding: var(--padding);
  border: var(--border);
  font: var(--font);
}
`)
	target := customPropertyTextElement(body, "div", "target", "Variable shorthand")

	frame := renderCustomPropertyFrame(t, document)
	box := findBox(frame.Root, target)
	if box == nil {
		t.Fatal("target box not found")
	}
	assertNear(t, "variable margin left", box.Bounds.X, 40)
	if box.Padding != (render.Edges{Top: 1, Right: 2, Bottom: 3, Left: 4}) {
		t.Errorf("variable padding = %#v, want 1px 2px 3px 4px", box.Padding)
	}
	if box.Border != (render.Edges{Top: 5, Right: 5, Bottom: 5, Left: 5}) {
		t.Errorf("variable border = %#v, want 5px on every side", box.Border)
	}
	fragment := findTextFragment(collectTextFragments(frame.Root), "Variable")
	if fragment == nil {
		t.Fatal("target text fragment not found")
	}
	assertNear(t, "variable font size", fragment.FontSize, 20)
	assertNear(t, "variable line height", fragment.Height, 30)
	if fragment.FontWeight != render.FontWeightBold {
		t.Errorf("variable font weight = %v, want bold", fragment.FontWeight)
	}
}

func TestRenderExternalCustomPropertyFeedsLaterEmbeddedStyles(t *testing.T) {
	t.Parallel()

	document, head, body := customPropertyDocument()
	owner := dom.NewElement("link",
		dom.Attribute{Name: "rel", Value: "stylesheet"},
		dom.Attribute{Name: "href", Value: "/theme.css"},
	)
	head.AppendChild(owner)
	addCustomPropertyStyle(head, `#target { color: var(--external-tone); }`)
	target := customPropertyTextElement(body, "p", "target", "External variable")
	stylesheet, err := css.Parse(`:root { --external-tone: #2468ac; }`)
	if err != nil {
		t.Fatalf("css.Parse() error = %v", err)
	}

	frame, err := render.RenderWithResources(document, render.Viewport{Width: 400, Height: 300}, render.Resources{
		Stylesheets: map[*dom.Node]css.Stylesheet{owner: stylesheet},
	})
	if err != nil {
		t.Fatalf("RenderWithResources() error = %v", err)
	}
	assertCustomPropertyColor(t, frame, target, "External variable", color.NRGBA{R: 0x24, G: 0x68, B: 0xac, A: 0xff})
}

func customPropertyDocument() (*dom.Node, *dom.Node, *dom.Node) {
	document := dom.NewDocument()
	html := dom.NewElement("html")
	document.AppendChild(html)
	head := dom.NewElement("head")
	body := dom.NewElement("body", dom.Attribute{Name: "style", Value: "margin: 0"})
	html.AppendChild(head)
	html.AppendChild(body)
	return document, head, body
}

func addCustomPropertyStyle(head *dom.Node, source string) {
	style := dom.NewElement("style")
	style.AppendChild(dom.NewText(source))
	head.AppendChild(style)
}

func customPropertyTextElement(parent *dom.Node, name, id, text string) *dom.Node {
	element := dom.NewElement(name, dom.Attribute{Name: "id", Value: id})
	element.AppendChild(dom.NewText(text))
	parent.AppendChild(element)
	return element
}

func renderCustomPropertyFrame(t *testing.T, document *dom.Node) *render.Frame {
	t.Helper()
	frame, err := render.Render(document, render.Viewport{Width: 400, Height: 300})
	if err != nil {
		t.Fatalf("Render() error = %v", err)
	}
	return frame
}

func assertCustomPropertyColor(t *testing.T, frame *render.Frame, node *dom.Node, text string, want color.NRGBA) {
	t.Helper()
	fragment := findTextFragment(collectTextFragments(frame.Root), text)
	if fragment == nil {
		t.Fatalf("text fragment %q for node %#v not found", text, node)
	}
	if fragment.Color != want {
		t.Errorf("%q color = %#v, want %#v", text, fragment.Color, want)
	}
}
