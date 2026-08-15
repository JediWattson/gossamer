package render_test

import (
	"image/color"
	"testing"

	"github.com/JediWattson/gossamer/internal/css"
	"github.com/JediWattson/gossamer/internal/dom"
	"github.com/JediWattson/gossamer/internal/render"
)

func TestRenderCascadeLayerPrecedence(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		stylesheet  string
		inlineStyle string
		want        color.NRGBA
	}{
		{
			name: "later normal layer outranks specificity and source order",
			stylesheet: `
@layer early, late;
@layer late { p { color: #222222; } }
@layer early { #target { color: #111111; } }
`,
			want: color.NRGBA{R: 0x22, G: 0x22, B: 0x22, A: 0xff},
		},
		{
			name: "invalid important declaration in later layer does not mask valid fallback",
			stylesheet: `
@layer early, late;
@layer early { p { color: #111111; } }
@layer late { #target { color: not-a-color !important; } }
`,
			want: color.NRGBA{R: 0x11, G: 0x11, B: 0x11, A: 0xff},
		},
		{
			name: "invalid later declaration in same layer does not mask valid fallback",
			stylesheet: `
@layer app {
  p { color: #111111; }
  p { color: not-a-color; }
}
`,
			want: color.NRGBA{R: 0x11, G: 0x11, B: 0x11, A: 0xff},
		},
		{
			name: "unlayered normal outranks layered normal",
			stylesheet: `
@layer components { #target { color: #111111; } }
p { color: #222222; }
`,
			want: color.NRGBA{R: 0x22, G: 0x22, B: 0x22, A: 0xff},
		},
		{
			name:        "inline normal outranks unlayered normal",
			stylesheet:  `#target { color: #111111; }`,
			inlineStyle: `color: #222222`,
			want:        color.NRGBA{R: 0x22, G: 0x22, B: 0x22, A: 0xff},
		},
		{
			name: "earlier important layer outranks later specificity and source order",
			stylesheet: `
@layer early, late;
@layer early { p { color: #111111 !important; } }
@layer late { #target { color: #222222 !important; } }
`,
			want: color.NRGBA{R: 0x11, G: 0x11, B: 0x11, A: 0xff},
		},
		{
			name: "layered important outranks unlayered important",
			stylesheet: `
@layer early { p { color: #111111 !important; } }
#target { color: #222222 !important; }
`,
			want: color.NRGBA{R: 0x11, G: 0x11, B: 0x11, A: 0xff},
		},
		{
			name: "inline important outranks layered important",
			stylesheet: `
@layer early { #target { color: #111111 !important; } }
p { color: #333333 !important; }
`,
			inlineStyle: `color: #222222 !important`,
			want:        color.NRGBA{R: 0x22, G: 0x22, B: 0x22, A: 0xff},
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

func TestRenderMediaQueriesAtWideAndNarrowViewports(t *testing.T) {
	t.Parallel()

	base := color.NRGBA{R: 0x11, G: 0x11, B: 0x11, A: 0xff}
	active := color.NRGBA{R: 0xab, G: 0xcd, B: 0xef, A: 0xff}
	type mediaCase struct {
		name         string
		query        string
		wideActive   bool
		narrowActive bool
	}
	tests := []mediaCase{
		{name: "screen min width", query: "screen and (min-width: 700px)", wideActive: true},
		{name: "all max width", query: "all and (max-width: 400px)", narrowActive: true},
		{name: "minimum height", query: "screen and (min-height: 620px)", narrowActive: true},
		{name: "maximum height inclusive", query: "screen and (max-height: 600px)", wideActive: true},
		{name: "landscape orientation", query: "(orientation: landscape)", wideActive: true},
		{name: "portrait orientation", query: "screen and (orientation: portrait)", narrowActive: true},
		{name: "comma list is or", query: "print, screen and (max-width: 400px)", narrowActive: true},
		{name: "not negates whole query", query: "not screen and (max-width: 400px)", wideActive: true},
		{name: "and combines features", query: "screen and (min-width: 700px) and (max-height: 600px)", wideActive: true},
	}

	viewports := []struct {
		name     string
		viewport render.Viewport
		active   func(mediaCase) bool
	}{
		{name: "wide landscape", viewport: render.Viewport{Width: 800, Height: 600}, active: func(test mediaCase) bool { return test.wideActive }},
		{name: "narrow portrait", viewport: render.Viewport{Width: 360, Height: 640}, active: func(test mediaCase) bool { return test.narrowActive }},
	}

	for _, test := range tests {
		test := test
		for _, viewport := range viewports {
			viewport := viewport
			t.Run(test.name+"/"+viewport.name, func(t *testing.T) {
				t.Parallel()
				stylesheet := `p { color: #111111; } @media ` + test.query + ` { p { color: #abcdef; } }`
				got := renderConditionalTextColor(t, stylesheet, "", viewport.viewport)
				want := base
				if viewport.active(test) {
					want = active
				}
				if got != want {
					t.Errorf("text color = %#v, want %#v for @media %s", got, want, test.query)
				}
			})
		}
	}
}

func TestRenderNestedMediaWithinLayer(t *testing.T) {
	t.Parallel()

	stylesheet := `
@layer base, responsive;
@layer base { #target { color: #111111; } }
@layer responsive {
  @media screen and (min-width: 700px) {
    @media (orientation: landscape), (min-height: 900px) {
      p { color: #abcdef; }
    }
  }
}
`
	tests := []struct {
		name     string
		viewport render.Viewport
		want     color.NRGBA
	}{
		{name: "all nested conditions match", viewport: render.Viewport{Width: 800, Height: 600}, want: color.NRGBA{R: 0xab, G: 0xcd, B: 0xef, A: 0xff}},
		{name: "outer media condition does not match", viewport: render.Viewport{Width: 360, Height: 640}, want: color.NRGBA{R: 0x11, G: 0x11, B: 0x11, A: 0xff}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			got := renderConditionalTextColor(t, stylesheet, "", test.viewport)
			if got != test.want {
				t.Errorf("text color = %#v, want %#v", got, test.want)
			}
		})
	}
}

func TestRenderNamedLayerOrderMergesAcrossExternalStylesheetOwners(t *testing.T) {
	t.Parallel()

	document, head, body := resourceTestDocument()
	first := dom.NewElement("link",
		dom.Attribute{Name: "rel", Value: "stylesheet"},
		dom.Attribute{Name: "href", Value: "/first.css"},
	)
	second := dom.NewElement("link",
		dom.Attribute{Name: "rel", Value: "stylesheet"},
		dom.Attribute{Name: "href", Value: "/second.css"},
	)
	head.AppendChild(first)
	head.AppendChild(second)
	paragraph := dom.NewElement("p", dom.Attribute{Name: "id", Value: "target"})
	paragraph.AppendChild(dom.NewText("Cross-owner target"))
	body.AppendChild(paragraph)

	resources := render.Resources{Stylesheets: map[*dom.Node]css.Stylesheet{
		first: parseResourceStylesheet(t, `
@layer reset, theme;
@layer reset { #target { color: #111111; } }
`),
		second: parseResourceStylesheet(t, `
@layer theme { p { color: #222222; } }
@layer reset { #target { color: #333333; } }
`),
	}}
	frame, err := render.RenderWithResources(document, render.Viewport{Width: 800, Height: 600}, resources)
	if err != nil {
		t.Fatalf("RenderWithResources() error = %v", err)
	}
	fragment := resourceTextFragment(frame.Root, "Cross-owner target")
	if fragment == nil {
		t.Fatal("cross-owner target text fragment not found")
	}
	want := color.NRGBA{R: 0x22, G: 0x22, B: 0x22, A: 0xff}
	if fragment.Color != want {
		t.Errorf("text color = %#v, want %#v; reopening reset in the later owner must not move it after theme", fragment.Color, want)
	}
}

func TestRenderSameNamedLayerUsesSpecificityThenGlobalSourceOrder(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		first  string
		second string
		want   color.NRGBA
	}{
		{
			name:   "specificity survives a later owner",
			first:  `@layer app { #target { color: #111111; } }`,
			second: `@layer app { p { color: #222222; } }`,
			want:   color.NRGBA{R: 0x11, G: 0x11, B: 0x11, A: 0xff},
		},
		{
			name:   "equal specificity uses later owner source order",
			first:  `@layer app { p { color: #111111; } }`,
			second: `@layer app { p { color: #222222; } }`,
			want:   color.NRGBA{R: 0x22, G: 0x22, B: 0x22, A: 0xff},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			document, head, body := resourceTestDocument()
			first := dom.NewElement("link", dom.Attribute{Name: "rel", Value: "stylesheet"})
			second := dom.NewElement("link", dom.Attribute{Name: "rel", Value: "stylesheet"})
			head.AppendChild(first)
			head.AppendChild(second)
			paragraph := dom.NewElement("p", dom.Attribute{Name: "id", Value: "target"})
			paragraph.AppendChild(dom.NewText("Same-layer target"))
			body.AppendChild(paragraph)

			frame, err := render.RenderWithResources(document, render.Viewport{Width: 800, Height: 600}, render.Resources{
				Stylesheets: map[*dom.Node]css.Stylesheet{
					first:  parseResourceStylesheet(t, test.first),
					second: parseResourceStylesheet(t, test.second),
				},
			})
			if err != nil {
				t.Fatalf("RenderWithResources() error = %v", err)
			}
			fragment := resourceTextFragment(frame.Root, "Same-layer target")
			if fragment == nil {
				t.Fatal("same-layer target text fragment not found")
			}
			if fragment.Color != test.want {
				t.Errorf("text color = %#v, want %#v", fragment.Color, test.want)
			}
		})
	}
}

