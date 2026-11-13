package render

import (
	"fmt"
	"image"
	"image/color"
	"io"
	"math"
	"sort"
	"sync"

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
	fonts, err := newFontBook(resources.Fonts)
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
		InlineDeclarations:   resources.InlineDeclarations,
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
		InlineDeclarations:   resources.InlineDeclarations,
		UserStylesheets:      resources.UserStylesheets,
		UserAgentStylesheets: resources.UserAgentStylesheets,
		SelectorState:        resources.SelectorState,
	})
}

// RestyleStyleSnapshotFromReadView incrementally updates an immutable style
// snapshot for a coherent mutation-journal suffix. It returns reused=false
// when the renderer must run ComputeStyleSnapshotFromReadView instead.
func RestyleStyleSnapshotFromReadView(
	view dom.ReadView,
	viewport Viewport,
	resources Resources,
	previous *computed.Snapshot,
	records []dom.MutationRecord,
) (*computed.Snapshot, bool, error) {
	if viewport.Width <= 0 || viewport.Height <= 0 {
		return nil, false, fmt.Errorf("render: invalid viewport %dx%d", viewport.Width, viewport.Height)
	}
	if previous == nil {
		return nil, false, nil
	}
	return previous.RestyleReadViewAfterMutations(view, computed.Input{
		Environment:          styleEnvironment(viewport),
		Stylesheets:          resources.Stylesheets,
		InlineDeclarations:   resources.InlineDeclarations,
		UserStylesheets:      resources.UserStylesheets,
		UserAgentStylesheets: resources.UserAgentStylesheets,
		SelectorState:        resources.SelectorState,
	}, records)
}

// RenderWithStyleSnapshot lays out and paints document using an already
// computed immutable style snapshot. The snapshot must belong to document and
// the supplied viewport; decoded images continue to come from resources.
func RenderWithStyleSnapshot(document *dom.Node, viewport Viewport, resources Resources, snapshot *computed.Snapshot) (*Frame, error) {
	fonts, err := newFontBook(resources.Fonts)
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
	fonts, err := newFontBook(resources.Fonts)
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
	fonts, err := newFontBook(resources.Fonts)
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
	fonts, err := newFontBook(resources.Fonts)
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
	fonts, err := newFontBook(resources.Fonts)
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
	return newPointerLayoutSnapshot(document, rootBox, styles, resources.BackgroundImages, viewport, snapshot, resources.Fonts), nil
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
	return newStableLayoutSnapshot(access, rootBox, styles, resources.BackgroundImages, viewport, snapshot, resources.Fonts)
}

func layoutProjectedStyles(styledRoot *styledNode, viewport Viewport, resources Resources, fonts *fontBook) (*Box, map[*dom.Node]computedStyle, error) {
	fixupTableFormattingTree(styledRoot)
	rootBox, styles, err := layoutDocument(styledRoot, viewport, resources.Images, fonts)
	if err != nil {
		return nil, nil, fmt.Errorf("render: layout: %w", err)
	}
	return rootBox, styles, nil
}

