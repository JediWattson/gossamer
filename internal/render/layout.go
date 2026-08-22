package render

import (
	"fmt"
	"image"
	"math"
	"sort"
	"strconv"
	"strings"

	"github.com/JediWattson/gossamer/internal/dom"
	computed "github.com/JediWattson/gossamer/internal/style"
)

type layoutContext struct {
	viewport       Viewport
	fonts          *fontBook
	styles         map[*dom.Node]computedStyle
	images         map[*dom.Node]image.Image
	intrinsicCache map[intrinsicCacheKey]intrinsicWidths
	marginCache    map[marginCacheKey]blockMarginProfile
	axisCloneCache map[layoutAxisCloneKey]*styledNode
}

type layoutAxisCloneKey struct {
	node *styledNode
	mode writingMode
}

// marginStrut is one collapsed vertical-margin group. CSS collapses the
// largest positive margin with the most negative margin rather than applying
// pairwise max, which is observably different for mixed-sign groups.
type marginStrut struct {
	positive float64
	negative float64
}

func (strut marginStrut) add(value float64) marginStrut {
	if value >= 0 {
		strut.positive = math.Max(strut.positive, value)
	} else {
		strut.negative = math.Min(strut.negative, value)
	}
	return strut
}

func (strut marginStrut) merge(other marginStrut) marginStrut {
	strut.positive = math.Max(strut.positive, other.positive)
	strut.negative = math.Min(strut.negative, other.negative)
	return strut
}

func (strut marginStrut) value() float64 {
	return strut.positive + strut.negative
}

type blockMarginProfile struct {
	start   marginStrut
	end     marginStrut
	through bool
}

type marginCacheKey struct {
	node             *styledNode
	availableWidth   float64
	containingHeight float64
	heightDefinite   bool
}

type inlineToken struct {
	text          string
	node          *dom.Node
	pseudo        computed.PseudoElement
	style         computedStyle
	atomic        *styledNode
	image         image.Image
	replaced      bool
	opacity       float64
	leadingSpace  bool
	wrapBefore    bool
	lineBreak     bool
	verticalAlign verticalAlignment
}

type inlinePiece struct {
	text                  string
	node                  *dom.Node
	pseudo                computed.PseudoElement
	style                 computedStyle
	box                   *Box
	image                 image.Image
	replaced              bool
	percentHeightResolved bool
	justifyBefore         bool
	opacity               float64
	x                     float64
	width                 float64
	height                float64
	baseline              float64
	metrics               textMetrics
	verticalAlign         verticalAlignment
	orientation           textPaintOrientation
}

type measuredVerticalTextRun struct {
	verticalTextRun
	metrics textMetrics
}

type inlineLayout struct {
	fragments []InlineFragment
	flow      []flowItem
	height    float64
}

func layoutDocument(root *styledNode, viewport Viewport, images map[*dom.Node]image.Image, fonts *fontBook) (*Box, map[*dom.Node]computedStyle, error) {
	context := &layoutContext{
		viewport:       viewport,
		fonts:          fonts,
		styles:         make(map[*dom.Node]computedStyle),
		images:         images,
		intrinsicCache: make(map[intrinsicCacheKey]intrinsicWidths),
		marginCache:    make(map[marginCacheKey]blockMarginProfile),
		axisCloneCache: make(map[layoutAxisCloneKey]*styledNode),
	}
	context.indexStyles(root)

	documentBox := &Box{
		Node:          root.node,
		Bounds:        Rect{Width: float64(viewport.Width), Height: float64(viewport.Height)},
		ContentBounds: Rect{Width: float64(viewport.Width), Height: float64(viewport.Height)},
		style:         root.style,
		hasStyle:      true,
	}
	html := findStyledElement(root, "html")
	if html == nil || html.style.Display() == displayNone {
		return documentBox, context.styles, nil
	}

	htmlBox := &Box{
		Node:          html.node,
		Bounds:        Rect{Width: float64(viewport.Width), Height: float64(viewport.Height)},
		ContentBounds: Rect{Width: float64(viewport.Width), Height: float64(viewport.Height)},
		style:         html.style,
		hasStyle:      true,
	}
	documentBox.Children = append(documentBox.Children, htmlBox)

	body := directStyledElement(html, "body")
	if body == nil || body.style.Display() == displayNone {
		return documentBox, context.styles, nil
	}
	var bodyContainingHeight *float64
	viewportHeight := float64(viewport.Height)
	if height, definite := context.resolveSpecifiedContentHeight(html.style, &viewportHeight, 0); definite {
		bodyContainingHeight = &height
	}
	bodyMargins := context.blockMargins(body, float64(viewport.Width), bodyContainingHeight)
	bodyY := bodyMargins.start.value()
	bodyBox, err := context.layoutBlock(body, 0, bodyY, float64(viewport.Width), bodyContainingHeight)
	if err != nil {
		return nil, nil, err
	}
	htmlBox.Children = append(htmlBox.Children, bodyBox)
	bodyBottomMargin := bodyMargins.end.value()
	if bodyMargins.through {
		bodyBottomMargin = 0
	}
	bodyBottom := bodyBox.Bounds.Y + bodyBox.Bounds.Height + bodyBottomMargin
	if bodyBottom > htmlBox.Bounds.Height {
		htmlBox.Bounds.Height = bodyBottom
		htmlBox.ContentBounds.Height = bodyBottom
	}
	if err := context.layoutPositionedDescendants(root, documentBox); err != nil {
		return nil, nil, err
	}
	return documentBox, context.styles, nil
}

func (context *layoutContext) indexStyles(node *styledNode) {
	if node == nil {
		return
	}
	if node.node != nil && node.pseudo == computed.PseudoElementNone {
		context.styles[node.node] = node.style
	}
	for _, child := range node.children {
		context.indexStyles(child)
	}
}

func (context *layoutContext) blockMargins(node *styledNode, availableWidth float64, containingHeight *float64) blockMarginProfile {
	if node == nil {
		return blockMarginProfile{}
	}
	key := marginCacheKey{node: node, availableWidth: availableWidth}
	if containingHeight != nil {
		key.containingHeight = *containingHeight
		key.heightDefinite = true
	}
	if cached, ok := context.marginCache[key]; ok {
		return cached
	}

	style := node.style
	profile := blockMarginProfile{
		start: marginStrut{}.add(resolveLength(style.MarginTop(), availableWidth, context.viewport, 0)),
		end:   marginStrut{}.add(resolveLength(style.MarginBottom(), availableWidth, context.viewport, 0)),
	}
	padding := context.resolvePadding(style, availableWidth)
	border := context.resolveBorder(style, availableWidth)
	verticalInsets := padding.Top + padding.Bottom + border.Top + border.Bottom
	childAvailableWidth := context.blockContentWidth(style, availableWidth, padding, border)
	var childContainingHeight *float64
	if height, definite := context.resolveSpecifiedContentHeight(style, containingHeight, verticalInsets); definite {
		childContainingHeight = &height
	}
	canCollapseContents := context.marginCollapsingContext(style)
	canCollapseStart := canCollapseContents && padding.Top == 0 && border.Top == 0
	canCollapseEnd := canCollapseContents && padding.Bottom == 0 && border.Bottom == 0 &&
		!resolvableHeight(style.Height(), containingHeight) &&
		context.minimumContentHeight(style, containingHeight, verticalInsets) == 0

	if canCollapseStart {
		for _, child := range node.children {
			if child == nil || child.style.Display() == displayNone || isOutOfFlow(child.style.Position()) {
				continue
			}
			if isBlockFlowChild(child) {
				childProfile := context.blockMargins(child, childAvailableWidth, childContainingHeight)
				profile.start = profile.start.merge(childProfile.start)
				if childProfile.through {
					continue
				}
				break
			}
			if context.inlineNodeProducesLayout(child) {
				break
			}
		}
	}
	if canCollapseEnd {
		for index := len(node.children) - 1; index >= 0; index-- {
			child := node.children[index]
			if child == nil || child.style.Display() == displayNone || isOutOfFlow(child.style.Position()) {
				continue
			}
			if isBlockFlowChild(child) {
				childProfile := context.blockMargins(child, childAvailableWidth, childContainingHeight)
				profile.end = profile.end.merge(childProfile.end)
				if childProfile.through {
					continue
				}
				break
			}
			if context.inlineNodeProducesLayout(child) {
				break
			}
		}
	}

	profile.through = canCollapseStart && canCollapseEnd && !context.blockGeneratesOwnContent(node)
	if profile.through {
		for _, child := range node.children {
			if child == nil || child.style.Display() == displayNone || isOutOfFlow(child.style.Position()) {
				continue
			}
			if isBlockFlowChild(child) {
				if !context.blockMargins(child, childAvailableWidth, childContainingHeight).through {
					profile.through = false
					break
				}
				continue
			}
			if context.inlineNodeProducesLayout(child) {
				profile.through = false
				break
			}
		}
	}
	if profile.through {
		collapsed := profile.start.merge(profile.end)
		profile.start = collapsed
		profile.end = collapsed
	}
	context.marginCache[key] = profile
	return profile
}

func (context *layoutContext) marginCollapsingContext(style computedStyle) bool {
	if style.Display().Inside() != computed.DisplayInsideFlow || isOutOfFlow(style.Position()) {
		return false
	}
	for _, overflow := range []computed.OverflowMode{style.OverflowX(), style.OverflowY()} {
		if overflow != computed.OverflowVisible && overflow != computed.OverflowClip {
			return false
		}
	}
	return true
}

func (context *layoutContext) minimumContentHeight(style computedStyle, containingHeight *float64, verticalInsets float64) float64 {
	minimum := style.MinHeight()
	if !resolvableHeight(minimum, containingHeight) {
		return 0
	}
	percentageBase := 0.0
	if containingHeight != nil {
		percentageBase = *containingHeight
	}
	value := math.Max(0, resolveLength(minimum, percentageBase, context.viewport, 0))
	if style.BoxSizing() == boxSizingBorderBox {
		value = math.Max(0, value-verticalInsets)
	}
	return value
}

func (context *layoutContext) blockContentWidth(style computedStyle, availableWidth float64, padding, border Edges) float64 {
	horizontalInsets := padding.Left + padding.Right + border.Left + border.Right
	left := resolveLength(style.MarginLeft(), availableWidth, context.viewport, 0)
	right := resolveLength(style.MarginRight(), availableWidth, context.viewport, 0)
	width := availableWidth - left - right - horizontalInsets
	if style.Width().Unit() != lengthAuto {
		width = resolveLength(style.Width(), availableWidth, context.viewport, availableWidth)
		if style.BoxSizing() == boxSizingBorderBox {
			width -= horizontalInsets
		}
	}
	return context.constrainWidth(style, math.Max(0, width), availableWidth, horizontalInsets)
}

func (context *layoutContext) inlineNodeProducesLayout(node *styledNode) bool {
	if node == nil {
		return false
	}
	builder := inlineTokenBuilder{images: context.images}
	builder.add(node, 1)
	return len(builder.tokens) != 0
}

func (context *layoutContext) blockGeneratesOwnContent(node *styledNode) bool {
	if node == nil {
		return false
	}
	if node.style.Display() == displayListItem && node.style.ListStyleType() != listStyleNone {
		return true
	}
	return node.node != nil && node.node.Type == dom.ElementNode && node.node.Data == "img" &&
		(context.images[node.node] != nil || hasExplicitImageDimensions(node.style))
}

func (context *layoutContext) layoutBlock(node *styledNode, containingX, contentY, availableWidth float64, containingHeight *float64) (*Box, error) {
	return context.layoutBlockWithOverrides(node, containingX, contentY, availableWidth, containingHeight, blockLayoutOverrides{})
}

func (context *layoutContext) layoutBlockWithOverrides(node *styledNode, containingX, contentY, availableWidth float64, containingHeight *float64, overrides blockLayoutOverrides) (*Box, error) {
	return context.layoutBlockSizedWithSubgrid(node, containingX, contentY, availableWidth, containingHeight, nil, false, nil, overrides)
}

func (context *layoutContext) layoutBlockSized(node *styledNode, containingX, contentY, availableWidth float64, containingHeight, forcedContentWidth *float64, independentFormattingContext bool) (*Box, error) {
	return context.layoutBlockSizedWithSubgrid(node, containingX, contentY, availableWidth, containingHeight, forcedContentWidth, independentFormattingContext, nil, blockLayoutOverrides{})
}

type blockLayoutOverrides struct {
	ignoreSpecifiedWidth   bool
	ignoreSpecifiedHeight  bool
	forceZeroContentHeight bool
	forceContentHeight     *float64
	ignoreHorizontalMargin bool
	childContainingHeight  *float64
	tableCellFirstPass     bool
}

