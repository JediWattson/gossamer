package render

import (
	"fmt"
	"image"
	"image/color"
	"io"
	"math"

	"github.com/JediWattson/gossamer/internal/dom"
)

var opaqueWhite = color.NRGBA{R: 0xff, G: 0xff, B: 0xff, A: 0xff}

// Render computes retained layout geometry and a backend-neutral display list
// for document. The resulting frame can be painted to a PNG now and replayed
// by a window backend later.
func Render(document *dom.Node, viewport Viewport) (*Frame, error) {
	return RenderWithResources(document, viewport, Resources{})
}

// RenderWithResources computes a frame using decoded external stylesheets and
// images associated with their initiating DOM elements.
func RenderWithResources(document *dom.Node, viewport Viewport, resources Resources) (*Frame, error) {
	fonts, err := newFontBook()
	if err != nil {
		return nil, err
	}
	defer fonts.Close()
	return renderWithFonts(document, viewport, resources, fonts)
}

// RenderPNG lays out document and encodes the painted viewport as PNG.
func RenderPNG(writer io.Writer, document *dom.Node, viewport Viewport) error {
	return RenderPNGWithResources(writer, document, viewport, Resources{})
}

// RenderPNGWithResources lays out and paints a document with its decoded
// external resources, then encodes the viewport as PNG.
func RenderPNGWithResources(writer io.Writer, document *dom.Node, viewport Viewport, resources Resources) error {
	if writer == nil {
		return fmt.Errorf("render: nil PNG writer")
	}
	fonts, err := newFontBook()
	if err != nil {
		return err
	}
	defer fonts.Close()

	frame, err := renderWithFonts(document, viewport, resources, fonts)
	if err != nil {
		return err
	}
	canvas, err := rasterize(frame.DisplayList, fonts)
	if err != nil {
		return err
	}
	return encodePNG(writer, canvas)
}

func renderWithFonts(document *dom.Node, viewport Viewport, resources Resources, fonts *fontBook) (*Frame, error) {
	if viewport.Width <= 0 || viewport.Height <= 0 {
		return nil, fmt.Errorf("render: invalid viewport %dx%d", viewport.Width, viewport.Height)
	}
	if err := validateDocument(document); err != nil {
		return nil, err
	}

	styledRoot := buildStyleTree(document, viewport, resources.Stylesheets)
	rootBox, styles, err := layoutDocument(styledRoot, viewport, resources.Images, fonts)
	if err != nil {
		return nil, fmt.Errorf("render: layout: %w", err)
	}
	displayList := buildDisplayList(document, rootBox, styles, viewport)
	return &Frame{
		Viewport:    viewport,
		Root:        rootBox,
		DisplayList: displayList,
	}, nil
}

func buildDisplayList(document *dom.Node, root *Box, styles map[*dom.Node]computedStyle, viewport Viewport) DisplayList {
	list := DisplayList{Viewport: viewport}
	canvas := Rect{Width: float64(viewport.Width), Height: float64(viewport.Height)}
	list.Commands = append(list.Commands, Command{Kind: FillRectCommand, Rect: canvas, Color: opaqueWhite})

	htmlNode := findElement(document, "html")
	bodyNode := findElement(document, "body")
	if htmlStyle, ok := styles[htmlNode]; ok && htmlStyle.hasBackground {
		list.Commands = append(list.Commands, Command{Kind: FillRectCommand, Rect: canvas, Color: htmlStyle.background})
	} else if bodyStyle, ok := styles[bodyNode]; ok && bodyStyle.hasBackground {
		// HTML propagates the body background to the canvas when the root has
		// no background of its own.
		list.Commands = append(list.Commands, Command{Kind: FillRectCommand, Rect: canvas, Color: bodyStyle.background})
	}

	for _, child := range root.Children {
		paintBox(&list, child, styles)
	}
	return list
}

func paintBox(list *DisplayList, box *Box, styles map[*dom.Node]computedStyle) {
	if box == nil {
		return
	}
	style, hasStyle := styles[box.Node]
	grouped := hasStyle && style.opacity < 1
	if grouped {
		list.Commands = append(list.Commands, Command{Kind: BeginOpacityCommand, Opacity: style.opacity})
	}
	if hasStyle && style.hasBackground && box.Bounds.Width > 0 && box.Bounds.Height > 0 {
		list.Commands = append(list.Commands, Command{
			Kind:  FillRectCommand,
			Rect:  box.Bounds,
			Color: style.background,
		})
	}
	if len(box.flow) != 0 {
		for _, item := range box.flow {
			if item.box != nil {
				paintBox(list, item.box, styles)
				continue
			}
			paintInlineFragment(list, item.fragment, styles)
		}
	} else {
		for _, fragment := range box.Fragments {
			paintInlineFragment(list, fragment, styles)
		}
	}
	if len(box.flow) == 0 && len(box.Fragments) == 0 {
		for _, fragment := range box.Text {
			paintTextFragment(list, fragment, styles)
		}
	}
	if len(box.flow) == 0 {
		for _, child := range box.Children {
			paintBox(list, child, styles)
		}
	}
	if grouped {
		list.Commands = append(list.Commands, Command{Kind: EndOpacityCommand})
	}
}

func paintInlineFragment(list *DisplayList, fragment InlineFragment, styles map[*dom.Node]computedStyle) {
	switch fragment.Kind {
	case TextFragmentKind:
		paintTextFragment(list, fragment.Text, styles)
	case ImageFragmentKind:
		paintImageFragment(list, fragment.Image)
	}
}

func paintTextFragment(list *DisplayList, fragment TextFragment, styles map[*dom.Node]computedStyle) {
	list.Commands = append(list.Commands, Command{
		Kind: DrawTextCommand, Color: fragment.Color, Text: fragment.Text,
		X: fragment.X, BaselineY: fragment.BaselineY,
		FontSize: fragment.FontSize, FontWeight: fragment.FontWeight,
	})
	if fragmentStyle, ok := styles[fragment.Node]; ok && fragmentStyle.underline {
		list.Commands = append(list.Commands, Command{
			Kind:  FillRectCommand,
			Rect:  Rect{X: fragment.X, Y: fragment.BaselineY + math.Max(1, fragment.FontSize/16), Width: fragment.Width, Height: math.Max(1, fragment.FontSize/16)},
			Color: fragment.Color,
		})
	}
}

func paintImageFragment(list *DisplayList, fragment ImageFragment) {
	if fragment.Image == nil || fragment.Bounds.Width <= 0 || fragment.Bounds.Height <= 0 {
		return
	}
	list.Commands = append(list.Commands, Command{Kind: DrawImageCommand, Rect: fragment.Bounds, Image: fragment.Image, Opacity: fragment.Opacity})
}

func findElement(root *dom.Node, name string) *dom.Node {
	if root == nil {
		return nil
	}
	if root.Type == dom.ElementNode && root.Data == name {
		return root
	}
	for _, child := range root.Children {
		if found := findElement(child, name); found != nil {
			return found
		}
	}
	return nil
}

// Rasterize paints a frame to an in-memory image. Window backends can instead
// consume Frame.DisplayList directly.
func Rasterize(frame *Frame) (*image.RGBA, error) {
	if frame == nil {
		return nil, fmt.Errorf("render: nil frame")
	}
	fonts, err := newFontBook()
	if err != nil {
		return nil, err
	}
	defer fonts.Close()
	return rasterize(frame.DisplayList, fonts)
}
