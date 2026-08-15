package render

import (
	"fmt"
	"image"
	"math"
	"strconv"
	"strings"
	"unicode"

	"github.com/JediWattson/gossamer/internal/dom"
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
	style        computedStyle
	image        image.Image
	replaced     bool
	opacity      float64
	leadingSpace bool
	lineBreak    bool
}

type inlinePiece struct {
	text     string
	node     *dom.Node
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
	}
	html := findStyledElement(root, "html")
	if html == nil || html.style.Display() == displayNone {
		return documentBox, context.styles, nil
	}

	htmlBox := &Box{
		Node:          html.node,
		Bounds:        Rect{Width: float64(viewport.Width), Height: float64(viewport.Height)},
		ContentBounds: Rect{Width: float64(viewport.Width), Height: float64(viewport.Height)},
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
	return documentBox, context.styles, nil
}

func (context *layoutContext) indexStyles(node *styledNode) {
	if node == nil {
		return
	}
	if node.node != nil {
		context.styles[node.node] = node.style
	}
	for _, child := range node.children {
		context.indexStyles(child)
	}
}

func (context *layoutContext) layoutBlock(node *styledNode, containingX, contentY, availableWidth float64) (*Box, error) {
	style := node.style
	leftAuto := style.MarginLeft().Unit() == lengthAuto
	rightAuto := style.MarginRight().Unit() == lengthAuto
	left := resolveLength(style.MarginLeft(), availableWidth, context.viewport, 0)
	right := resolveLength(style.MarginRight(), availableWidth, context.viewport, 0)
	padding := context.resolvePadding(style, availableWidth)
	border := context.resolveBorder(style, availableWidth)
	if node.node.Type == dom.ElementNode && node.node.Data == "img" {
		decoded := context.images[node.node]
		imageWidth, imageHeight, ok := context.replacedDimensions(style, decoded, availableWidth)
		if !ok {
			box := &Box{
				Node:          node.node,
				Bounds:        Rect{X: containingX + left, Y: contentY, Width: border.Left + padding.Left + padding.Right + border.Right, Height: border.Top + padding.Top + padding.Bottom + border.Bottom},
				ContentBounds: Rect{X: containingX + left + border.Left + padding.Left, Y: contentY + border.Top + padding.Top},
				Padding:       padding,
				Border:        border,
			}
			return context.finalizeBlock(node, box)
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
			Bounds:        bounds,
			ContentBounds: contentBounds,
			Padding:       padding,
			Border:        border,
			Fragments:     []InlineFragment{fragment},
			flow:          []flowItem{{fragment: fragment}},
		}
		return context.finalizeBlock(node, box)
	}

	width := availableWidth - left - right - padding.Left - padding.Right - border.Left - border.Right
	if style.Width().Unit() != lengthAuto {
		width = resolveLength(style.Width(), availableWidth, context.viewport, availableWidth)
	}
	width = context.constrainWidth(style, math.Max(0, width), availableWidth)
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
		Node:    node.node,
		Bounds:  Rect{X: containingX + left, Y: contentY, Width: outerWidth},
		Padding: padding,
		Border:  border,
	}
	box.ContentBounds = Rect{
		X:     box.Bounds.X + border.Left + padding.Left,
		Y:     box.Bounds.Y + border.Top + padding.Top,
		Width: width,
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
		for end < len(node.children) && !isBlockLevel(node.children[end].style.Display()) {
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
	if style.Height().Unit() != lengthAuto && style.Height().Unit() != lengthPercent {
		contentHeight = math.Max(0, resolveLength(style.Height(), 0, context.viewport, contentHeight))
	}
	box.ContentBounds.Height = contentHeight
	box.Bounds.Height = border.Top + padding.Top + box.ContentBounds.Height + padding.Bottom + border.Bottom
	return context.finalizeBlock(node, box)
}

func (context *layoutContext) finalizeBlock(node *styledNode, box *Box) (*Box, error) {
	if node.style.Display() == displayListItem && node.style.ListStyleType() != listStyleNone {
		if err := context.addListMarker(node, box); err != nil {
			return nil, err
		}
	}
	return box, nil
}

func (context *layoutContext) addListMarker(node *styledNode, box *Box) error {
	markerText := context.listMarkerText(node.node, node.style.ListStyleType())
	if markerText == "" {
		return nil
	}
	metrics, err := context.fonts.metrics(markerText, node.style.FontSize(), node.style.FontWeight())
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
		Text:       markerText,
		X:          box.Bounds.X - node.style.FontSize()*.5 - metrics.width,
		BaselineY:  baseline,
		Width:      metrics.width,
		Height:     node.style.LineHeight().Pixels(node.style.FontSize()),
		FontSize:   node.style.FontSize(),
		FontWeight: node.style.FontWeight(),
		Color:      node.style.Color(),
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
		case alignRight:
			lineOffset = width - lineWidth
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
				Text:       piece.text,
				X:          x + lineOffset + piece.x,
				BaselineY:  baseline,
				Width:      piece.width,
				Height:     lineHeight,
				FontSize:   piece.style.FontSize(),
				FontWeight: piece.style.FontWeight(),
				Color:      textColor,
			}
			if len(fragments) != 0 && fragments[len(fragments)-1].Kind == TextFragmentKind {
				previous := &fragments[len(fragments)-1].Text
				if previous.Node == text.Node &&
					previous.BaselineY == text.BaselineY &&
					previous.FontSize == text.FontSize &&
					previous.FontWeight == text.FontWeight &&
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

	for _, token := range builder.tokens {
		if token.lineBreak {
			flushLine()
			continue
		}
		wordMetrics := textMetrics{}
		imageWidth := 0.0
		imageHeight := 0.0
		if token.replaced {
			var ok bool
			imageWidth, imageHeight, ok = context.replacedDimensions(token.style, token.image, width)
			if !ok {
				continue
			}
			wordMetrics = textMetrics{width: imageWidth, ascent: imageHeight}
		} else {
			var err error
			wordMetrics, err = context.fonts.metrics(token.text, token.style.FontSize(), token.style.FontWeight())
			if err != nil {
				return nil, 0, err
			}
		}
		prefix := ""
		prefixWidth := 0.0
		if token.leadingSpace && len(line) != 0 {
			spaceMetrics, err := context.fonts.metrics(" ", token.style.FontSize(), token.style.FontWeight())
			if err != nil {
				return nil, 0, err
			}
			prefix = " "
			prefixWidth = spaceMetrics.width
		}
		if len(line) != 0 && lineWidth+prefixWidth+wordMetrics.width > width {
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
		pieceMetrics, err := context.fonts.metrics(pieceText, token.style.FontSize(), token.style.FontWeight())
		if err != nil {
			return nil, 0, err
		}
		line = append(line, inlinePiece{text: pieceText, node: token.node, style: token.style, opacity: token.opacity, x: lineWidth, width: pieceMetrics.width, metrics: pieceMetrics})
		lineWidth += pieceMetrics.width
	}
	flushLine()
	return fragments, cursorY - y, nil
}

func (context *layoutContext) replacedDimensions(style computedStyle, decoded image.Image, availableWidth float64) (float64, float64, bool) {
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
	heightSpecified := style.Height().Unit() != lengthAuto && style.Height().Unit() != lengthPercent
	if !hasNaturalSize && !(widthSpecified && heightSpecified) {
		return 0, 0, false
	}
	width := naturalWidth
	height := naturalHeight
	if widthSpecified {
		width = resolveLength(style.Width(), availableWidth, context.viewport, naturalWidth)
	}
	if heightSpecified {
		height = resolveLength(style.Height(), naturalHeight, context.viewport, naturalHeight)
	}
	switch {
	case widthSpecified && !heightSpecified:
		height = naturalHeight * width / naturalWidth
	case heightSpecified && !widthSpecified:
		width = naturalWidth * height / naturalHeight
	}
	constrainedWidth := context.constrainWidth(style, width, availableWidth)
	if constrainedWidth != width && !heightSpecified && width > 0 {
		height *= constrainedWidth / width
	}
	width = constrainedWidth
	if width < 0 || height < 0 || !isFinite(width) || !isFinite(height) {
		return 0, 0, false
	}
	return width, height, true
}

func (context *layoutContext) resolvePadding(style computedStyle, availableWidth float64) Edges {
	return Edges{
		Top:    resolveLength(style.PaddingTop(), availableWidth, context.viewport, 0),
		Right:  resolveLength(style.PaddingRight(), availableWidth, context.viewport, 0),
		Bottom: resolveLength(style.PaddingBottom(), availableWidth, context.viewport, 0),
		Left:   resolveLength(style.PaddingLeft(), availableWidth, context.viewport, 0),
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

func (context *layoutContext) constrainWidth(style computedStyle, width, availableWidth float64) float64 {
	maximum := math.Inf(1)
	if style.MaxWidth().Unit() != lengthAuto {
		maximum = math.Max(0, resolveLength(style.MaxWidth(), availableWidth, context.viewport, width))
	}
	minimum := 0.0
	if style.MinWidth().Unit() != lengthAuto {
		minimum = math.Max(0, resolveLength(style.MinWidth(), availableWidth, context.viewport, 0))
	}
	return math.Max(minimum, math.Min(width, maximum))
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
	if node.node.Type == dom.ElementNode && node.node.Data == "br" {
		builder.tokens = append(builder.tokens, inlineToken{lineBreak: true})
		builder.pendingSpace = false
		builder.hasContent = false
		return
	}
	if node.node.Type == dom.ElementNode && node.node.Data == "img" {
		decoded := builder.images[node.node]
		if decoded != nil || hasExplicitImageDimensions(node.style) {
			builder.tokens = append(builder.tokens, inlineToken{
				node:         node.node,
				style:        node.style,
				image:        decoded,
				replaced:     true,
				opacity:      opacity,
				leadingSpace: builder.pendingSpace && builder.hasContent,
			})
			builder.pendingSpace = false
			builder.hasContent = true
		}
		return
	}
	if node.node.Type == dom.TextNode {
		builder.addText(node.node.Data, node.node, node.style, opacity)
		return
	}
	for _, child := range node.children {
		if !isBlockLevel(child.style.Display()) {
			builder.add(child, opacity)
		}
	}
}

func (builder *inlineTokenBuilder) addText(source string, node *dom.Node, style computedStyle, opacity float64) {
	start := -1
	flushWord := func(end int) {
		if start < 0 {
			return
		}
		builder.tokens = append(builder.tokens, inlineToken{
			text:         source[start:end],
			node:         node,
			style:        style,
			opacity:      opacity,
			leadingSpace: builder.pendingSpace && builder.hasContent,
		})
		builder.hasContent = true
		builder.pendingSpace = false
		start = -1
	}
	for index, runeValue := range source {
		if unicode.IsSpace(runeValue) {
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

func hasExplicitImageDimensions(style computedStyle) bool {
	return style.Width().Unit() != lengthAuto && style.Height().Unit() != lengthAuto && style.Height().Unit() != lengthPercent
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
