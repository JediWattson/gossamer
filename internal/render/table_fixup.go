package render

import (
	"github.com/JediWattson/gossamer/internal/dom"
	computed "github.com/JediWattson/gossamer/internal/style"
)

// fixupTableFormattingTree generates the anonymous table wrappers required by
// CSS Tables before layout. It mutates only the renderer's private projection;
// neither the DOM nor the immutable computed-style snapshot is changed.
func fixupTableFormattingTree(node *styledNode) {
	if node == nil {
		return
	}
	for _, child := range node.children {
		fixupTableFormattingTree(child)
	}
	switch node.style.Display() {
	case displayTable, displayInlineTable:
		node.children = fixTableRootChildren(node)
	case displayRowGroup, displayHeaderGroup, displayFooterGroup:
		node.children = fixTableRowGroupChildren(node)
	case displayTableRow:
		node.children = fixTableRowChildren(node)
	case displayColumnGroup:
		node.children = fixTableColumnGroupChildren(node.children)
	case displayTableColumn:
		node.children = nil
	default:
		node.children = wrapMisparentedTableChildren(node)
	}
}

func fixTableRootChildren(parent *styledNode) []*styledNode {
	children := dropDirectTableWhitespace(parent.children)
	result := make([]*styledNode, 0, len(children))
	for index := 0; index < len(children); {
		if properTableChild(children[index]) {
			result = append(result, children[index])
			index++
			continue
		}
		end := index + 1
		for end < len(children) && !properTableChild(children[end]) {
			end++
		}
		group := trimTableWhitespace(children[index:end])
		if len(group) != 0 {
			row := anonymousTableNode(parent, displayTableRow, group)
			fixupTableFormattingTree(row)
			result = append(result, row)
		}
		index = end
	}
	return result
}

func fixTableRowGroupChildren(parent *styledNode) []*styledNode {
	children := dropDirectTableWhitespace(parent.children)
	result := make([]*styledNode, 0, len(children))
	for index := 0; index < len(children); {
		if children[index].style.Display() == displayTableRow {
			result = append(result, children[index])
			index++
			continue
		}
		end := index + 1
		for end < len(children) && children[end].style.Display() != displayTableRow {
			end++
		}
		group := trimTableWhitespace(children[index:end])
		if len(group) != 0 {
			row := anonymousTableNode(parent, displayTableRow, group)
			fixupTableFormattingTree(row)
			result = append(result, row)
		}
		index = end
	}
	return result
}

func fixTableRowChildren(parent *styledNode) []*styledNode {
	children := dropDirectTableWhitespace(parent.children)
	result := make([]*styledNode, 0, len(children))
	for index := 0; index < len(children); {
		if children[index].style.Display() == displayTableCell {
			result = append(result, children[index])
			index++
			continue
		}
		end := index + 1
		for end < len(children) && children[end].style.Display() != displayTableCell {
			end++
		}
		group := trimTableWhitespace(children[index:end])
		if len(group) != 0 {
			result = append(result, anonymousTableNode(parent, displayTableCell, group))
		}
		index = end
	}
	return result
}

func fixTableColumnGroupChildren(children []*styledNode) []*styledNode {
	result := make([]*styledNode, 0, len(children))
	for _, child := range children {
		if child != nil && child.style.Display() == displayTableColumn {
			result = append(result, child)
		}
	}
	return result
}

func wrapMisparentedTableChildren(parent *styledNode) []*styledNode {
	result := make([]*styledNode, 0, len(parent.children))
	for index := 0; index < len(parent.children); {
		child := parent.children[index]
		if child == nil || !tableRelatedDisplay(child.style.Display()) {
			result = append(result, child)
			index++
			continue
		}
		end := index + 1
		for end < len(parent.children) {
			candidate := parent.children[end]
			if candidate != nil && (tableRelatedDisplay(candidate.style.Display()) || tableWhitespaceNode(candidate)) {
				end++
				continue
			}
			break
		}
		group := trimTableWhitespace(parent.children[index:end])
		if len(group) != 0 {
			display := displayTable
			if parent.style.Display().Outside() == computed.DisplayOutsideInline {
				display = displayInlineTable
			}
			wrapper := anonymousTableNode(parent, display, group)
			fixupTableFormattingTree(wrapper)
			result = append(result, wrapper)
		}
		index = end
	}
	return result
}

func anonymousTableNode(parent *styledNode, display displayMode, children []*styledNode) *styledNode {
	style := computedStyle{}.WithAnonymousDisplay(display)
	if parent != nil {
		style = parent.style.WithAnonymousDisplay(display)
	}
	return &styledNode{style: style, children: append([]*styledNode(nil), children...)}
}

func properTableChild(node *styledNode) bool {
	if node == nil {
		return false
	}
	switch node.style.Display() {
	case displayTableRow, displayRowGroup, displayHeaderGroup, displayFooterGroup,
		displayColumnGroup, displayTableColumn, displayCaption:
		return true
	default:
		return false
	}
}

func tableRelatedDisplay(display displayMode) bool {
	switch display {
	case displayRowGroup, displayHeaderGroup, displayFooterGroup, displayTableRow,
		displayTableCell, displayColumnGroup, displayTableColumn, displayCaption:
		return true
	default:
		return false
	}
}

func dropDirectTableWhitespace(children []*styledNode) []*styledNode {
	result := make([]*styledNode, 0, len(children))
	for _, child := range children {
		if !tableWhitespaceNode(child) {
			result = append(result, child)
		}
	}
	return result
}

func trimTableWhitespace(children []*styledNode) []*styledNode {
	start, end := 0, len(children)
	for start < end && tableWhitespaceNode(children[start]) {
		start++
	}
	for end > start && tableWhitespaceNode(children[end-1]) {
		end--
	}
	return children[start:end]
}

func tableWhitespaceNode(node *styledNode) bool {
	if node == nil || node.node == nil || node.node.Type != dom.TextNode {
		return false
	}
	for _, character := range node.node.Data {
		switch character {
		case ' ', '\t', '\n', '\r', '\f':
		default:
			return false
		}
	}
	return true
}
