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
// laying out, and painting a document. Entries are keyed by the DOM element
// that initiated the request so duplicate URLs remain distinct consumers.
type Resources struct {
	Stylesheets map[*dom.Node]css.Stylesheet
	Images      map[*dom.Node]image.Image
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

// Box is retained layout geometry for painting and future hit testing.
type Box struct {
	Node          *dom.Node
	Bounds        Rect
	ContentBounds Rect
	Padding       Edges
	Border        Edges
	Children      []*Box
	Fragments     []InlineFragment
	Text          []TextFragment
	flow          []flowItem
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
	Node       *dom.Node
	Text       string
	X          float64
	BaselineY  float64
	Width      float64
	Height     float64
	FontSize   float64
	FontWeight FontWeight
	Color      color.NRGBA
}

// ImageFragment is one positioned decoded image.
type ImageFragment struct {
	Node    *dom.Node
	Image   image.Image
	Bounds  Rect
	Opacity float64
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

	Rect  Rect
	Color color.NRGBA

	Text       string
	X          float64
	BaselineY  float64
	FontSize   float64
	FontWeight FontWeight
	Opacity    float64
	Image      image.Image
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
	ComputedStyles *computed.Snapshot
	DisplayList    DisplayList
}
