package render_test

import (
	"fmt"
	"image/color"
	"strings"
	"testing"

	"github.com/JediWattson/gossamer/internal/dom"
	"github.com/JediWattson/gossamer/internal/render"
)

func TestRenderRevertLayerTraversesNormalAndImportantLayers(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		stylesheet string
		want       color.NRGBA
	}{
		{
			name: "normal declaration reveals preceding layer",
			stylesheet: `
@layer base, theme;
@layer base { #target { color: #112233; } }
@layer theme { #target { color: #445566; color: revert-layer; } }
`,
			want: color.NRGBA{R: 0x11, G: 0x22, B: 0x33, A: 0xff},
		},
		{
			name: "important declaration crosses mirrored layer order",
			stylesheet: `
@layer base, theme;
@layer base { #target { color: #112233; } }
@layer theme { #target { color: #445566 !important; color: revert-layer !important; } }
#target { color: #778899 !important; }
`,
			want: color.NRGBA{R: 0x11, G: 0x22, B: 0x33, A: 0xff},
		},
		{
			name: "important first layer falls back to prior origin",
			stylesheet: `
@layer first { #target { color: #445566; color: revert-layer !important; } }
#target { color: #778899 !important; }
`,
			want: color.NRGBA{A: 0xff},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			got := renderConditionalTextColor(t, test.stylesheet, "", render.Viewport{Width: 400, Height: 300})
			if got != test.want {
				t.Errorf("text color = %#v, want %#v", got, test.want)
			}
		})
	}
}

func TestRenderElementAttachedAndUnlayeredRevertLayer(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		stylesheet  string
		inlineStyle string
	}{
		{
			name: "normal unlayered declaration reveals named layer",
			stylesheet: `
@layer base { #target { color: #112233; } }
#target { color: #445566; color: revert-layer; }
`,
		},
		{
			name:        "normal inline declaration reveals author rule",
			stylesheet:  `#target { color: #112233; }`,
			inlineStyle: `color: #445566; color: revert-layer`,
		},
		{
			name:        "important inline declaration preserves author important rule",
			stylesheet:  `@layer base { #target { color: #112233 !important; } }`,
			inlineStyle: `color: #445566 !important; color: revert-layer !important`,
		},
	}

	want := color.NRGBA{R: 0x11, G: 0x22, B: 0x33, A: 0xff}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			got := renderConditionalTextColor(t, test.stylesheet, test.inlineStyle, render.Viewport{Width: 400, Height: 300})
			if got != want {
				t.Errorf("text color = %#v, want %#v", got, want)
			}
		})
	}
}

func TestRenderRevertRevealsUserAgentLinkStyle(t *testing.T) {
	t.Parallel()

	document, head, body := resourceTestDocument()
	style := dom.NewElement("style")
	style.AppendChild(dom.NewText(`a { color: #ff0000; color: revert; }`))
	head.AppendChild(style)
	link := dom.NewElement("a",
		dom.Attribute{Name: "id", Value: "target"},
		dom.Attribute{Name: "href", Value: "https://example.test/"},
	)
	link.AppendChild(dom.NewText("Reverted link"))
	body.AppendChild(link)

	frame, err := render.Render(document, render.Viewport{Width: 400, Height: 300})
	if err != nil {
		t.Fatalf("Render() error = %v", err)
	}
	fragment := resourceTextFragment(frame.Root, "Reverted link")
	if fragment == nil {
		t.Fatal("reverted link text fragment not found")
	}
	want := color.NRGBA{B: 0xee, A: 0xff}
	if fragment.Color != want {
		t.Errorf("reverted link color = %#v, want UA color %#v", fragment.Color, want)
	}
}

func TestRenderExhaustedRevertLayerPreservesUserAgentDisplay(t *testing.T) {
	t.Parallel()

	document, head, body := customPropertyDocument()
	addCustomPropertyStyle(head, `#target { display: inline; display: revert-layer; }`)
	target := customPropertyTextElement(body, "div", "target", "User-agent block")

	frame := renderCustomPropertyFrame(t, document)
	if box := findBox(frame.Root, target); box == nil {
		t.Fatal("revert-layer exhausted the author origin as unset; div lost its user-agent block box")
	}
}

func TestRenderCSSWideInitialInheritAndUnset(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		keyword string
		want    color.NRGBA
	}{
		{name: "initial", keyword: "initial", want: color.NRGBA{A: 0xff}},
		{name: "inherit", keyword: "inherit", want: color.NRGBA{R: 0x12, G: 0x34, B: 0x56, A: 0xff}},
		{name: "escaped inherit", keyword: `\69nherit`, want: color.NRGBA{R: 0x12, G: 0x34, B: 0x56, A: 0xff}},
		{name: "unset inherited property", keyword: "unset", want: color.NRGBA{R: 0x12, G: 0x34, B: 0x56, A: 0xff}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			stylesheet := "body { color: #123456; } #target { color: #abcdef; color: " + test.keyword + "; }"
			got := renderConditionalTextColor(t, stylesheet, "", render.Viewport{Width: 400, Height: 300})
			if got != test.want {
				t.Errorf("text color = %#v, want %#v", got, test.want)
			}
		})
	}
}

