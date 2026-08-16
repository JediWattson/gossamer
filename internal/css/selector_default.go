package css

import "github.com/JediWattson/gossamer/internal/dom"

func htmlDefaultState(node *dom.Node, state *selectorMatchState) bool {
	if node == nil || node.Type != dom.ElementNode || node.NamespaceURI != dom.HTMLNamespace {
		return false
	}
	if isHTMLElementNamed(node, "input") {
		switch normalizedInputType(node) {
		case "checkbox", "radio":
			return hasAttribute(node, "checked")
		}
	}
	if isHTMLElementNamed(node, "option") {
		return hasAttribute(node, "selected")
	}
	if !isHTMLSubmitButton(node) {
		return false
	}
	owner, ok := selectorFormOwner(node, state)
	if !ok || owner == nil {
		return false
	}
	root := selectorTreeRoot(node)
	stack := []*dom.Node{root}
	for len(stack) != 0 {
		last := len(stack) - 1
		candidate := stack[last]
		stack = stack[:last]
		if !state.take() {
			return false
		}
		if candidate != nil && candidate.Type == dom.ElementNode && isHTMLSubmitButton(candidate) {
			candidateOwner, ownerOK := selectorFormOwner(candidate, state)
			if !ownerOK {
				return false
			}
			if candidateOwner == owner {
				return candidate == node
			}
		}
		if candidate == nil {
			continue
		}
		for index := len(candidate.Children) - 1; index >= 0; index-- {
			stack = append(stack, candidate.Children[index])
		}
	}
	return false
}

func isHTMLSubmitButton(node *dom.Node) bool {
	if isHTMLElementNamed(node, "input") {
		return normalizedInputType(node) == "submit"
	}
	if !isHTMLElementNamed(node, "button") || isHTMLElementNamed(node.Parent, "select") {
		return false
	}
	typeName, found := attributeValue(node, "type")
	if found {
		switch lowerASCII(typeName) {
		case "submit":
			return true
		case "reset", "button":
			return false
		}
	}
	return !hasAttribute(node, "command") && !hasAttribute(node, "commandfor")
}

func selectorFormOwner(node *dom.Node, state *selectorMatchState) (*dom.Node, bool) {
	explicit, found := attributeValue(node, "form")
	if found {
		root := selectorTreeRoot(node)
		if root != nil && root.Type == dom.DocumentNode {
			stack := []*dom.Node{root}
			for len(stack) != 0 {
				last := len(stack) - 1
				candidate := stack[last]
				stack = stack[:last]
				if !state.take() {
					return nil, false
				}
				if candidate != nil && candidate.Type == dom.ElementNode {
					if id, hasID := attributeValue(candidate, "id"); hasID && id == explicit {
						if isHTMLElementNamed(candidate, "form") {
							return candidate, true
						}
						return nil, true
					}
				}
				if candidate == nil {
					continue
				}
				for index := len(candidate.Children) - 1; index >= 0; index-- {
					stack = append(stack, candidate.Children[index])
				}
			}
			return nil, true
		}
	}
	for ancestor := node.Parent; ancestor != nil; ancestor = ancestor.Parent {
		if !state.take() {
			return nil, false
		}
		if isHTMLElementNamed(ancestor, "form") {
			return ancestor, true
		}
	}
	return nil, true
}

func selectorTreeRoot(node *dom.Node) *dom.Node {
	for node != nil && node.Parent != nil {
		node = node.Parent
	}
	return node
}
