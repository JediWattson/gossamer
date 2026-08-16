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
	viewport Viewport
	fonts    *fontBook
	styles   map[*dom.Node]computedStyle
	images   map[*dom.Node]image.Image
}

type inlineToken struct {
	text         string
	node         *dom.Node
	pseudo       computed.PseudoElement
	style        computedStyle
	image        image.Image
	replaced     bool
	opacity      float64
	leadingSpace bool
	wrapBefore   bool
	lineBreak    bool
}

type inlinePiece struct {
	text     string
	node     *dom.Node
	pseudo   computed.PseudoElement
	style    computedStyle
	image    image.Image
	replaced bool
	opacity  float64
	x        float64
	width    float64
	height   float64
	metrics  textMetrics
}

func layoutDocument(root *styledNode, viewport Viewport, images map[*dom.Node]image.Image, fonts *fontBook) (*Box, map[*dom.Node]computedStyle, error) {
	context := &layoutContext{
		viewport: viewport,
		fonts:    fonts,
		styles:   make(map[*dom.Node]computedStyle),
		images:   images,
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
	bodyY := resolveLength(body.style.MarginTop(), float64(viewport.Width), viewport, 0)
	bodyBox, err := context.layoutBlock(body, 0, bodyY, float64(viewport.Width))
	if err != nil {
		return nil, nil, err
	}
	htmlBox.Children = append(htmlBox.Children, bodyBox)
	bodyBottom := bodyBox.Bounds.Y + bodyBox.Bounds.Height + resolveLength(body.style.MarginBottom(), float64(viewport.Width), viewport, 0)
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

func (context *layoutContext) layoutBlock(node *styledNode, containingX, contentY, availableWidth float64) (*Box, error) {
	return context.layoutBlockSized(node, containingX, contentY, availableWidth, nil)
}

func (context *layoutContext) layoutBlockSized(node *styledNode, containingX, contentY, availableWidth float64, forcedContentWidth *float64) (*Box, error) {
	style := node.style
	leftAuto := style.MarginLeft().Unit() == lengthAuto
	rightAuto := style.MarginRight().Unit() == lengthAuto
	left := resolveLength(style.MarginLeft(), availableWidth, context.viewport, 0)
	right := resolveLength(style.MarginRight(), availableWidth, context.viewport, 0)
	padding := context.resolvePadding(style, availableWidth)
	border := context.resolveBorder(style, availableWidth)
	horizontalInsets := padding.Left + padding.Right + border.Left + border.Right
	verticalInsets := padding.Top + padding.Bottom + border.Top + border.Bottom
	if node.node.Type == dom.ElementNode && node.node.Data == "img" {
		decoded := context.images[node.node]
		imageWidth, imageHeight, ok := context.replacedDimensions(style, decoded, availableWidth, horizontalInsets, verticalInsets)
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
			Node:          node.node,
			Pseudo:        node.pseudo,
			Bounds:        bounds,
			ContentBounds: contentBounds,
			Padding:       padding,
			Border:        border,
			Fragments:     []InlineFragment{fragment},
			flow:          []flowItem{{fragment: fragment}},
			style:         node.style,
			hasStyle:      true,
		}
		return context.finalizeBlock(node, box, availableWidth)
	}

	width := availableWidth - left - right - padding.Left - padding.Right - border.Left - border.Right
	if forcedContentWidth != nil {
		width = *forcedContentWidth
	} else if style.Width().Unit() != lengthAuto {
		width = resolveLength(style.Width(), availableWidth, context.viewport, availableWidth)
		if style.BoxSizing() == boxSizingBorderBox {
			width -= horizontalInsets
		}
	}
	width = context.constrainWidth(style, math.Max(0, width), availableWidth, horizontalInsets)
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
		Node:     node.node,
		Pseudo:   node.pseudo,
		Bounds:   Rect{X: containingX + left, Y: contentY, Width: outerWidth},
		Padding:  padding,
		Border:   border,
		style:    node.style,
		hasStyle: true,
	}
	box.ContentBounds = Rect{
		X:     box.Bounds.X + border.Left + padding.Left,
		Y:     box.Bounds.Y + border.Top + padding.Top,
		Width: width,
	}
	if style.Display() == displayFlex {
		contentHeight, err := context.layoutFlexContainer(node, box, width)
		if err != nil {
			return nil, err
		}
		box.ContentBounds.Height = contentHeight
		box.Bounds.Height = border.Top + padding.Top + contentHeight + padding.Bottom + border.Bottom
		return context.finalizeBlock(node, box, availableWidth)
	}
	cursorY := box.ContentBounds.Y
	previousBottomMargin := 0.0
	hasContent := false

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
		if isBlockLevel(child.style.Display()) {
			topMargin := resolveLength(child.style.MarginTop(), width, context.viewport, 0)
			gap := math.Max(previousBottomMargin, topMargin)
			// A first block child's top margin collapses through an auto-height
			// parent with no border or padding in this initial box model.
			if !hasContent && padding.Top == 0 && border.Top == 0 {
				gap = 0
			}
			childBox, err := context.layoutBlock(child, box.ContentBounds.X, cursorY+gap, width)
			if err != nil {
				return nil, err
			}
			box.Children = append(box.Children, childBox)
			box.flow = append(box.flow, flowItem{box: childBox})
			cursorY = childBox.Bounds.Y + childBox.Bounds.Height
			previousBottomMargin = resolveLength(child.style.MarginBottom(), width, context.viewport, 0)
			hasContent = true
			index++
			continue
		}

		end := index
		for end < len(node.children) &&
			!isBlockLevel(node.children[end].style.Display()) &&
			!isOutOfFlow(node.children[end].style.Position()) {
			end++
		}
		fragments, height, err := context.layoutInline(node.children[index:end], box.ContentBounds.X, cursorY, width, node.style.TextAlignment())
		if err != nil {
			return nil, err
		}
		if len(fragments) != 0 {
			if hasContent {
				cursorY += previousBottomMargin
				for fragmentIndex := range fragments {
					fragment := &fragments[fragmentIndex]
					switch fragment.Kind {
					case TextFragmentKind:
						fragment.Text.BaselineY += previousBottomMargin
					case ImageFragmentKind:
						fragment.Image.Bounds.Y += previousBottomMargin
					}
				}
			}
			box.Fragments = append(box.Fragments, fragments...)
			for _, fragment := range fragments {
				box.flow = append(box.flow, flowItem{fragment: fragment})
				if fragment.Kind == TextFragmentKind {
					box.Text = append(box.Text, fragment.Text)
				}
			}
			cursorY += height
			previousBottomMargin = 0
			hasContent = true
		}
		index = end
	}

	if hasContent && (padding.Bottom > 0 || border.Bottom > 0) {
		cursorY += previousBottomMargin
	}
	contentHeight := math.Max(0, cursorY-box.ContentBounds.Y)
	// Percentage heights remain auto until the containing-block height is
	// definite. This layout slice does not yet pass definite heights downward.
	if style.Height().Unit() != lengthAuto && !style.Height().DependsOnPercent() {
		contentHeight = math.Max(0, resolveLength(style.Height(), 0, context.viewport, contentHeight))
		if style.BoxSizing() == boxSizingBorderBox {
			contentHeight = math.Max(0, contentHeight-verticalInsets)
		}
	}
	contentHeight = context.constrainHeight(style, contentHeight, verticalInsets)
	box.ContentBounds.Height = contentHeight
	box.Bounds.Height = border.Top + padding.Top + box.ContentBounds.Height + padding.Bottom + border.Bottom
	return context.finalizeBlock(node, box, availableWidth)
}

