package render_test

import (
	"bytes"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"reflect"
	"strings"
	"testing"

	"github.com/JediWattson/gossamer/internal/css"
	"github.com/JediWattson/gossamer/internal/dom"
	"github.com/JediWattson/gossamer/internal/render"
)

func TestRenderWithResourcesInterleavesExternalStylesheetInDOMOrder(t *testing.T) {
	t.Parallel()

	document, head, body := resourceTestDocument()
	before := dom.NewElement("style")
	before.AppendChild(dom.NewText(`p { color: #111111; font-size: 12px; }`))
	head.AppendChild(before)
	link := dom.NewElement("link",
		dom.Attribute{Name: "rel", Value: "stylesheet"},
		dom.Attribute{Name: "href", Value: "theme.css"},
	)
	head.AppendChild(link)
	after := dom.NewElement("style")
	after.AppendChild(dom.NewText(`p { color: #333333; }`))
	head.AppendChild(after)

	paragraph := dom.NewElement("p")
	paragraph.AppendChild(dom.NewText("Cascade order"))
	body.AppendChild(paragraph)

	frame, err := render.RenderWithResources(document, render.Viewport{Width: 320, Height: 200}, render.Resources{
		Stylesheets: map[*dom.Node]css.Stylesheet{
			link: parseResourceStylesheet(t, `p { color: #222222; font-size: 24px; }`),
		},
	})
	if err != nil {
		t.Fatalf("RenderWithResources() error = %v", err)
	}

	fragment := resourceTextFragment(frame.Root, "Cascade order")
	if fragment == nil {
		t.Fatal("paragraph text fragment not found")
	}
	if got, want := fragment.FontSize, 24.0; got != want {
		t.Errorf("font size = %.2f, want %.2f from external stylesheet", got, want)
	}
	if got, want := fragment.Color, (color.NRGBA{R: 0x33, G: 0x33, B: 0x33, A: 0xff}); got != want {
		t.Errorf("color = %#v, want %#v from trailing inline stylesheet", got, want)
	}
}

func TestRenderWithResourcesAppliesOnlyScreenCSSStyleElements(t *testing.T) {
	t.Parallel()

	document, head, body := resourceTestDocument()
	screen := dom.NewElement("style", dom.Attribute{Name: "media", Value: "screen and (min-width: 0px)"})
	screen.AppendChild(dom.NewText(`p { color: #123456; font-size: 22px; }`))
	head.AppendChild(screen)
	printOnly := dom.NewElement("style", dom.Attribute{Name: "media", Value: "print"})
	printOnly.AppendChild(dom.NewText(`p { color: #ff0000; font-size: 30px; }`))
	head.AppendChild(printOnly)
	nonCSS := dom.NewElement("style", dom.Attribute{Name: "type", Value: "text/plain; charset=utf-8"})
	nonCSS.AppendChild(dom.NewText(`p { color: #00ff00; font-size: 40px; }`))
	head.AppendChild(nonCSS)

	paragraph := dom.NewElement("p")
	paragraph.AppendChild(dom.NewText("Screen styles"))
	body.AppendChild(paragraph)

	frame, err := render.RenderWithResources(document, render.Viewport{Width: 320, Height: 200}, render.Resources{})
	if err != nil {
		t.Fatalf("RenderWithResources() error = %v", err)
	}
	fragment := resourceTextFragment(frame.Root, "Screen styles")
	if fragment == nil {
		t.Fatal("paragraph text fragment not found")
	}
	if got, want := fragment.FontSize, 22.0; got != want {
		t.Errorf("font size = %.2f, want %.2f from screen stylesheet", got, want)
	}
	if got, want := fragment.Color, (color.NRGBA{R: 0x12, G: 0x34, B: 0x56, A: 0xff}); got != want {
		t.Errorf("color = %#v, want %#v from screen stylesheet", got, want)
	}
}