func TestRenderStylesheetOwnerMediaUsesViewport(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		viewport render.Viewport
		want     color.NRGBA
	}{
		{
			name:     "wide style owner applies",
			viewport: render.Viewport{Width: 800, Height: 600},
			want:     color.NRGBA{R: 0x22, G: 0x22, B: 0x22, A: 0xff},
		},
		{
			name:     "narrow link owner applies",
			viewport: render.Viewport{Width: 360, Height: 640},
			want:     color.NRGBA{R: 0x33, G: 0x33, B: 0x33, A: 0xff},
		},
		{
			name:     "nonmatching owners leave base style",
			viewport: render.Viewport{Width: 600, Height: 600},
			want:     color.NRGBA{R: 0x11, G: 0x11, B: 0x11, A: 0xff},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			document, head, body := resourceTestDocument()
			base := dom.NewElement("style")
			base.AppendChild(dom.NewText(`p { color: #111111; }`))
			wide := dom.NewElement("style", dom.Attribute{Name: "media", Value: "screen and (min-width: 700px)"})
			wide.AppendChild(dom.NewText(`p { color: #222222; }`))
			narrow := dom.NewElement("link",
				dom.Attribute{Name: "rel", Value: "stylesheet"},
				dom.Attribute{Name: "href", Value: "/narrow.css"},
				dom.Attribute{Name: "media", Value: "screen and (max-width: 400px)"},
			)
			printOnly := dom.NewElement("style", dom.Attribute{Name: "media", Value: "print"})
			printOnly.AppendChild(dom.NewText(`p { color: #444444; }`))
			head.AppendChild(base)
			head.AppendChild(wide)
			head.AppendChild(narrow)
			head.AppendChild(printOnly)
			paragraph := dom.NewElement("p")
			paragraph.AppendChild(dom.NewText("Owner-media target"))
			body.AppendChild(paragraph)

			frame, err := render.RenderWithResources(document, test.viewport, render.Resources{
				Stylesheets: map[*dom.Node]css.Stylesheet{
					narrow: parseResourceStylesheet(t, `p { color: #333333; }`),
				},
			})
			if err != nil {
				t.Fatalf("RenderWithResources() error = %v", err)
			}
			fragment := resourceTextFragment(frame.Root, "Owner-media target")
			if fragment == nil {
				t.Fatal("owner-media target text fragment not found")
			}
			if fragment.Color != test.want {
				t.Errorf("text color = %#v, want %#v", fragment.Color, test.want)
			}
		})
	}
}

