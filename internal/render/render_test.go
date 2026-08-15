package render_test

import (
	"bytes"
	"image/color"
	"image/png"
	"io"
	"math"
	"strings"
	"testing"

	"github.com/JediWattson/gossamer/internal/dom"
	"github.com/JediWattson/gossamer/internal/render"
)

func TestRenderRejectsInvalidViewport(t *testing.T) {
	t.Parallel()

	document := newTextDocument("viewport")
	tests := []struct {
		name     string
		viewport render.Viewport
	}{
		{name: "zero width", viewport: render.Viewport{Width: 0, Height: 600}},
		{name: "negative width", viewport: render.Viewport{Width: -1, Height: 600}},
		{name: "zero height", viewport: render.Viewport{Width: 800, Height: 0}},
		{name: "negative height", viewport: render.Viewport{Width: 800, Height: -1}},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			frame, err := render.Render(document, test.viewport)
			if frame != nil {
				t.Errorf("Render() frame = %#v, want nil", frame)
			}
			if err == nil || !strings.Contains(err.Error(), "invalid viewport") {
				t.Errorf("Render() error = %v, want invalid viewport error", err)
			}

			err = render.RenderPNG(io.Discard, document, test.viewport)
			if err == nil || !strings.Contains(err.Error(), "invalid viewport") {
				t.Errorf("RenderPNG() error = %v, want invalid viewport error", err)
			}
		})
	}
}

func TestRenderExampleDocumentWithAuthorStyles(t *testing.T) {
	t.Parallel()

	fixture := newExampleDocument()
	viewport := render.Viewport{Width: 800, Height: 600}
	frame, err := render.Render(fixture.document, viewport)
	if err != nil {
		t.Fatalf("Render() error = %v", err)
	}

	if frame.Viewport != viewport {
		t.Errorf("Frame.Viewport = %#v, want %#v", frame.Viewport, viewport)
	}
	if frame.DisplayList.Viewport != viewport {
		t.Errorf("DisplayList.Viewport = %#v, want %#v", frame.DisplayList.Viewport, viewport)
	}
	if frame.Root == nil {
		t.Fatal("Frame.Root = nil, want retained layout tree")
	}

	bodyBox := findBox(frame.Root, fixture.body)
	if bodyBox == nil {
		t.Fatal("body layout box not found")
	}
	assertNear(t, "body x", bodyBox.Bounds.X, 100)
	assertNear(t, "body width", bodyBox.Bounds.Width, 600)
	assertNear(t, "body top margin", bodyBox.Bounds.Y, 40)

	fragments := collectTextFragments(frame.Root)
	heading := findTextFragment(fragments, "Example Domain")
	if heading == nil {
		t.Fatal("heading text fragment not found")
	}
	assertNear(t, "heading font size", heading.FontSize, 32)
	if heading.FontWeight != render.FontWeightBold {
		t.Errorf("heading font weight = %d, want bold", heading.FontWeight)
	}

	link := findTextFragment(fragments, "More information")
	if link == nil {
		t.Fatal("link text fragment not found")
	}
	if want := (color.NRGBA{R: 0x38, G: 0x48, B: 0x8f, A: 0xff}); link.Color != want {
		t.Errorf("link color = %#v, want %#v", link.Color, want)
	}

	commands := frame.DisplayList.Commands
	if len(commands) == 0 {
		t.Fatal("display list is empty")
	}
	bodyBackground := commandIndex(commands, func(command render.Command) bool {
		return command.Kind == render.FillRectCommand &&
			command.Color == (color.NRGBA{R: 0xf0, G: 0xf0, B: 0xf2, A: 0xff}) &&
			near(command.Rect.X, bodyBox.Bounds.X) &&
			near(command.Rect.Width, bodyBox.Bounds.Width)
	})
	firstText := commandIndex(commands, func(command render.Command) bool {
		return command.Kind == render.DrawTextCommand
	})
	if bodyBackground < 0 {
		t.Error("body background fill command not found")
	}
	if firstText < 0 {
		t.Error("text draw command not found")
	}
	if bodyBackground >= 0 && firstText >= 0 && bodyBackground > firstText {
		t.Errorf("body background command index = %d, want before first text at %d", bodyBackground, firstText)
	}

	headingCommand := textCommandIndex(commands, "Example Domain")
	paragraphCommand := textCommandIndex(commands, "This domain is for examples.")
	linkCommand := textCommandIndex(commands, "More information")
	if headingCommand < 0 || paragraphCommand < 0 || linkCommand < 0 {
		t.Errorf(
			"text command indexes = heading:%d paragraph:%d link:%d, want all present",
			headingCommand,
			paragraphCommand,
			linkCommand,
		)
	} else if !(headingCommand < paragraphCommand && paragraphCommand < linkCommand) {
		t.Errorf(
			"text command order = heading:%d paragraph:%d link:%d, want DOM paint order",
			headingCommand,
			paragraphCommand,
			linkCommand,
		)
	}
	if cssCommand := commandIndex(commands, func(command render.Command) bool {
		return command.Kind == render.DrawTextCommand && strings.Contains(command.Text, "background-color")
	}); cssCommand >= 0 {
		t.Errorf("style element text was painted at command %d", cssCommand)
	}
}