func (context *layoutContext) layoutBlockSizedWithSubgrid(node *styledNode, containingX, contentY, availableWidth float64, containingHeight, forcedContentWidth *float64, independentFormattingContext bool, subgrid *gridSubgridContext, overrides blockLayoutOverrides) (*Box, error) {
	style := node.style
	flowContainer := style.Display().Inside() == computed.DisplayInsideFlow || style.Display().Inside() == computed.DisplayInsideFlowRoot
	verticalFlow := flowContainer &&
		!style.verticalLayout() && style.WritingMode() != writingModeHorizontalTB
	horizontalFlowInVertical := flowContainer &&
		style.verticalLayout() && style.WritingMode() == writingModeHorizontalTB
	reversedVerticalFlow := flowContainer &&
		style.verticalLayout() && style.WritingMode() != writingModeHorizontalTB && style.WritingMode() != style.layoutAxes
	horizontalTableInVertical := style.Display().Inside() == computed.DisplayInsideTable &&
		style.verticalLayout() && style.WritingMode() == writingModeHorizontalTB
	reversedVerticalTable := style.Display().Inside() == computed.DisplayInsideTable &&
		style.verticalLayout() && style.WritingMode() != writingModeHorizontalTB && style.WritingMode() != style.layoutAxes
	if verticalFlow {
		return context.layoutVerticalFlowContainer(node, containingX, contentY, availableWidth, containingHeight, forcedContentWidth, overrides)
	}
	if horizontalFlowInVertical {
		return context.layoutHorizontalFlowInVerticalPlane(node, containingX, contentY, availableWidth, containingHeight, forcedContentWidth, overrides, style.layoutAxes)
	}
	if reversedVerticalFlow {
		return context.layoutReversedVerticalFlowInVerticalPlane(node, containingX, contentY, availableWidth, containingHeight, forcedContentWidth, overrides, style.layoutAxes)
	}
	if horizontalTableInVertical {
		return context.layoutHorizontalTableInVerticalPlane(node, containingX, contentY, availableWidth, containingHeight, forcedContentWidth, overrides, style.layoutAxes)
	}
	if reversedVerticalTable {
		return context.layoutReversedVerticalTableInVerticalPlane(node, containingX, contentY, availableWidth, containingHeight, forcedContentWidth, overrides, style.layoutAxes)
	}
	verticalTable := style.Display().Inside() == computed.DisplayInsideTable &&
		!style.verticalLayout() && style.WritingMode() != writingModeHorizontalTB
	verticalGrid := style.Display().Inside() == computed.DisplayInsideGrid &&
		!style.verticalLayout() && style.WritingMode() != writingModeHorizontalTB
	verticalFlex := style.Display().Inside() == computed.DisplayInsideFlex &&
		!style.verticalLayout() && style.WritingMode() != writingModeHorizontalTB
	horizontalGridInVertical := style.Display().Inside() == computed.DisplayInsideGrid &&
		style.verticalLayout() && style.WritingMode() == writingModeHorizontalTB
	horizontalFlexInVertical := style.Display().Inside() == computed.DisplayInsideFlex &&
		style.verticalLayout() && style.WritingMode() == writingModeHorizontalTB
	reversedVerticalGrid := style.Display().Inside() == computed.DisplayInsideGrid &&
		style.verticalLayout() && style.WritingMode() != writingModeHorizontalTB && style.WritingMode() != style.layoutAxes
	reversedVerticalFlex := style.Display().Inside() == computed.DisplayInsideFlex &&
		style.verticalLayout() && style.WritingMode() != writingModeHorizontalTB && style.WritingMode() != style.layoutAxes
	leftAuto := !overrides.ignoreHorizontalMargin && style.MarginLeft().Unit() == lengthAuto
	rightAuto := !overrides.ignoreHorizontalMargin && style.MarginRight().Unit() == lengthAuto
	left, right := 0.0, 0.0
	if !overrides.ignoreHorizontalMargin {
		left = resolveLength(style.MarginLeft(), availableWidth, context.viewport, 0)
		right = resolveLength(style.MarginRight(), availableWidth, context.viewport, 0)
	}
	padding := context.resolvePadding(style, availableWidth)
	border := context.resolveBorder(style, availableWidth)
	if style.Display().Inside() == computed.DisplayInsideTable && style.BorderCollapse() == computed.BorderCollapseCollapse {
		borderNode := node
		if verticalTable {
			borderNode = context.cloneStyledNodeWithLayoutAxes(node, style.WritingMode())
		}
		collapsedBorder, err := context.collapsedTableOuterEdges(borderNode)
		if err != nil {
			return nil, err
		}
		if verticalTable {
			collapsedBorder = physicalEdgesFromLogical(collapsedBorder, style.WritingMode())
		}
		// CSS collapsed-border tables ignore table padding and use half of the
		// harmonized outer grid border as their table box border.
		padding = Edges{}
		border = collapsedBorder
	}
	horizontalInsets := padding.Left + padding.Right + border.Left + border.Right
	verticalInsets := padding.Top + padding.Bottom + border.Top + border.Bottom
	specifiedContentHeight, hasDefiniteHeight := context.resolveSpecifiedContentHeight(style, containingHeight, verticalInsets)
	if overrides.ignoreSpecifiedHeight {
		specifiedContentHeight = 0
		hasDefiniteHeight = false
	}
	if overrides.forceZeroContentHeight {
		specifiedContentHeight = 0
		hasDefiniteHeight = true
	}
	if overrides.forceContentHeight != nil {
		specifiedContentHeight = math.Max(0, *overrides.forceContentHeight)
		hasDefiniteHeight = true
	}
	var childContainingHeight *float64
	if hasDefiniteHeight {
		childContainingHeight = &specifiedContentHeight
	}
	if overrides.childContainingHeight != nil {
		childContainingHeight = overrides.childContainingHeight
	}
	if node.node != nil && node.node.Type == dom.ElementNode && node.node.Data == "img" {
		decoded := context.images[node.node]
		imageWidth, imageHeight, ok := context.replacedDimensions(style, decoded, availableWidth, containingHeight, horizontalInsets, verticalInsets)
		if !ok {
			box := &Box{
				Node:          node.node,
				Pseudo:        node.pseudo,
				Bounds:        Rect{X: containingX + left, Y: contentY, Width: border.Left + padding.Left + padding.Right + border.Right, Height: border.Top + padding.Top + padding.Bottom + border.Bottom},
				ContentBounds: Rect{X: containingX + left + border.Left + padding.Left, Y: contentY + border.Top + padding.Top},
				Padding:       padding,
				Border:        border,
				style:         node.style,
				hasStyle:      true,
			}
			return context.finalizeBlock(node, box, availableWidth)
		}
		outerWidth := imageWidth + padding.Left + padding.Right + border.Left + border.Right
		remaining := availableWidth - outerWidth - left - right
		switch {
		case leftAuto && rightAuto:
			left = math.Max(0, remaining/2)
		case leftAuto:
			left = math.Max(0, remaining)
		}
		bounds := Rect{X: containingX + left, Y: contentY, Width: outerWidth, Height: imageHeight + padding.Top + padding.Bottom + border.Top + border.Bottom}
		contentBounds := Rect{X: bounds.X + border.Left + padding.Left, Y: bounds.Y + border.Top + padding.Top, Width: imageWidth, Height: imageHeight}
		fragment := InlineFragment{
			Kind:  ImageFragmentKind,
			Image: ImageFragment{Node: node.node, Image: decoded, Bounds: contentBounds, Opacity: 1},
		}
		box := &Box{
			Node:                  node.node,
			Pseudo:                node.pseudo,
			Bounds:                bounds,
			ContentBounds:         contentBounds,
			Padding:               padding,
			Border:                border,
			Fragments:             []InlineFragment{fragment},
			flow:                  []flowItem{{fragment: fragment}},
			style:                 node.style,
			hasStyle:              true,
			percentHeightResolved: style.Height().DependsOnPercent() && hasDefiniteHeight,
		}
		return context.finalizeBlock(node, box, availableWidth)
	}

	width := availableWidth - left - right - padding.Left - padding.Right - border.Left - border.Right
	var tableIntrinsic intrinsicWidths
	if style.Display().Inside() == computed.DisplayInsideTable && !verticalTable {
		intrinsic, err := context.intrinsicContentWidths(node, availableWidth)
		if err != nil {
			return nil, err
		}
		tableIntrinsic = intrinsic
	}
	if forcedContentWidth != nil {
		width = *forcedContentWidth
	} else if !overrides.ignoreSpecifiedWidth && style.Width().Unit() != lengthAuto {
		width = resolveLength(style.Width(), availableWidth, context.viewport, availableWidth)
		if style.BoxSizing() == boxSizingBorderBox {
			width -= horizontalInsets
		}
	} else if style.Display().Inside() == computed.DisplayInsideTable && !verticalTable {
		availableContent := math.Max(0, width)
		width = math.Min(math.Max(tableIntrinsic.minimum, availableContent), tableIntrinsic.preferred)
	}
	width = context.constrainWidth(style, math.Max(0, width), availableWidth, horizontalInsets)
	if style.Display().Inside() == computed.DisplayInsideTable && !verticalTable {
		// A table's grid and caption minimums override a smaller specified or
		// max-constrained width; the table overflows rather than crushing tracks.
		width = math.Max(width, tableIntrinsic.minimum)
	}
	outerWidth := width + padding.Left + padding.Right + border.Left + border.Right
	remaining := availableWidth - outerWidth - left - right
	switch {
	case leftAuto && rightAuto:
		left = math.Max(0, remaining/2)
		right = math.Max(0, remaining/2)
	case leftAuto:
		left = math.Max(0, remaining)
	case rightAuto:
		right = math.Max(0, remaining)
	}

	box := &Box{
		Node:                  node.node,
		Pseudo:                node.pseudo,
		Bounds:                Rect{X: containingX + left, Y: contentY, Width: outerWidth},
		Padding:               padding,
		Border:                border,
		style:                 node.style,
		hasStyle:              node.node != nil,
		percentHeightResolved: style.Height().DependsOnPercent() && hasDefiniteHeight,
	}
	box.ContentBounds = Rect{
		X:     box.Bounds.X + border.Left + padding.Left,
		Y:     box.Bounds.Y + border.Top + padding.Top,
		Width: width,
	}
	if style.Display().Inside() == computed.DisplayInsideTable {
		tableRoot := box
		tableRoot.skipLayoutIndex = true
		tableRoot.skipGeometryIndex = true
		// Opacity and the outer positioning properties apply once to the
		// anonymous wrapper, not independently to the table-root box.
		tableRoot.hasOpacity = true
		tableRoot.paintOpacity = 1
		wrapper := &Box{
			Node: node.node, Pseudo: node.pseudo,
			Bounds: Rect{X: tableRoot.Bounds.X, Y: tableRoot.Bounds.Y, Width: tableRoot.Bounds.Width},
			style:  node.style, hasStyle: node.node != nil,
			tableRoot: tableRoot, tableWrapper: true, hitTransparent: true,
			suppressDecorations: true, suppressBorders: true,
		}
		var err error
		if verticalTable {
			err = context.layoutVerticalTableContainer(
				node, wrapper, tableRoot, availableWidth, containingHeight,
				width, forcedContentWidth != nil || style.Width().Unit() != lengthAuto,
			)
		} else {
			_, err = context.layoutTableContainer(
				node, wrapper, tableRoot, width, childContainingHeight, containingHeight,
				specifiedContentHeight, hasDefiniteHeight, verticalInsets,
			)
		}
		if err != nil {
			return nil, err
		}
		if wrapper.Bounds.Width != outerWidth {
			baseLeft, baseRight := left, right
			if leftAuto {
				baseLeft = 0
			}
			if rightAuto {
				baseRight = 0
			}
			remaining := availableWidth - wrapper.Bounds.Width - baseLeft - baseRight
			newLeft := baseLeft
			switch {
			case leftAuto && rightAuto:
				newLeft = math.Max(0, remaining/2)
			case leftAuto:
				newLeft = math.Max(0, remaining)
			}
			translateLayoutBox(wrapper, containingX+newLeft-wrapper.Bounds.X, 0)
		}
		return context.finalizeBlock(node, wrapper, availableWidth)
	}
	if style.Display().Inside() == computed.DisplayInsideFlex {
		if verticalFlex {
			if _, err := context.layoutVerticalFlexContainer(node, box, containingHeight, width); err != nil {
				return nil, err
			}
			return context.finalizeBlock(node, box, availableWidth)
		}
		if horizontalFlexInVertical {
			if _, err := context.layoutHorizontalFlexInVerticalPlane(node, box, containingHeight, width, style.layoutAxes); err != nil {
				return nil, err
			}
			return context.finalizeBlock(node, box, availableWidth)
		}
		if reversedVerticalFlex {
			if _, err := context.layoutReversedVerticalFlexInVerticalPlane(node, box, width, childContainingHeight, style.layoutAxes); err != nil {
				return nil, err
			}
			return context.finalizeBlock(node, box, availableWidth)
		}
		contentHeight, err := context.layoutFlexContainer(node, box, width, childContainingHeight)
		if err != nil {
			return nil, err
		}
		if hasDefiniteHeight {
			contentHeight = specifiedContentHeight
		}
		contentHeight = context.constrainHeight(style, contentHeight, verticalInsets, containingHeight)
		box.ContentBounds.Height = contentHeight
		box.Bounds.Height = border.Top + padding.Top + contentHeight + padding.Bottom + border.Bottom
		return context.finalizeBlock(node, box, availableWidth)
	}
	if style.Display().Inside() == computed.DisplayInsideGrid {
		if verticalGrid {
			if _, err := context.layoutVerticalGridContainer(node, box, availableWidth, containingHeight, width, subgrid); err != nil {
				return nil, err
			}
			return context.finalizeBlock(node, box, availableWidth)
		}
		if horizontalGridInVertical {
			if _, err := context.layoutHorizontalGridInVerticalPlane(node, box, containingHeight, width, subgrid, style.layoutAxes); err != nil {
				return nil, err
			}
			return context.finalizeBlock(node, box, availableWidth)
		}
		repeatHeight := childContainingHeight
		repeatFulfillsMinimum := false
		if repeatHeight == nil {
			percentageBase := 0.0
			if containingHeight != nil {
				percentageBase = *containingHeight
			}
			minimum, hasMinimum := 0.0, resolvableHeight(style.MinHeight(), containingHeight)
			if hasMinimum {
				minimum = math.Max(0, resolveLength(style.MinHeight(), percentageBase, context.viewport, 0))
				if style.BoxSizing() == boxSizingBorderBox {
					minimum = math.Max(0, minimum-verticalInsets)
				}
			}
			if resolvableHeight(style.MaxHeight(), containingHeight) {
				maximum := math.Max(0, resolveLength(style.MaxHeight(), percentageBase, context.viewport, 0))
				if style.BoxSizing() == boxSizingBorderBox {
					maximum = math.Max(0, maximum-verticalInsets)
				}
				maximum = math.Max(maximum, minimum)
				repeatHeight = &maximum
			} else if hasMinimum {
				repeatHeight = &minimum
				repeatFulfillsMinimum = true
			}
		}
		contentHeight := 0.0
		var err error
		if reversedVerticalGrid {
			contentHeight, err = context.layoutReversedVerticalGridInVerticalPlane(node, box, width, childContainingHeight, repeatHeight, repeatFulfillsMinimum, subgrid, style.layoutAxes)
		} else {
			contentHeight, err = context.layoutGridContainer(node, box, width, childContainingHeight, repeatHeight, repeatFulfillsMinimum, subgrid)
		}
		if err != nil {
			return nil, err
		}
		if hasDefiniteHeight {
			contentHeight = specifiedContentHeight
		}
		contentHeight = context.constrainHeight(style, contentHeight, verticalInsets, containingHeight)
		box.ContentBounds.Height = contentHeight
		box.Bounds.Height = border.Top + padding.Top + contentHeight + padding.Bottom + border.Bottom
		return context.finalizeBlock(node, box, availableWidth)
	}
	cursorY := box.ContentBounds.Y
	pendingMargin := marginStrut{}
	var pendingThroughBoxes []*Box
	beforeFirstContent := true
	canCollapseStart := !independentFormattingContext && context.marginCollapsingContext(style) && padding.Top == 0 && border.Top == 0
	canCollapseEnd := !independentFormattingContext && context.marginCollapsingContext(style) && padding.Bottom == 0 && border.Bottom == 0 &&
		!hasDefiniteHeight && context.minimumContentHeight(style, containingHeight, verticalInsets) == 0
	placePendingThroughBoxes := func(targetY float64) {
		for _, pending := range pendingThroughBoxes {
			translateLayoutBox(pending, 0, targetY-pending.Bounds.Y)
		}
		pendingThroughBoxes = nil
	}

	for index := 0; index < len(node.children); {
		child := node.children[index]
		if child.style.Display() == displayNone {
			index++
			continue
		}
		if isOutOfFlow(child.style.Position()) {
			index++
			continue
		}
		if isBlockFlowChild(child) {
			childOverrides := blockLayoutOverrides{}
			if overrides.tableCellFirstPass && tableCellFirstPassZeroHeight(child) {
				childOverrides.forceZeroContentHeight = true
			}
			margins := context.blockMargins(child, width, childContainingHeight)
			if margins.through {
				if !(beforeFirstContent && canCollapseStart) {
					pendingMargin = pendingMargin.merge(margins.start)
				}
				childBox, err := context.layoutBlockWithOverrides(child, box.ContentBounds.X, cursorY, width, childContainingHeight, childOverrides)
				if err != nil {
					return nil, err
				}
				box.Children = append(box.Children, childBox)
				box.flow = append(box.flow, flowItem{box: childBox})
				pendingThroughBoxes = append(pendingThroughBoxes, childBox)
				index++
				continue
			}
			if !(beforeFirstContent && canCollapseStart) {
				pendingMargin = pendingMargin.merge(margins.start)
			}
			gap := pendingMargin.value()
			targetY := cursorY + gap
			placePendingThroughBoxes(targetY)
			childBox, err := context.layoutBlockWithOverrides(child, box.ContentBounds.X, targetY, width, childContainingHeight, childOverrides)
			if err != nil {
				return nil, err
			}
			box.Children = append(box.Children, childBox)
			box.flow = append(box.flow, flowItem{box: childBox})
			cursorY = childBox.Bounds.Y + childBox.Bounds.Height
			pendingMargin = margins.end
			beforeFirstContent = false
			index++
			continue
		}

		end := index
		for end < len(node.children) &&
			!isBlockFlowChild(node.children[end]) &&
			!isOutOfFlow(node.children[end].style.Position()) {
			end++
		}
		inline, err := context.layoutInline(node.children[index:end], box.ContentBounds.X, cursorY, width, childContainingHeight, node.style, overrides.tableCellFirstPass)
		if err != nil {
			return nil, err
		}
		if len(inline.flow) != 0 {
			gap := pendingMargin.value()
			placePendingThroughBoxes(cursorY + gap)
			cursorY += gap
			translateInlineLayout(&inline, 0, gap)
			box.Fragments = append(box.Fragments, inline.fragments...)
			for _, item := range inline.flow {
				box.flow = append(box.flow, item)
				if item.box != nil {
					box.Children = append(box.Children, item.box)
					continue
				}
				if item.fragment.Kind == TextFragmentKind {
					box.Text = append(box.Text, item.fragment.Text)
				}
			}
			cursorY += inline.height
			pendingMargin = marginStrut{}
			beforeFirstContent = false
		}
		index = end
	}

	if canCollapseEnd {
		placePendingThroughBoxes(cursorY)
	} else {
		gap := pendingMargin.value()
		placePendingThroughBoxes(cursorY + gap)
		cursorY += gap
	}
	contentHeight := math.Max(0, cursorY-box.ContentBounds.Y)
	if hasDefiniteHeight {
		contentHeight = specifiedContentHeight
	}
	contentHeight = context.constrainHeight(style, contentHeight, verticalInsets, containingHeight)
	box.ContentBounds.Height = contentHeight
	box.Bounds.Height = border.Top + padding.Top + box.ContentBounds.Height + padding.Bottom + border.Bottom
	return context.finalizeBlock(node, box, availableWidth)
}

