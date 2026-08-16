package render

import (
	"fmt"
	"image"
	"image/color"
	"io"
	"math"
	"sort"

	"github.com/JediWattson/gossamer/internal/dom"
	computed "github.com/JediWattson/gossamer/internal/style"
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

// ComputeStyleSnapshot runs the style pipeline without performing layout or
// paint. Browser pages can cache the returned immutable snapshot and reuse it
// for synchronous computed-style reads and the next render.
func ComputeStyleSnapshot(document *dom.Node, viewport Viewport, resources Resources) (*computed.Snapshot, error) {
	if viewport.Width <= 0 || viewport.Height <= 0 {
		return nil, fmt.Errorf("render: invalid viewport %dx%d", viewport.Width, viewport.Height)
	}
	if err := validateDocument(document); err != nil {
		return nil, err
	}
	return computed.Compute(document, computed.Input{
		Environment:          styleEnvironment(viewport),
		Stylesheets:          resources.Stylesheets,
		UserStylesheets:      resources.UserStylesheets,
		UserAgentStylesheets: resources.UserAgentStylesheets,
		SelectorState:        resources.SelectorState,
	}), nil
}

// ComputeDocumentStyleSnapshot computes styles from one coherent stable-ID
// document read. The returned snapshot records the document mutation version
// and can be queried directly with dom.NodeID values.
func ComputeDocumentStyleSnapshot(document *dom.Document, viewport Viewport, resources Resources) (*computed.Snapshot, error) {
	if document == nil {
		return nil, fmt.Errorf("render: nil document")
	}
	if viewport.Width <= 0 || viewport.Height <= 0 {
		return nil, fmt.Errorf("render: invalid viewport %dx%d", viewport.Width, viewport.Height)
	}
	var snapshot *computed.Snapshot
	err := document.WithReadView(func(view dom.ReadView) error {
		var computeErr error
		snapshot, computeErr = ComputeStyleSnapshotFromReadView(view, viewport, resources)
		return computeErr
	})
	if err != nil {
		return nil, err
	}
	return snapshot, nil
}

// ComputeStyleSnapshotFromReadView computes styles without reacquiring the
// Document identity lock. It is intended for browser code that must validate a
// NodeID and compute its style from the same coherent DOM read.
func ComputeStyleSnapshotFromReadView(view dom.ReadView, viewport Viewport, resources Resources) (*computed.Snapshot, error) {
	if viewport.Width <= 0 || viewport.Height <= 0 {
		return nil, fmt.Errorf("render: invalid viewport %dx%d", viewport.Width, viewport.Height)
	}
	return computed.ComputeReadView(view, computed.Input{
		Environment:          styleEnvironment(viewport),
		Stylesheets:          resources.Stylesheets,
		UserStylesheets:      resources.UserStylesheets,
		UserAgentStylesheets: resources.UserAgentStylesheets,
		SelectorState:        resources.SelectorState,
	})
}

// RenderWithStyleSnapshot lays out and paints document using an already
// computed immutable style snapshot. The snapshot must belong to document and
// the supplied viewport; decoded images continue to come from resources.
func RenderWithStyleSnapshot(document *dom.Node, viewport Viewport, resources Resources, snapshot *computed.Snapshot) (*Frame, error) {
	fonts, err := newFontBook()
	if err != nil {
		return nil, err
	}
	defer fonts.Close()
	return renderWithStyleSnapshotAndFonts(document, viewport, resources, snapshot, fonts)
}

// ComputeLayoutSnapshotWithStyleSnapshot lays out document without building a
// display list. The result can serve synchronous resolved-style reads and can
// later be painted without repeating layout.
func ComputeLayoutSnapshotWithStyleSnapshot(document *dom.Node, viewport Viewport, resources Resources, snapshot *computed.Snapshot) (*LayoutSnapshot, error) {
	fonts, err := newFontBook()
	if err != nil {
		return nil, err
	}
	defer fonts.Close()
	return computeLayoutWithStyleSnapshotAndFonts(document, viewport, resources, snapshot, fonts)
}

// ComputeLayoutSnapshotFromReadView lays out one coherent stable-ID DOM read
// without publishing a Frame or building a display list.
func ComputeLayoutSnapshotFromReadView(view dom.ReadView, viewport Viewport, resources Resources, snapshot *computed.Snapshot) (*LayoutSnapshot, error) {
	access, err := view.Acquire()
	if err != nil {
		return nil, err
	}
	defer access.Close()
	fonts, err := newFontBook()
	if err != nil {
		return nil, err
	}
	defer fonts.Close()
	return computeReadAccessLayoutWithStyleSnapshotAndFonts(access, viewport, resources, snapshot, fonts)
}

// RenderWithLayoutSnapshot paints a pointer-based layout snapshot without
// repeating style or layout computation.
func RenderWithLayoutSnapshot(document *dom.Node, snapshot *LayoutSnapshot) (*Frame, error) {
	if err := validatePointerLayoutSnapshot(document, snapshot); err != nil {
		return nil, err
	}
	return frameFromLayout(document, snapshot), nil
}

// RenderReadViewWithLayoutSnapshot paints a stable-ID layout snapshot from the
// same Document version without repeating style or layout computation.
func RenderReadViewWithLayoutSnapshot(view dom.ReadView, snapshot *LayoutSnapshot) (*Frame, error) {
	access, err := view.Acquire()
	if err != nil {
		return nil, err
	}
	defer access.Close()
	if err := validateStableLayoutSnapshot(access, snapshot); err != nil {
		return nil, err
	}
	return frameFromLayout(access.Root(), snapshot), nil
}

// RenderReadViewWithStyleSnapshot lays out and paints one coherent stable-ID
// DOM read using an ID-only Snapshot produced by ComputeStyleSnapshotFromReadView.
// Neither the Snapshot nor the resulting Frame's computed-style data retains
// callback-scoped DOM pointers.
func RenderReadViewWithStyleSnapshot(view dom.ReadView, viewport Viewport, resources Resources, snapshot *computed.Snapshot) (*Frame, error) {
	access, err := view.Acquire()
	if err != nil {
		return nil, err
	}
	defer access.Close()
	fonts, err := newFontBook()
	if err != nil {
		return nil, err
	}
	defer fonts.Close()
	return renderReadAccessWithStyleSnapshotAndFonts(access, viewport, resources, snapshot, fonts)
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
	snapshot, err := ComputeStyleSnapshot(document, viewport, resources)
	if err != nil {
		return nil, err
	}
	return renderWithStyleSnapshotAndFonts(document, viewport, resources, snapshot, fonts)
}

func renderWithStyleSnapshotAndFonts(document *dom.Node, viewport Viewport, resources Resources, snapshot *computed.Snapshot, fonts *fontBook) (*Frame, error) {
	layout, err := computeLayoutWithStyleSnapshotAndFonts(document, viewport, resources, snapshot, fonts)
	if err != nil {
		return nil, err
	}
	return frameFromLayout(document, layout), nil
}

func computeLayoutWithStyleSnapshotAndFonts(document *dom.Node, viewport Viewport, resources Resources, snapshot *computed.Snapshot, fonts *fontBook) (*LayoutSnapshot, error) {
	if viewport.Width <= 0 || viewport.Height <= 0 {
		return nil, fmt.Errorf("render: invalid viewport %dx%d", viewport.Width, viewport.Height)
	}
	if err := validateDocument(document); err != nil {
		return nil, err
	}
	if snapshot == nil {
		return nil, fmt.Errorf("render: nil style snapshot")
	}
	if snapshot.Root() != document {
		return nil, fmt.Errorf("render: style snapshot belongs to a different document")
	}
	environment := snapshot.Environment()
	if environment.Width != viewport.Width || environment.Height != viewport.Height {
		return nil, fmt.Errorf("render: style snapshot viewport is %dx%d, want %dx%d", environment.Width, environment.Height, viewport.Width, viewport.Height)
	}
	styledRoot := projectStyleTree(document, snapshot)
	if styledRoot == nil {
		return nil, fmt.Errorf("render: style snapshot does not contain the document root")
	}
	rootBox, styles, err := layoutProjectedStyles(styledRoot, viewport, resources, fonts)
	if err != nil {
		return nil, err
	}
	return newPointerLayoutSnapshot(document, rootBox, styles, viewport, snapshot), nil
}

func renderReadAccessWithStyleSnapshotAndFonts(access *dom.ReadAccess, viewport Viewport, resources Resources, snapshot *computed.Snapshot, fonts *fontBook) (*Frame, error) {
	layout, err := computeReadAccessLayoutWithStyleSnapshotAndFonts(access, viewport, resources, snapshot, fonts)
	if err != nil {
		return nil, err
	}
	return frameFromLayout(access.Root(), layout), nil
}

func computeReadAccessLayoutWithStyleSnapshotAndFonts(access *dom.ReadAccess, viewport Viewport, resources Resources, snapshot *computed.Snapshot, fonts *fontBook) (*LayoutSnapshot, error) {
	if viewport.Width <= 0 || viewport.Height <= 0 {
		return nil, fmt.Errorf("render: invalid viewport %dx%d", viewport.Width, viewport.Height)
	}
	document := access.Root()
	if err := validateDocument(document); err != nil {
		return nil, err
	}
	if snapshot == nil {
		return nil, fmt.Errorf("render: nil style snapshot")
	}
	if snapshot.DocumentIdentity() != access.Identity() {
		return nil, fmt.Errorf("render: style snapshot belongs to a different document")
	}
	rootID, ok := access.ID(document)
	if !ok || snapshot.RootID() != rootID {
		return nil, fmt.Errorf("render: style snapshot belongs to a different document")
	}
	if snapshot.Version() != access.Version() {
		return nil, fmt.Errorf("render: style snapshot version is %d, want %d", snapshot.Version(), access.Version())
	}
	environment := snapshot.Environment()
	if environment.Width != viewport.Width || environment.Height != viewport.Height {
		return nil, fmt.Errorf("render: style snapshot viewport is %dx%d, want %dx%d", environment.Width, environment.Height, viewport.Width, viewport.Height)
	}
	styledRoot := projectReadAccessStyleTree(document, access, snapshot)
	if styledRoot == nil {
		return nil, fmt.Errorf("render: style snapshot does not contain the document root")
	}
	rootBox, styles, err := layoutProjectedStyles(styledRoot, viewport, resources, fonts)
	if err != nil {
		return nil, err
	}
	return newStableLayoutSnapshot(access, rootBox, styles, viewport, snapshot)
}

func layoutProjectedStyles(styledRoot *styledNode, viewport Viewport, resources Resources, fonts *fontBook) (*Box, map[*dom.Node]computedStyle, error) {
	rootBox, styles, err := layoutDocument(styledRoot, viewport, resources.Images, fonts)
	if err != nil {
		return nil, nil, fmt.Errorf("render: layout: %w", err)
	}
	return rootBox, styles, nil
}

func frameFromLayout(document *dom.Node, layout *LayoutSnapshot) *Frame {
	displayList := buildDisplayList(document, layout.root, layout.styles, layout.viewport)
	return &Frame{
		Viewport:       layout.viewport,
		Root:           layout.root,
		Layout:         layout,
		ComputedStyles: layout.computedStyles,
		DisplayList:    displayList,
	}
}

func validatePointerLayoutSnapshot(document *dom.Node, snapshot *LayoutSnapshot) error {
	if err := validateDocument(document); err != nil {
		return err
	}
	if snapshot == nil {
		return fmt.Errorf("render: nil layout snapshot")
	}
	if snapshot.rootNode != document || snapshot.document != (dom.DocumentIdentity{}) {
		return fmt.Errorf("render: layout snapshot belongs to a different document")
	}
	return nil
}

func validateStableLayoutSnapshot(access *dom.ReadAccess, snapshot *LayoutSnapshot) error {
	document := access.Root()
	if err := validateDocument(document); err != nil {
		return err
	}
	if snapshot == nil {
		return fmt.Errorf("render: nil layout snapshot")
	}
	if snapshot.document != access.Identity() {
		return fmt.Errorf("render: layout snapshot belongs to a different document")
	}
	rootID, ok := access.ID(document)
	if !ok || snapshot.rootID != rootID {
		return fmt.Errorf("render: layout snapshot belongs to a different document")
	}
	if snapshot.version != access.Version() {
		return fmt.Errorf("render: layout snapshot version is %d, want %d", snapshot.version, access.Version())
	}
	return nil
}

func buildDisplayList(document *dom.Node, root *Box, styles map[*dom.Node]computedStyle, viewport Viewport) DisplayList {
	list := DisplayList{Viewport: viewport}
	canvas := Rect{Width: float64(viewport.Width), Height: float64(viewport.Height)}
	list.Commands = append(list.Commands, Command{Kind: FillRectCommand, Rect: canvas, Color: opaqueWhite})

	htmlNode := findElement(document, "html")
	bodyNode := findElement(document, "body")
	htmlBackground, htmlHasBackground := color.NRGBA{}, false
	if htmlStyle, ok := styles[htmlNode]; ok && htmlStyle.Visibility() == visibilityVisible {
		htmlBackground, htmlHasBackground = htmlStyle.Background()
	}
	bodyBackground, bodyHasBackground := color.NRGBA{}, false
	if bodyStyle, ok := styles[bodyNode]; ok && bodyStyle.Visibility() == visibilityVisible {
		bodyBackground, bodyHasBackground = bodyStyle.Background()
	}
	if htmlHasBackground {
		list.Commands = append(list.Commands, Command{Kind: FillRectCommand, Rect: canvas, Color: htmlBackground})
	} else if bodyHasBackground {
		// HTML propagates the body background to the canvas when the root has
		// no background of its own.
		list.Commands = append(list.Commands, Command{Kind: FillRectCommand, Rect: canvas, Color: bodyBackground})
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
	visible := hasStyle && style.Visibility() == visibilityVisible
	grouped := hasStyle && style.Opacity() < 1
	if grouped {
		list.Commands = append(list.Commands, Command{Kind: BeginOpacityCommand, Opacity: style.Opacity()})
	}
	background, hasBackground := style.Background()
	if visible && hasBackground && box.Bounds.Width > 0 && box.Bounds.Height > 0 {
		list.Commands = append(list.Commands, Command{
			Kind:  FillRectCommand,
			Node:  box.Node,
			Rect:  box.Bounds,
			Color: background,
		})
	}
	if visible {
		paintBoxBorders(list, box, style)
	}
	negative, nonNegative := positionedPaintChildren(box)
	for _, child := range negative {
		paintBox(list, child, styles)
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
			if !child.positioned {
				paintBox(list, child, styles)
			}
		}
	}
	for _, child := range nonNegative {
		paintBox(list, child, styles)
	}
	if grouped {
		list.Commands = append(list.Commands, Command{Kind: EndOpacityCommand})
	}
}

func positionedPaintChildren(box *Box) (negative, nonNegative []*Box) {
	if box == nil {
		return nil, nil
	}
	for _, child := range box.Children {
		if child == nil || !child.positioned {
			continue
		}
		if !child.zIndexAuto && child.zIndex < 0 {
			negative = append(negative, child)
		} else {
			nonNegative = append(nonNegative, child)
		}
	}
	sort.SliceStable(negative, func(left, right int) bool {
		return negative[left].zIndex < negative[right].zIndex
	})
	sort.SliceStable(nonNegative, func(left, right int) bool {
		leftIndex := nonNegative[left].zIndex
		if nonNegative[left].zIndexAuto {
			leftIndex = 0
		}
		rightIndex := nonNegative[right].zIndex
		if nonNegative[right].zIndexAuto {
			rightIndex = 0
		}
		return leftIndex < rightIndex
	})
	return negative, nonNegative
}

func paintBoxBorders(list *DisplayList, box *Box, style computedStyle) {
	bounds := box.Bounds
	borders := box.Border
	// Physical sides are painted in top/right/bottom/left order. Uniform solid
	// borders are exact; diagonal corner splitting for differently colored
	// adjacent sides is deferred with the remaining advanced border geometry.
	paintBorderEdge(list, box.Node, Rect{X: bounds.X, Y: bounds.Y, Width: bounds.Width, Height: borders.Top}, borderPaintColor(style.BorderTop(), style.Color()))
	paintBorderEdge(list, box.Node, Rect{X: bounds.X + bounds.Width - borders.Right, Y: bounds.Y, Width: borders.Right, Height: bounds.Height}, borderPaintColor(style.BorderRight(), style.Color()))
	paintBorderEdge(list, box.Node, Rect{X: bounds.X, Y: bounds.Y + bounds.Height - borders.Bottom, Width: bounds.Width, Height: borders.Bottom}, borderPaintColor(style.BorderBottom(), style.Color()))
	paintBorderEdge(list, box.Node, Rect{X: bounds.X, Y: bounds.Y, Width: borders.Left, Height: bounds.Height}, borderPaintColor(style.BorderLeft(), style.Color()))
}

func paintBorderEdge(list *DisplayList, node *dom.Node, rectangle Rect, edgeColor color.NRGBA) {
	if rectangle.Width <= 0 || rectangle.Height <= 0 || edgeColor.A == 0 {
		return
	}
	list.Commands = append(list.Commands, Command{Kind: FillRectCommand, Node: node, Rect: rectangle, Color: edgeColor})
}

func borderPaintColor(side borderSide, currentColor color.NRGBA) color.NRGBA {
	if side.Style() != borderStyleSolid {
		return color.NRGBA{}
	}
	if borderColor, hasColor := side.Color(); hasColor {
		return borderColor
	}
	return currentColor
}

func paintInlineFragment(list *DisplayList, fragment InlineFragment, styles map[*dom.Node]computedStyle) {
	switch fragment.Kind {
	case TextFragmentKind:
		paintTextFragment(list, fragment.Text, styles)
	case ImageFragmentKind:
		paintImageFragment(list, fragment.Image, styles)
	}
}

func paintTextFragment(list *DisplayList, fragment TextFragment, styles map[*dom.Node]computedStyle) {
	if style, ok := styles[fragment.Node]; ok && style.Visibility() != visibilityVisible {
		return
	}
	list.Commands = append(list.Commands, Command{
		Kind: DrawTextCommand, Node: fragment.Node, Color: fragment.Color, Text: fragment.Text,
		X: fragment.X, BaselineY: fragment.BaselineY,
		FontSize: fragment.FontSize, FontFamily: fragment.FontFamily,
		FontWeight: fragment.FontWeight, FontStyle: fragment.FontStyle,
	})
	if fragmentStyle, ok := styles[fragment.Node]; ok && fragmentStyle.Underline() {
		list.Commands = append(list.Commands, Command{
			Kind:  FillRectCommand,
			Node:  fragment.Node,
			Rect:  Rect{X: fragment.X, Y: fragment.BaselineY + math.Max(1, fragment.FontSize/16), Width: fragment.Width, Height: math.Max(1, fragment.FontSize/16)},
			Color: fragment.Color,
		})
	}
}

func paintImageFragment(list *DisplayList, fragment ImageFragment, styles map[*dom.Node]computedStyle) {
	if fragment.Image == nil || fragment.Bounds.Width <= 0 || fragment.Bounds.Height <= 0 {
		return
	}
	if style, ok := styles[fragment.Node]; ok && style.Visibility() != visibilityVisible {
		return
	}
	list.Commands = append(list.Commands, Command{Kind: DrawImageCommand, Node: fragment.Node, Rect: fragment.Bounds, Image: fragment.Image, Opacity: fragment.Opacity})
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
