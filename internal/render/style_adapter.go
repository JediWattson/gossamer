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
	displayGrid        = computed.DisplayGrid
	displayInlineGrid  = computed.DisplayInlineGrid
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

type direction = computed.Direction

const (
	directionLTR = computed.DirectionLTR
	directionRTL = computed.DirectionRTL
)

type writingMode = computed.WritingMode

const (
	writingModeHorizontalTB = computed.WritingModeHorizontalTB
	writingModeVerticalRL   = computed.WritingModeVerticalRL
	writingModeVerticalLR   = computed.WritingModeVerticalLR
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
	borderStyleHidden = computed.BorderStyleHidden
	borderStyleDotted = computed.BorderStyleDotted
	borderStyleDashed = computed.BorderStyleDashed
	borderStyleSolid  = computed.BorderStyleSolid
	borderStyleDouble = computed.BorderStyleDouble
	borderStyleGroove = computed.BorderStyleGroove
	borderStyleRidge  = computed.BorderStyleRidge
	borderStyleInset  = computed.BorderStyleInset
	borderStyleOutset = computed.BorderStyleOutset
)

type borderCollapse = computed.BorderCollapse

const (
	borderCollapseSeparate = computed.BorderCollapseSeparate
	borderCollapseCollapse = computed.BorderCollapseCollapse
)

type captionSide = computed.CaptionSide

const (
	captionSideTop    = computed.CaptionSideTop
	captionSideBottom = computed.CaptionSideBottom
)

type emptyCells = computed.EmptyCells

const (
	emptyCellsShow = computed.EmptyCellsShow
	emptyCellsHide = computed.EmptyCellsHide
)

type tableLayout = computed.TableLayout

const (
	tableLayoutAuto  = computed.TableLayoutAuto
	tableLayoutFixed = computed.TableLayoutFixed
)

type positionMode = computed.PositionMode