func TestRenderRevertLayerStopsAtInvalidComputedWinner(t *testing.T) {
	t.Parallel()

	stylesheet := `
body { color: #123456; }
@layer low, middle, high;
@layer low { #target { color: #abcdef; } }
@layer middle { #target { color: var(--missing); } }
@layer high { #target { color: #fedcba; color: rev\65 rt-layer; } }
`
	got := renderConditionalTextColor(t, stylesheet, "", render.Viewport{Width: 400, Height: 300})
	want := color.NRGBA{R: 0x12, G: 0x34, B: 0x56, A: 0xff}
	if got != want {
		t.Errorf("text color = %#v, want invalid middle-layer winner to compute as unset %#v", got, want)
	}
}

func TestRenderCustomPropertyRevertAndRevertLayer(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		stylesheet string
		want       color.NRGBA
	}{
		{
			name: "revert layer selects preceding custom property",
			stylesheet: `
@layer base, theme;
@layer base { #target { --tone: #112233; } }
@layer theme { #target { --tone: #445566; --tone: revert-layer; } }
#target { color: var(--tone); }
`,
			want: color.NRGBA{R: 0x11, G: 0x22, B: 0x33, A: 0xff},
		},
		{
			name: "revert removes local custom property and inherits",
			stylesheet: `
body { --tone: #123456; }
#target { --tone: #abcdef; --tone: revert; color: var(--tone); }
`,
			want: color.NRGBA{R: 0x12, G: 0x34, B: 0x56, A: 0xff},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			got := renderConditionalTextColor(t, test.stylesheet, "", render.Viewport{Width: 400, Height: 300})
			if got != test.want {
				t.Errorf("text color = %#v, want %#v", got, test.want)
			}
		})
	}
}

func TestRenderVarProducedCustomPropertyRevertLayerResolvesDependenciesFirst(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		stylesheet string
	}{
		{
			name: "source sorts before dependent",
			stylesheet: `
@layer base, theme;
@layer base { #target { --a: green; --b: blue; } }
@layer theme {
  #target {
    --a: var(--missing, revert-layer);
    --b: var(--a);
    color: var(--b);
  }
}
`,
		},
		{
			name: "dependent sorts before renamed source",
			stylesheet: `
@layer base, theme;
@layer base { #target { --z-source: #008000; --a-dependent: #0000ff; } }
@layer theme {
  #target {
    --z-source: var(--missing, revert-layer);
    --a-dependent: var(--z-source);
    color: var(--a-dependent);
  }
}
`,
		},
	}

	want := color.NRGBA{G: 0x80, A: 0xff}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			got := renderConditionalTextColor(t, test.stylesheet, "", render.Viewport{Width: 400, Height: 300})
			if got != want {
				t.Errorf("text color = %#v, want dependency's post-rollback value %#v", got, want)
			}
		})
	}
}

func TestRenderVarProducedCSSWideKeywordAppliesToSourceCustomProperty(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		stylesheet string
		want       color.NRGBA
	}{
		{
			name: "initial makes source missing so dependent uses fallback",
			stylesheet: `
body { --z-source: #123456; }
#target {
  --z-source: var(--missing, initial);
  --a-dependent: var(--z-source, #fedcba);
  color: var(--a-dependent);
}
`,
			want: color.NRGBA{R: 0xfe, G: 0xdc, B: 0xba, A: 0xff},
		},
		{
			name: "inherit selects parent source before dependent computes",
			stylesheet: `
body { --z-source: #123456; }
#target {
  --z-source: var(--missing, inherit);
  --a-dependent: var(--z-source);
  color: var(--a-dependent);
}
`,
			want: color.NRGBA{R: 0x12, G: 0x34, B: 0x56, A: 0xff},
		},
		{
			name: "unset inherits custom property before dependent computes",
			stylesheet: `
body { --z-source: #123456; }
#target {
  --z-source: var(--missing, unset);
  --a-dependent: var(--z-source);
  color: var(--a-dependent);
}
`,
			want: color.NRGBA{R: 0x12, G: 0x34, B: 0x56, A: 0xff},
		},
		{
			name: "revert selects parent source before dependent computes",
			stylesheet: `
body { --z-source: #123456; }
#target {
  --z-source: var(--missing, revert);
  --a-dependent: var(--z-source);
  color: var(--a-dependent);
}
`,
			want: color.NRGBA{R: 0x12, G: 0x34, B: 0x56, A: 0xff},
		},
		{
			name: "revert layer selects prior candidate before dependent computes",
			stylesheet: `
@layer base, theme;
@layer base { #target { --z-source: #112233; } }
@layer theme {
  #target {
    --z-source: var(--missing, revert-layer);
    --a-dependent: var(--z-source);
    color: var(--a-dependent);
  }
}
`,
			want: color.NRGBA{R: 0x11, G: 0x22, B: 0x33, A: 0xff},
		},
		{
			name: "revert layer keeps invalid lower winner from exposing parent",
			stylesheet: `
body { --z-source: #123456; }
@layer base, theme;
@layer base { #target { --z-source: var(--base-missing); } }
@layer theme { #target { --z-source: var(--missing, revert-layer); } }
#target {
  --a-dependent: var(--z-source, #fedcba);
  color: var(--a-dependent);
}
`,
			want: color.NRGBA{R: 0xfe, G: 0xdc, B: 0xba, A: 0xff},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			got := renderConditionalTextColor(t, test.stylesheet, "", render.Viewport{Width: 400, Height: 300})
			if got != test.want {
				t.Errorf("text color = %#v, want %#v", got, test.want)
			}
		})
	}
}

