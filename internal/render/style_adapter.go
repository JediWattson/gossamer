package render

import (
	"math"
	"strings"

	"github.com/JediWattson/gossamer/internal/css"
	"github.com/JediWattson/gossamer/internal/dom"
	computed "github.com/JediWattson/gossamer/internal/style"
)

type displayMode = computed.DisplayMode

const (
	displayInline      = computed.DisplayInline
	displayInlineBlock = computed.DisplayInlineBlock
	displayBlock       = computed.DisplayBlock
	displayListItem    = computed.DisplayListItem
	displayFlex        = computed.DisplayFlex
	displayInlineFlex  = computed.DisplayInlineFlex
	displayTable       = computed.DisplayTable
	displayInlineTable = computed.DisplayInlineTable
	displayRowGroup    = computed.DisplayTableRowGroup
	displayHeaderGroup = computed.DisplayTableHeaderGroup
	displayFooterGroup = computed.DisplayTableFooterGroup
	displayTableRow    = computed.DisplayTableRow
	displayTableCell   = computed.DisplayTableCell
	displayColumnGroup = computed.DisplayTableColumnGroup
	displayTableColumn = computed.DisplayTableColumn
	displayCaption     = computed.DisplayTableCaption
	displayNone        = computed.DisplayNone
)

type textAlignment = computed.TextAlignment

const (
	alignLeft    = computed.AlignLeft
	alignCenter  = computed.AlignCenter
	alignRight   = computed.AlignRight
	alignStart   = computed.AlignStart
	alignEnd     = computed.AlignEnd
	alignJustify = computed.AlignJustify
)

type verticalAlignMode = computed.VerticalAlignMode
type verticalAlignment = computed.VerticalAlignment

const (
	verticalAlignBaseline   = computed.VerticalAlignBaseline
	verticalAlignSub        = computed.VerticalAlignSub
	verticalAlignSuper      = computed.VerticalAlignSuper
	verticalAlignTextTop    = computed.VerticalAlignTextTop
	verticalAlignTextBottom = computed.VerticalAlignTextBottom
	verticalAlignMiddle     = computed.VerticalAlignMiddle
	verticalAlignTop        = computed.VerticalAlignTop
	verticalAlignBottom     = computed.VerticalAlignBottom
	verticalAlignLength     = computed.VerticalAlignLength
)

type listStyleType = computed.ListStyleType

const (
	listStyleDisc    = computed.ListStyleDisc
	listStyleCircle  = computed.ListStyleCircle
	listStyleSquare  = computed.ListStyleSquare
	listStyleDecimal = computed.ListStyleDecimal
	listStyleNone    = computed.ListStyleNone
)

type borderStyle = computed.BorderStyle

const (
	borderStyleNone   = computed.BorderStyleNone
	borderStyleSolid  = computed.BorderStyleSolid
	borderStyleHidden = computed.BorderStyleHidden
)

type positionMode = computed.PositionMode

const (
	positionStatic   = computed.PositionStatic
	positionRelative = computed.PositionRelative
	positionAbsolute = computed.PositionAbsolute
	positionFixed    = computed.PositionFixed
)

type lengthUnit = computed.LengthUnit

const (
	lengthAuto    = computed.LengthAuto
	lengthPX      = computed.LengthPX
	lengthPercent = computed.LengthPercent
	lengthVW      = computed.LengthVW
	lengthVH      = computed.LengthVH
)

type length = computed.Length
type computedLineHeight = computed.LineHeight
type borderSide = computed.BorderSide
type computedStyle = computed.ComputedStyle
type whiteSpaceMode = computed.WhiteSpaceMode

const (
	boxSizingContentBox = computed.BoxSizingContentBox
	boxSizingBorderBox  = computed.BoxSizingBorderBox
	visibilityVisible   = computed.VisibilityVisible
	whiteSpaceNormal    = computed.WhiteSpaceNormal
	whiteSpaceNoWrap    = computed.WhiteSpaceNoWrap
	whiteSpacePre       = computed.WhiteSpacePre
	whiteSpacePreWrap   = computed.WhiteSpacePreWrap
	whiteSpacePreLine   = computed.WhiteSpacePreLine
	whiteSpaceBreak     = computed.WhiteSpaceBreakSpaces
)

type styledNode struct {
	node          *dom.Node
	pseudo        css.PseudoElement
	generated     bool
	generatedText string
	style         computedStyle
	children      []*styledNode
}