func tableCellFirstPassZeroHeight(node *styledNode) bool {
	if node == nil || !node.style.Height().DependsOnPercent() {
		return false
	}
	if node.node != nil && node.node.Type == dom.ElementNode && node.node.Data == "img" {
		return false
	}
	for _, overflow := range []computed.OverflowMode{node.style.OverflowX(), node.style.OverflowY()} {
		if overflow == computed.OverflowAuto || overflow == computed.OverflowScroll {
			return true
		}
	}
	return false
}

func isBlockFlowChild(node *styledNode) bool {
	return node != nil && !node.generated && isBlockLevel(node.style.Display())
}

type flexLayoutItem struct {
	node          *styledNode
	box           *Box
	originalIndex int
	crossSize     float64
	mainSize      float64
	outerMain     float64
	marginBefore  float64
	marginAfter   float64
}

func (context *layoutContext) layoutFlexContainer(node *styledNode, box *Box, contentWidth float64, definiteHeight *float64) (float64, error) {
	items := make([]flexLayoutItem, 0, len(node.children))
	for index, child := range node.children {
		if child.generated || child.node == nil || child.node.Type != dom.ElementNode ||
			child.style.Display() == displayNone || isOutOfFlow(child.style.Position()) {
			continue
		}
		items = append(items, flexLayoutItem{node: child, originalIndex: index})
	}
	sort.SliceStable(items, func(left, right int) bool {
		return items[left].node.style.Order() < items[right].node.style.Order()
	})
	direction := node.style.FlexDirection()
	reverse := direction == computed.FlexDirectionColumnReverse
	if direction == computed.FlexDirectionRow || direction == computed.FlexDirectionRowReverse {
		reverse = (direction == computed.FlexDirectionRowReverse) != (node.style.Direction() == directionRTL)
	}
	if reverse {
		for left, right := 0, len(items)-1; left < right; left, right = left+1, right-1 {
			items[left], items[right] = items[right], items[left]
		}
	}
	if direction == computed.FlexDirectionColumn || direction == computed.FlexDirectionColumnReverse {
		return context.layoutFlexColumn(node, box, contentWidth, definiteHeight, items, reverse)
	}
	return context.layoutFlexRow(node, box, contentWidth, definiteHeight, items, reverse)
}

func (context *layoutContext) layoutFlexRow(node *styledNode, box *Box, contentWidth float64, definiteHeight *float64, items []flexLayoutItem, reverse bool) (float64, error) {
	if len(items) == 0 {
		return flexContainerHeight(definiteHeight, 0), nil
	}
	gap := math.Max(0, resolveLength(node.style.ColumnGap(), contentWidth, context.viewport, 0))
	totalOuter := gap * float64(len(items)-1)
	totalGrow := 0.0
	totalShrinkWeight := 0.0
	for index := range items {
		item := &items[index]
		style := item.node.style
		padding := context.resolvePadding(style, contentWidth)
		border := context.resolveBorder(style, contentWidth)
		item.marginBefore = resolveLength(style.MarginLeft(), contentWidth, context.viewport, 0)
		item.marginAfter = resolveLength(style.MarginRight(), contentWidth, context.viewport, 0)
		decoration := padding.Left + padding.Right + border.Left + border.Right + item.marginBefore + item.marginAfter
		basis := 0.0
		switch {
		case style.FlexBasis().Unit() != lengthAuto:
			basis = resolveLength(style.FlexBasis(), contentWidth, context.viewport, 0)
		case style.Width().Unit() != lengthAuto:
			basis = resolveLength(style.Width(), contentWidth, context.viewport, 0)
		default:
			intrinsic, err := context.intrinsicOuterWidths(item.node, contentWidth)
			if err != nil {
				return 0, err
			}
			basis = math.Max(0, intrinsic.preferred-decoration)
		}
		item.mainSize = math.Max(0, basis)
		item.outerMain = item.mainSize + decoration
		totalOuter += item.outerMain
		totalGrow += style.FlexGrow()
		totalShrinkWeight += style.FlexShrink() * item.mainSize
	}
	free := contentWidth - totalOuter
	if free > 0 && totalGrow > 0 {
		for index := range items {
			item := &items[index]
			addition := free * item.node.style.FlexGrow() / totalGrow
			item.mainSize += addition
			item.outerMain += addition
		}
		free = 0
	} else if free < 0 && totalShrinkWeight > 0 {
		deficit := -free
		for index := range items {
			item := &items[index]
			weight := item.node.style.FlexShrink() * item.mainSize
			reduction := math.Min(item.mainSize, deficit*weight/totalShrinkWeight)
			item.mainSize -= reduction
			item.outerMain -= reduction
		}
		free = 0
	}
	start, extraGap := justifyFlexSpace(node.style.JustifyContent(), node.style.JustifyContentOverflow(), free, len(items), reverse)
	cursorX := box.ContentBounds.X + start
	maxCross := 0.0
	var firstBaselineGroup, lastBaselineGroup baselineAlignmentGroup
	for index := range items {
		item := &items[index]
		marginTop := resolveLength(item.node.style.MarginTop(), contentWidth, context.viewport, 0)
		marginBottom := resolveLength(item.node.style.MarginBottom(), contentWidth, context.viewport, 0)
		childBox, err := context.layoutBlockSized(item.node, cursorX, box.ContentBounds.Y+marginTop, item.outerMain, definiteHeight, &item.mainSize, true)
		if err != nil {
			return 0, err
		}
		item.box = childBox
		cross := marginTop + childBox.Bounds.Height + marginBottom
		maxCross = math.Max(maxCross, cross)
		alignment := resolvedSelfAlignment(item.node.style.AlignSelf(), node.style.AlignItems())
		switch alignment {
		case computed.AlignBaseline:
			firstBaselineGroup.include(boxBaselineDistances(childBox, marginTop, marginBottom, false))
		case computed.AlignLastBaseline:
			lastBaselineGroup.include(boxBaselineDistances(childBox, marginTop, marginBottom, true))
		}
		cursorX += item.outerMain + gap + extraGap
	}
	maxCross = math.Max(maxCross, firstBaselineGroup.size())
	maxCross = math.Max(maxCross, lastBaselineGroup.size())
	containerHeight := flexContainerHeight(definiteHeight, maxCross)
	for index := range items {
		item := &items[index]
		marginTop := resolveLength(item.node.style.MarginTop(), contentWidth, context.viewport, 0)
		marginBottom := resolveLength(item.node.style.MarginBottom(), contentWidth, context.viewport, 0)
		availableCross := math.Max(0, containerHeight-marginTop-marginBottom)
		alignment := resolvedSelfAlignment(item.node.style.AlignSelf(), node.style.AlignItems())
		overflow := resolvedSelfOverflow(item.node.style.AlignSelf(), item.node.style.AlignSelfOverflow(), node.style.AlignItemsOverflow())
		if alignmentStretches(alignment) && item.node.style.Height().Unit() == lengthAuto {
			setBoxOuterHeight(item.box, availableCross)
		}
		offset := 0.0
		switch alignment {
		case computed.AlignBaseline:
			distances := boxBaselineDistances(item.box, marginTop, marginBottom, false)
			offset = firstBaselineGroup.start - distances.start
		case computed.AlignLastBaseline:
			distances := boxBaselineDistances(item.box, marginTop, marginBottom, true)
			offset = containerHeight - lastBaselineGroup.end - distances.start
		default:
			offset = alignFlexOffset(alignment, overflow, availableCross-item.box.Bounds.Height)
		}
		translateLayoutBox(item.box, 0, box.ContentBounds.Y+marginTop+offset-item.box.Bounds.Y)
		box.Children = append(box.Children, item.box)
		box.flow = append(box.flow, flowItem{box: item.box})
	}
	return containerHeight, nil
}

