package css

import "github.com/JediWattson/gossamer/internal/dom"

// htmlReadWriteState implements HTML's definition of user-alterable elements.
// The boolean result is paired with an operation-budget result so :read-only
// also fails closed rather than turning an exhausted :read-write check into a
// match.
func htmlReadWriteState(node *dom.Node, state *selectorMatchState) (bool, bool) {
	if node == nil || node.Type != dom.ElementNode {
		return false, true
	}
	if isHTMLElementNamed(node, "input") {
		if !inputSupportsReadOnly(normalizedInputType(node)) {
			return false, true
		}
		_, disabled := htmlDisabledState(node)
		return !disabled && !hasAttribute(node, "readonly"), true
	}
	if isHTMLElementNamed(node, "textarea") {
		_, disabled := htmlDisabledState(node)
		return !disabled && !hasAttribute(node, "readonly"), true
	}

	for current := node; current != nil && current.Type == dom.ElementNode; current = current.Parent {
		if !state.take() {
			return false, false
		}
		if current.NamespaceURI != dom.HTMLNamespace {
			continue
		}
		value, found := attributeValue(current, "contenteditable")
		if !found {
			continue
		}
		switch lowerASCII(value) {
		case "", "true", "plaintext-only":
			return true, true
		case "false":
			return false, true
		}
		// Missing and invalid contenteditable values inherit.
	}
	return false, true
}

func inputSupportsReadOnly(typeName string) bool {
	switch typeName {
	case "text", "search", "tel", "url", "email", "password",
		"date", "month", "week", "time", "datetime-local", "number":
		return true
	default:
		return false
	}
}

func htmlPlaceholderShown(node *dom.Node, state *selectorMatchState) bool {
	if node == nil || node.Type != dom.ElementNode || node.NamespaceURI != dom.HTMLNamespace ||
		!hasAttribute(node, "placeholder") {
		return false
	}
	if isHTMLElementNamed(node, "input") {
		if !inputSupportsPlaceholder(normalizedInputType(node)) {
			return false
		}
		if node.Control != nil && node.Control.ValueDirty {
			return node.Control.Value == ""
		}
		value, _ := attributeValue(node, "value")
		return value == ""
	}
	if !isHTMLElementNamed(node, "textarea") {
		return false
	}
	if node.Control != nil && node.Control.ValueDirty {
		return node.Control.Value == ""
	}
	return textareaDefaultValueEmpty(node, state)
}

func inputSupportsPlaceholder(typeName string) bool {
	switch typeName {
	case "text", "search", "tel", "url", "email", "password", "number":
		return true
	default:
		return false
	}
}

func textareaDefaultValueEmpty(root *dom.Node, state *selectorMatchState) bool {
	stack := make([]*dom.Node, 0, len(root.Children))
	for index := len(root.Children) - 1; index >= 0; index-- {
		stack = append(stack, root.Children[index])
	}
	for len(stack) != 0 {
		last := len(stack) - 1
		current := stack[last]
		stack = stack[:last]
		if !state.take() {
			return false
		}
		if current == nil {
			continue
		}
		if current.Type == dom.TextNode && current.Data != "" {
			return false
		}
		for index := len(current.Children) - 1; index >= 0; index-- {
			stack = append(stack, current.Children[index])
		}
	}
	return true
}