func TestRenderWithResourcesCascadeOrderBeyond1024Declarations(t *testing.T) {
	t.Parallel()

	var source strings.Builder
	source.WriteString("p {")
	for index := range 1025 {
		_, _ = fmt.Fprintf(&source, "--filler-%d: %d;", index, index)
	}
	source.WriteString("color: #111111; } p { color: #abcdef; }")
	stylesheet := parseResourceStylesheet(t, source.String())
	if got := len(stylesheet.Rules); got != 2 {
		t.Fatalf("stylesheet rule count = %d, want 2", got)
	}
	if got := len(stylesheet.Rules[0].Declarations); got <= 1024 {
		t.Fatalf("first rule declaration count = %d, want more than 1024", got)
	}

	document, head, body := resourceTestDocument()
	link := dom.NewElement("link",
		dom.Attribute{Name: "rel", Value: "stylesheet"},
		dom.Attribute{Name: "href", Value: "large.css"},
	)
	head.AppendChild(link)
	paragraph := dom.NewElement("p")
	paragraph.AppendChild(dom.NewText("Later rule"))
	body.AppendChild(paragraph)

	frame, err := render.RenderWithResources(document, render.Viewport{Width: 320, Height: 200}, render.Resources{
		Stylesheets: map[*dom.Node]css.Stylesheet{link: stylesheet},
	})
	if err != nil {
		t.Fatalf("RenderWithResources() error = %v", err)
	}
	fragment := resourceTextFragment(frame.Root, "Later rule")
	if fragment == nil {
		t.Fatal("paragraph text fragment not found")
	}
	if got, want := fragment.Color, (color.NRGBA{R: 0xab, G: 0xcd, B: 0xef, A: 0xff}); got != want {
		t.Errorf("color = %#v, want %#v from later equal-specificity rule", got, want)
	}
}

func TestRenderWithResourcesOrdersInlineImageFragmentsAndPushesFollowingBlock(t *testing.T) {
	t.Parallel()

	document, _, body := resourceTestDocument()
	body.Attributes = append(body.Attributes, dom.Attribute{Name: "style", Value: "margin: 0"})
	before := dom.NewText("Before ")
	imageNode := dom.NewElement("img", dom.Attribute{Name: "src", Value: "tall.png"})
	after := dom.NewText(" after")
	body.AppendChild(before)
	body.AppendChild(imageNode)
	body.AppendChild(after)
	following := dom.NewElement("p")
	following.AppendChild(dom.NewText("Below"))
	body.AppendChild(following)

	decoded := image.NewNRGBA(image.Rect(0, 0, 12, 40))
	frame, err := render.RenderWithResources(document, render.Viewport{Width: 320, Height: 200}, render.Resources{
		Images: map[*dom.Node]image.Image{imageNode: decoded},
	})
	if err != nil {
		t.Fatalf("RenderWithResources() error = %v", err)
	}

	bodyBox := resourceBoxForNode(frame.Root, body)
	if bodyBox == nil {
		t.Fatal("body layout box not found")
	}
	if got, want := len(bodyBox.Fragments), 3; got != want {
		t.Fatalf("body fragment count = %d, want %d", got, want)
	}
	if got, want := []render.InlineFragmentKind{
		bodyBox.Fragments[0].Kind,
		bodyBox.Fragments[1].Kind,
		bodyBox.Fragments[2].Kind,
	}, []render.InlineFragmentKind{
		render.TextFragmentKind,
		render.ImageFragmentKind,
		render.TextFragmentKind,
	}; !reflect.DeepEqual(got, want) {
		t.Errorf("fragment kinds = %v, want %v", got, want)
	}
	if got := strings.TrimSpace(bodyBox.Fragments[0].Text.Text); got != "Before" {
		t.Errorf("first fragment text = %q, want Before", got)
	}
	if got := bodyBox.Fragments[1].Image; got.Node != imageNode || got.Image != decoded || got.Bounds.Width != 12 || got.Bounds.Height != 40 {
		t.Errorf("image fragment = %#v, want intrinsic 12x40 image for img node", got)
	}
	if got := strings.TrimSpace(bodyBox.Fragments[2].Text.Text); got != "after" {
		t.Errorf("last fragment text = %q, want after", got)
	}

	imageBounds := bodyBox.Fragments[1].Image.Bounds
	followingBox := resourceBoxForNode(frame.Root, following)
	if followingBox == nil {
		t.Fatal("following paragraph layout box not found")
	}
	if imageBottom := imageBounds.Y + imageBounds.Height; followingBox.Bounds.Y < imageBottom {
		t.Errorf("following block y = %.2f, want at or below tall image bottom %.2f", followingBox.Bounds.Y, imageBottom)
	}

	var paintOrder []string
	for _, command := range frame.DisplayList.Commands {
		switch command.Kind {
		case render.DrawTextCommand:
			paintOrder = append(paintOrder, strings.TrimSpace(command.Text))
		case render.DrawImageCommand:
			paintOrder = append(paintOrder, "<image>")
		}
	}
	if want := []string{"Before", "<image>", "after", "Below"}; !reflect.DeepEqual(paintOrder, want) {
		t.Errorf("paint order = %q, want %q", paintOrder, want)
	}
}