func (context *layoutContext) layoutFlexColumn(node *styledNode, box *Box, contentWidth float64, definiteHeight *float64, items []flexLayoutItem, reverse bool) (float64, error) {
	gap := math.Max(0, resolveLength(node.style.RowGap(), contentWidth, context.viewport, 0))
	totalMain := gap * math.Max(0, float64(len(items)-1))
	for index := range items {
		item := &items[index]
		itemWidth := contentWidth
		alignment := resolvedSelfAlignment(item.node.style.AlignSelf(), node.style.AlignItems())
		if !alignmentStretches(alignment) && item.node.style.Width().Unit() == lengthAuto {
			intrinsic, intrinsicErr := context.intrinsicOuterWidths(item.node, contentWidth)
			if intrinsicErr != nil {
				return 0, intrinsicErr
			}
			itemWidth = math.Min(contentWidth, intrinsic.preferred)
		}
		item.crossSize = itemWidth
		childBox, err := context.layoutBlockSized(item.node, box.ContentBounds.X, box.ContentBounds.Y, itemWidth, definiteHeight, nil, true)
		if err != nil {
			return 0, err
		}
		item.box = childBox
		marginTop := resolveLength(item.node.style.MarginTop(), contentWidth, context.viewport, 0)
		marginBottom := resolveLength(item.node.style.MarginBottom(), contentWidth, context.viewport, 0)
		basis := childBox.Bounds.Height
		if item.node.style.FlexBasis().Unit() != lengthAuto {
			basis = resolveLength(item.node.style.FlexBasis(), 0, context.viewport, basis)
		}
		item.mainSize = math.Max(0, basis)
		item.outerMain = marginTop + item.mainSize + marginBottom
		totalMain += item.outerMain
	}
	containerHeight := flexContainerHeight(definiteHeight, totalMain)
	free := containerHeight - totalMain
	totalGrow := 0.0
	totalShrinkWeight := 0.0
	for index := range items {
		totalGrow += items[index].node.style.FlexGrow()
		totalShrinkWeight += items[index].node.style.FlexShrink() * items[index].mainSize
	}
	if free > 0 && totalGrow > 0 {
		for index := range items {
			addition := free * items[index].node.style.FlexGrow() / totalGrow
			items[index].mainSize += addition
			items[index].outerMain += addition
		}
		free = 0
	} else if free < 0 && totalShrinkWeight > 0 {
		deficit := -free
		for index := range items {
			weight := items[index].node.style.FlexShrink() * items[index].mainSize
			reduction := math.Min(items[index].mainSize, deficit*weight/totalShrinkWeight)
			items[index].mainSize -= reduction
			items[index].outerMain -= reduction
		}
		free = 0
	}
	start, extraGap := justifyFlexSpace(node.style.JustifyContent(), node.style.JustifyContentOverflow(), free, len(items), reverse)
	cursorY := box.ContentBounds.Y + start
	for index := range items {
		item := &items[index]
		if item.box != nil && item.mainSize != item.box.Bounds.Height {
			// Flexing establishes a definite used main size. Re-layout the item
			// with that size so percentage-height descendants resolve against the
			// final flexed content box rather than its intrinsic first pass.
			decoration := item.box.Bounds.Height - item.box.ContentBounds.Height
			forcedContentHeight := math.Max(0, item.mainSize-decoration)
			relaid, err := context.layoutBlockSizedWithSubgrid(
				item.node,
				box.ContentBounds.X,
				box.ContentBounds.Y,
				item.crossSize,
				definiteHeight,
				nil,
				true,
				nil,
				blockLayoutOverrides{forceContentHeight: &forcedContentHeight},
			)
			if err != nil {
				return 0, err
			}
			item.box = relaid
		}
		marginTop := resolveLength(item.node.style.MarginTop(), contentWidth, context.viewport, 0)
		marginBottom := resolveLength(item.node.style.MarginBottom(), contentWidth, context.viewport, 0)
		marginLeft := resolveLength(item.node.style.MarginLeft(), contentWidth, context.viewport, 0)
		marginRight := resolveLength(item.node.style.MarginRight(), contentWidth, context.viewport, 0)
		setBoxOuterHeight(item.box, item.mainSize)
		availableCross := math.Max(0, contentWidth-marginLeft-marginRight)
		alignment := resolvedSelfAlignment(item.node.style.AlignSelf(), node.style.AlignItems())
		overflow := resolvedSelfOverflow(item.node.style.AlignSelf(), item.node.style.AlignSelfOverflow(), node.style.AlignItemsOverflow())
		xOffset := alignFlexOffset(alignment, overflow, availableCross-item.box.Bounds.Width)
		translateLayoutBox(item.box, box.ContentBounds.X+marginLeft+xOffset-item.box.Bounds.X, cursorY+marginTop-item.box.Bounds.Y)
		box.Children = append(box.Children, item.box)
		box.flow = append(box.flow, flowItem{box: item.box})
		cursorY += marginTop + item.mainSize + marginBottom + gap + extraGap
	}
	return containerHeight, nil
}

func flexContainerHeight(definiteHeight *float64, fallback float64) float64 {
	if definiteHeight != nil {
		return math.Max(0, *definiteHeight)
	}
	return math.Max(0, fallback)
}

func setBoxOuterHeight(box *Box, outerHeight float64) {
	if box == nil {
		return
	}
	box.Bounds.Height = math.Max(0, outerHeight)
	box.ContentBounds.Height = math.Max(0, box.Bounds.Height-box.Border.Top-box.Padding.Top-box.Padding.Bottom-box.Border.Bottom)
}

func justifyFlexSpace(justify computed.JustifyContent, overflow computed.OverflowAlignment, free float64, count int, reverse bool) (start, extraGap float64) {
	if count == 0 {
		return 0, 0
	}
	if free < 0 {
		if overflow == computed.OverflowAlignmentSafe {
			return flexMainStart(free, reverse), 0
		}
		switch justify {
		case computed.JustifyEnd:
			return free, 0
		case computed.JustifyFlexEnd:
			return flexMainEnd(free, reverse), 0
		case computed.JustifyCenter:
			return free / 2, 0
		case computed.JustifyFlexStart, computed.JustifyNormal, computed.JustifyStretch:
			return flexMainStart(free, reverse), 0
		default:
			// Distributed values use their safe fallback alignment when
			// there is no space to distribute.
			if justify == computed.JustifySpaceBetween || justify == computed.JustifySpaceAround || justify == computed.JustifySpaceEvenly {
				return flexMainStart(free, reverse), 0
			}
			return 0, 0
		}
	}
	if free == 0 {
		return 0, 0
	}
	switch justify {
	case computed.JustifyEnd:
		return free, 0
	case computed.JustifyFlexEnd:
		return flexMainEnd(free, reverse), 0
	case computed.JustifyFlexStart, computed.JustifyNormal, computed.JustifyStretch:
		return flexMainStart(free, reverse), 0
	case computed.JustifyCenter:
		return free / 2, 0
	case computed.JustifySpaceBetween:
		if count > 1 {
			return 0, free / float64(count-1)
		}
		return flexMainStart(free, reverse), 0
	case computed.JustifySpaceAround:
		space := free / float64(count)
		return space / 2, space
	case computed.JustifySpaceEvenly:
		space := free / float64(count+1)
		return space, space
	}
	return 0, 0
}

func flexMainStart(free float64, reverse bool) float64 {
	if reverse {
		return free
	}
	return 0
}

func flexMainEnd(free float64, reverse bool) float64 {
	if reverse {
		return 0
	}
	return free
}

func alignFlexOffset(align computed.AlignItems, overflow computed.OverflowAlignment, free float64) float64 {
	if free < 0 && overflow == computed.OverflowAlignmentSafe {
		return 0
	}
	switch align {
	case computed.AlignEndItems, computed.AlignFlexEnd, computed.AlignSelfEnd:
		return free
	case computed.AlignLastBaseline:
		if free > 0 {
			return free
		}
		return 0
	case computed.AlignCenterItems:
		return free / 2
	default:
		return 0
	}
}

func resolvedSelfAlignment(self, parent computed.AlignItems) computed.AlignItems {
	if self == computed.AlignAuto {
		return parent
	}
	return self
}

func resolvedSelfOverflow(self computed.AlignItems, overflow, parent computed.OverflowAlignment) computed.OverflowAlignment {
	if self == computed.AlignAuto {
		return parent
	}
	return overflow
}

func alignmentStretches(align computed.AlignItems) bool {
	return align == computed.AlignNormal || align == computed.AlignStretch
}

type baselineDistances struct {
	start float64
	end   float64
}

type baselineAlignmentGroup struct {
	start   float64
	end     float64
	present bool
}

func (group *baselineAlignmentGroup) include(distances baselineDistances) {
	if group == nil {
		return
	}
	group.present = true
	group.start = math.Max(group.start, distances.start)
	group.end = math.Max(group.end, distances.end)
}

func (group baselineAlignmentGroup) size() float64 {
	if !group.present {
		return 0
	}
	return group.start + group.end
}

func boxBaselineDistances(box *Box, marginStart, marginEnd float64, last bool) baselineDistances {
	if box == nil {
		return baselineDistances{start: marginStart, end: marginEnd}
	}
	baseline := box.Bounds.Y + box.Bounds.Height
	var ok bool
	if last {
		baseline, ok = lastBoxBaseline(box)
	} else {
		baseline, ok = firstBoxBaseline(box)
	}
	if !ok {
		baseline = box.Bounds.Y + box.Bounds.Height
	}
	offset := clamp(baseline-box.Bounds.Y, 0, box.Bounds.Height)
	return baselineDistances{
		start: marginStart + offset,
		end:   marginEnd + box.Bounds.Height - offset,
	}
}

func (context *layoutContext) finalizeBlock(node *styledNode, box *Box, containingWidth float64) (*Box, error) {
	if node.style.Display() == displayListItem && node.style.ListStyleType() != listStyleNone {
		if err := context.addListMarker(node, box); err != nil {
			return nil, err
		}
	}
	if node.style.Position() == positionRelative {
		deltaX := relativePositionOffset(node.style.Left(), node.style.Right(), containingWidth, context.viewport)
		deltaY := relativePositionOffset(node.style.Top(), node.style.Bottom(), box.Bounds.Height, context.viewport)
		translateLayoutBox(box, deltaX, deltaY)
	}
	return box, nil
}

func relativePositionOffset(primary, opposite length, percentageBase float64, viewport Viewport) float64 {
	if primary.Unit() != lengthAuto {
		return resolveLength(primary, percentageBase, viewport, 0)
	}
	if opposite.Unit() != lengthAuto {
		return -resolveLength(opposite, percentageBase, viewport, 0)
	}
	return 0
}

func translateLayoutBox(box *Box, deltaX, deltaY float64) {
	if box == nil || (deltaX == 0 && deltaY == 0) {
		return
	}
	box.Bounds.X += deltaX
	box.Bounds.Y += deltaY
	box.ContentBounds.X += deltaX
	box.ContentBounds.Y += deltaY
	if box.hasDecorationBounds {
		box.decorationBounds.X += deltaX
		box.decorationBounds.Y += deltaY
	}
	if box.hasClipBounds {
		box.clipBounds.X += deltaX
		box.clipBounds.Y += deltaY
	}
	for index := range box.backgroundRects {
		box.backgroundRects[index].X += deltaX
		box.backgroundRects[index].Y += deltaY
	}
	for index := range box.afterPaint {
		box.afterPaint[index].Rect.X += deltaX
		box.afterPaint[index].Rect.Y += deltaY
	}
	for index := range box.tableClientRects {
		box.tableClientRects[index].X += deltaX
		box.tableClientRects[index].Y += deltaY
	}
	for index := range box.Fragments {
		translateInlineFragment(&box.Fragments[index], deltaX, deltaY)
	}
	for index := range box.Text {
		translateTextFragment(&box.Text[index], deltaX, deltaY)
	}
	for index := range box.flow {
		if box.flow[index].box == nil {
			translateInlineFragment(&box.flow[index].fragment, deltaX, deltaY)
		}
	}
	for _, child := range box.Children {
		translateLayoutBox(child, deltaX, deltaY)
	}
}

func translateInlineFragment(fragment *InlineFragment, deltaX, deltaY float64) {
	if fragment == nil {
		return
	}
	switch fragment.Kind {
	case TextFragmentKind:
		translateTextFragment(&fragment.Text, deltaX, deltaY)
	case ImageFragmentKind:
		fragment.Image.Bounds.X += deltaX
		fragment.Image.Bounds.Y += deltaY
	}
}

func translateTextFragment(fragment *TextFragment, deltaX, deltaY float64) {
	if fragment == nil {
		return
	}
	fragment.X += deltaX
	fragment.BaselineY += deltaY
	if fragment.paintOrientation != textPaintHorizontal {
		fragment.paintBounds.X += deltaX
		fragment.paintBounds.Y += deltaY
	}
}

