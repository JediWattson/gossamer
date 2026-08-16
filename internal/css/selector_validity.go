package css

import "github.com/JediWattson/gossamer/internal/dom"

func htmlValidityState(node *dom.Node, state *selectorMatchState) (participates, valid, completed bool) {
	if node == nil || node.Type != dom.ElementNode || node.NamespaceURI != dom.HTMLNamespace {
		return false, false, true
	}
	if !isHTMLElementNamed(node, "form") && !isHTMLElementNamed(node, "fieldset") {
		return dom.EvaluateConstraintValidity(node, state.take)
	}

	root := node
	if isHTMLElementNamed(node, "form") {
		root = selectorTreeRoot(node)
	}
	stack := make([]*dom.Node, 0, len(root.Children))
	for index := len(root.Children) - 1; index >= 0; index-- {
		stack = append(stack, root.Children[index])
	}
	for len(stack) != 0 {
		last := len(stack) - 1
		candidateNode := stack[last]
		stack = stack[:last]
		if !state.take() {
			return true, false, false
		}
		if candidateNode == nil {
			continue
		}
		candidate, candidateValid, ok := dom.EvaluateConstraintValidity(candidateNode, state.take)
		if !ok {
			return true, false, false
		}
		if candidate && isHTMLElementNamed(node, "form") {
			owner, ownerOK := selectorFormOwner(candidateNode, state)
			if !ownerOK {
				return true, false, false
			}
			candidate = owner == node
		}
		if candidate && !candidateValid {
			return true, false, true
		}
		for index := len(candidateNode.Children) - 1; index >= 0; index-- {
			stack = append(stack, candidateNode.Children[index])
		}
	}
	return true, true, true
}

func htmlUserValidityState(node *dom.Node, state *selectorMatchState) (participates, valid, completed bool) {
	if node == nil || node.Type != dom.ElementNode || node.NamespaceURI != dom.HTMLNamespace ||
		(node.Data != "input" && node.Data != "textarea" && node.Data != "select") ||
		node.Control == nil || !node.Control.UserValidity {
		return false, false, true
	}
	return dom.EvaluateConstraintValidity(node, state.take)
}