func TestRenderWithResourcesPaintsBlockBeforeLaterInlineImage(t *testing.T) {
	t.Parallel()

	document, _, body := resourceTestDocument()
	body.Attributes = append(body.Attributes, dom.Attribute{Name: "style", Value: "margin: 0"})
	block := dom.NewElement("div")
	block.AppendChild(dom.NewText("Earlier block"))
	body.AppendChild(block)
	imageNode := dom.NewElement("img", dom.Attribute{Name: "src", Value: "later.png"})
	body.AppendChild(imageNode)

	decoded := image.NewNRGBA(image.Rect(0, 0, 8, 8))
	frame, err := render.RenderWithResources(document, render.Viewport{Width: 160, Height: 80}, render.Resources{
		Images: map[*dom.Node]image.Image{imageNode: decoded},
	})
	if err != nil {
		t.Fatalf("RenderWithResources() error = %v", err)
	}

	blockCommand := commandIndex(frame.DisplayList.Commands, func(command render.Command) bool {
		return command.Kind == render.DrawTextCommand && strings.Contains(command.Text, "Earlier block")
	})
	imageCommand := commandIndex(frame.DisplayList.Commands, func(command render.Command) bool {
		return command.Kind == render.DrawImageCommand && command.Image == decoded
	})
	if blockCommand < 0 || imageCommand < 0 {
		t.Fatalf("paint command indexes = block:%d image:%d, want both present", blockCommand, imageCommand)
	}
	if blockCommand >= imageCommand {
		t.Errorf("paint command indexes = block:%d image:%d, want earlier block painted first", blockCommand, imageCommand)
	}
}

func TestRenderWithResourcesAppliesInlineAncestorOpacityDirectlyToImage(t *testing.T) {
	t.Parallel()

	document, _, body := resourceTestDocument()
	body.Attributes = append(body.Attributes, dom.Attribute{Name: "style", Value: "margin: 0; font-size: 1px; line-height: 1"})
	ancestor := dom.NewElement("span", dom.Attribute{Name: "style", Value: "opacity: 0.25"})
	imageNode := dom.NewElement("img",
		dom.Attribute{Name: "src", Value: "red.png"},
		dom.Attribute{Name: "style", Value: "font-size: 1px; line-height: 1"},
	)
	ancestor.AppendChild(imageNode)
	body.AppendChild(ancestor)

	decoded := image.NewNRGBA(image.Rect(0, 0, 1, 1))
	decoded.SetNRGBA(0, 0, color.NRGBA{R: 0xff, A: 0xff})
	frame, err := render.RenderWithResources(document, render.Viewport{Width: 2, Height: 2}, render.Resources{
		Images: map[*dom.Node]image.Image{imageNode: decoded},
	})
	if err != nil {
		t.Fatalf("RenderWithResources() error = %v", err)
	}

	var imageCommands []render.Command
	for _, command := range frame.DisplayList.Commands {
		switch command.Kind {
		case render.BeginOpacityCommand:
			t.Error("display list contains BeginOpacityCommand for flattened inline ancestor")
		case render.DrawImageCommand:
			imageCommands = append(imageCommands, command)
		}
	}
	if len(imageCommands) != 1 {
		t.Fatalf("image command count = %d, want 1", len(imageCommands))
	}
	if got, want := imageCommands[0].Opacity, 0.25; got != want {
		t.Errorf("image command opacity = %.2f, want %.2f from inline ancestor", got, want)
	}

	painted, err := render.Rasterize(frame)
	if err != nil {
		t.Fatalf("Rasterize() error = %v", err)
	}
	assertResourcePixel(t, painted, 0, 0, color.NRGBA{R: 0xff, G: 0xbf, B: 0xbf, A: 0xff})
}