type layoutNodeKey struct {
	node   *dom.Node
	pseudo computed.PseudoElement
}

func indexLayoutBoxes(box *Box, boxes map[layoutNodeKey]*Box) {
	if box == nil {
		return
	}
	if box.Node != nil && !box.skipLayoutIndex {
		boxes[layoutNodeKey{node: box.Node, pseudo: box.Pseudo}] = box
	}
	for _, child := range box.Children {
		indexLayoutBoxes(child, boxes)
	}
}

// layoutPositionedDescendants adds out-of-flow boxes after normal flow has
// established every possible containing block. The boxes stay attached to
// their nearest represented DOM ancestor for geometry and input traversal,
// while their used coordinates come from the nearest positioned ancestor (or
// the viewport for fixed positioning).
func (context *layoutContext) layoutPositionedDescendants(root *styledNode, rootBox *Box) error {
	boxes := make(map[layoutNodeKey]*Box)
	indexLayoutBoxes(rootBox, boxes)
	viewport := Rect{Width: float64(context.viewport.Width), Height: float64(context.viewport.Height)}

	var visit func(*styledNode, *Box, Rect) error
	visit = func(node *styledNode, parentBox *Box, containingBlock Rect) error {
		if node == nil || node.style.Display() == displayNone {
			return nil
		}
		if node.generated {
			return nil
		}
		currentBox := boxes[layoutNodeKey{node: node.node, pseudo: node.pseudo}]
		if isOutOfFlow(node.style.Position()) {
			if parentBox == nil {
				parentBox = rootBox
			}
			usedContainingBlock := containingBlock
			if node.style.Position() == positionFixed {
				usedContainingBlock = viewport
			}
			staticY := usedContainingBlock.Y + resolveLength(node.style.MarginTop(), usedContainingBlock.Width, context.viewport, 0)
			containingHeight := usedContainingBlock.Height
			positioned, err := context.layoutBlock(node, usedContainingBlock.X, staticY, usedContainingBlock.Width, &containingHeight)
			if err != nil {
				return err
			}
			positionOutOfFlowBox(positioned, node.style, usedContainingBlock, context.viewport)
			positioned.positioned = true
			positioned.zIndex = node.style.ZIndex().Value()
			positioned.zIndexAuto = node.style.ZIndex().IsAuto()
			parentBox.Children = append(parentBox.Children, positioned)
			indexLayoutBoxes(positioned, boxes)
			currentBox = positioned
		}
		if currentBox == nil {
			currentBox = parentBox
		}
		nextContainingBlock := containingBlock
		if currentBox != nil && node.style.Position() != positionStatic {
			nextContainingBlock = boxClientBounds(currentBox)
		}
		for _, child := range node.children {
			if err := visit(child, currentBox, nextContainingBlock); err != nil {
				return err
			}
		}
		return nil
	}

	return visit(root, rootBox, viewport)
}

func positionOutOfFlowBox(box *Box, style computedStyle, containingBlock Rect, viewport Viewport) {
	if box == nil {
		return
	}
	desiredX := box.Bounds.X
	desiredY := box.Bounds.Y
	marginLeft := resolveLength(style.MarginLeft(), containingBlock.Width, viewport, 0)
	marginRight := resolveLength(style.MarginRight(), containingBlock.Width, viewport, 0)
	marginTop := resolveLength(style.MarginTop(), containingBlock.Width, viewport, 0)
	marginBottom := resolveLength(style.MarginBottom(), containingBlock.Width, viewport, 0)
	if style.Left().Unit() != lengthAuto {
		desiredX = containingBlock.X + resolveLength(style.Left(), containingBlock.Width, viewport, 0) + marginLeft
	} else if style.Right().Unit() != lengthAuto {
		desiredX = containingBlock.X + containingBlock.Width - resolveLength(style.Right(), containingBlock.Width, viewport, 0) - marginRight - box.Bounds.Width
	}
	if style.Top().Unit() != lengthAuto {
		desiredY = containingBlock.Y + resolveLength(style.Top(), containingBlock.Height, viewport, 0) + marginTop
	} else if style.Bottom().Unit() != lengthAuto {
		desiredY = containingBlock.Y + containingBlock.Height - resolveLength(style.Bottom(), containingBlock.Height, viewport, 0) - marginBottom - box.Bounds.Height
	}
	translateLayoutBox(box, desiredX-box.Bounds.X, desiredY-box.Bounds.Y)
}

func (context *layoutContext) addListMarker(node *styledNode, box *Box) error {
	markerText := context.listMarkerText(node.node, node.style.ListStyleType())
	if markerText == "" {
		return nil
	}
	metrics, err := context.fonts.metrics(markerText, node.style.FontSize(), node.style.FontWeight(), node.style.FontStyle(), node.style.FontFamily())
	if err != nil {
		return err
	}
	markerOrientation := textPaintHorizontal
	if node.style.verticalLayout() {
		runs := verticalTextRuns(markerText, node.style.TextOrientation())
		if len(runs) != 0 {
			markerOrientation = runs[0].orientation
			if markerOrientation == textPaintUpright {
				metrics.width = float64(runs[0].units) * node.style.FontSize()
			}
		}
	}
	markerLineHeight := math.Max(node.style.LineHeight().Pixels(node.style.FontSize()), metrics.ascent+metrics.descent)
	markerLeading := math.Max(0, markerLineHeight-metrics.ascent-metrics.descent)
	baselineOffset := markerLeading/2 + metrics.ascent
	baseline, hasBaseline := firstBoxBaseline(box)
	if !hasBaseline {
		baseline = box.ContentBounds.Y + baselineOffset
		if box.ContentBounds.Height < markerLineHeight && node.style.Height().Unit() == lengthAuto {
			box.ContentBounds.Height = markerLineHeight
			box.Bounds.Height = box.Border.Top + box.Padding.Top + markerLineHeight + box.Padding.Bottom + box.Border.Bottom
		}
	}
	marker := TextFragment{
		Node:                node.node,
		Pseudo:              node.pseudo,
		Text:                markerText,
		X:                   box.Bounds.X - node.style.FontSize()*.5 - metrics.width,
		BaselineY:           baseline,
		BaselineOffset:      baselineOffset,
		Width:               metrics.width,
		Height:              markerLineHeight,
		FontSize:            node.style.FontSize(),
		FontFamily:          node.style.FontFamily(),
		FontWeight:          node.style.FontWeight(),
		FontStyle:           node.style.FontStyle(),
		Color:               node.style.Color(),
		Visible:             node.style.Visibility() == visibilityVisible,
		Underline:           node.style.Underline(),
		paintOpacity:        1,
		verticalOrientation: markerOrientation,
	}
	fragment := InlineFragment{Kind: TextFragmentKind, Text: marker}
	box.flow = append([]flowItem{{fragment: fragment}}, box.flow...)
	return nil
}

func firstBoxBaseline(box *Box) (float64, bool) {
	if box == nil {
		return 0, false
	}
	if box.tableWrapper && box.tableRoot != nil {
		return firstBoxBaseline(box.tableRoot)
	}
	for _, item := range box.flow {
		if item.box != nil {
			if baseline, ok := firstBoxBaseline(item.box); ok {
				return baseline, true
			}
			continue
		}
		switch item.fragment.Kind {
		case TextFragmentKind:
			return item.fragment.Text.BaselineY, true
		case ImageFragmentKind:
			return item.fragment.Image.Bounds.Y + item.fragment.Image.Bounds.Height, true
		}
	}
	for _, child := range box.Children {
		if baseline, ok := firstBoxBaseline(child); ok {
			return baseline, true
		}
	}
	return 0, false
}

func (context *layoutContext) listMarkerText(node *dom.Node, marker listStyleType) string {
	switch marker {
	case listStyleDisc:
		return "•"
	case listStyleCircle:
		return "◦"
	case listStyleSquare:
		return "▪"
	case listStyleDecimal:
		return strconv.Itoa(context.generatedListItemValue(node)) + "."
	default:
		return ""
	}
}

func (context *layoutContext) generatedListItemValue(node *dom.Node) int {
	if node == nil || node.Parent == nil {
		return 1
	}
	container := node.Parent
	step := 1
	start := 1
	if container.Type == dom.ElementNode && container.Data == "ol" {
		if _, reversed := attribute(container, "reversed"); reversed {
			step = -1
			start = context.generatedListItemCount(container)
		}
		if source, ok := attribute(container, "start"); ok {
			if parsed, err := strconv.Atoi(strings.TrimSpace(source)); err == nil {
				start = parsed
			}
		}
	}
	value := start
	for _, child := range container.Children {
		childStyle, ok := context.styles[child]
		if !ok || childStyle.Display() != displayListItem {
			continue
		}
		if container.Type == dom.ElementNode && container.Data == "ol" && child.Type == dom.ElementNode && child.Data == "li" {
			if source, ok := attribute(child, "value"); ok {
				if parsed, err := strconv.Atoi(strings.TrimSpace(source)); err == nil {
					value = parsed
				}
			}
		}
		if child == node {
			return value
		}
		value += step
	}
	return value
}

func (context *layoutContext) generatedListItemCount(container *dom.Node) int {
	count := 0
	for _, child := range container.Children {
		if style, ok := context.styles[child]; ok && style.Display() == displayListItem {
			count++
		}
	}
	return count
}

