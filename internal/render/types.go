// Package render turns DOM documents into backend-neutral display lists.
package render

import (
	"image/color"

	"github.com/JediWattson/gossamer/internal/dom"
)

// Viewport defines the CSS-pixel dimensions of a rendered page.
type Viewport struct {
	Width  int
	Height int
}

// DefaultViewport is the initial headless browser viewport.
var DefaultViewport = Viewport{Width: 800, Height: 600}

// Rect is an axis-aligned rectangle in CSS pixels.
type Rect struct {
	X      float64
	Y      float64
	Width  float64
	Height float64
}

// FontWeight selects the initial supported font faces.
type FontWeight uint8

const (
	FontWeightNormal FontWeight = iota
	FontWeightBold
)

// Box is retained layout geometry for painting and future hit testing.
type Box struct {
	Node     *dom.Node
	Bounds   Rect
	Children []*Box
	Text     []TextFragment
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

// CommandKind identifies a display-list operation.
type CommandKind uint8

const (
	FillRectCommand CommandKind = iota
	DrawTextCommand
	BeginOpacityCommand
	EndOpacityCommand
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
}

// DisplayList is an ordered set of paint operations for a viewport.
type DisplayList struct {
	Viewport Viewport
	Commands []Command
}

// Frame contains reusable layout geometry and its paint operations.
type Frame struct {
	Viewport    Viewport
	Root        *Box
	DisplayList DisplayList
}