func TestRenderWithResourcesReservesExplicitSizeForMissingImage(t *testing.T) {
	t.Parallel()

	document, _, body := resourceTestDocument()
	body.Attributes = append(body.Attributes, dom.Attribute{Name: "style", Value: "margin: 0"})
	imageNode := dom.NewElement("img",
		dom.Attribute{Name: "src", Value: "missing.png"},
		dom.Attribute{Name: "width", Value: "40"},
		dom.Attribute{Name: "height", Value: "30"},
	)
	body.AppendChild(imageNode)
	following := dom.NewElement("div")
	following.AppendChild(dom.NewText("After missing image"))
	body.AppendChild(following)

	frame, err := render.RenderWithResources(document, render.Viewport{Width: 160, Height: 100}, render.Resources{})
	if err != nil {
		t.Fatalf("RenderWithResources() error = %v", err)
	}

	bodyBox := resourceBoxForNode(frame.Root, body)
	if bodyBox == nil {
		t.Fatal("body layout box not found")
	}
	var imageFragment *render.ImageFragment
	for index := range bodyBox.Fragments {
		fragment := &bodyBox.Fragments[index]
		if fragment.Kind == render.ImageFragmentKind && fragment.Image.Node == imageNode {
			imageFragment = &fragment.Image
			break
		}
	}
	if imageFragment == nil {
		t.Fatal("missing image layout fragment not found")
	}
	if imageFragment.Image != nil {
		t.Errorf("missing image fragment decoded image = %v, want nil", imageFragment.Image)
	}
	if got, want := imageFragment.Bounds.Width, 40.0; got != want {
		t.Errorf("missing image width = %.2f, want %.2f", got, want)
	}
	if got, want := imageFragment.Bounds.Height, 30.0; got != want {
		t.Errorf("missing image height = %.2f, want %.2f", got, want)
	}

	followingBox := resourceBoxForNode(frame.Root, following)
	if followingBox == nil {
		t.Fatal("following block layout box not found")
	}
	if imageBottom := imageFragment.Bounds.Y + imageFragment.Bounds.Height; followingBox.Bounds.Y < imageBottom {
		t.Errorf("following block y = %.2f, want at or below missing image bottom %.2f", followingBox.Bounds.Y, imageBottom)
	}
	for index, command := range frame.DisplayList.Commands {
		if command.Kind == render.DrawImageCommand {
			t.Errorf("display command %d is DrawImageCommand for missing decoded image", index)
		}
	}
}

func TestRenderPNGWithResourcesPaintsImagePixels(t *testing.T) {
	t.Parallel()

	document, _, body := resourceTestDocument()
	body.Attributes = append(body.Attributes, dom.Attribute{Name: "style", Value: "margin: 0; font-size: 1px; line-height: 1"})
	imageNode := dom.NewElement("img",
		dom.Attribute{Name: "src", Value: "colors.png"},
		dom.Attribute{Name: "style", Value: "font-size: 1px; line-height: 1"},
	)
	body.AppendChild(imageNode)

	decoded := image.NewNRGBA(image.Rect(0, 0, 2, 1))
	red := color.NRGBA{R: 0xff, A: 0xff}
	blue := color.NRGBA{B: 0xff, A: 0xff}
	decoded.SetNRGBA(0, 0, red)
	decoded.SetNRGBA(1, 0, blue)

	var output bytes.Buffer
	if err := render.RenderPNGWithResources(
		&output,
		document,
		render.Viewport{Width: 4, Height: 3},
		render.Resources{Images: map[*dom.Node]image.Image{imageNode: decoded}},
	); err != nil {
		t.Fatalf("RenderPNGWithResources() error = %v", err)
	}

	painted, err := png.Decode(bytes.NewReader(output.Bytes()))
	if err != nil {
		t.Fatalf("png.Decode() error = %v", err)
	}
	assertResourcePixel(t, painted, 0, 0, red)
	assertResourcePixel(t, painted, 1, 0, blue)
	assertResourcePixel(t, painted, 3, 2, color.NRGBA{R: 0xff, G: 0xff, B: 0xff, A: 0xff})
}