func projectStyleTree(node *dom.Node, snapshot *computed.Snapshot) *styledNode {
	if node == nil || snapshot == nil {
		return nil
	}
	value, ok := snapshot.Lookup(node)
	if !ok {
		return nil
	}
	projected := &styledNode{node: node, style: value}
	projected.children = make([]*styledNode, 0, len(node.Children)+2)
	if before := projectPseudoStyle(node, css.PseudoElementBefore, snapshot.LookupPseudo); before != nil {
		projected.children = append(projected.children, before)
	}
	for _, child := range node.Children {
		if projectedChild := projectStyleTree(child, snapshot); projectedChild != nil {
			projected.children = append(projected.children, projectedChild)
		}
	}
	if after := projectPseudoStyle(node, css.PseudoElementAfter, snapshot.LookupPseudo); after != nil {
		projected.children = append(projected.children, after)
	}
	return projected
}

func projectReadAccessStyleTree(node *dom.Node, access *dom.ReadAccess, snapshot *computed.Snapshot) *styledNode {
	if node == nil || snapshot == nil {
		return nil
	}
	id, ok := access.ID(node)
	if !ok {
		return nil
	}
	value, ok := snapshot.LookupID(id)
	if !ok {
		return nil
	}
	projected := &styledNode{node: node, style: value}
	projected.children = make([]*styledNode, 0, len(node.Children)+2)
	lookupPseudo := func(origin *dom.Node, pseudo css.PseudoElement) (computedStyle, bool) {
		originID, found := access.ID(origin)
		if !found {
			return computedStyle{}, false
		}
		return snapshot.LookupPseudoID(originID, pseudo)
	}
	if before := projectPseudoStyle(node, css.PseudoElementBefore, lookupPseudo); before != nil {
		projected.children = append(projected.children, before)
	}
	for _, child := range node.Children {
		if projectedChild := projectReadAccessStyleTree(child, access, snapshot); projectedChild != nil {
			projected.children = append(projected.children, projectedChild)
		}
	}
	if after := projectPseudoStyle(node, css.PseudoElementAfter, lookupPseudo); after != nil {
		projected.children = append(projected.children, after)
	}
	return projected
}

func projectPseudoStyle(
	origin *dom.Node,
	pseudo css.PseudoElement,
	lookup func(*dom.Node, css.PseudoElement) (computedStyle, bool),
) *styledNode {
	if origin == nil || origin.Type != dom.ElementNode || origin.Data == "img" {
		return nil
	}
	value, ok := lookup(origin, pseudo)
	if !ok || value.Display() == displayNone {
		return nil
	}
	text, generated := value.Content().GeneratedText(origin)
	if !generated {
		return nil
	}
	textNode := &styledNode{
		node:          origin,
		pseudo:        pseudo,
		generated:     true,
		generatedText: text,
		style:         value,
	}
	return &styledNode{
		node:     origin,
		pseudo:   pseudo,
		style:    value,
		children: []*styledNode{textNode},
	}
}

func styleEnvironment(viewport Viewport) computed.Environment {
	return computed.Environment{
		Width:           viewport.Width,
		Height:          viewport.Height,
		MediaType:       "screen",
		InitialFontSize: 16,
	}
}

func resolveLength(value length, percentBase float64, viewport Viewport, autoValue float64) float64 {
	if resolved, ok := value.Resolve(percentBase, float64(viewport.Width), float64(viewport.Height)); ok {
		return resolved
	}
	return autoValue
}

func isBlockLevel(display displayMode) bool {
	return display.Outside() == computed.DisplayOutsideBlock
}

func isAtomicInline(display displayMode) bool {
	return display.Outside() == computed.DisplayOutsideInline &&
		display.Inside() != computed.DisplayInsideFlow
}

func isOutOfFlow(position positionMode) bool {
	return position == positionAbsolute || position == positionFixed
}

func attribute(node *dom.Node, name string) (string, bool) {
	for _, candidate := range node.Attributes {
		if strings.EqualFold(candidate.Name, name) {
			return candidate.Value, true
		}
	}
	return "", false
}

func isFinite(value float64) bool {
	return !math.IsNaN(value) && !math.IsInf(value, 0)
}

func clamp(value, minimum, maximum float64) float64 {
	if value < minimum {
		return minimum
	}
	if value > maximum {
		return maximum
	}
	return value
}