func (context *layoutContext) layoutInline(nodes []*styledNode, x, y, width float64, containingHeight *float64, containerStyle computedStyle, tableCellFirstPass bool) (inlineLayout, error) {
	builder := inlineTokenBuilder{images: context.images}
	var directChildren map[*styledNode]struct{}
	if tableCellFirstPass {
		directChildren = make(map[*styledNode]struct{}, len(nodes))
	}
	for _, node := range nodes {
		builder.add(node, 1)
		if tableCellFirstPass {
			directChildren[node] = struct{}{}
		}
	}
	if len(builder.tokens) == 0 {
		return inlineLayout{}, nil
	}
	containerMetrics, err := context.fonts.metrics("M", containerStyle.FontSize(), containerStyle.FontWeight(), containerStyle.FontStyle(), containerStyle.FontFamily())
	if err != nil {
		return inlineLayout{}, err
	}
	containerLineHeight := math.Max(containerStyle.LineHeight().Pixels(containerStyle.FontSize()), containerMetrics.ascent+containerMetrics.descent)
	containerLeading := math.Max(0, containerLineHeight-containerMetrics.ascent-containerMetrics.descent)
	containerBaseline := containerLeading/2 + containerMetrics.ascent
	containerDescent := containerLineHeight - containerBaseline
	containerXHeight, err := context.fonts.xHeight(containerStyle.FontSize(), containerStyle.FontWeight(), containerStyle.FontStyle(), containerStyle.FontFamily())
	if err != nil {
		return inlineLayout{}, err
	}
	alignment := containerStyle.TextAlignment()
	inlineDirection := containerStyle.Direction()
	if containerStyle.verticalLayout() && containerStyle.TextOrientation() == computed.TextOrientationUpright {
		// CSS Writing Modes forces the used direction to ltr for upright text.
		// The computed direction remains unchanged and is still exposed by CSSOM.
		inlineDirection = directionLTR
	}

	var result inlineLayout
	var line []inlinePiece
	lineWidth := 0.0
	cursorY := y
	appendFragment := func(fragment InlineFragment) {
		if fragment.Kind == TextFragmentKind && len(result.fragments) != 0 && len(result.flow) != 0 {
			previous := &result.fragments[len(result.fragments)-1]
			flow := &result.flow[len(result.flow)-1]
			if flow.box == nil && flow.fragment.Kind == TextFragmentKind && previous.Kind == TextFragmentKind {
				text := fragment.Text
				if previous.Text.Node == text.Node &&
					previous.Text.Pseudo == text.Pseudo &&
					previous.Text.BaselineY == text.BaselineY &&
					previous.Text.BaselineOffset == text.BaselineOffset &&
					previous.Text.Height == text.Height &&
					previous.Text.FontSize == text.FontSize &&
					previous.Text.FontFamily == text.FontFamily &&
					previous.Text.FontWeight == text.FontWeight &&
					previous.Text.FontStyle == text.FontStyle &&
					previous.Text.Color == text.Color &&
					previous.Text.verticalOrientation == text.verticalOrientation &&
					previous.Text.X+previous.Text.Width == text.X {
					previous.Text.Text += text.Text
					previous.Text.Width += text.Width
					flow.fragment = *previous
					return
				}
			}
		}
		result.fragments = append(result.fragments, fragment)
		result.flow = append(result.flow, flowItem{fragment: fragment})
	}
	flushLine := func(justify bool) {
		if len(line) == 0 {
			return
		}
		lineAscent := containerBaseline
		lineDescent := containerDescent
		raises := make([]float64, len(line))
		lineRelativeHeight := 0.0
		for index := range line {
			piece := &line[index]
			if piece.box == nil && !piece.replaced {
				piece.height = math.Max(piece.style.LineHeight().Pixels(piece.style.FontSize()), piece.metrics.ascent+piece.metrics.descent)
				leading := math.Max(0, piece.height-piece.metrics.ascent-piece.metrics.descent)
				piece.baseline = leading/2 + piece.metrics.ascent
			} else if piece.replaced {
				piece.baseline = piece.height
			}
			mode := piece.verticalAlign.Mode()
			if mode == verticalAlignTop || mode == verticalAlignBottom {
				lineRelativeHeight = math.Max(lineRelativeHeight, piece.height)
				continue
			}
			raise := context.inlineVerticalRaise(*piece, containerMetrics.ascent, containerMetrics.descent, containerXHeight, containerStyle.FontSize())
			raises[index] = raise
			top := -piece.baseline - raise
			bottom := top + piece.height
			lineAscent = math.Max(lineAscent, -top)
			lineDescent = math.Max(lineDescent, bottom)
		}
		lineHeight := math.Max(lineAscent+lineDescent, lineRelativeHeight)
		if extra := lineHeight - lineAscent - lineDescent; extra > 0 {
			lineAscent += extra / 2
			lineDescent += extra - extra/2
		}
		baseline := cursorY + lineAscent
		lineOffset := 0.0
		switch alignment {
		case alignCenter:
			lineOffset = (width - lineWidth) / 2
		case alignRight:
			lineOffset = width - lineWidth
		case alignEnd:
			if inlineDirection == directionLTR {
				lineOffset = width - lineWidth
			}
		case alignStart:
			if inlineDirection == directionRTL {
				lineOffset = width - lineWidth
			}
		case alignLeft, alignJustify:
		}
		justifyStep := 0.0
		if alignment == alignJustify && justify && lineWidth < width {
			opportunities := 0
			for _, piece := range line {
				if piece.justifyBefore {
					opportunities++
				}
			}
			if opportunities != 0 {
				justifyStep = (width - lineWidth) / float64(opportunities)
			}
		}
		justifiedOffset := 0.0
		for index, piece := range line {
			if piece.justifyBefore {
				justifiedOffset += justifyStep
			}
			targetY := baseline - piece.baseline - raises[index]
			switch piece.verticalAlign.Mode() {
			case verticalAlignTop:
				targetY = cursorY
			case verticalAlignBottom:
				targetY = cursorY + lineHeight - piece.height
			}
			if piece.box != nil {
				targetX := x + lineOffset + piece.x + justifiedOffset
				translateLayoutBox(piece.box, targetX, targetY)
				result.flow = append(result.flow, flowItem{box: piece.box})
				continue
			}
			if piece.replaced {
				appendFragment(InlineFragment{
					Kind: ImageFragmentKind,
					Image: ImageFragment{
						Node:                  piece.node,
						Image:                 piece.image,
						Bounds:                Rect{X: x + lineOffset + piece.x + justifiedOffset, Y: targetY, Width: piece.width, Height: piece.height},
						Opacity:               piece.opacity,
						percentHeightResolved: piece.percentHeightResolved,
					},
				})
				continue
			}
			textColor := piece.style.Color()
			textColor.A = uint8(math.Round(float64(textColor.A) * clamp(piece.opacity, 0, 1)))
			text := TextFragment{
				Node:                piece.node,
				Pseudo:              piece.pseudo,
				Text:                piece.text,
				X:                   x + lineOffset + piece.x + justifiedOffset,
				BaselineY:           targetY + piece.baseline,
				BaselineOffset:      piece.baseline,
				Width:               piece.width,
				Height:              piece.height,
				FontSize:            piece.style.FontSize(),
				FontFamily:          piece.style.FontFamily(),
				FontWeight:          piece.style.FontWeight(),
				FontStyle:           piece.style.FontStyle(),
				Color:               textColor,
				Visible:             piece.style.Visibility() == visibilityVisible,
				Underline:           piece.style.Underline(),
				paintOpacity:        clamp(piece.opacity, 0, 1),
				verticalOrientation: piece.orientation,
			}
			appendFragment(InlineFragment{Kind: TextFragmentKind, Text: text})
		}
		cursorY += lineHeight
		line = nil
		lineWidth = 0
	}

	lastWasBreak := false
	var lastBreakStyle computedStyle
	verticalTextLayout := containerStyle.verticalLayout()
	for _, token := range builder.tokens {
		if token.lineBreak {
			if len(line) == 0 {
				cursorY += token.style.LineHeight().Pixels(token.style.FontSize())
			} else {
				flushLine(false)
			}
			lastWasBreak = true
			lastBreakStyle = token.style
			continue
		}
		lastWasBreak = false
		wordMetrics := textMetrics{}
		var atomicBox *Box
		atomicHeight := 0.0
		atomicBaseline := 0.0
		imageWidth := 0.0
		imageHeight := 0.0
		var measuredRuns []measuredVerticalTextRun
		if token.atomic != nil {
			var err error
			_, direct := directChildren[token.atomic]
			atomicBox, wordMetrics.width, atomicHeight, atomicBaseline, err = context.layoutAtomicInline(token.atomic, width, containingHeight, token.opacity, tableCellFirstPass && direct)
			if err != nil {
				return inlineLayout{}, err
			}
			wordMetrics.ascent = atomicBaseline
			wordMetrics.descent = math.Max(0, atomicHeight-atomicBaseline)
		} else if token.replaced {
			var ok bool
			imageWidth, imageHeight, ok = context.replacedDimensions(token.style, token.image, width, containingHeight, 0, 0)
			if !ok {
				continue
			}
			wordMetrics = textMetrics{width: imageWidth, ascent: imageHeight}
		} else if verticalTextLayout {
			var err error
			measuredRuns, wordMetrics, err = context.measureVerticalTextRuns(token)
			if err != nil {
				return inlineLayout{}, err
			}
		} else {
			var err error
			wordMetrics, err = context.fonts.metrics(token.text, token.style.FontSize(), token.style.FontWeight(), token.style.FontStyle(), token.style.FontFamily())
			if err != nil {
				return inlineLayout{}, err
			}
		}
		prefix := ""
		prefixWidth := 0.0
		if token.leadingSpace && len(line) != 0 {
			spaceMetrics, err := context.fonts.metrics(" ", token.style.FontSize(), token.style.FontWeight(), token.style.FontStyle(), token.style.FontFamily())
			if err != nil {
				return inlineLayout{}, err
			}
			prefix = " "
			prefixWidth = spaceMetrics.width
		}
		if token.wrapBefore && len(line) != 0 && lineWidth+prefixWidth+wordMetrics.width > width {
			flushLine(true)
			prefix = ""
			prefixWidth = 0
		}
		justifyBefore := prefix != ""
		if atomicBox != nil {
			line = append(line, inlinePiece{
				node:          token.node,
				pseudo:        token.pseudo,
				style:         token.style,
				box:           atomicBox,
				x:             lineWidth + prefixWidth,
				width:         wordMetrics.width,
				height:        atomicHeight,
				baseline:      atomicBaseline,
				metrics:       wordMetrics,
				justifyBefore: justifyBefore,
				verticalAlign: token.verticalAlign,
			})
			lineWidth += prefixWidth + wordMetrics.width
			continue
		}
		if token.replaced {
			line = append(line, inlinePiece{
				node:                  token.node,
				style:                 token.style,
				image:                 token.image,
				replaced:              true,
				percentHeightResolved: token.style.Height().DependsOnPercent() && containingHeight != nil,
				justifyBefore:         justifyBefore,
				opacity:               token.opacity,
				x:                     lineWidth + prefixWidth,
				width:                 wordMetrics.width,
				height:                imageHeight,
				metrics:               wordMetrics,
				verticalAlign:         token.verticalAlign,
			})
			lineWidth += prefixWidth + wordMetrics.width
			continue
		}
		if verticalTextLayout {
			if prefix != "" {
				spaceMetrics, err := context.fonts.metrics(" ", token.style.FontSize(), token.style.FontWeight(), token.style.FontStyle(), token.style.FontFamily())
				if err != nil {
					return inlineLayout{}, err
				}
				line = append(line, inlinePiece{
					text: " ", node: token.node, pseudo: token.pseudo, style: token.style,
					opacity: token.opacity, x: lineWidth, width: prefixWidth, metrics: spaceMetrics,
					justifyBefore: true, verticalAlign: token.verticalAlign, orientation: textPaintSidewaysRight,
				})
				lineWidth += prefixWidth
			}
			for _, run := range measuredRuns {
				line = append(line, inlinePiece{
					text: run.text, node: token.node, pseudo: token.pseudo, style: token.style,
					opacity: token.opacity, x: lineWidth, width: run.metrics.width, metrics: run.metrics,
					verticalAlign: token.verticalAlign, orientation: run.orientation,
				})
				lineWidth += run.metrics.width
			}
			continue
		}
		pieceText := prefix + token.text
		pieceMetrics, err := context.fonts.metrics(pieceText, token.style.FontSize(), token.style.FontWeight(), token.style.FontStyle(), token.style.FontFamily())
		if err != nil {
			return inlineLayout{}, err
		}
		line = append(line, inlinePiece{text: pieceText, node: token.node, pseudo: token.pseudo, style: token.style, opacity: token.opacity, x: lineWidth, width: pieceMetrics.width, metrics: pieceMetrics, justifyBefore: justifyBefore, verticalAlign: token.verticalAlign})
		lineWidth += pieceMetrics.width
	}
	if lastWasBreak {
		cursorY += lastBreakStyle.LineHeight().Pixels(lastBreakStyle.FontSize())
	} else {
		flushLine(false)
	}
	result.height = cursorY - y
	return result, nil
}

func (context *layoutContext) measureVerticalTextRuns(token inlineToken) ([]measuredVerticalTextRun, textMetrics, error) {
	runs := verticalTextRuns(token.text, token.style.TextOrientation())
	measured := make([]measuredVerticalTextRun, 0, len(runs))
	combined := textMetrics{}
	for _, run := range runs {
		metrics, err := context.fonts.metrics(run.text, token.style.FontSize(), token.style.FontWeight(), token.style.FontStyle(), token.style.FontFamily())
		if err != nil {
			return nil, textMetrics{}, err
		}
		if run.orientation == textPaintUpright {
			// Go's bundled font API exposes horizontal metrics only. CSS permits
			// synthesizing vertical metrics; use one em advance per typographic
			// character unit and retain the face's ascent/descent cross metrics.
			metrics.width = float64(run.units) * token.style.FontSize()
		}
		combined.width += metrics.width
		combined.height = math.Max(combined.height, metrics.height)
		combined.ascent = math.Max(combined.ascent, metrics.ascent)
		combined.descent = math.Max(combined.descent, metrics.descent)
		measured = append(measured, measuredVerticalTextRun{verticalTextRun: run, metrics: metrics})
	}
	return measured, combined, nil
}

func (context *layoutContext) inlineVerticalRaise(piece inlinePiece, parentAscent, parentDescent, parentXHeight, parentFontSize float64) float64 {
	switch piece.verticalAlign.Mode() {
	case verticalAlignSub:
		// CSS Inline permits font metrics here and defines one fifth of the
		// parent's font size as the fallback when no subscript metric exists.
		return -parentFontSize / 5
	case verticalAlignSuper:
		// The corresponding superscript fallback is one third of the parent
		// font size.
		return parentFontSize / 3
	case verticalAlignTextTop:
		return parentAscent - piece.baseline
	case verticalAlignTextBottom:
		return piece.height - piece.baseline - parentDescent
	case verticalAlignMiddle:
		return piece.height/2 + parentXHeight/2 - piece.baseline
	case verticalAlignLength:
		lineHeight := piece.style.LineHeight().Pixels(piece.style.FontSize())
		if resolved, ok := piece.verticalAlign.Offset().Resolve(lineHeight, float64(context.viewport.Width), float64(context.viewport.Height)); ok {
			return resolved
		}
	}
	return 0
}

type intrinsicWidths struct {
	minimum   float64
	preferred float64
}

type intrinsicCacheKey struct {
	node           *styledNode
	availableWidth float64
}

func (context *layoutContext) layoutAtomicInline(node *styledNode, availableWidth float64, containingHeight *float64, opacity float64, tableCellFirstPass bool) (*Box, float64, float64, float64, error) {
	style := node.style
	marginLeft := resolveLength(style.MarginLeft(), availableWidth, context.viewport, 0)
	marginRight := resolveLength(style.MarginRight(), availableWidth, context.viewport, 0)
	marginTop := resolveLength(style.MarginTop(), availableWidth, context.viewport, 0)
	marginBottom := resolveLength(style.MarginBottom(), availableWidth, context.viewport, 0)

	var box *Box
	var err error
	overrides := blockLayoutOverrides{}
	if tableCellFirstPass && tableCellFirstPassZeroHeight(node) {
		overrides.forceZeroContentHeight = true
	}
	orthogonalVerticalRoot := !style.verticalLayout() && style.WritingMode() != writingModeHorizontalTB
	if style.Width().Unit() == lengthAuto && !orthogonalVerticalRoot {
		padding := context.resolvePadding(style, availableWidth)
		border := context.resolveBorder(style, availableWidth)
		horizontalInsets := padding.Left + padding.Right + border.Left + border.Right
		intrinsic, measureErr := context.intrinsicContentWidths(node, availableWidth)
		if measureErr != nil {
			return nil, 0, 0, 0, measureErr
		}
		availableContent := math.Max(0, availableWidth-marginLeft-marginRight-horizontalInsets)
		contentWidth := math.Min(math.Max(intrinsic.minimum, availableContent), intrinsic.preferred)
		box, err = context.layoutBlockSizedWithSubgrid(node, 0, marginTop, availableWidth, containingHeight, &contentWidth, true, nil, overrides)
	} else {
		box, err = context.layoutBlockWithOverrides(node, 0, marginTop, availableWidth, containingHeight, overrides)
	}
	if err != nil {
		return nil, 0, 0, 0, err
	}
	// Auto inline-axis margins on an inline-level atomic box compute to zero;
	// layoutBlock's block-formatting centering rule must not leak across this
	// outer display boundary.
	translateLayoutBox(box, marginLeft-box.Bounds.X, 0)
	box.paintOpacity = opacity
	box.hasOpacity = true

	outerWidth := math.Max(0, marginLeft+box.Bounds.Width+marginRight)
	outerHeight := math.Max(0, marginTop+box.Bounds.Height+marginBottom)
	baseline := outerHeight
	if style.OverflowX() == computed.OverflowVisible && style.OverflowY() == computed.OverflowVisible {
		if candidate, ok := lastBoxBaseline(box); ok {
			baseline = clamp(candidate, 0, outerHeight)
		}
	}
	return box, outerWidth, outerHeight, baseline, nil
}

