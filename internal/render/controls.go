package render

import (
	"math"
	"strconv"
	"strings"

	"github.com/JediWattson/gossamer/internal/dom"
	computed "github.com/JediWattson/gossamer/internal/style"
)

const formControlPlaceholderOpacity = 0.55

func isFormControlElement(node *dom.Node) bool {
	return node != nil && node.Type == dom.ElementNode && node.NamespaceURI == dom.HTMLNamespace &&
		(node.Data == "input" || node.Data == "textarea")
}

func isHiddenInputElement(node *dom.Node) bool {
	return node != nil && node.Type == dom.ElementNode && node.NamespaceURI == dom.HTMLNamespace &&
		node.Data == "input" && formControlInputType(node) == "hidden"
}

func isRenderedFormControl(node *dom.Node) bool {
	return isFormControlElement(node) && !isHiddenInputElement(node)
}

func formControlAttr(node *dom.Node, name string) (string, bool) {
	if node == nil {
		return "", false
	}
	for _, attribute := range node.Attributes {
		if attribute.Name == name {
			return attribute.Value, true
		}
	}
	return "", false
}

func formControlPositiveIntAttr(node *dom.Node, name string, fallback int) int {
	raw, ok := formControlAttr(node, name)
	if !ok {
		return fallback
	}
	parsed, err := strconv.Atoi(strings.TrimSpace(raw))
	if err != nil || parsed <= 0 {
		return fallback
	}
	return parsed
}

func formControlInputType(node *dom.Node) string {
	if node == nil || node.Data != "input" {
		return ""
	}
	raw, ok := formControlAttr(node, "type")
	if !ok {
		return "text"
	}
	trimmed := strings.ToLower(strings.TrimSpace(raw))
	if trimmed == "" {
		return "text"
	}
	return trimmed
}

func formControlDescendantText(node *dom.Node) string {
	if node == nil {
		return ""
	}
	var builder strings.Builder
	var walk func(*dom.Node)
	walk = func(current *dom.Node) {
		for _, child := range current.Children {
			switch child.Type {
			case dom.TextNode:
				builder.WriteString(child.Data)
			case dom.ElementNode:
				walk(child)
			}
		}
	}
	walk(node)
	return builder.String()
}

func formControlCurrentValue(node *dom.Node) string {
	if node == nil {
		return ""
	}
	if node.Control != nil && node.Control.ValueDirty {
		return node.Control.Value
	}
	if node.Data == "textarea" {
		return formControlDescendantText(node)
	}
	value, _ := formControlAttr(node, "value")
	return value
}

func formControlDisplayText(node *dom.Node) (text string, isPlaceholder bool, singleLine bool) {
	if node == nil {
		return "", false, true
	}
	if node.Data == "textarea" {
		value := formControlCurrentValue(node)
		if value != "" {
			return value, false, false
		}
		placeholder, _ := formControlAttr(node, "placeholder")
		return placeholder, placeholder != "", false
	}

	inputType := formControlInputType(node)
	switch inputType {
	case "hidden", "checkbox", "radio", "file", "image", "color", "range":
		return "", false, true
	case "submit", "reset", "button":
		if value, ok := formControlAttr(node, "value"); ok {
			return value, false, true
		}
		switch inputType {
		case "submit":
			return "Submit", false, true
		case "reset":
			return "Reset", false, true
		default:
			return "", false, true
		}
	default:
		value := formControlCurrentValue(node)
		if value != "" {
			if inputType == "password" {
				count := len([]rune(value))
				return strings.Repeat("\u2022", count), false, true
			}
			return value, false, true
		}
		placeholder, _ := formControlAttr(node, "placeholder")
		return placeholder, placeholder != "", true
	}
}

func (context *layoutContext) intrinsicFormControlDimensions(node *dom.Node, style computedStyle) (float64, float64) {
	if isHiddenInputElement(node) {
		return 0, 0
	}
	lineHeight := style.LineHeight().Pixels(style.FontSize())
	spaceWidth := style.FontSize() * 0.55
	if context.fonts != nil {
		if metrics, err := context.fonts.metricsForStyle("0", style); err == nil {
			lineHeight = math.Max(lineHeight, metrics.ascent+metrics.descent)
			if metrics.width > 0 {
				spaceWidth = metrics.width
			}
		}
	}
	if node.Data == "textarea" {
		cols := formControlPositiveIntAttr(node, "cols", 20)
		rows := formControlPositiveIntAttr(node, "rows", 2)
		return float64(cols) * spaceWidth, float64(rows) * lineHeight
	}
	inputType := formControlInputType(node)
	switch inputType {
	case "checkbox", "radio":
		return 13, 13
	case "submit", "reset", "button":
		label, _, _ := formControlDisplayText(node)
		labelWidth := spaceWidth * float64(len([]rune(label)))
		if label != "" && context.fonts != nil {
			if metrics, err := context.fonts.metricsForStyle(label, style); err == nil {
				labelWidth = metrics.width
			}
		}
		return labelWidth, lineHeight
	default:
		size := formControlPositiveIntAttr(node, "size", 20)
		return float64(size) * spaceWidth, lineHeight
	}
}

func (context *layoutContext) layoutFormControlBox(
	node *styledNode,
	box *Box,
	width float64,
	containingHeight *float64,
	hasDefiniteHeight bool,
	specifiedContentHeight float64,
	availableWidth float64,
	verticalInsets float64,
) (*Box, error) {
	if isHiddenInputElement(node.node) {
		box.ContentBounds.Width = 0
		box.ContentBounds.Height = 0
		box.Bounds.Width = 0
		box.Bounds.Height = 0
		return context.finalizeBlock(node, box, availableWidth)
	}
	_, defaultHeight := context.intrinsicFormControlDimensions(node.node, node.style)
	displayText, isPlaceholder, singleLine := formControlDisplayText(node.node)
	opacity := 1.0
	if isPlaceholder {
		opacity = formControlPlaceholderOpacity
	}
	var inline inlineLayout
	if displayText != "" {
		builder := inlineTokenBuilder{images: context.images}
		builder.addPreservedText(displayText, node.node, computed.PseudoElementNone, node.style, opacity, verticalAlignment{}, !singleLine)
		if len(builder.tokens) != 0 {
			var err error
			inline, err = context.layoutInlineTokens(builder.tokens, nil, box.ContentBounds.X, box.ContentBounds.Y, width, containingHeight, node.style, false)
			if err != nil {
				return nil, err
			}
		}
	}
	contentHeight := defaultHeight
	if !hasDefiniteHeight {
		contentHeight = math.Max(defaultHeight, inline.height)
	} else {
		contentHeight = specifiedContentHeight
	}
	contentHeight = context.constrainHeight(node.style, contentHeight, verticalInsets, containingHeight)
	if singleLine && len(inline.flow) != 0 && contentHeight > inline.height {
		translateInlineLayout(&inline, 0, (contentHeight-inline.height)/2)
	}
	box.Fragments = append(box.Fragments, inline.fragments...)
	for _, item := range inline.flow {
		box.flow = append(box.flow, item)
		if item.fragment.Kind == TextFragmentKind {
			box.Text = append(box.Text, item.fragment.Text)
		}
	}
	box.ContentBounds.Height = contentHeight
	box.Bounds.Height = box.Border.Top + box.Padding.Top + contentHeight + box.Padding.Bottom + box.Border.Bottom
	return context.finalizeBlock(node, box, availableWidth)
}