type flexLayoutItem struct {
	node          *styledNode
	box           *Box
	originalIndex int
	mainSize      float64
	outerMain     float64
	marginBefore  float64
	marginAfter   float64
}

func (context *layoutContext) layoutFlexContainer(node *styledNode, box *Box, contentWidth float64) (float64, error) {
	items := make([]flexLayoutItem, 0, len(node.children))
	for index, child := range node.children {
		if child.node == nil || child.node.Type != dom.ElementNode ||
			child.style.Display() == displayNone || isOutOfFlow(child.style.Position()) {
			continue
		}
		items = append(items, flexLayoutItem{node: child, originalIndex: index})
	}
	sort.SliceStable(items, func(left, right int) bool {
		return items[left].node.style.Order() < items[right].node.style.Order()
	})
	direction := node.style.FlexDirection()
	if direction == computed.FlexDirectionRowReverse || direction == computed.FlexDirectionColumnReverse {
		for left, right := 0, len(items)-1; left < right; left, right = left+1, right-1 {
			items[left], items[right] = items[right], items[left]
		}
	}
	if direction == computed.FlexDirectionColumn || direction == computed.FlexDirectionColumnReverse {
		return context.layoutFlexColumn(node, box, contentWidth, items)
	}
	return context.layoutFlexRow(node, box, contentWidth, items)
}