func TestRenderCustomPropertyCSSWideResolutionHasBoundedWork(t *testing.T) {
	t.Parallel()

	const propertyCount = 256
	var stylesheet strings.Builder
	stylesheet.WriteString(":root { --empty: ;")
	for index := 0; index < propertyCount; index++ {
		name := fmt.Sprintf("--p%03d", index)
		if index == 0 {
			fmt.Fprintf(&stylesheet, "%s:var(--missing,initial)var(--empty);", name)
			continue
		}
		previous := fmt.Sprintf("--p%03d", index-1)
		fmt.Fprintf(&stylesheet, "%s:var(--missing,initial)var(--empty,var(%s));", name, previous)
	}
	stylesheet.WriteString("--d:var(--p127,)var(--p128,revert);")
	stylesheet.WriteString("} #target { color:var(--d,#123456); }")

	got := renderConditionalTextColor(t, stylesheet.String(), "", render.Viewport{Width: 400, Height: 300})
	want := color.NRGBA{R: 0x12, G: 0x34, B: 0x56, A: 0xff}
	if got != want {
		t.Errorf("text color = %#v, want bounded CSS-wide chain fallback %#v", got, want)
	}
}

func TestRenderVarFallbackCanProduceRevertKeywords(t *testing.T) {
	t.Parallel()

	t.Run("revert layer", func(t *testing.T) {
		t.Parallel()
		stylesheet := `
@layer base, theme;
@layer base { #target { color: #112233; } }
@layer theme { #target { color: #445566; color: var(--missing, rev\65 rt-layer); } }
`
		got := renderConditionalTextColor(t, stylesheet, "", render.Viewport{Width: 400, Height: 300})
		want := color.NRGBA{R: 0x11, G: 0x22, B: 0x33, A: 0xff}
		if got != want {
			t.Errorf("text color = %#v, want %#v", got, want)
		}
	})

	t.Run("revert", func(t *testing.T) {
		t.Parallel()
		document, head, body := resourceTestDocument()
		style := dom.NewElement("style")
		style.AppendChild(dom.NewText(`a { color: #ff0000; color: var(--missing, rev\65 rt); }`))
		head.AppendChild(style)
		link := dom.NewElement("a", dom.Attribute{Name: "href", Value: "https://example.test/"})
		link.AppendChild(dom.NewText("Fallback reverted link"))
		body.AppendChild(link)

		frame, err := render.Render(document, render.Viewport{Width: 400, Height: 300})
		if err != nil {
			t.Fatalf("Render() error = %v", err)
		}
		fragment := resourceTextFragment(frame.Root, "Fallback reverted link")
		if fragment == nil {
			t.Fatal("fallback reverted link text fragment not found")
		}
		want := color.NRGBA{B: 0xee, A: 0xff}
		if fragment.Color != want {
			t.Errorf("reverted link color = %#v, want UA color %#v", fragment.Color, want)
		}
	})
}

func TestRenderVarFallbackRevertLayerExpandsShorthandPerTarget(t *testing.T) {
	t.Parallel()

	document, head, body := customPropertyDocument()
	addCustomPropertyStyle(head, `
@layer base, theme;
@layer base { #target { margin-left: 40px; } }
@layer theme { #target { margin-left: 20px; margin: var(--missing, revert-layer); } }
#target { display: block; width: 100px; }
`)
	target := customPropertyTextElement(body, "div", "target", "Reverted shorthand")

	frame := renderCustomPropertyFrame(t, document)
	box := findBox(frame.Root, target)
	if box == nil {
		t.Fatal("target box not found")
	}
	assertNear(t, "reverted shorthand margin left", box.Bounds.X, 40)
}