func frameFromLayout(document *dom.Node, layout *LayoutSnapshot) *Frame {
	displayList := buildDisplayList(document, layout.root, layout.styles, layout.backgroundImages, layout.viewport)
	return &Frame{
		Viewport:       layout.viewport,
		Root:           layout.root,
		Layout:         layout,
		ComputedStyles: layout.computedStyles,
		DisplayList:    displayList,
		Fonts:          layout.fonts,
		paintArtifact:  newPaintArtifactID(),
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

func buildDisplayList(document *dom.Node, root *Box, styles map[*dom.Node]computedStyle, backgroundImages map[*dom.Node]map[string]image.Image, viewport Viewport) DisplayList {
	list := DisplayList{Viewport: viewport, backgroundImages: backgroundImages}
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
	commandStart := len(list.Commands)
	style, hasStyle := box.style, box.hasStyle
	if !hasStyle {
		style, hasStyle = styles[box.Node]
	}
	visible := hasStyle && style.Visibility() == visibilityVisible
	transformed := hasStyle && hasUsedTransform(style)
	if transformed {
		list.Commands = append(list.Commands, Command{
			Kind: BeginTransformCommand, Node: box.Node, Pseudo: box.Pseudo,
			Transform: usedTransform(style, box.Bounds, list.Viewport),
		})
	}
	opacity := style.Opacity()
	if box.hasOpacity {
		opacity = box.paintOpacity
	}
	grouped := hasStyle && opacity < 1
	if grouped {
		list.Commands = append(list.Commands, Command{Kind: BeginOpacityCommand, Node: box.Node, Pseudo: box.Pseudo, Opacity: opacity})
	}
	paintBoxDecorations(list, box, style, visible)
	clipped := hasStyle && clipsOverflow(style)
	if clipped {
		clip := boxClientBounds(box)
		outer := usedCornerRadii(style, box.Bounds, list.Viewport)
		list.Commands = append(list.Commands, Command{
			Kind: BeginClipCommand, Node: box.Node, Pseudo: box.Pseudo,
			Rect: clip, Radii: insetCornerRadii(outer, box.Border),
		})
	}
	if box.tableWrapper && box.tableRoot != nil {
		paintTableWrapperContents(list, box, styles)
	} else {
		paintBoxContents(list, box, styles)
	}
	if clipped {
		list.Commands = append(list.Commands, Command{Kind: EndClipCommand, Node: box.Node, Pseudo: box.Pseudo})
	}
	if grouped {
		list.Commands = append(list.Commands, Command{Kind: EndOpacityCommand, Node: box.Node, Pseudo: box.Pseudo})
	}
	if transformed {
		list.Commands = append(list.Commands, Command{Kind: EndTransformCommand, Node: box.Node, Pseudo: box.Pseudo})
	}
	if box.hasClipBounds {
		clipPaintCommands(list, commandStart, len(list.Commands), box.clipBounds)
	}
}

func paintBoxDecorations(list *DisplayList, box *Box, style computedStyle, visible bool) {
	bounds := box.Bounds
	if box.hasDecorationBounds {
		bounds = box.decorationBounds
	}
	radii := usedCornerRadii(style, bounds, list.Viewport)
	if visible && !box.suppressDecorations {
		paintBoxShadows(list, box, style, bounds, radii, false)
	}
	background, hasBackground := style.Background()
	if visible && !box.suppressDecorations && hasBackground {
		if len(box.backgroundRects) == 0 {
			if bounds.Width > 0 && bounds.Height > 0 {
				list.Commands = append(list.Commands, Command{
					Kind: FillRectCommand, Node: box.Node, Pseudo: box.Pseudo,
					Rect: bounds, Color: background, Radii: radii,
				})
			}
		} else {
			for _, rectangle := range box.backgroundRects {
				if rectangle.Width <= 0 || rectangle.Height <= 0 {
					continue
				}
				list.Commands = append(list.Commands, Command{
					Kind: FillRectCommand, Node: box.Node, Pseudo: box.Pseudo,
					Rect: rectangle, Color: background,
				})
			}
		}
	}
	if visible && !box.suppressDecorations && len(box.backgroundRects) == 0 {
		paintBackgroundImages(list, box, style, bounds, radii)
		paintBoxShadows(list, box, style, bounds, radii, true)
	}
	if visible && !box.suppressDecorations && !box.suppressBorders {
		paintBoxBorders(list, box, style)
	}
}

func clipsOverflow(style computedStyle) bool {
	return style.OverflowX() != computed.OverflowVisible || style.OverflowY() != computed.OverflowVisible
}

func insetCornerRadii(outer CornerRadii, edges Edges) CornerRadii {
	return CornerRadii{
		TopLeft: CornerRadius{
			X: math.Max(0, outer.TopLeft.X-edges.Left),
			Y: math.Max(0, outer.TopLeft.Y-edges.Top),
		},
		TopRight: CornerRadius{
			X: math.Max(0, outer.TopRight.X-edges.Right),
			Y: math.Max(0, outer.TopRight.Y-edges.Top),
		},
		BottomRight: CornerRadius{
			X: math.Max(0, outer.BottomRight.X-edges.Right),
			Y: math.Max(0, outer.BottomRight.Y-edges.Bottom),
		},
		BottomLeft: CornerRadius{
			X: math.Max(0, outer.BottomLeft.X-edges.Left),
			Y: math.Max(0, outer.BottomLeft.Y-edges.Bottom),
		},
	}
}

func usedCornerRadii(style computedStyle, bounds Rect, viewport Viewport) CornerRadii {
	resolve := func(radius computed.CornerRadius) CornerRadius {
		x, xOK := radius.Horizontal().Resolve(bounds.Width, float64(viewport.Width), float64(viewport.Height))
		y, yOK := radius.Vertical().Resolve(bounds.Height, float64(viewport.Width), float64(viewport.Height))
		if !xOK || !yOK || !isFinite(x) || !isFinite(y) {
			return CornerRadius{}
		}
		return CornerRadius{X: math.Max(0, x), Y: math.Max(0, y)}
	}
	radii := CornerRadii{
		TopLeft:     resolve(style.BorderTopLeftRadius()),
		TopRight:    resolve(style.BorderTopRightRadius()),
		BottomRight: resolve(style.BorderBottomRightRadius()),
		BottomLeft:  resolve(style.BorderBottomLeftRadius()),
	}
	factor := 1.0
	fit := func(available, requested float64) {
		if requested > 0 && requested > available {
			factor = math.Min(factor, available/requested)
		}
	}
	fit(bounds.Width, radii.TopLeft.X+radii.TopRight.X)
	fit(bounds.Width, radii.BottomLeft.X+radii.BottomRight.X)
	fit(bounds.Height, radii.TopLeft.Y+radii.BottomLeft.Y)
	fit(bounds.Height, radii.TopRight.Y+radii.BottomRight.Y)
	if factor < 1 {
		for _, radius := range []*CornerRadius{&radii.TopLeft, &radii.TopRight, &radii.BottomRight, &radii.BottomLeft} {
			radius.X *= factor
			radius.Y *= factor
		}
	}
	return radii
}

func paintBoxShadows(list *DisplayList, box *Box, style computedStyle, bounds Rect, radii CornerRadii, inset bool) {
	shadows := style.BoxShadows()
	for index := shadows.Len() - 1; index >= 0; index-- {
		shadow, ok := shadows.At(index)
		if !ok || shadow.Inset() != inset {
			continue
		}
		offsetX := resolveLength(shadow.OffsetX(), 0, list.Viewport, 0)
		offsetY := resolveLength(shadow.OffsetY(), 0, list.Viewport, 0)
		blur := math.Max(0, resolveLength(shadow.BlurRadius(), 0, list.Viewport, 0))
		spread := resolveLength(shadow.SpreadRadius(), 0, list.Viewport, 0)
		shadowColor := shadow.Color(style.Color())
		if shadowColor.A == 0 || !isFinite(offsetX) || !isFinite(offsetY) || !isFinite(blur) || !isFinite(spread) {
			continue
		}
		list.Commands = append(list.Commands, Command{
			Kind: DrawBoxShadowCommand, Node: box.Node, Pseudo: box.Pseudo,
			Rect: bounds, Radii: radii, Color: shadowColor,
			ShadowOffsetX: offsetX, ShadowOffsetY: offsetY,
			ShadowBlur: blur, ShadowSpread: spread, ShadowInset: inset,
		})
	}
}

func paintBackgroundImages(list *DisplayList, box *Box, style computedStyle, bounds Rect, radii CornerRadii) {
	images := style.BackgroundImages()
	for index := images.Len() - 1; index >= 0; index-- {
		background, ok := images.At(index)
		if !ok {
			continue
		}
		command := Command{Node: box.Node, Pseudo: box.Pseudo, Rect: bounds, Radii: radii}
		switch background.Kind() {
		case computed.BackgroundImageURL:
			decoded := list.backgroundImages[box.Node][background.URL()]
			if decoded == nil {
				continue
			}
			command.Kind, command.Image, command.Opacity = DrawImageCommand, decoded, 1
		case computed.BackgroundImageLinearGradient, computed.BackgroundImageRadialGradient:
			command.Kind = DrawGradientCommand
			command.GradientKind = LinearGradient
			if background.Kind() == computed.BackgroundImageRadialGradient {
				command.GradientKind = RadialGradient
			}
			command.GradientAngle = background.AngleDegrees()
			command.GradientRepeating = background.Repeating()
			if !resolveGradientStops(&command, background, bounds, style.Color(), list.Viewport) {
				continue
			}
		default:
			continue
		}
		if !radii.empty() {
			appendRoundedClip(&command, RoundedClip{Rect: bounds, Radii: radii})
		}
		list.Commands = append(list.Commands, command)
	}
}

func resolveGradientStops(command *Command, background computed.BackgroundImage, bounds Rect, currentColor color.NRGBA, viewport Viewport) bool {
	if command == nil || background.StopCount() < 2 || background.StopCount() > maxPaintGradientStops {
		return false
	}
	length := math.Hypot(bounds.Width/2, bounds.Height/2)
	if background.Kind() == computed.BackgroundImageLinearGradient {
		angle := background.AngleDegrees() * math.Pi / 180
		length = math.Abs(math.Sin(angle))*bounds.Width + math.Abs(math.Cos(angle))*bounds.Height
	}
	if length <= 0 || !isFinite(length) {
		return false
	}
	offsets := [maxPaintGradientStops]float64{}
	specified := [maxPaintGradientStops]bool{}
	count := background.StopCount()
	for index := 0; index < count; index++ {
		stop, _ := background.Stop(index)
		command.GradientStops[index].Color = stop.Color(currentColor)
		if position, ok := stop.Position(); ok {
			resolved, valid := position.Resolve(length, float64(viewport.Width), float64(viewport.Height))
			if !valid {
				return false
			}
			offsets[index], specified[index] = resolved/length, true
		}
	}
	if !specified[0] {
		offsets[0], specified[0] = 0, true
	}
	if !specified[count-1] {
		offsets[count-1], specified[count-1] = 1, true
	}
	last := offsets[0]
	for index := 1; index < count; index++ {
		if specified[index] {
			if offsets[index] < last {
				offsets[index] = last
			}
			last = offsets[index]
		}
	}
	for start := 0; start < count-1; {
		end := start + 1
		for end < count && !specified[end] {
			end++
		}
		span := offsets[end] - offsets[start]
		for index := start + 1; index < end; index++ {
			offsets[index] = offsets[start] + span*float64(index-start)/float64(end-start)
		}
		start = end
	}
	for index := 0; index < count; index++ {
		command.GradientStops[index].Offset = offsets[index]
	}
	command.GradientStopCount = uint8(count)
	return true
}

func usedTransform(style computedStyle, bounds Rect, viewport Viewport) AffineTransform {
	result := identityTransform()
	translate := style.Translate()
	if !translate.IsNone() {
		result = multiplyTransforms(result, translatedTransform(
			resolveLength(translate.X(), bounds.Width, viewport, 0),
			resolveLength(translate.Y(), bounds.Height, viewport, 0),
		))
	}
	rotate := style.Rotate()
	if !rotate.IsNone() {
		angle := rotate.Radians()
		result = multiplyTransforms(result, AffineTransform{A: math.Cos(angle), B: math.Sin(angle), C: -math.Sin(angle), D: math.Cos(angle)})
	}
	scale := style.Scale()
	if !scale.IsNone() {
		result = multiplyTransforms(result, AffineTransform{A: scale.X(), D: scale.Y()})
	}
	transforms := style.Transforms()
	for index := 0; index < transforms.Len(); index++ {
		operation, ok := transforms.At(index)
		if !ok {
			continue
		}
		a, b, c, d, e, f := operation.Values()
		matrix := identityTransform()
		switch operation.Kind() {
		case computed.TransformMatrix:
			matrix = AffineTransform{A: a, B: b, C: c, D: d, E: e, F: f}
		case computed.TransformTranslate:
			x := resolveLength(operation.X(), bounds.Width, viewport, 0)
			y := resolveLength(operation.Y(), bounds.Height, viewport, 0)
			matrix = translatedTransform(x, y)
		case computed.TransformScale:
			matrix = AffineTransform{A: a, D: b}
		case computed.TransformRotate:
			matrix = AffineTransform{A: math.Cos(a), B: math.Sin(a), C: -math.Sin(a), D: math.Cos(a)}
		case computed.TransformSkew:
			matrix = AffineTransform{A: 1, B: math.Tan(b), C: math.Tan(a), D: 1}
		}
		result = multiplyTransforms(result, matrix)
	}
	origin := style.TransformOrigin()
	originX := bounds.X + resolveLength(origin.X(), bounds.Width, viewport, bounds.Width/2)
	originY := bounds.Y + resolveLength(origin.Y(), bounds.Height, viewport, bounds.Height/2)
	return multiplyTransforms(
		translatedTransform(originX, originY),
		multiplyTransforms(result, translatedTransform(-originX, -originY)),
	)
}

func hasUsedTransform(style computedStyle) bool {
	return !style.Translate().IsNone() || !style.Rotate().IsNone() || !style.Scale().IsNone() || style.Transforms().Len() > 0
}

func paintBoxContents(list *DisplayList, box *Box, styles map[*dom.Node]computedStyle) {
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
	for _, overlay := range box.afterPaint {
		if overlay.Rect.Width <= 0 || overlay.Rect.Height <= 0 || overlay.Color.A == 0 {
			continue
		}
		paintBorderPattern(list, overlay.Node, overlay.Pseudo, overlay.Rect, overlay.Style, overlay.Color, overlay.Edge)
	}
}

func paintTableWrapperContents(list *DisplayList, wrapper *Box, styles map[*dom.Node]computedStyle) {
	root := wrapper.tableRoot
	rootStart := len(list.Commands)
	rootStyle, hasRootStyle := root.style, root.hasStyle
	if !hasRootStyle {
		rootStyle, hasRootStyle = styles[root.Node]
	}
	paintBoxDecorations(list, root, rootStyle, hasRootStyle && rootStyle.Visibility() == visibilityVisible)
	rootDecorationsEnd := len(list.Commands)

	negative, nonNegative := positionedPaintChildrenExcluding(wrapper, root)
	for _, child := range negative {
		paintBox(list, child, styles)
	}
	for _, child := range wrapper.Children {
		if child != root && !child.positioned {
			paintBox(list, child, styles)
		}
	}
	rootContentsStart := len(list.Commands)
	paintBoxContents(list, root, styles)
	rootContentsEnd := len(list.Commands)
	for _, child := range nonNegative {
		paintBox(list, child, styles)
	}
	if root.hasClipBounds {
		clipPaintCommands(list, rootStart, rootDecorationsEnd, root.clipBounds)
		clipPaintCommands(list, rootContentsStart, rootContentsEnd, root.clipBounds)
	}
}

func clipPaintCommands(list *DisplayList, start, end int, bounds Rect) {
	for index := start; index < end; index++ {
		command := &list.Commands[index]
		if command.HasClip {
			command.Clip = intersectRects(command.Clip, bounds)
		} else {
			command.HasClip = true
			command.Clip = bounds
		}
	}
}

func clipPaintCommandsRounded(list *DisplayList, start, end int, clip RoundedClip) {
	if clip.Rect.Width <= 0 || clip.Rect.Height <= 0 {
		clip.Rect.Width, clip.Rect.Height = 0, 0
	}
	for index := start; index < end; index++ {
		command := &list.Commands[index]
		switch command.Kind {
		case BeginOpacityCommand, EndOpacityCommand, BeginTransformCommand, EndTransformCommand, BeginClipCommand, EndClipCommand:
			continue
		}
		if command.HasClip {
			command.Clip = intersectRects(command.Clip, clip.Rect)
		} else {
			command.HasClip = true
			command.Clip = clip.Rect
		}
		appendRoundedClip(command, clip)
	}
}

func appendRoundedClip(command *Command, clip RoundedClip) {
	if command == nil || clip.Radii.empty() || command.RoundedClipCount >= maxCommandRoundedClips {
		return
	}
	command.RoundedClips[command.RoundedClipCount] = clip
	command.RoundedClipCount++
}

func positionedPaintChildrenExcluding(box, excluded *Box) (negative, nonNegative []*Box) {
	if box == nil {
		return nil, nil
	}
	for _, child := range box.Children {
		if child == excluded || child == nil || !child.positioned {
			continue
		}
		if !child.zIndexAuto && child.zIndex < 0 {
			negative = append(negative, child)
		} else {
			nonNegative = append(nonNegative, child)
		}
	}
	sortPositionedPaintChildren(negative, nonNegative)
	return negative, nonNegative
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
	sortPositionedPaintChildren(negative, nonNegative)
	return negative, nonNegative
}

func sortPositionedPaintChildren(negative, nonNegative []*Box) {
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
}

func paintBoxBorders(list *DisplayList, box *Box, style computedStyle) {
	bounds := box.Bounds
	if box.hasDecorationBounds {
		bounds = box.decorationBounds
	}
	borders := box.Border
	radii := usedCornerRadii(style, bounds, list.Viewport)
	if !radii.empty() && roundedBorderSupported(style) {
		sides := [4]borderSide{style.BorderTop(), style.BorderRight(), style.BorderBottom(), style.BorderLeft()}
		command := Command{
			Kind: DrawRoundedBorderCommand, Node: box.Node, Pseudo: box.Pseudo,
			Rect: bounds, Radii: radii, BorderWidths: borders,
		}
		visible := false
		for index, side := range sides {
			command.BorderStyles[index] = side.Style()
			command.BorderColors[index] = side.ResolveColor(style.Color())
			width := []float64{borders.Top, borders.Right, borders.Bottom, borders.Left}[index]
			visible = visible || width > 0 && command.BorderColors[index].A > 0 && side.Style() == borderStyleSolid
		}
		if visible {
			list.Commands = append(list.Commands, command)
		}
		return
	}
	// Physical sides are painted in top/right/bottom/left order. Uniform solid
	// borders are exact; diagonal corner splitting for differently colored
	// adjacent sides is deferred with the remaining advanced border geometry.
	paintBorderSide(list, box.Node, box.Pseudo, Rect{X: bounds.X, Y: bounds.Y, Width: bounds.Width, Height: borders.Top}, style.BorderTop(), style.Color(), borderPaintTop)
	paintBorderSide(list, box.Node, box.Pseudo, Rect{X: bounds.X + bounds.Width - borders.Right, Y: bounds.Y, Width: borders.Right, Height: bounds.Height}, style.BorderRight(), style.Color(), borderPaintRight)
	paintBorderSide(list, box.Node, box.Pseudo, Rect{X: bounds.X, Y: bounds.Y + bounds.Height - borders.Bottom, Width: bounds.Width, Height: borders.Bottom}, style.BorderBottom(), style.Color(), borderPaintBottom)
	paintBorderSide(list, box.Node, box.Pseudo, Rect{X: bounds.X, Y: bounds.Y, Width: borders.Left, Height: bounds.Height}, style.BorderLeft(), style.Color(), borderPaintLeft)
}

func roundedBorderSupported(style computedStyle) bool {
	for _, side := range []borderSide{style.BorderTop(), style.BorderRight(), style.BorderBottom(), style.BorderLeft()} {
		switch side.Style() {
		case borderStyleNone, borderStyleHidden, borderStyleSolid:
		default:
			return false
		}
	}
	return true
}

type borderPaintEdge uint8

const (
	borderPaintTop borderPaintEdge = iota
	borderPaintRight
	borderPaintBottom
	borderPaintLeft
)

const maxBorderPatternSegments = 4096

func paintBorderSide(list *DisplayList, node *dom.Node, pseudo computed.PseudoElement, rectangle Rect, side borderSide, currentColor color.NRGBA, edge borderPaintEdge) {
	edgeColor := side.ResolveColor(currentColor)
	paintBorderPattern(list, node, pseudo, rectangle, side.Style(), edgeColor, edge)
}

func paintBorderPattern(list *DisplayList, node *dom.Node, pseudo computed.PseudoElement, rectangle Rect, style borderStyle, edgeColor color.NRGBA, edge borderPaintEdge) {
	if rectangle.Width <= 0 || rectangle.Height <= 0 || edgeColor.A == 0 || style == borderStyleNone || style == borderStyleHidden {
		return
	}
	switch style {
	case borderStyleDotted:
		paintDottedBorder(list, node, pseudo, rectangle, edgeColor, edge)
	case borderStyleDashed:
		paintDashedBorder(list, node, pseudo, rectangle, edgeColor, edge)
	case borderStyleDouble:
		third := borderThickness(rectangle, edge) / 3
		appendBorderFill(list, node, pseudo, borderStripe(rectangle, edge, 0, third), edgeColor)
		appendBorderFill(list, node, pseudo, borderStripe(rectangle, edge, 2*third, third), edgeColor)
	case borderStyleGroove, borderStyleRidge:
		thickness := borderThickness(rectangle, edge)
		outerDark := style == borderStyleGroove
		if edge == borderPaintRight || edge == borderPaintBottom {
			outerDark = !outerDark
		}
		outer, inner := lightenBorderColor(edgeColor), darkenBorderColor(edgeColor)
		if outerDark {
			outer, inner = inner, outer
		}
		appendBorderFill(list, node, pseudo, borderStripe(rectangle, edge, 0, thickness/2), outer)
		appendBorderFill(list, node, pseudo, borderStripe(rectangle, edge, thickness/2, thickness-thickness/2), inner)
	case borderStyleInset, borderStyleOutset:
		dark := edge == borderPaintTop || edge == borderPaintLeft
		if style == borderStyleOutset {
			dark = !dark
		}
		if dark {
			edgeColor = darkenBorderColor(edgeColor)
		} else {
			edgeColor = lightenBorderColor(edgeColor)
		}
		appendBorderFill(list, node, pseudo, rectangle, edgeColor)
	default:
		appendBorderFill(list, node, pseudo, rectangle, edgeColor)
	}
}

func appendBorderFill(list *DisplayList, node *dom.Node, pseudo computed.PseudoElement, rectangle Rect, edgeColor color.NRGBA) {
	if rectangle.Width <= 0 || rectangle.Height <= 0 || edgeColor.A == 0 {
		return
	}
	list.Commands = append(list.Commands, Command{Kind: FillRectCommand, Node: node, Pseudo: pseudo, Rect: rectangle, Color: edgeColor})
}

func borderThickness(rectangle Rect, edge borderPaintEdge) float64 {
	if edge == borderPaintTop || edge == borderPaintBottom {
		return rectangle.Height
	}
	return rectangle.Width
}

func borderLength(rectangle Rect, edge borderPaintEdge) float64 {
	if edge == borderPaintTop || edge == borderPaintBottom {
		return rectangle.Width
	}
	return rectangle.Height
}

func borderStripe(rectangle Rect, edge borderPaintEdge, outsideOffset, thickness float64) Rect {
	result := rectangle
	switch edge {
	case borderPaintTop:
		result.Y += outsideOffset
		result.Height = thickness
	case borderPaintRight:
		result.X += rectangle.Width - outsideOffset - thickness
		result.Width = thickness
	case borderPaintBottom:
		result.Y += rectangle.Height - outsideOffset - thickness
		result.Height = thickness
	case borderPaintLeft:
		result.X += outsideOffset
		result.Width = thickness
	}
	return result
}

func paintDottedBorder(list *DisplayList, node *dom.Node, pseudo computed.PseudoElement, rectangle Rect, edgeColor color.NRGBA, edge borderPaintEdge) {
	diameter := borderThickness(rectangle, edge)
	length := borderLength(rectangle, edge)
	if diameter <= 0 || length <= 0 {
		return
	}
	count := max(1, int(math.Round((length+diameter)/(2*diameter))))
	if count > maxBorderPatternSegments {
		appendBorderFill(list, node, pseudo, rectangle, edgeColor)
		return
	}
	step := 0.0
	if count > 1 {
		step = (length - diameter) / float64(count-1)
	}
	for index := 0; index < count; index++ {
		position := step * float64(index)
		dot := rectangle
		if edge == borderPaintTop || edge == borderPaintBottom {
			dot.X += position
			dot.Width = diameter
		} else {
			dot.Y += position
			dot.Height = diameter
		}
		list.Commands = append(list.Commands, Command{Kind: FillEllipseCommand, Node: node, Pseudo: pseudo, Rect: dot, Color: edgeColor})
	}
}

func paintDashedBorder(list *DisplayList, node *dom.Node, pseudo computed.PseudoElement, rectangle Rect, edgeColor color.NRGBA, edge borderPaintEdge) {
	thickness := borderThickness(rectangle, edge)
	length := borderLength(rectangle, edge)
	if thickness <= 0 || length <= 0 {
		return
	}
	count := max(1, int(math.Round(length/(6*thickness))))
	if count > maxBorderPatternSegments {
		appendBorderFill(list, node, pseudo, rectangle, edgeColor)
		return
	}
	dash := length / float64(2*count-1)
	for index := 0; index < count; index++ {
		position := 2 * float64(index) * dash
		segment := rectangle
		if edge == borderPaintTop || edge == borderPaintBottom {
			segment.X += position
			segment.Width = math.Min(dash, length-position)
		} else {
			segment.Y += position
			segment.Height = math.Min(dash, length-position)
		}
		appendBorderFill(list, node, pseudo, segment, edgeColor)
	}
}

func lightenBorderColor(value color.NRGBA) color.NRGBA {
	return mixBorderColor(value, color.NRGBA{R: 0xff, G: 0xff, B: 0xff, A: value.A}, 0.35)
}

func darkenBorderColor(value color.NRGBA) color.NRGBA {
	return mixBorderColor(value, color.NRGBA{A: value.A}, 0.35)
}

func mixBorderColor(from, to color.NRGBA, amount float64) color.NRGBA {
	mix := func(left, right uint8) uint8 {
		return uint8(math.Round(float64(left)*(1-amount) + float64(right)*amount))
	}
	return color.NRGBA{R: mix(from.R, to.R), G: mix(from.G, to.G), B: mix(from.B, to.B), A: from.A}
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
	if !fragment.Visible {
		return
	}
	command := Command{
		Kind: DrawTextCommand, Node: fragment.Node, Pseudo: fragment.Pseudo, Color: fragment.Color, Text: fragment.Text,
		X: fragment.X, BaselineY: fragment.BaselineY,
		FontSize: fragment.FontSize, FontFamily: fragment.FontFamily,
		FontWeight: fragment.FontWeight, FontStyle: fragment.FontStyle, WebFont: fragment.WebFont,
		textBounds: textFragmentBounds(fragment),
	}
	if fragment.paintOrientation != textPaintHorizontal {
		command.Rect = fragment.paintBounds
		command.textOrientation = fragment.paintOrientation
		command.textWidth = fragment.logicalWidth
		command.textHeight = fragment.logicalHeight
		command.textBaseline = fragment.logicalBaseline
	}
	list.Commands = append(list.Commands, command)
	if fragment.Underline {
		thickness := math.Max(1, fragment.FontSize/16)
		underline := Rect{X: fragment.X, Y: fragment.BaselineY + thickness, Width: fragment.Width, Height: thickness}
		if fragment.paintOrientation != textPaintHorizontal {
			underline = Rect{
				X: fragment.paintBounds.X, Y: fragment.paintBounds.Y,
				Width: thickness, Height: fragment.paintBounds.Height,
			}
		}
		list.Commands = append(list.Commands, Command{
			Kind:   FillRectCommand,
			Node:   fragment.Node,
			Pseudo: fragment.Pseudo,
			Rect:   underline,
			Color:  fragment.Color,
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

// FrameRasterizer replays immutable frames while retaining parsed font faces,
// a scrolling underlay, bounded paint caches, and lazy overlay workers across
// presentations. Calls and Close are serialized so one rasterizer is safe to
// keep at a session boundary even when presentation and teardown race. A zero
// FrameRasterizer is ready for use.
type FrameRasterizer struct {
	mutex           sync.Mutex
	fonts           *fontBook
	fontSet         []webFontIdentity
	scrollBase      *image.RGBA
	scrollBaseFrame *Frame
	scrollBaseScale float64
	scrollBaseStart int
	scrollOverlay   retainedOverlayIdentity
	overlayPaint    rasterOverlayPaintCache
	overlayJobs     chan overlayReplayJob
	overlayResults  chan overlayReplayResult
	overlayWorkers  sync.WaitGroup
	closed          bool
}

// NewFrameRasterizer creates a reusable frame painter. Call Close when its
// session ends so retained fonts, framebuffers, paint caches, and workers can
// release their resources.
func NewFrameRasterizer() *FrameRasterizer {
	return &FrameRasterizer{}
}

// Rasterize paints a frame at one device pixel per CSS pixel.
func (rasterizer *FrameRasterizer) Rasterize(frame *Frame) (*image.RGBA, error) {
	return rasterizer.RasterizeAtScale(frame, 1)
}

// RasterizeAtScale paints CSS-pixel display-list geometry into a device-pixel
// image and retains compatible font faces for the next frame.
func (rasterizer *FrameRasterizer) RasterizeAtScale(frame *Frame, scale float64) (*image.RGBA, error) {
	if rasterizer == nil {
		return nil, fmt.Errorf("render: nil frame rasterizer")
	}
	if frame == nil {
		return nil, fmt.Errorf("render: nil frame")
	}
	viewport, err := scaledViewport(frame.DisplayList.Viewport, scale)
	if err != nil {
		return nil, err
	}

	rasterizer.mutex.Lock()
	defer rasterizer.mutex.Unlock()
	if rasterizer.closed {
		return nil, fmt.Errorf("render: frame rasterizer is closed")
	}
	if err := rasterizer.prepareFontsLocked(frame.Fonts); err != nil {
		return nil, err
	}
	return rasterizer.rasterizeFullFrameLocked(frame, scale, viewport)
}

func (rasterizer *FrameRasterizer) prepareFontsLocked(frameFonts []WebFontFace) error {
	if rasterizer.fonts == nil || !sameWebFonts(rasterizer.fontSet, frameFonts) {
		fonts, err := newFontBook(frameFonts)
		if err != nil {
			return err
		}
		previous := rasterizer.fonts
		rasterizer.fonts = fonts
		rasterizer.fontSet = snapshotWebFonts(frameFonts)
		rasterizer.scrollBase = nil
		rasterizer.scrollBaseFrame = nil
		rasterizer.scrollBaseScale = 0
		// Cached opacity layers can contain glyphs rendered through the old
		// font book. Rounded coverage shares the same bounded cache owner and
		// is cheap to rebuild when a document's immutable font set changes.
		rasterizer.overlayPaint.clear()
		if previous != nil {
			if err := previous.Close(); err != nil {
				return fmt.Errorf("render: replace retained fonts: %w", err)
			}
		}
	}
	return nil
}

// Close releases all retained font faces. It is safe to call more than once.
func (rasterizer *FrameRasterizer) Close() error {
	if rasterizer == nil {
		return nil
	}
	rasterizer.mutex.Lock()
	defer rasterizer.mutex.Unlock()
	if rasterizer.closed {
		return nil
	}
	rasterizer.closed = true
	fonts := rasterizer.fonts
	rasterizer.fonts = nil
	rasterizer.fontSet = nil
	rasterizer.scrollBase = nil
	rasterizer.scrollBaseFrame = nil
	rasterizer.scrollBaseScale = 0
	rasterizer.scrollBaseStart = 0
	rasterizer.scrollOverlay = retainedOverlayIdentity{}
	rasterizer.overlayPaint.clear()
	rasterizer.closeOverlayWorkersLocked()
	if fonts != nil {
		return fonts.Close()
	}
	return nil
}

// Rasterize paints a frame to an in-memory image. Window backends can instead
// consume Frame.DisplayList directly.
func Rasterize(frame *Frame) (*image.RGBA, error) {
	return RasterizeAtScale(frame, 1)
}

// RasterizeAtScale paints CSS-pixel display-list geometry into a device-pixel
// image. Layout and hit testing remain in CSS pixels; only this final immutable
// paint projection is scaled for a high-density presentation surface. Use a
// FrameRasterizer when painting multiple frames in one session.
func RasterizeAtScale(frame *Frame, scale float64) (*image.RGBA, error) {
	if frame == nil {
		return nil, fmt.Errorf("render: nil frame")
	}
	viewport, err := scaledViewport(frame.DisplayList.Viewport, scale)
	if err != nil {
		return nil, err
	}
	fonts, err := newFontBook(frame.Fonts)
	if err != nil {
		return nil, err
	}
	defer fonts.Close()
	return rasterizeAtScale(frame.DisplayList, fonts, scale, viewport)
}
