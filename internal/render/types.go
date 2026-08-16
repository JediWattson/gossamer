// Package render turns DOM documents into backend-neutral display lists.
package render

import (
	"image"
	"image/color"

	"github.com/JediWattson/gossamer/internal/css"
	"github.com/JediWattson/gossamer/internal/dom"
	computed "github.com/JediWattson/gossamer/internal/style"
)

// Viewport defines the CSS-pixel dimensions of a rendered page.
type Viewport struct {
	Width  int
	Height int
}

// DefaultViewport is the initial headless browser viewport.
var DefaultViewport = Viewport{Width: 800, Height: 600}

// Resources contains decoded navigation resources consumed while styling,
// laying out, and painting a document. Author stylesheets and images are keyed
// by the DOM element that initiated the request so duplicate URLs remain
// distinct consumers. User and user-agent sheets retain their slice order
// within their respective cascade origins.
type Resources struct {
	Stylesheets          map[*dom.Node]css.Stylesheet
	InlineDeclarations   map[*dom.Node][]css.SourcedDeclaration
	UserStylesheets      []css.Stylesheet
	UserAgentStylesheets []css.Stylesheet
	SelectorState        computed.SelectorState
	Images               map[*dom.Node]image.Image
}

// Rect is an axis-aligned rectangle in CSS pixels.
type Rect struct {
	X      float64
	Y      float64
	Width  float64
	Height float64
}

// Edges contains the used top, right, bottom, and left CSS-pixel widths of a
// box-model layer.
type Edges struct {
	Top    float64
	Right  float64
	Bottom float64
	Left   float64
}

// FontWeight selects the initial supported font faces. The alias preserves the
// renderer API while computed-value ownership lives in internal/style.
type FontWeight = computed.FontWeight

const (
	FontWeightNormal = computed.FontWeightNormal
	FontWeightBold   = computed.FontWeightBold
)

type FontStyle = computed.FontStyle

const (
	FontStyleNormal  = computed.FontStyleNormal
	FontStyleItalic  = computed.FontStyleItalic
	FontStyleOblique = computed.FontStyleOblique
)

type FontFamily = computed.FontFamily

const (
	FontFamilySerif     = computed.FontFamilySerif
	FontFamilySansSerif = computed.FontFamilySansSerif
	FontFamilyMonospace = computed.FontFamilyMonospace
	FontFamilySystemUI  = computed.FontFamilySystemUI
)

// Box is retained layout geometry for painting and future hit testing.
type Box struct {
	Node          *dom.Node
	Pseudo        css.PseudoElement
	Bounds        Rect
	ContentBounds Rect
	Padding       Edges
	Border        Edges
	Children      []*Box
	Fragments     []InlineFragment
	Text          []TextFragment
	flow          []flowItem
	positioned    bool
	zIndex        int
	zIndexAuto    bool
	style         computedStyle
	hasStyle      bool
	paintOpacity  float64
	hasOpacity    bool
	// percentHeightResolved distinguishes a percentage-dependent specified
	// height that had a definite containing-block base from the same value
	// computing to auto in an indefinite-height containing block. CSSOM uses
	// this bit to decide whether layout may replace the computed percentage
	// with a used pixel height.
	percentHeightResolved bool
	// Table formatting can paint a grid independently of the wrapper occupied
	// by captions, clip structural backgrounds to cell tracks, suppress empty
	// cell decorations, and draw harmonized collapsed borders after its cells.
	decorationBounds    Rect
	hasDecorationBounds bool
	backgroundRects     []Rect
	suppressDecorations bool
	suppressBorders     bool
	afterPaint          []boxPaintRect
	gridColumnSizes     []float64
	gridRowSizes        []float64
	gridColumnLineNames [][]string
	gridRowLineNames    [][]string
	gridColumnSubgrid   bool
	gridRowSubgrid      bool
}

type boxPaintRect struct {
	Node   *dom.Node
	Pseudo css.PseudoElement
	Rect   Rect
	Color  color.NRGBA
}

type flowItem struct {
	fragment InlineFragment
	box      *Box
}

// InlineFragmentKind identifies one atomic inline layout result.
type InlineFragmentKind uint8

const (
	TextFragmentKind InlineFragmentKind = iota
	ImageFragmentKind
)

// InlineFragment preserves DOM/layout order across text and replaced content.
// Exactly one of Text or Image is populated according to Kind.
type InlineFragment struct {
	Kind  InlineFragmentKind
	Text  TextFragment
	Image ImageFragment
}