func (context *layoutContext) intrinsicContentWidths(node *styledNode, availableWidth float64) (intrinsicWidths, error) {
	key := intrinsicCacheKey{node: node, availableWidth: availableWidth}
	if cached, ok := context.intrinsicCache[key]; ok {
		return cached, nil
	}
	measured, err := context.intrinsicContentWidthsUncached(node, availableWidth)
	if err == nil {
		context.intrinsicCache[key] = measured
	}
	return measured, err
}

func (context *layoutContext) intrinsicContentWidthsUncached(node *styledNode, availableWidth float64) (intrinsicWidths, error) {
	if node == nil {
		return intrinsicWidths{}, nil
	}
	if node.generated {
		if node.style.verticalLayout() || node.style.WritingMode() != writingModeHorizontalTB {
			_, metrics, err := context.measureVerticalTextRuns(inlineToken{text: node.generatedText, style: node.style})
			if err != nil {
				return intrinsicWidths{}, err
			}
			return intrinsicWidths{minimum: metrics.width, preferred: metrics.width}, nil
		}
		metrics, err := context.fonts.metrics(node.generatedText, node.style.FontSize(), node.style.FontWeight(), node.style.FontStyle(), node.style.FontFamily())
		if err != nil {
			return intrinsicWidths{}, err
		}
		return intrinsicWidths{minimum: metrics.width, preferred: metrics.width}, nil
	}
	if node.style.Display().Inside() == computed.DisplayInsideTable {
		return context.intrinsicTableContentWidths(node, availableWidth)
	}
	if node.style.Display().Inside() == computed.DisplayInsideFlex {
		return context.intrinsicFlexContentWidths(node, availableWidth)
	}
	if node.style.Display().Inside() == computed.DisplayInsideGrid {
		return context.intrinsicGridContentWidths(node, availableWidth)
	}

	var result intrinsicWidths
	var inlineGroup []*styledNode
	flushInline := func() error {
		if len(inlineGroup) == 0 {
			return nil
		}
		measured, err := context.intrinsicInlineWidths(inlineGroup, availableWidth)
		if err != nil {
			return err
		}
		result.minimum = math.Max(result.minimum, measured.minimum)
		result.preferred = math.Max(result.preferred, measured.preferred)
		inlineGroup = inlineGroup[:0]
		return nil
	}
	for _, child := range node.children {
		if child == nil || child.style.Display() == displayNone || isOutOfFlow(child.style.Position()) {
			continue
		}
		if !isBlockFlowChild(child) {
			inlineGroup = append(inlineGroup, child)
			continue
		}
		if err := flushInline(); err != nil {
			return intrinsicWidths{}, err
		}
		measured, err := context.intrinsicOuterWidths(child, availableWidth)
		if err != nil {
			return intrinsicWidths{}, err
		}
		result.minimum = math.Max(result.minimum, measured.minimum)
		result.preferred = math.Max(result.preferred, measured.preferred)
	}
	if err := flushInline(); err != nil {
		return intrinsicWidths{}, err
	}
	return result, nil
}

func (context *layoutContext) intrinsicOuterWidths(node *styledNode, availableWidth float64) (intrinsicWidths, error) {
	if node == nil || node.style.Display() == displayNone {
		return intrinsicWidths{}, nil
	}
	style := node.style
	padding := context.resolvePadding(style, availableWidth)
	border := context.resolveBorder(style, availableWidth)
	horizontalInsets := padding.Left + padding.Right + border.Left + border.Right
	margins := resolveLength(style.MarginLeft(), availableWidth, context.viewport, 0) +
		resolveLength(style.MarginRight(), availableWidth, context.viewport, 0)

	if node.node != nil && node.node.Type == dom.ElementNode && node.node.Data == "img" {
		width, _, ok := context.replacedDimensions(style, context.images[node.node], availableWidth, nil, horizontalInsets, 0)
		if !ok {
			return intrinsicWidths{}, nil
		}
		outer := math.Max(0, width+horizontalInsets+margins)
		return intrinsicWidths{minimum: outer, preferred: outer}, nil
	}
	// Percentage-dependent widths are cyclic during intrinsic measurement and
	// therefore behave as auto. In particular, table-cell descendants cannot
	// turn a percentage of their not-yet-known cell width into an intrinsic
	// contribution.
	if style.Width().Unit() != lengthAuto && !style.Width().DependsOnPercent() {
		content := resolveLength(style.Width(), availableWidth, context.viewport, 0)
		if style.BoxSizing() == boxSizingBorderBox {
			content = math.Max(0, content-horizontalInsets)
		}
		content = context.constrainIntrinsicWidth(style, content, availableWidth, horizontalInsets)
		outer := math.Max(0, content+horizontalInsets+margins)
		return intrinsicWidths{minimum: outer, preferred: outer}, nil
	}

	content, err := context.intrinsicContentWidths(node, availableWidth)
	if err != nil {
		return intrinsicWidths{}, err
	}
	content.minimum = context.constrainIntrinsicWidth(style, content.minimum, availableWidth, horizontalInsets)
	content.preferred = context.constrainIntrinsicWidth(style, content.preferred, availableWidth, horizontalInsets)
	if content.preferred < content.minimum {
		content.preferred = content.minimum
	}
	content.minimum += horizontalInsets + margins
	content.preferred += horizontalInsets + margins
	return content, nil
}

func (context *layoutContext) intrinsicInlineWidths(nodes []*styledNode, availableWidth float64) (intrinsicWidths, error) {
	builder := inlineTokenBuilder{images: context.images}
	for _, node := range nodes {
		builder.add(node, 1)
	}
	var result intrinsicWidths
	lineWidth := 0.0
	segmentWidth := 0.0
	finishLine := func() {
		result.preferred = math.Max(result.preferred, lineWidth)
		result.minimum = math.Max(result.minimum, segmentWidth)
		lineWidth = 0
		segmentWidth = 0
	}
	for _, token := range builder.tokens {
		if token.lineBreak {
			finishLine()
			continue
		}
		measured := intrinsicWidths{}
		switch {
		case token.atomic != nil:
			var err error
			measured, err = context.intrinsicOuterWidths(token.atomic, availableWidth)
			if err != nil {
				return intrinsicWidths{}, err
			}
		case token.replaced:
			width, _, ok := context.replacedDimensions(token.style, token.image, availableWidth, nil, 0, 0)
			if !ok {
				continue
			}
			measured = intrinsicWidths{minimum: width, preferred: width}
		default:
			if token.style.verticalLayout() || token.style.WritingMode() != writingModeHorizontalTB {
				_, metrics, err := context.measureVerticalTextRuns(token)
				if err != nil {
					return intrinsicWidths{}, err
				}
				measured = intrinsicWidths{minimum: metrics.width, preferred: metrics.width}
			} else {
				metrics, err := context.fonts.metrics(token.text, token.style.FontSize(), token.style.FontWeight(), token.style.FontStyle(), token.style.FontFamily())
				if err != nil {
					return intrinsicWidths{}, err
				}
				measured = intrinsicWidths{minimum: metrics.width, preferred: metrics.width}
			}
		}
		spaceWidth := 0.0
		if token.leadingSpace && lineWidth != 0 {
			metrics, err := context.fonts.metrics(" ", token.style.FontSize(), token.style.FontWeight(), token.style.FontStyle(), token.style.FontFamily())
			if err != nil {
				return intrinsicWidths{}, err
			}
			spaceWidth = metrics.width
		}
		lineWidth += spaceWidth + measured.preferred
		if token.wrapBefore && segmentWidth != 0 {
			result.minimum = math.Max(result.minimum, segmentWidth)
			segmentWidth = measured.minimum
		} else {
			segmentWidth += spaceWidth + measured.minimum
		}
	}
	finishLine()
	return result, nil
}

func (context *layoutContext) intrinsicFlexContentWidths(node *styledNode, availableWidth float64) (intrinsicWidths, error) {
	var result intrinsicWidths
	count := 0
	row := node.style.FlexDirection() == computed.FlexDirectionRow || node.style.FlexDirection() == computed.FlexDirectionRowReverse
	for _, child := range node.children {
		if child == nil || child.generated || child.style.Display() == displayNone || isOutOfFlow(child.style.Position()) {
			continue
		}
		measured, err := context.intrinsicOuterWidths(child, availableWidth)
		if err != nil {
			return intrinsicWidths{}, err
		}
		if row {
			result.minimum = math.Max(result.minimum, measured.minimum)
			result.preferred += measured.preferred
		} else {
			result.minimum = math.Max(result.minimum, measured.minimum)
			result.preferred = math.Max(result.preferred, measured.preferred)
		}
		count++
	}
	if row && count > 1 {
		gap := math.Max(0, resolveLength(node.style.ColumnGap(), availableWidth, context.viewport, 0))
		result.preferred += float64(count-1) * gap
	}
	if result.preferred < result.minimum {
		result.preferred = result.minimum
	}
	return result, nil
}

func translateInlineLayout(layout *inlineLayout, deltaX, deltaY float64) {
	if layout == nil || (deltaX == 0 && deltaY == 0) {
		return
	}
	for index := range layout.fragments {
		translateInlineFragment(&layout.fragments[index], deltaX, deltaY)
	}
	for index := range layout.flow {
		if layout.flow[index].box != nil {
			translateLayoutBox(layout.flow[index].box, deltaX, deltaY)
		} else {
			translateInlineFragment(&layout.flow[index].fragment, deltaX, deltaY)
		}
	}
}

func lastBoxBaseline(box *Box) (float64, bool) {
	if box == nil {
		return 0, false
	}
	if box.tableWrapper && box.tableRoot != nil {
		return lastBoxBaseline(box.tableRoot)
	}
	for index := len(box.flow) - 1; index >= 0; index-- {
		item := box.flow[index]
		if item.box != nil {
			if baseline, ok := lastBoxBaseline(item.box); ok {
				return baseline, true
			}
			continue
		}
		switch item.fragment.Kind {
		case TextFragmentKind:
			return item.fragment.Text.BaselineY, true
		case ImageFragmentKind:
			return item.fragment.Image.Bounds.Y + item.fragment.Image.Bounds.Height, true
		}
	}
	for index := len(box.Children) - 1; index >= 0; index-- {
		if box.Children[index].positioned {
			continue
		}
		if baseline, ok := lastBoxBaseline(box.Children[index]); ok {
			return baseline, true
		}
	}
	return 0, false
}

func (context *layoutContext) replacedDimensions(style computedStyle, decoded image.Image, availableWidth float64, containingHeight *float64, horizontalInsets, verticalInsets float64) (float64, float64, bool) {
	naturalWidth := 0.0
	naturalHeight := 0.0
	hasNaturalSize := false
	if decoded != nil {
		bounds := decoded.Bounds()
		naturalWidth = float64(bounds.Dx())
		naturalHeight = float64(bounds.Dy())
		hasNaturalSize = naturalWidth > 0 && naturalHeight > 0
	}

	widthSpecified := style.Width().Unit() != lengthAuto
	heightSpecified := resolvableHeight(style.Height(), containingHeight)
	if !hasNaturalSize && !(widthSpecified && heightSpecified) {
		return 0, 0, false
	}
	width := naturalWidth
	height := naturalHeight
	if widthSpecified {
		width = resolveLength(style.Width(), availableWidth, context.viewport, naturalWidth)
		if style.BoxSizing() == boxSizingBorderBox {
			width = math.Max(0, width-horizontalInsets)
		}
	}
	if heightSpecified {
		percentageBase := 0.0
		if containingHeight != nil {
			percentageBase = *containingHeight
		}
		height = resolveLength(style.Height(), percentageBase, context.viewport, naturalHeight)
		if style.BoxSizing() == boxSizingBorderBox {
			height = math.Max(0, height-verticalInsets)
		}
	}
	switch {
	case widthSpecified && !heightSpecified:
		height = naturalHeight * width / naturalWidth
	case heightSpecified && !widthSpecified:
		width = naturalWidth * height / naturalHeight
	}
	constrainedWidth := context.constrainWidth(style, width, availableWidth, horizontalInsets)
	if constrainedWidth != width && !heightSpecified && width > 0 {
		height *= constrainedWidth / width
	}
	width = constrainedWidth
	height = context.constrainHeight(style, height, verticalInsets, containingHeight)
	if width < 0 || height < 0 || !isFinite(width) || !isFinite(height) {
		return 0, 0, false
	}
	return width, height, true
}