func (context *layoutContext) layoutFlexRow(node *styledNode, box *Box, contentWidth float64, items []flexLayoutItem) (float64, error) {
	if len(items) == 0 {
		return context.definiteFlexHeight(node.style, 0), nil
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
		basis := 0.0
		switch {
		case style.FlexBasis().Unit() != lengthAuto:
			basis = resolveLength(style.FlexBasis(), contentWidth, context.viewport, 0)
		case style.Width().Unit() != lengthAuto:
			basis = resolveLength(style.Width(), contentWidth, context.viewport, 0)
		}
		item.mainSize = math.Max(0, basis)
		decoration := padding.Left + padding.Right + border.Left + border.Right + item.marginBefore + item.marginAfter
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
	start, extraGap := justifyFlexSpace(node.style.JustifyContent(), math.Max(0, free), len(items))
	cursorX := box.ContentBounds.X + start
	maxCross := 0.0
	for index := range items {
		item := &items[index]
		marginTop := resolveLength(item.node.style.MarginTop(), contentWidth, context.viewport, 0)
		marginBottom := resolveLength(item.node.style.MarginBottom(), contentWidth, context.viewport, 0)
		childBox, err := context.layoutBlockSized(item.node, cursorX, box.ContentBounds.Y+marginTop, item.outerMain, &item.mainSize)
		if err != nil {
			return 0, err
		}
		item.box = childBox
		cross := marginTop + childBox.Bounds.Height + marginBottom
		maxCross = math.Max(maxCross, cross)
		cursorX += item.outerMain + gap + extraGap
	}
	containerHeight := context.definiteFlexHeight(node.style, maxCross)
	for index := range items {
		item := &items[index]
		marginTop := resolveLength(item.node.style.MarginTop(), contentWidth, context.viewport, 0)
		marginBottom := resolveLength(item.node.style.MarginBottom(), contentWidth, context.viewport, 0)
		availableCross := math.Max(0, containerHeight-marginTop-marginBottom)
		if node.style.AlignItems() == computed.AlignStretch && item.node.style.Height().Unit() == lengthAuto {
			setBoxOuterHeight(item.box, availableCross)
		}
		offset := alignFlexOffset(node.style.AlignItems(), math.Max(0, availableCross-item.box.Bounds.Height))
		translateLayoutBox(item.box, 0, box.ContentBounds.Y+marginTop+offset-item.box.Bounds.Y)
		box.Children = append(box.Children, item.box)
		box.flow = append(box.flow, flowItem{box: item.box})
	}
	return containerHeight, nil
}

func (context *layoutContext) layoutFlexColumn(node *styledNode, box *Box, contentWidth float64, items []flexLayoutItem) (float64, error) {
	gap := math.Max(0, resolveLength(node.style.RowGap(), contentWidth, context.viewport, 0))
	totalMain := gap * math.Max(0, float64(len(items)-1))
	for index := range items {
		item := &items[index]
		childBox, err := context.layoutBlock(item.node, box.ContentBounds.X, box.ContentBounds.Y, contentWidth)
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
	containerHeight := context.definiteFlexHeight(node.style, totalMain)
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
	start, extraGap := justifyFlexSpace(node.style.JustifyContent(), math.Max(0, free), len(items))
	cursorY := box.ContentBounds.Y + start
	for index := range items {
		item := &items[index]
		marginTop := resolveLength(item.node.style.MarginTop(), contentWidth, context.viewport, 0)
		marginBottom := resolveLength(item.node.style.MarginBottom(), contentWidth, context.viewport, 0)
		marginLeft := resolveLength(item.node.style.MarginLeft(), contentWidth, context.viewport, 0)
		marginRight := resolveLength(item.node.style.MarginRight(), contentWidth, context.viewport, 0)
		setBoxOuterHeight(item.box, item.mainSize)
		availableCross := math.Max(0, contentWidth-marginLeft-marginRight)
		xOffset := alignFlexOffset(node.style.AlignItems(), math.Max(0, availableCross-item.box.Bounds.Width))
		translateLayoutBox(item.box, box.ContentBounds.X+marginLeft+xOffset-item.box.Bounds.X, cursorY+marginTop-item.box.Bounds.Y)
		box.Children = append(box.Children, item.box)
		box.flow = append(box.flow, flowItem{box: item.box})
		cursorY += marginTop + item.mainSize + marginBottom + gap + extraGap
	}
	return containerHeight, nil
}

func (context *layoutContext) definiteFlexHeight(style computedStyle, fallback float64) float64 {
	if style.Height().Unit() != lengthAuto && !style.Height().DependsOnPercent() {
		return math.Max(0, resolveLength(style.Height(), 0, context.viewport, fallback))
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

func justifyFlexSpace(justify computed.JustifyContent, free float64, count int) (start, extraGap float64) {
	if count == 0 || free <= 0 {
		return 0, 0
	}
	switch justify {
	case computed.JustifyFlexEnd:
		return free, 0
	case computed.JustifyCenter:
		return free / 2, 0
	case computed.JustifySpaceBetween:
		if count > 1 {
			return 0, free / float64(count-1)
		}
	case computed.JustifySpaceAround:
		space := free / float64(count)
		return space / 2, space
	case computed.JustifySpaceEvenly:
		space := free / float64(count+1)
		return space, space
	}
	return 0, 0
}

func alignFlexOffset(align computed.AlignItems, free float64) float64 {
	switch align {
	case computed.AlignFlexEnd:
		return free
	case computed.AlignCenterItems:
		return free / 2
	default:
		return 0
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
	for index := range box.Fragments {
		translateInlineFragment(&box.Fragments[index], deltaX, deltaY)
	}
	for index := range box.Text {
		box.Text[index].X += deltaX
		box.Text[index].BaselineY += deltaY
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
		fragment.Text.X += deltaX
		fragment.Text.BaselineY += deltaY
	case ImageFragmentKind:
		fragment.Image.Bounds.X += deltaX
		fragment.Image.Bounds.Y += deltaY
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
	if box.Node != nil {
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
			positioned, err := context.layoutBlock(node, usedContainingBlock.X, staticY, usedContainingBlock.Width)
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
	baseline, hasBaseline := firstBoxBaseline(box)
	if !hasBaseline {
		lineHeight := math.Max(node.style.LineHeight().Pixels(node.style.FontSize()), metrics.ascent+metrics.descent)
		leading := math.Max(0, lineHeight-metrics.ascent-metrics.descent)
		baseline = box.ContentBounds.Y + leading/2 + metrics.ascent
		if box.ContentBounds.Height < lineHeight && node.style.Height().Unit() == lengthAuto {
			box.ContentBounds.Height = lineHeight
			box.Bounds.Height = box.Border.Top + box.Padding.Top + lineHeight + box.Padding.Bottom + box.Border.Bottom
		}
	}
	marker := TextFragment{
		Node:       node.node,
		Pseudo:     node.pseudo,
		Text:       markerText,
		X:          box.Bounds.X - node.style.FontSize()*.5 - metrics.width,
		BaselineY:  baseline,
		Width:      metrics.width,
		Height:     node.style.LineHeight().Pixels(node.style.FontSize()),
		FontSize:   node.style.FontSize(),
		FontFamily: node.style.FontFamily(),
		FontWeight: node.style.FontWeight(),
		FontStyle:  node.style.FontStyle(),
		Color:      node.style.Color(),
		Visible:    node.style.Visibility() == visibilityVisible,
		Underline:  node.style.Underline(),
	}
	fragment := InlineFragment{Kind: TextFragmentKind, Text: marker}
	box.flow = append([]flowItem{{fragment: fragment}}, box.flow...)
	return nil
}

func firstBoxBaseline(box *Box) (float64, bool) {
	if box == nil {
		return 0, false
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

func (context *layoutContext) layoutInline(nodes []*styledNode, x, y, width float64, alignment textAlignment) ([]InlineFragment, float64, error) {
	builder := inlineTokenBuilder{images: context.images}
	for _, node := range nodes {
		builder.add(node, 1)
	}
	if len(builder.tokens) == 0 {
		return nil, 0, nil
	}

	var fragments []InlineFragment
	var line []inlinePiece
	lineWidth := 0.0
	cursorY := y
	flushLine := func() {
		if len(line) == 0 {
			return
		}
		lineAscent := 0.0
		lineDescent := 0.0
		lineHeight := 0.0
		for _, piece := range line {
			lineAscent = math.Max(lineAscent, piece.metrics.ascent)
			lineDescent = math.Max(lineDescent, piece.metrics.descent)
			lineHeight = math.Max(lineHeight, piece.style.LineHeight().Pixels(piece.style.FontSize()))
		}
		lineHeight = math.Max(lineHeight, lineAscent+lineDescent)
		leading := math.Max(0, lineHeight-lineAscent-lineDescent)
		baseline := cursorY + leading/2 + lineAscent
		lineOffset := 0.0
		switch alignment {
		case alignCenter:
			lineOffset = (width - lineWidth) / 2
		case alignRight, alignEnd:
			lineOffset = width - lineWidth
		case alignLeft, alignStart, alignJustify:
			// The current formatter has no bidi or justification pass. Preserve
			// its existing left-aligned used behavior without collapsing the
			// distinct computed values.
		}
		for _, piece := range line {
			if piece.replaced {
				fragments = append(fragments, InlineFragment{
					Kind: ImageFragmentKind,
					Image: ImageFragment{
						Node:    piece.node,
						Image:   piece.image,
						Bounds:  Rect{X: x + lineOffset + piece.x, Y: baseline - piece.height, Width: piece.width, Height: piece.height},
						Opacity: piece.opacity,
					},
				})
				continue
			}
			textColor := piece.style.Color()
			textColor.A = uint8(math.Round(float64(textColor.A) * clamp(piece.opacity, 0, 1)))
			text := TextFragment{
				Node:       piece.node,
				Pseudo:     piece.pseudo,
				Text:       piece.text,
				X:          x + lineOffset + piece.x,
				BaselineY:  baseline,
				Width:      piece.width,
				Height:     lineHeight,
				FontSize:   piece.style.FontSize(),
				FontFamily: piece.style.FontFamily(),
				FontWeight: piece.style.FontWeight(),
				FontStyle:  piece.style.FontStyle(),
				Color:      textColor,
				Visible:    piece.style.Visibility() == visibilityVisible,
				Underline:  piece.style.Underline(),
			}
			if len(fragments) != 0 && fragments[len(fragments)-1].Kind == TextFragmentKind {
				previous := &fragments[len(fragments)-1].Text
				if previous.Node == text.Node &&
					previous.Pseudo == text.Pseudo &&
					previous.BaselineY == text.BaselineY &&
					previous.FontSize == text.FontSize &&
					previous.FontFamily == text.FontFamily &&
					previous.FontWeight == text.FontWeight &&
					previous.FontStyle == text.FontStyle &&
					previous.Color == text.Color {
					previous.Text += text.Text
					previous.Width += text.Width
					continue
				}
			}
			fragments = append(fragments, InlineFragment{Kind: TextFragmentKind, Text: text})
		}
		cursorY += lineHeight
		line = nil
		lineWidth = 0
	}

	lastWasBreak := false
	var lastBreakStyle computedStyle
	for _, token := range builder.tokens {
		if token.lineBreak {
			if len(line) == 0 {
				cursorY += token.style.LineHeight().Pixels(token.style.FontSize())
			} else {
				flushLine()
			}
			lastWasBreak = true
			lastBreakStyle = token.style
			continue
		}
		lastWasBreak = false
		wordMetrics := textMetrics{}
		imageWidth := 0.0
		imageHeight := 0.0
		if token.replaced {
			var ok bool
			imageWidth, imageHeight, ok = context.replacedDimensions(token.style, token.image, width, 0, 0)
			if !ok {
				continue
			}
			wordMetrics = textMetrics{width: imageWidth, ascent: imageHeight}
		} else {
			var err error
			wordMetrics, err = context.fonts.metrics(token.text, token.style.FontSize(), token.style.FontWeight(), token.style.FontStyle(), token.style.FontFamily())
			if err != nil {
				return nil, 0, err
			}
		}
		prefix := ""
		prefixWidth := 0.0
		if token.leadingSpace && len(line) != 0 {
			spaceMetrics, err := context.fonts.metrics(" ", token.style.FontSize(), token.style.FontWeight(), token.style.FontStyle(), token.style.FontFamily())
			if err != nil {
				return nil, 0, err
			}
			prefix = " "
			prefixWidth = spaceMetrics.width
		}
		if token.wrapBefore && len(line) != 0 && lineWidth+prefixWidth+wordMetrics.width > width {
			flushLine()
			prefix = ""
			prefixWidth = 0
		}
		if token.replaced {
			line = append(line, inlinePiece{
				node:     token.node,
				style:    token.style,
				image:    token.image,
				replaced: true,
				opacity:  token.opacity,
				x:        lineWidth + prefixWidth,
				width:    wordMetrics.width,
				height:   imageHeight,
				metrics:  wordMetrics,
			})
			lineWidth += prefixWidth + wordMetrics.width
			continue
		}
		pieceText := prefix + token.text
		pieceMetrics, err := context.fonts.metrics(pieceText, token.style.FontSize(), token.style.FontWeight(), token.style.FontStyle(), token.style.FontFamily())
		if err != nil {
			return nil, 0, err
		}
		line = append(line, inlinePiece{text: pieceText, node: token.node, pseudo: token.pseudo, style: token.style, opacity: token.opacity, x: lineWidth, width: pieceMetrics.width, metrics: pieceMetrics})
		lineWidth += pieceMetrics.width
	}
	if lastWasBreak {
		cursorY += lastBreakStyle.LineHeight().Pixels(lastBreakStyle.FontSize())
	} else {
		flushLine()
	}
	return fragments, cursorY - y, nil
}

func (context *layoutContext) replacedDimensions(style computedStyle, decoded image.Image, availableWidth, horizontalInsets, verticalInsets float64) (float64, float64, bool) {
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
	// Percentage heights resolve to auto while the containing block has the
	// auto height used by this layout slice.
	heightSpecified := style.Height().Unit() != lengthAuto && !style.Height().DependsOnPercent()
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
		height = resolveLength(style.Height(), naturalHeight, context.viewport, naturalHeight)
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
	height = context.constrainHeight(style, height, verticalInsets)
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

func (context *layoutContext) constrainHeight(style computedStyle, height, verticalInsets float64) float64 {
	maximum := math.Inf(1)
	if style.MaxHeight().Unit() != lengthAuto && !style.MaxHeight().DependsOnPercent() {
		maximum = math.Max(0, resolveLength(style.MaxHeight(), 0, context.viewport, height))
		if style.BoxSizing() == boxSizingBorderBox {
			maximum = math.Max(0, maximum-verticalInsets)
		}
	}
	minimum := 0.0
	if style.MinHeight().Unit() != lengthAuto && !style.MinHeight().DependsOnPercent() {
		minimum = math.Max(0, resolveLength(style.MinHeight(), 0, context.viewport, 0))
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
	if node == nil || node.style.Display() == displayNone {
		return
	}
	opacity := clamp(inheritedOpacity*node.style.Opacity(), 0, 1)
	if node.pseudo != computed.PseudoElementNone {
		builder.addText(node.generatedText, node.node, node.pseudo, node.style, opacity)
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
				node:         node.node,
				style:        node.style,
				image:        decoded,
				replaced:     true,
				opacity:      opacity,
				leadingSpace: leadingSpace,
				wrapBefore:   whiteSpaceAllowsSoftWrap(node.style.WhiteSpace()) && (leadingSpace || builder.hasContent),
			})
			builder.pendingSpace = false
			builder.hasContent = true
		}
		return
	}
	if node.node.Type == dom.TextNode {
		builder.addText(node.node.Data, node.node, computed.PseudoElementNone, node.style, opacity)
		return
	}
	for _, child := range node.children {
		if !isBlockLevel(child.style.Display()) {
			builder.add(child, opacity)
		}
	}
}

func (builder *inlineTokenBuilder) addText(source string, node *dom.Node, pseudo computed.PseudoElement, style computedStyle, opacity float64) {
	switch style.WhiteSpace() {
	case whiteSpacePre:
		builder.addPreservedText(source, node, pseudo, style, opacity, false)
	case whiteSpacePreWrap, whiteSpaceBreak:
		builder.addPreservedText(source, node, pseudo, style, opacity, true)
	case whiteSpacePreLine:
		builder.addCollapsedText(source, node, pseudo, style, opacity, true, true)
	case whiteSpaceNoWrap:
		builder.addCollapsedText(source, node, pseudo, style, opacity, false, false)
	default:
		builder.addCollapsedText(source, node, pseudo, style, opacity, true, false)
	}
}

func (builder *inlineTokenBuilder) addCollapsedText(source string, node *dom.Node, pseudo computed.PseudoElement, style computedStyle, opacity float64, wrap, preserveBreaks bool) {
	if preserveBreaks {
		parts := strings.Split(normalizeSegmentBreaks(source), "\n")
		for index, part := range parts {
			builder.addCollapsedText(part, node, pseudo, style, opacity, wrap, false)
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
			text:         source[start:end],
			node:         node,
			pseudo:       pseudo,
			style:        style,
			opacity:      opacity,
			leadingSpace: leadingSpace,
			wrapBefore:   wrap && leadingSpace,
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

func (builder *inlineTokenBuilder) addPreservedText(source string, node *dom.Node, pseudo computed.PseudoElement, style computedStyle, opacity float64, wrap bool) {
	source = strings.ReplaceAll(normalizeSegmentBreaks(source), "\t", "        ")
	parts := strings.Split(source, "\n")
	for partIndex, part := range parts {
		if part != "" {
			if !wrap {
				builder.tokens = append(builder.tokens, inlineToken{text: part, node: node, pseudo: pseudo, style: style, opacity: opacity})
			} else {
				start := 0
				wrapBefore := false
				for index, runeValue := range part {
					if runeValue != ' ' {
						continue
					}
					end := index + 1
					builder.tokens = append(builder.tokens, inlineToken{text: part[start:end], node: node, pseudo: pseudo, style: style, opacity: opacity, wrapBefore: wrapBefore})
					start = end
					wrapBefore = true
				}
				if start < len(part) {
					builder.tokens = append(builder.tokens, inlineToken{text: part[start:], node: node, pseudo: pseudo, style: style, opacity: opacity, wrapBefore: wrapBefore})
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
	return style.Width().Unit() != lengthAuto && style.Height().Unit() != lengthAuto && !style.Height().DependsOnPercent()
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