// TextFragment is one positioned run of text.
type TextFragment struct {
	Node      *dom.Node
	Pseudo    css.PseudoElement
	Text      string
	X         float64
	BaselineY float64
	// BaselineOffset is the distance from the fragment's top edge to its
	// alphabetic baseline. Older synthetic fragments leave it zero and use
	// Height as the conservative bounds fallback.
	BaselineOffset float64
	Width          float64
	Height         float64
	FontSize       float64
	FontFamily     FontFamily
	FontWeight     FontWeight
	FontStyle      FontStyle
	Color          color.NRGBA
	Visible        bool
	Underline      bool
}

func textFragmentBounds(fragment TextFragment) Rect {
	baselineOffset := fragment.BaselineOffset
	if baselineOffset <= 0 || baselineOffset > fragment.Height {
		baselineOffset = fragment.Height
	}
	return Rect{
		X:      fragment.X,
		Y:      fragment.BaselineY - baselineOffset,
		Width:  fragment.Width,
		Height: fragment.Height,
	}
}

// ImageFragment is one positioned decoded image.
type ImageFragment struct {
	Node                  *dom.Node
	Image                 image.Image
	Bounds                Rect
	Opacity               float64
	percentHeightResolved bool
}

// CommandKind identifies a display-list operation.
type CommandKind uint8

const (
	FillRectCommand CommandKind = iota
	DrawTextCommand
	BeginOpacityCommand
	EndOpacityCommand
	DrawImageCommand
)

// Command is a backend-neutral paint operation.
type Command struct {
	Kind CommandKind
	// Node identifies the DOM owner of this paint operation. It is retained
	// only with the Frame and lets Page project immutable document-space paint
	// through mutable scroll offsets without rebuilding layout.
	Node *dom.Node
	// Pseudo identifies generated paint owned by Node. Zero is principal DOM
	// content.
	Pseudo css.PseudoElement

	Rect  Rect
	Color color.NRGBA

	Text       string
	X          float64
	BaselineY  float64
	FontSize   float64
	FontFamily FontFamily
	FontWeight FontWeight
	FontStyle  FontStyle
	Opacity    float64
	Image      image.Image
	HasClip    bool
	Clip       Rect
}

// VisualTransform projects one node-owned command into viewport space.
// Offsets are subtracted from document coordinates. Clip is already expressed
// in viewport coordinates and represents the intersection of scrollports that
// contain the command.
type VisualTransform struct {
	OffsetX float64
	OffsetY float64
	HasClip bool
	Clip    Rect
}

// DisplayList is an ordered set of paint operations for a viewport.
type DisplayList struct {
	Viewport Viewport
	Commands []Command
}

// Frame contains reusable layout geometry and its paint operations.
type Frame struct {
	Viewport       Viewport
	Root           *Box
	Layout         *LayoutSnapshot
	ComputedStyles *computed.Snapshot
	DisplayList    DisplayList
}

// ScrollDisplayList returns a shallow frame copy whose paint commands are
// translated from document coordinates into viewport coordinates. Layout and
// stable node geometry remain immutable document-space snapshots.
func ScrollDisplayList(frame *Frame, x, y float64) *Frame {
	if frame == nil || (x == 0 && y == 0) {
		return frame
	}
	result := *frame
	result.DisplayList = frame.DisplayList
	result.DisplayList.Commands = append([]Command(nil), frame.DisplayList.Commands...)
	for index := range result.DisplayList.Commands {
		command := &result.DisplayList.Commands[index]
		switch command.Kind {
		case FillRectCommand, DrawImageCommand:
			command.Rect.X -= x
			command.Rect.Y -= y
		case DrawTextCommand:
			command.X -= x
			command.BaselineY -= y
		}
	}
	return &result
}

// TransformDisplayList returns a shallow Frame copy with node-owned paint
// commands projected through Page-owned root and element scrolling state.
// Layout, boxes, and stable geometry remain immutable document-space data.
func TransformDisplayList(frame *Frame, transforms map[*dom.Node]VisualTransform) *Frame {
	if frame == nil || len(transforms) == 0 {
		return frame
	}
	result := *frame
	result.DisplayList = frame.DisplayList
	result.DisplayList.Commands = append([]Command(nil), frame.DisplayList.Commands...)
	for index := range result.DisplayList.Commands {
		command := &result.DisplayList.Commands[index]
		transform, ok := transforms[command.Node]
		if !ok || command.Node == nil {
			continue
		}
		switch command.Kind {
		case FillRectCommand, DrawImageCommand:
			command.Rect.X -= transform.OffsetX
			command.Rect.Y -= transform.OffsetY
		case DrawTextCommand:
			command.X -= transform.OffsetX
			command.BaselineY -= transform.OffsetY
		}
		command.HasClip = transform.HasClip
		command.Clip = transform.Clip
	}
	return &result
}