const (
	positionStatic   = computed.PositionStatic
	positionRelative = computed.PositionRelative
	positionAbsolute = computed.PositionAbsolute
	positionFixed    = computed.PositionFixed
	positionSticky   = computed.PositionSticky
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

// computedStyle keeps the immutable computed value while allowing one layout
// pass to interpret physical dimensions through a logical coordinate space.
// The adapter is renderer-private: snapshots and CSSOM always retain the
// original physical computed values.
type computedStyle struct {
	computed.ComputedStyle
	layoutAxes writingMode
}

func physicalComputedStyle(value computed.ComputedStyle) computedStyle {
	return computedStyle{ComputedStyle: value}
}

func (style computedStyle) withLayoutAxes(mode writingMode) computedStyle {
	style.layoutAxes = mode
	return style
}

func (style computedStyle) physical() computedStyle {
	style.layoutAxes = writingModeHorizontalTB
	return style
}

func (style computedStyle) verticalLayout() bool {
	return style.layoutAxes == writingModeVerticalRL || style.layoutAxes == writingModeVerticalLR
}

func (style computedStyle) Width() length {
	if style.verticalLayout() {
		return style.ComputedStyle.Height()
	}
	return style.ComputedStyle.Width()
}

func (style computedStyle) MinWidth() length {
	if style.verticalLayout() {
		return style.ComputedStyle.MinHeight()
	}
	return style.ComputedStyle.MinWidth()
}

func (style computedStyle) MaxWidth() length {
	if style.verticalLayout() {
		return style.ComputedStyle.MaxHeight()
	}
	return style.ComputedStyle.MaxWidth()
}

func (style computedStyle) Height() length {
	if style.verticalLayout() {
		return style.ComputedStyle.Width()
	}
	return style.ComputedStyle.Height()
}

func (style computedStyle) MinHeight() length {
	if style.verticalLayout() {
		return style.ComputedStyle.MinWidth()
	}
	return style.ComputedStyle.MinHeight()
}

func (style computedStyle) MaxHeight() length {
	if style.verticalLayout() {
		return style.ComputedStyle.MaxWidth()
	}
	return style.ComputedStyle.MaxHeight()
}

func (style computedStyle) MarginLeft() length {
	if style.verticalLayout() {
		return style.ComputedStyle.MarginTop()
	}
	return style.ComputedStyle.MarginLeft()
}

func (style computedStyle) MarginRight() length {
	if style.verticalLayout() {
		return style.ComputedStyle.MarginBottom()
	}
	return style.ComputedStyle.MarginRight()
}

func (style computedStyle) MarginTop() length {
	switch style.layoutAxes {
	case writingModeVerticalRL:
		return style.ComputedStyle.MarginRight()
	case writingModeVerticalLR:
		return style.ComputedStyle.MarginLeft()
	default:
		return style.ComputedStyle.MarginTop()
	}
}

func (style computedStyle) MarginBottom() length {
	switch style.layoutAxes {
	case writingModeVerticalRL:
		return style.ComputedStyle.MarginLeft()
	case writingModeVerticalLR:
		return style.ComputedStyle.MarginRight()
	default:
		return style.ComputedStyle.MarginBottom()
	}
}

func (style computedStyle) PaddingLeft() length {
	if style.verticalLayout() {
		return style.ComputedStyle.PaddingTop()
	}
	return style.ComputedStyle.PaddingLeft()
}

func (style computedStyle) PaddingRight() length {
	if style.verticalLayout() {
		return style.ComputedStyle.PaddingBottom()
	}
	return style.ComputedStyle.PaddingRight()
}

func (style computedStyle) PaddingTop() length {
	switch style.layoutAxes {
	case writingModeVerticalRL:
		return style.ComputedStyle.PaddingRight()
	case writingModeVerticalLR:
		return style.ComputedStyle.PaddingLeft()
	default:
		return style.ComputedStyle.PaddingTop()
	}
}

func (style computedStyle) PaddingBottom() length {
	switch style.layoutAxes {
	case writingModeVerticalRL:
		return style.ComputedStyle.PaddingLeft()
	case writingModeVerticalLR:
		return style.ComputedStyle.PaddingRight()
	default:
		return style.ComputedStyle.PaddingBottom()
	}
}

func (style computedStyle) BorderLeft() borderSide {
	if style.verticalLayout() {
		return style.ComputedStyle.BorderTop()
	}
	return style.ComputedStyle.BorderLeft()
}

func (style computedStyle) BorderRight() borderSide {
	if style.verticalLayout() {
		return style.ComputedStyle.BorderBottom()
	}
	return style.ComputedStyle.BorderRight()
}

func (style computedStyle) BorderTop() borderSide {
	switch style.layoutAxes {
	case writingModeVerticalRL:
		return style.ComputedStyle.BorderRight()
	case writingModeVerticalLR:
		return style.ComputedStyle.BorderLeft()
	default:
		return style.ComputedStyle.BorderTop()
	}
}

func (style computedStyle) BorderBottom() borderSide {
	switch style.layoutAxes {
	case writingModeVerticalRL:
		return style.ComputedStyle.BorderLeft()
	case writingModeVerticalLR:
		return style.ComputedStyle.BorderRight()
	default:
		return style.ComputedStyle.BorderBottom()
	}
}

func (style computedStyle) Left() length {
	if style.verticalLayout() {
		return style.ComputedStyle.Top()
	}
	return style.ComputedStyle.Left()
}

func (style computedStyle) Right() length {
	if style.verticalLayout() {
		return style.ComputedStyle.Bottom()
	}
	return style.ComputedStyle.Right()
}

func (style computedStyle) Top() length {
	switch style.layoutAxes {
	case writingModeVerticalRL:
		return style.ComputedStyle.Right()
	case writingModeVerticalLR:
		return style.ComputedStyle.Left()
	default:
		return style.ComputedStyle.Top()
	}
}

func (style computedStyle) Bottom() length {
	switch style.layoutAxes {
	case writingModeVerticalRL:
		return style.ComputedStyle.Left()
	case writingModeVerticalLR:
		return style.ComputedStyle.Right()
	default:
		return style.ComputedStyle.Bottom()
	}
}

func (style computedStyle) OverflowX() computed.OverflowMode {
	if style.verticalLayout() {
		return style.ComputedStyle.OverflowY()
	}
	return style.ComputedStyle.OverflowX()
}

func (style computedStyle) OverflowY() computed.OverflowMode {
	if style.verticalLayout() {
		return style.ComputedStyle.OverflowX()
	}
	return style.ComputedStyle.OverflowY()
}

func (style computedStyle) WithAnonymousDisplay(display displayMode) computedStyle {
	style.ComputedStyle = style.ComputedStyle.WithAnonymousDisplay(display)
	return style
}

func (style computedStyle) WithAnonymousGridItem() computedStyle {
	style.ComputedStyle = style.ComputedStyle.WithAnonymousGridItem()
	return style
}

func (style computedStyle) WithBlockifiedDisplay() computedStyle {
	style.ComputedStyle = style.ComputedStyle.WithBlockifiedDisplay()
	return style
}

type whiteSpaceMode = computed.WhiteSpaceMode

const (
	boxSizingContentBox = computed.BoxSizingContentBox
	boxSizingBorderBox  = computed.BoxSizingBorderBox
	visibilityVisible   = computed.VisibilityVisible
	visibilityCollapse  = computed.VisibilityCollapse
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
	projected := &styledNode{node: node, style: physicalComputedStyle(value)}
	projected.children = make([]*styledNode, 0, len(node.Children)+2)
	lookupPseudo := func(origin *dom.Node, pseudo css.PseudoElement) (computedStyle, bool) {
		value, found := snapshot.LookupPseudo(origin, pseudo)
		return physicalComputedStyle(value), found
	}
	if before := projectPseudoStyle(node, css.PseudoElementBefore, lookupPseudo); before != nil {
		projected.children = append(projected.children, before)
	}
	for _, child := range node.Children {
		if projectedChild := projectStyleTree(child, snapshot); projectedChild != nil {
			projected.children = append(projected.children, projectedChild)
		}
	}
	if after := projectPseudoStyle(node, css.PseudoElementAfter, lookupPseudo); after != nil {
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
	projected := &styledNode{node: node, style: physicalComputedStyle(value)}
	projected.children = make([]*styledNode, 0, len(node.Children)+2)
	lookupPseudo := func(origin *dom.Node, pseudo css.PseudoElement) (computedStyle, bool) {
		originID, found := access.ID(origin)
		if !found {
			return computedStyle{}, false
		}
		value, ok := snapshot.LookupPseudoID(originID, pseudo)
		return physicalComputedStyle(value), ok
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