func TestRenderWithResourcesLaysOutBlockImageDimensionHintsAndCSSOverride(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		attributes []dom.Attribute
		authorCSS  string
		wantWidth  float64
		wantHeight float64
	}{
		{
			name: "HTML width and height hints",
			attributes: []dom.Attribute{
				{Name: "width", Value: "120"},
				{Name: "height", Value: "70"},
			},
			authorCSS:  "img { display: block; }",
			wantWidth:  120,
			wantHeight: 70,
		},
		{
			name:       "author width overrides HTML width and preserves intrinsic aspect",
			attributes: []dom.Attribute{{Name: "width", Value: "120"}},
			authorCSS:  "img { display: block; width: 160px; }",
			wantWidth:  160,
			wantHeight: 80,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			document, head, body := resourceTestDocument()
			body.Attributes = append(body.Attributes, dom.Attribute{Name: "style", Value: "margin: 0"})
			style := dom.NewElement("style")
			style.AppendChild(dom.NewText(test.authorCSS))
			head.AppendChild(style)

			attributes := append([]dom.Attribute{{Name: "src", Value: "block.png"}}, test.attributes...)
			imageNode := dom.NewElement("img", attributes...)
			body.AppendChild(imageNode)
			following := dom.NewElement("p")
			following.AppendChild(dom.NewText("Following block"))
			body.AppendChild(following)

			decoded := image.NewNRGBA(image.Rect(0, 0, 80, 40))
			frame, err := render.RenderWithResources(document, render.Viewport{Width: 400, Height: 240}, render.Resources{
				Images: map[*dom.Node]image.Image{imageNode: decoded},
			})
			if err != nil {
				t.Fatalf("RenderWithResources() error = %v", err)
			}

			imageBox := resourceBoxForNode(frame.Root, imageNode)
			if imageBox == nil {
				t.Fatal("retained image box not found")
			}
			if imageBox.Bounds.Width != test.wantWidth || imageBox.Bounds.Height != test.wantHeight {
				t.Errorf(
					"image box dimensions = %.2fx%.2f, want %.2fx%.2f",
					imageBox.Bounds.Width,
					imageBox.Bounds.Height,
					test.wantWidth,
					test.wantHeight,
				)
			}

			var imageCommands []render.Command
			for _, command := range frame.DisplayList.Commands {
				if command.Kind == render.DrawImageCommand {
					imageCommands = append(imageCommands, command)
				}
			}
			if len(imageCommands) != 1 {
				t.Fatalf("image command count = %d, want 1", len(imageCommands))
			}
			if command := imageCommands[0]; command.Image != decoded || command.Rect != imageBox.Bounds {
				t.Errorf("image command = %#v, want decoded image at retained bounds %#v", command, imageBox.Bounds)
			}

			followingBox := resourceBoxForNode(frame.Root, following)
			if followingBox == nil {
				t.Fatal("following block layout box not found")
			}
			if imageBottom := imageBox.Bounds.Y + imageBox.Bounds.Height; followingBox.Bounds.Y < imageBottom {
				t.Errorf("following block y = %.2f, want at or below image bottom %.2f", followingBox.Bounds.Y, imageBottom)
			}
		})
	}
}

func resourceTestDocument() (document, head, body *dom.Node) {
	document = dom.NewDocument()
	html := dom.NewElement("html")
	document.AppendChild(html)
	head = dom.NewElement("head")
	html.AppendChild(head)
	body = dom.NewElement("body")
	html.AppendChild(body)
	return document, head, body
}

func parseResourceStylesheet(t *testing.T, source string) css.Stylesheet {
	t.Helper()
	stylesheet, err := css.Parse(source)
	if err != nil {
		t.Fatalf("css.Parse() error = %v", err)
	}
	return stylesheet
}

func resourceBoxForNode(box *render.Box, node *dom.Node) *render.Box {
	if box == nil {
		return nil
	}
	if box.Node == node {
		return box
	}
	for _, child := range box.Children {
		if found := resourceBoxForNode(child, node); found != nil {
			return found
		}
	}
	return nil
}

func resourceTextFragment(box *render.Box, text string) *render.TextFragment {
	if box == nil {
		return nil
	}
	for index := range box.Fragments {
		fragment := &box.Fragments[index]
		if fragment.Kind == render.TextFragmentKind && strings.Contains(fragment.Text.Text, text) {
			return &fragment.Text
		}
	}
	for _, child := range box.Children {
		if found := resourceTextFragment(child, text); found != nil {
			return found
		}
	}
	return nil
}

func assertResourcePixel(t *testing.T, image image.Image, x, y int, want color.NRGBA) {
	t.Helper()
	got := color.NRGBAModel.Convert(image.At(x, y)).(color.NRGBA)
	if got != want {
		t.Errorf("pixel (%d,%d) = %#v, want %#v", x, y, got, want)
	}
}
