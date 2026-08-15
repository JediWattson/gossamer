package css

import (
	"strings"

	"github.com/JediWattson/gossamer/internal/dom"
)

// Matches reports whether selector matches node. Selector matching only applies
// to elements; document, text, comment, doctype, and processing-instruction
// nodes never match.
func (selector Selector) Matches(node *dom.Node) bool {
	if node == nil || node.Type != dom.ElementNode {
		return false
	}
	if selector.Tag == "" && selector.ID == "" && len(selector.Classes) == 0 && len(selector.PseudoClasses) == 0 {
		return false
	}
	if selector.Tag != "" && selector.Tag != "*" && !equalASCIIFold(selector.Tag, node.Data) {
		return false
	}

	if selector.ID != "" {
		id, ok := attributeValue(node, "id")
		if !ok || id != selector.ID {
			return false
		}
	}

	classValue, hasClass := attributeValue(node, "class")
	for _, className := range selector.Classes {
		if !hasClass || !containsCSSSpaceSeparatedToken(classValue, className) {
			return false
		}
	}

	for _, pseudoClass := range selector.PseudoClasses {
		if !matchesPseudoClass(pseudoClass, node) {
			return false
		}
	}

	return true
}

// Match reports whether any selector in the rule matches node. When more than
// one selector matches, it returns the greatest matching specificity, which is
// the specificity the cascade must use for this rule.
func (rule Rule) Match(node *dom.Node) (Specificity, bool) {
	var greatest Specificity
	matched := false
	for _, selector := range rule.Selectors {
		if !selector.Matches(node) {
			continue
		}
		specificity := selector.calculatedSpecificity()
		if !matched || specificity.Compare(greatest) > 0 {
			greatest = specificity
		}
		matched = true
	}
	return greatest, matched
}

func (selector Selector) calculatedSpecificity() Specificity {
	specificity := Specificity{
		Classes: len(selector.Classes) + len(selector.PseudoClasses),
	}
	if selector.ID != "" {
		specificity.IDs = 1
	}
	if selector.Tag != "" && selector.Tag != "*" {
		specificity.Types = 1
	}
	return specificity
}

func matchesPseudoClass(name string, node *dom.Node) bool {
	switch strings.ToLower(name) {
	case "root":
		return isDocumentElement(node)
	case "empty":
		return isEmptyElement(node)
	case "first-child":
		return isFirstElementSibling(node, false)
	case "last-child":
		return isLastElementSibling(node, false)
	case "only-child":
		return isFirstElementSibling(node, false) && isLastElementSibling(node, false)
	case "first-of-type":
		return isFirstElementSibling(node, true)
	case "last-of-type":
		return isLastElementSibling(node, true)
	case "only-of-type":
		return isFirstElementSibling(node, true) && isLastElementSibling(node, true)
	case "link", "any-link":
		return isUnvisitedLink(node)
	case "visited", "hover", "active", "focus", "focus-visible", "focus-within", "target":
		// These selectors need history, interaction, focus, or navigation
		// state. They deliberately do not match until that state is modeled.
		return false
	default:
		// Selector values can be constructed directly even though the parser
		// rejects unsupported pseudo-classes.
		return false
	}
}

func supportedPseudoClass(name string) bool {
	switch name {
	case "root", "empty",
		"first-child", "last-child", "only-child",
		"first-of-type", "last-of-type", "only-of-type",
		"link", "any-link", "visited",
		"hover", "active", "focus", "focus-visible", "focus-within", "target":
		return true
	default:
		return false
	}
}

func isDocumentElement(node *dom.Node) bool {
	if node.Parent == nil || node.Parent.Type != dom.DocumentNode {
		return false
	}
	for _, child := range node.Parent.Children {
		if child != nil && child.Type == dom.ElementNode {
			return child == node
		}
	}
	return false
}

func isEmptyElement(node *dom.Node) bool {
	for _, child := range node.Children {
		if child == nil {
			continue
		}
		if child.Type == dom.ElementNode || child.Type == dom.TextNode && child.Data != "" {
			return false
		}
	}
	return true
}

func isFirstElementSibling(node *dom.Node, sameType bool) bool {
	if node.Parent == nil {
		return false
	}
	for _, sibling := range node.Parent.Children {
		if sibling == node {
			return true
		}
		if sibling == nil || sibling.Type != dom.ElementNode {
			continue
		}
		if !sameType || equalASCIIFold(sibling.Data, node.Data) {
			return false
		}
	}
	return false
}

func isLastElementSibling(node *dom.Node, sameType bool) bool {
	if node.Parent == nil {
		return false
	}
	found := false
	for _, sibling := range node.Parent.Children {
		if sibling == node {
			found = true
			continue
		}
		if !found || sibling == nil || sibling.Type != dom.ElementNode {
			continue
		}
		if !sameType || equalASCIIFold(sibling.Data, node.Data) {
			return false
		}
	}
	return found
}

func isUnvisitedLink(node *dom.Node) bool {
	if !equalASCIIFold(node.Data, "a") && !equalASCIIFold(node.Data, "area") {
		return false
	}
	_, hasHref := attributeValue(node, "href")
	return hasHref
}

func attributeValue(node *dom.Node, name string) (string, bool) {
	for _, attribute := range node.Attributes {
		if equalASCIIFold(attribute.Name, name) {
			return attribute.Value, true
		}
	}
	return "", false
}

func containsCSSSpaceSeparatedToken(value, token string) bool {
	for start := 0; start < len(value); {
		for start < len(value) && isCSSWhitespace(value[start]) {
			start++
		}
		end := start
		for end < len(value) && !isCSSWhitespace(value[end]) {
			end++
		}
		if start < end && value[start:end] == token {
			return true
		}
		start = end
	}
	return false
}

func equalASCIIFold(left, right string) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range len(left) {
		leftByte := left[index]
		rightByte := right[index]
		if leftByte >= 'A' && leftByte <= 'Z' {
			leftByte += 'a' - 'A'
		}
		if rightByte >= 'A' && rightByte <= 'Z' {
			rightByte += 'a' - 'A'
		}
		if leftByte != rightByte {
			return false
		}
	}
	return true
}

func lowerASCII(value string) string {
	var lowered strings.Builder
	lowered.Grow(len(value))
	for index := range len(value) {
		character := value[index]
		if character >= 'A' && character <= 'Z' {
			character += 'a' - 'A'
		}
		lowered.WriteByte(character)
	}
	return lowered.String()
}