func TestRenderUnsupportedLayerBlocksDoNotBecomeUnlayered(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		stylesheet string
	}{
		{
			name: "anonymous top-level layer",
			stylesheet: `
p { color: #111111; }
@layer { #target { color: #ff0000 !important; } }
`,
		},
		{
			name: "nested named layer",
			stylesheet: `
p { color: #111111; }
@layer outer {
	  @layer inner { #target { color: #ff0000 !important; } }
}
`,
		},
		{
			name: "dotted layer name",
			stylesheet: `
p { color: #111111; }
@layer outer.inner { #target { color: #ff0000 !important; } }
`,
		},
		{
			name: "multi-name layer block",
			stylesheet: `
p { color: #111111; }
@layer first, second { #target { color: #ff0000 !important; } }
`,
		},
		{
			name: "layer nested inside media",
			stylesheet: `
p { color: #111111; }
@media screen {
  @layer conditional { #target { color: #ff0000 !important; } }
}
`,
		},
	}

	want := color.NRGBA{R: 0x11, G: 0x11, B: 0x11, A: 0xff}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			got := renderConditionalTextColor(t, test.stylesheet, "", render.Viewport{Width: 800, Height: 600})
			if got != want {
				t.Errorf("text color = %#v, want %#v; unsupported layer contents must be skipped", got, want)
			}
		})
	}
}

func renderConditionalTextColor(t *testing.T, stylesheet, inlineStyle string, viewport render.Viewport) color.NRGBA {
	t.Helper()
	document, head, body := resourceTestDocument()
	style := dom.NewElement("style")
	style.AppendChild(dom.NewText(stylesheet))
	head.AppendChild(style)
	attributes := []dom.Attribute{{Name: "id", Value: "target"}}
	if inlineStyle != "" {
		attributes = append(attributes, dom.Attribute{Name: "style", Value: inlineStyle})
	}
	paragraph := dom.NewElement("p", attributes...)
	paragraph.AppendChild(dom.NewText("Conditional target"))
	body.AppendChild(paragraph)

	frame, err := render.Render(document, viewport)
	if err != nil {
		t.Fatalf("Render() error = %v", err)
	}
	fragment := resourceTextFragment(frame.Root, "Conditional target")
	if fragment == nil {
		t.Fatal("conditional target text fragment not found")
	}
	return fragment.Color
}
