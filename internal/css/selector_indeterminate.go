package css

import "github.com/JediWattson/gossamer/internal/dom"

func htmlIndeterminateState(node *dom.Node, state *selectorMatchState) bool {
	if node == nil || node.Type != dom.ElementNode || node.NamespaceURI != dom.HTMLNamespace {
		return false
	}
	if isHTMLElementNamed(node, "progress") {
		return !hasAttribute(node, "value")
	}
	if !isHTMLElementNamed(node, "input") {
		return false
	}
	switch normalizedInputType(node) {
	case "checkbox":
		return node.Control != nil && node.Control.Indeterminate
	case "radio":
		return htmlRadioGroupHasNoCheckedButton(node, state)
	default:
		return false
	}
}

func htmlRadioGroupHasNoCheckedButton(node *dom.Node, state *selectorMatchState) bool {
	if isHTMLChecked(node) {
		return false
	}
	name, named := attributeValue(node, "name")
	if !named || name == "" {
		return true
	}
	owner, ok := selectorFormOwner(node, state)
	if !ok {
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
		if candidate != nil && candidate != node &&
			isHTMLElementNamed(candidate, "input") && normalizedInputType(candidate) == "radio" {
			candidateName, candidateNamed := attributeValue(candidate, "name")
			if candidateNamed && candidateName == name {
				candidateOwner, ownerOK := selectorFormOwner(candidate, state)
				if !ownerOK {
					return false
				}
				if candidateOwner == owner && isHTMLChecked(candidate) {
					return false
				}
			}
		}
		if candidate == nil {
			continue
		}
		for index := len(candidate.Children) - 1; index >= 0; index-- {
			stack = append(stack, candidate.Children[index])
		}
	}
	return true
}