func TestRenderPNGProducesViewportImageWithOpaqueBackground(t *testing.T) {
	t.Parallel()

	viewport := render.Viewport{Width: 160, Height: 120}
	var output bytes.Buffer
	if err := render.RenderPNG(&output, newTextDocument("Gossamer"), viewport); err != nil {
		t.Fatalf("RenderPNG() error = %v", err)
	}

	pngSignature := []byte{0x89, 'P', 'N', 'G', '\r', '\n', 0x1a, '\n'}
	if !bytes.HasPrefix(output.Bytes(), pngSignature) {
		t.Fatalf("PNG prefix = % x, want % x", output.Bytes()[:min(len(output.Bytes()), len(pngSignature))], pngSignature)
	}

	configuration, err := png.DecodeConfig(bytes.NewReader(output.Bytes()))
	if err != nil {
		t.Fatalf("DecodeConfig() error = %v", err)
	}
	if configuration.Width != viewport.Width || configuration.Height != viewport.Height {
		t.Errorf(
			"PNG dimensions = %dx%d, want %dx%d",
			configuration.Width,
			configuration.Height,
			viewport.Width,
			viewport.Height,
		)
	}

	image, err := png.Decode(bytes.NewReader(output.Bytes()))
	if err != nil {
		t.Fatalf("Decode() error = %v", err)
	}
	gotBackground := color.NRGBAModel.Convert(image.At(viewport.Width-1, viewport.Height-1)).(color.NRGBA)
	wantBackground := color.NRGBA{R: 0xff, G: 0xff, B: 0xff, A: 0xff}
	if gotBackground != wantBackground {
		t.Errorf("bottom-right background = %#v, want %#v", gotBackground, wantBackground)
	}
}

type exampleFixture struct {
	document *dom.Node
	body     *dom.Node
}

func newExampleDocument() exampleFixture {
	document := dom.NewDocument()
	document.AppendChild(dom.NewDoctype("html"))

	html := dom.NewElement("html")
	document.AppendChild(html)
	head := dom.NewElement("head")
	html.AppendChild(head)
	style := dom.NewElement("style")
	style.AppendChild(dom.NewText(`
		html { background-color: #ffffff; }
		body {
			width: 600px;
			margin: 40px auto;
			background-color: #f0f0f2;
			color: #333333;
			font-size: 16px;
		}
		h1 { margin: 0 0 16px 0; font-size: 32px; font-weight: bold; }
		p { margin: 0 0 16px 0; }
		a { color: #38488f; }
	`))
	head.AppendChild(style)

	body := dom.NewElement("body")
	html.AppendChild(body)
	heading := dom.NewElement("h1")
	heading.AppendChild(dom.NewText("Example Domain"))
	body.AppendChild(heading)
	paragraph := dom.NewElement("p")
	paragraph.AppendChild(dom.NewText("This domain is for examples."))
	body.AppendChild(paragraph)
	linkParagraph := dom.NewElement("p")
	link := dom.NewElement("a", dom.Attribute{Name: "href", Value: "https://example.com/more"})
	link.AppendChild(dom.NewText("More information"))
	linkParagraph.AppendChild(link)
	body.AppendChild(linkParagraph)

	return exampleFixture{document: document, body: body}
}

func newTextDocument(text string) *dom.Node {
	document := dom.NewDocument()
	html := dom.NewElement("html")
	document.AppendChild(html)
	body := dom.NewElement("body")
	html.AppendChild(body)
	body.AppendChild(dom.NewText(text))
	return document
}

func findBox(box *render.Box, node *dom.Node) *render.Box {
	if box == nil {
		return nil
	}
	if box.Node == node {
		return box
	}
	for _, child := range box.Children {
		if found := findBox(child, node); found != nil {
			return found
		}
	}
	return nil
}

func collectTextFragments(box *render.Box) []render.TextFragment {
	if box == nil {
		return nil
	}
	fragments := append([]render.TextFragment(nil), box.Text...)
	for _, child := range box.Children {
		fragments = append(fragments, collectTextFragments(child)...)
	}
	return fragments
}

func findTextFragment(fragments []render.TextFragment, text string) *render.TextFragment {
	for index := range fragments {
		if strings.Contains(fragments[index].Text, text) {
			return &fragments[index]
		}
	}
	return nil
}

func commandIndex(commands []render.Command, matches func(render.Command) bool) int {
	for index, command := range commands {
		if matches(command) {
			return index
		}
	}
	return -1
}

func textCommandIndex(commands []render.Command, text string) int {
	return commandIndex(commands, func(command render.Command) bool {
		return command.Kind == render.DrawTextCommand && strings.Contains(command.Text, text)
	})
}

func assertNear(t *testing.T, name string, got, want float64) {
	t.Helper()
	if !near(got, want) {
		t.Errorf("%s = %.3f, want %.3f", name, got, want)
	}
}

func near(left, right float64) bool {
	return math.Abs(left-right) < 0.01
}
