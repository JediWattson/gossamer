package render

import (
	"fmt"
	"image"
	"math"
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
		Node:   root.node,
		Bounds: Rect{Width: float64(viewport.Width), Height: float64(viewport.Height)},
	}
	html := findStyledElement(root, "html")
	if html == nil || html.style.display == displayNone {
		return documentBox, context.styles, nil
	}

	htmlBox := &Box{
		Node:   html.node,
		Bounds: Rect{Width: float64(viewport.Width), Height: float64(viewport.Height)},
	}
	documentBox.Children = append(documentBox.Children, htmlBox)

	body := directStyledElement(html, "body")
	if body == nil || body.style.display == displayNone {
		return documentBox, context.styles, nil
	}
	bodyY := resolveLength(body.style.marginTop, float64(viewport.Width), viewport, 0)
	bodyBox, err := context.layoutBlock(body, 0, bodyY, float64(viewport.Width))
	if err != nil {
		return nil, nil, err
	}
	htmlBox.Children = append(htmlBox.Children, bodyBox)
	bodyBottom := bodyBox.Bounds.Y + bodyBox.Bounds.Height + resolveLength(body.style.marginBottom, float64(viewport.Width), viewport, 0)
	if bodyBottom > htmlBox.Bounds.Height {
		htmlBox.Bounds.Height = bodyBottom
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
	leftAuto := style.marginLeft.unit == lengthAuto
	rightAuto := style.marginRight.unit == lengthAuto
	left := resolveLength(style.marginLeft, availableWidth, context.viewport, 0)
	right := resolveLength(style.marginRight, availableWidth, context.viewport, 0)
	if node.node.Type == dom.ElementNode && node.node.Data == "img" {
		decoded := context.images[node.node]
		imageWidth, imageHeight, ok := context.replacedDimensions(style, decoded, availableWidth)
		if !ok {
			return &Box{Node: node.node, Bounds: Rect{X: containingX + left, Y: contentY}}, nil
		}
		remaining := availableWidth - imageWidth
		switch {
		case leftAuto && rightAuto:
			left = remaining / 2
		case leftAuto:
			left = remaining - right
		}
		bounds := Rect{X: containingX + left, Y: contentY, Width: imageWidth, Height: imageHeight}
		fragment := InlineFragment{
			Kind:  ImageFragmentKind,
			Image: ImageFragment{Node: node.node, Image: decoded, Bounds: bounds, Opacity: 1},
		}
		return &Box{
			Node:      node.node,
			Bounds:    bounds,
			Fragments: []InlineFragment{fragment},
			flow:      []flowItem{{fragment: fragment}},
		}, nil
	}

	width := availableWidth - left - right
	if style.width.unit != lengthAuto {
		width = resolveLength(style.width, availableWidth, context.viewport, availableWidth)
		remaining := availableWidth - width
		switch {
		case leftAuto && rightAuto:
			left = remaining / 2
			right = remaining / 2
		case leftAuto:
			left = remaining - right
		case rightAuto:
			right = remaining - left
		}
	}
	if width < 0 {
		width = 0
	}

	box := &Box{
		Node:   node.node,
		Bounds: Rect{X: containingX + left, Y: contentY, Width: width},
	}
	cursorY := contentY
	previousBottomMargin := 0.0
	hasContent := false

	for index := 0; index < len(node.children); {
		child := node.children[index]
		if child.style.display == displayNone {
			index++
			continue
		}
		if child.style.display == displayBlock {
			topMargin := resolveLength(child.style.marginTop, width, context.viewport, 0)
			gap := math.Max(previousBottomMargin, topMargin)
			// A first block child's top margin collapses through an auto-height
			// parent with no border or padding in this initial box model.
			if !hasContent {
				gap = 0
			}
			childBox, err := context.layoutBlock(child, box.Bounds.X, cursorY+gap, width)
			if err != nil {
				return nil, err
			}
			box.Children = append(box.Children, childBox)
			box.flow = append(box.flow, flowItem{box: childBox})
			cursorY = childBox.Bounds.Y + childBox.Bounds.Height
			previousBottomMargin = resolveLength(child.style.marginBottom, width, context.viewport, 0)
			hasContent = true
			index++
			continue
		}

		end := index
		for end < len(node.children) && node.children[end].style.display != displayBlock {
			end++
		}
		fragments, height, err := context.layoutInline(node.children[index:end], box.Bounds.X, cursorY, width)
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

	box.Bounds.Height = math.Max(0, cursorY-contentY)
	return box, nil
}

func (context *layoutContext) layoutInline(nodes []*styledNode, x, y, width float64) ([]InlineFragment, float64, error) {
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
			lineHeight = math.Max(lineHeight, piece.style.fontSize*piece.style.lineHeight)
		}
		lineHeight = math.Max(lineHeight, lineAscent+lineDescent)
		leading := math.Max(0, lineHeight-lineAscent-lineDescent)
		baseline := cursorY + leading/2 + lineAscent
		for _, piece := range line {
			if piece.replaced {
				fragments = append(fragments, InlineFragment{
					Kind: ImageFragmentKind,
					Image: ImageFragment{
						Node:    piece.node,
						Image:   piece.image,
						Bounds:  Rect{X: x + piece.x, Y: baseline - piece.height, Width: piece.width, Height: piece.height},
						Opacity: piece.opacity,
					},
				})
				continue
			}
			textColor := piece.style.color
			textColor.A = uint8(math.Round(float64(textColor.A) * clamp(piece.opacity, 0, 1)))
			text := TextFragment{
				Node:       piece.node,
				Text:       piece.text,
				X:          x + piece.x,
				BaselineY:  baseline,
				Width:      piece.width,
				Height:     lineHeight,
				FontSize:   piece.style.fontSize,
				FontWeight: piece.style.fontWeight,
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
			wordMetrics, err = context.fonts.metrics(token.text, token.style.fontSize, token.style.fontWeight)
			if err != nil {
				return nil, 0, err
			}
		}
		prefix := ""
		prefixWidth := 0.0
		if token.leadingSpace && len(line) != 0 {
			spaceMetrics, err := context.fonts.metrics(" ", token.style.fontSize, token.style.fontWeight)
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
		pieceMetrics, err := context.fonts.metrics(pieceText, token.style.fontSize, token.style.fontWeight)
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

	widthSpecified := style.width.unit != lengthAuto
	// Percentage heights resolve to auto while the containing block has the
	// auto height used by this layout slice.
	heightSpecified := style.height.unit != lengthAuto && style.height.unit != lengthPercent
	if !hasNaturalSize && !(widthSpecified && heightSpecified) {
		return 0, 0, false
	}
	width := naturalWidth
	height := naturalHeight
	if widthSpecified {
		width = resolveLength(style.width, availableWidth, context.viewport, naturalWidth)
	}
	if heightSpecified {
		height = resolveLength(style.height, naturalHeight, context.viewport, naturalHeight)
	}
	switch {
	case widthSpecified && !heightSpecified:
		height = naturalHeight * width / naturalWidth
	case heightSpecified && !widthSpecified:
		width = naturalWidth * height / naturalHeight
	}
	if width < 0 || height < 0 || !isFinite(width) || !isFinite(height) {
		return 0, 0, false
	}
	return width, height, true
}

type inlineTokenBuilder struct {
	tokens       []inlineToken
	images       map[*dom.Node]image.Image
	pendingSpace bool
	hasContent   bool
}

func (builder *inlineTokenBuilder) add(node *styledNode, inheritedOpacity float64) {
	if node == nil || node.style.display == displayNone {
		return
	}
	opacity := clamp(inheritedOpacity*node.style.opacity, 0, 1)
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
		if child.style.display != displayBlock {
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
	return style.width.unit != lengthAuto && style.height.unit != lengthAuto && style.height.unit != lengthPercent
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