func (context *layoutContext) resolvePadding(style computedStyle, availableWidth float64) Edges {
	return Edges{
		Top:    math.Max(0, resolveLength(style.PaddingTop(), availableWidth, context.viewport, 0)),
		Right:  math.Max(0, resolveLength(style.PaddingRight(), availableWidth, context.viewport, 0)),
		Bottom: math.Max(0, resolveLength(style.PaddingBottom(), availableWidth, context.viewport, 0)),
		Left:   math.Max(0, resolveLength(style.PaddingLeft(), availableWidth, context.viewport, 0)),
	}
}

func (context *layoutContext) resolveBorder(style computedStyle, availableWidth float64) Edges {
	return Edges{
		Top:    context.resolveBorderWidth(style.BorderTop(), availableWidth),
		Right:  context.resolveBorderWidth(style.BorderRight(), availableWidth),
		Bottom: context.resolveBorderWidth(style.BorderBottom(), availableWidth),
		Left:   context.resolveBorderWidth(style.BorderLeft(), availableWidth),
	}
}

func (context *layoutContext) resolveBorderWidth(side borderSide, availableWidth float64) float64 {
	if side.Style() == borderStyleNone || side.Style() == borderStyleHidden {
		return 0
	}
	return math.Max(0, resolveLength(side.Width(), availableWidth, context.viewport, 0))
}

func (context *layoutContext) constrainWidth(style computedStyle, width, availableWidth, horizontalInsets float64) float64 {
	maximum := math.Inf(1)
	if style.MaxWidth().Unit() != lengthAuto {
		maximum = math.Max(0, resolveLength(style.MaxWidth(), availableWidth, context.viewport, width))
		if style.BoxSizing() == boxSizingBorderBox {
			maximum = math.Max(0, maximum-horizontalInsets)
		}
	}
	minimum := 0.0
	if style.MinWidth().Unit() != lengthAuto {
		minimum = math.Max(0, resolveLength(style.MinWidth(), availableWidth, context.viewport, 0))
		if style.BoxSizing() == boxSizingBorderBox {
			minimum = math.Max(0, minimum-horizontalInsets)
		}
	}
	return math.Max(minimum, math.Min(width, maximum))
}

func (context *layoutContext) constrainIntrinsicWidth(style computedStyle, width, availableWidth, horizontalInsets float64) float64 {
	maximum := math.Inf(1)
	if style.MaxWidth().Unit() != lengthAuto && !style.MaxWidth().DependsOnPercent() {
		maximum = math.Max(0, resolveLength(style.MaxWidth(), availableWidth, context.viewport, width))
		if style.BoxSizing() == boxSizingBorderBox {
			maximum = math.Max(0, maximum-horizontalInsets)
		}
	}
	minimum := 0.0
	if style.MinWidth().Unit() != lengthAuto && !style.MinWidth().DependsOnPercent() {
		minimum = math.Max(0, resolveLength(style.MinWidth(), availableWidth, context.viewport, 0))
		if style.BoxSizing() == boxSizingBorderBox {
			minimum = math.Max(0, minimum-horizontalInsets)
		}
	}
	return math.Max(minimum, math.Min(width, maximum))
}

func (context *layoutContext) resolveSpecifiedContentHeight(style computedStyle, containingHeight *float64, verticalInsets float64) (float64, bool) {
	value := style.Height()
	if !resolvableHeight(value, containingHeight) {
		return 0, false
	}
	percentageBase := 0.0
	if containingHeight != nil {
		percentageBase = *containingHeight
	}
	resolved, ok := value.Resolve(percentageBase, float64(context.viewport.Width), float64(context.viewport.Height))
	if !ok {
		return 0, false
	}
	resolved = math.Max(0, resolved)
	if style.BoxSizing() == boxSizingBorderBox {
		resolved = math.Max(0, resolved-verticalInsets)
	}
	return context.constrainHeight(style, resolved, verticalInsets, containingHeight), true
}

func resolvableHeight(value length, containingHeight *float64) bool {
	return value.Unit() != lengthAuto && (!value.DependsOnPercent() || containingHeight != nil)
}

func (context *layoutContext) constrainHeight(style computedStyle, height, verticalInsets float64, containingHeight *float64) float64 {
	percentageBase := 0.0
	if containingHeight != nil {
		percentageBase = *containingHeight
	}
	maximum := math.Inf(1)
	if resolvableHeight(style.MaxHeight(), containingHeight) {
		maximum = math.Max(0, resolveLength(style.MaxHeight(), percentageBase, context.viewport, height))
		if style.BoxSizing() == boxSizingBorderBox {
			maximum = math.Max(0, maximum-verticalInsets)
		}
	}
	minimum := 0.0
	if resolvableHeight(style.MinHeight(), containingHeight) {
		minimum = math.Max(0, resolveLength(style.MinHeight(), percentageBase, context.viewport, 0))
		if style.BoxSizing() == boxSizingBorderBox {
			minimum = math.Max(0, minimum-verticalInsets)
		}
	}
	return math.Max(minimum, math.Min(height, maximum))
}

type inlineTokenBuilder struct {
	tokens       []inlineToken
	images       map[*dom.Node]image.Image
	pendingSpace bool
	hasContent   bool
}

func (builder *inlineTokenBuilder) add(node *styledNode, inheritedOpacity float64) {
	builder.addAligned(node, inheritedOpacity, verticalAlignment{})
}

func (builder *inlineTokenBuilder) addAligned(node *styledNode, inheritedOpacity float64, inheritedAlignment verticalAlignment) {
	if node == nil || node.style.Display() == displayNone {
		return
	}
	alignment := inheritedAlignment
	if own := node.style.VerticalAlignment(); own.Mode() != verticalAlignBaseline || alignment.Mode() == verticalAlignBaseline {
		alignment = own
	}
	if node.generated {
		// The synthetic text is the pseudo box's content, not a second styled
		// box. Its parent's opacity has already been flattened for inline
		// pseudos or will group the containing atomic/block pseudo box.
		builder.addText(node.generatedText, node.node, node.pseudo, node.style, inheritedOpacity, alignment)
		return
	}
	opacity := clamp(inheritedOpacity*node.style.Opacity(), 0, 1)
	if isAtomicInline(node.style.Display()) {
		leadingSpace := builder.pendingSpace && builder.hasContent
		builder.tokens = append(builder.tokens, inlineToken{
			node:          node.node,
			pseudo:        node.pseudo,
			style:         node.style,
			atomic:        node,
			opacity:       opacity,
			leadingSpace:  leadingSpace,
			wrapBefore:    whiteSpaceAllowsSoftWrap(node.style.WhiteSpace()) && builder.hasContent,
			verticalAlign: alignment,
		})
		builder.pendingSpace = false
		builder.hasContent = true
		return
	}
	if node.node.Type == dom.ElementNode && node.node.Data == "br" {
		builder.tokens = append(builder.tokens, inlineToken{node: node.node, style: node.style, lineBreak: true})
		builder.pendingSpace = false
		builder.hasContent = false
		return
	}
	if node.node.Type == dom.ElementNode && node.node.Data == "img" {
		decoded := builder.images[node.node]
		if decoded != nil || hasExplicitImageDimensions(node.style) {
			leadingSpace := builder.pendingSpace && builder.hasContent
			builder.tokens = append(builder.tokens, inlineToken{
				node:          node.node,
				style:         node.style,
				image:         decoded,
				replaced:      true,
				opacity:       opacity,
				leadingSpace:  leadingSpace,
				wrapBefore:    whiteSpaceAllowsSoftWrap(node.style.WhiteSpace()) && (leadingSpace || builder.hasContent),
				verticalAlign: alignment,
			})
			builder.pendingSpace = false
			builder.hasContent = true
		}
		return
	}
	if node.node.Type == dom.TextNode {
		builder.addText(node.node.Data, node.node, computed.PseudoElementNone, node.style, opacity, alignment)
		return
	}
	for _, child := range node.children {
		if !isBlockLevel(child.style.Display()) {
			builder.addAligned(child, opacity, alignment)
		}
	}
}

func (builder *inlineTokenBuilder) addText(source string, node *dom.Node, pseudo computed.PseudoElement, style computedStyle, opacity float64, alignment verticalAlignment) {
	switch style.WhiteSpace() {
	case whiteSpacePre:
		builder.addPreservedText(source, node, pseudo, style, opacity, alignment, false)
	case whiteSpacePreWrap, whiteSpaceBreak:
		builder.addPreservedText(source, node, pseudo, style, opacity, alignment, true)
	case whiteSpacePreLine:
		builder.addCollapsedText(source, node, pseudo, style, opacity, alignment, true, true)
	case whiteSpaceNoWrap:
		builder.addCollapsedText(source, node, pseudo, style, opacity, alignment, false, false)
	default:
		builder.addCollapsedText(source, node, pseudo, style, opacity, alignment, true, false)
	}
}

func (builder *inlineTokenBuilder) addCollapsedText(source string, node *dom.Node, pseudo computed.PseudoElement, style computedStyle, opacity float64, alignment verticalAlignment, wrap, preserveBreaks bool) {
	if preserveBreaks {
		parts := strings.Split(normalizeSegmentBreaks(source), "\n")
		for index, part := range parts {
			builder.addCollapsedText(part, node, pseudo, style, opacity, alignment, wrap, false)
			if index != len(parts)-1 {
				builder.tokens = append(builder.tokens, inlineToken{node: node, pseudo: pseudo, style: style, lineBreak: true})
				builder.pendingSpace = false
				builder.hasContent = false
			}
		}
		return
	}
	start := -1
	flushWord := func(end int) {
		if start < 0 {
			return
		}
		leadingSpace := builder.pendingSpace && builder.hasContent
		builder.tokens = append(builder.tokens, inlineToken{
			text:          source[start:end],
			node:          node,
			pseudo:        pseudo,
			style:         style,
			opacity:       opacity,
			verticalAlign: alignment,
			leadingSpace:  leadingSpace,
			wrapBefore:    wrap && leadingSpace,
		})
		builder.hasContent = true
		builder.pendingSpace = false
		start = -1
	}
	for index, runeValue := range source {
		if cssCollapsibleSpace(runeValue) {
			flushWord(index)
			if builder.hasContent {
				builder.pendingSpace = true
			}
			continue
		}
		if start < 0 {
			start = index
		}
	}
	flushWord(len(source))
}

func (builder *inlineTokenBuilder) addPreservedText(source string, node *dom.Node, pseudo computed.PseudoElement, style computedStyle, opacity float64, alignment verticalAlignment, wrap bool) {
	source = strings.ReplaceAll(normalizeSegmentBreaks(source), "\t", "        ")
	parts := strings.Split(source, "\n")
	for partIndex, part := range parts {
		if part != "" {
			if !wrap {
				builder.tokens = append(builder.tokens, inlineToken{text: part, node: node, pseudo: pseudo, style: style, opacity: opacity, verticalAlign: alignment})
			} else {
				start := 0
				wrapBefore := false
				for index, runeValue := range part {
					if runeValue != ' ' {
						continue
					}
					end := index + 1
					builder.tokens = append(builder.tokens, inlineToken{text: part[start:end], node: node, pseudo: pseudo, style: style, opacity: opacity, wrapBefore: wrapBefore, verticalAlign: alignment})
					start = end
					wrapBefore = true
				}
				if start < len(part) {
					builder.tokens = append(builder.tokens, inlineToken{text: part[start:], node: node, pseudo: pseudo, style: style, opacity: opacity, wrapBefore: wrapBefore, verticalAlign: alignment})
				}
			}
			builder.hasContent = true
		}
		builder.pendingSpace = false
		if partIndex != len(parts)-1 {
			builder.tokens = append(builder.tokens, inlineToken{node: node, pseudo: pseudo, style: style, lineBreak: true})
			builder.hasContent = false
		}
	}
}

func normalizeSegmentBreaks(source string) string {
	source = strings.ReplaceAll(source, "\r\n", "\n")
	source = strings.ReplaceAll(source, "\r", "\n")
	return strings.ReplaceAll(source, "\f", "\n")
}

func cssCollapsibleSpace(value rune) bool {
	return value == ' ' || value == '\t' || value == '\n' || value == '\r' || value == '\f'
}

func whiteSpaceAllowsSoftWrap(mode whiteSpaceMode) bool {
	return mode == whiteSpaceNormal || mode == whiteSpacePreWrap || mode == whiteSpacePreLine || mode == whiteSpaceBreak
}

func hasExplicitImageDimensions(style computedStyle) bool {
	return style.Width().Unit() != lengthAuto && style.Height().Unit() != lengthAuto
}

func findStyledElement(root *styledNode, name string) *styledNode {
	if root == nil {
		return nil
	}
	if root.node != nil && root.node.Type == dom.ElementNode && root.node.Data == name {
		return root
	}
	for _, child := range root.children {
		if found := findStyledElement(child, name); found != nil {
			return found
		}
	}
	return nil
}

func directStyledElement(parent *styledNode, name string) *styledNode {
	for _, child := range parent.children {
		if child.node.Type == dom.ElementNode && child.node.Data == name {
			return child
		}
	}
	return nil
}

func validateDocument(document *dom.Node) error {
	if document == nil {
		return fmt.Errorf("render: nil document")
	}
	if document.Type != dom.DocumentNode {
		return fmt.Errorf("render: root node must be a document")
	}
	return nil
}
