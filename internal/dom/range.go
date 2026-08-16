package dom

import (
	"fmt"
	"unicode/utf16"
)

// RangeContentOperation identifies the native contents algorithm requested by
// a V8 Range facade.
type RangeContentOperation uint8

const (
	RangeCloneContents RangeContentOperation = iota + 1
	RangeExtractContents
	RangeDeleteContents
)

type boundaryPoint struct {
	container *Node
	offset    int
}

// RangeContents applies cloneContents, extractContents, or deleteContents to
// two DOM boundary points. Returned fragments and every partial clone receive
// stable IDs before crossing the browser/V8 boundary.
func (document *Document) RangeContents(
	startID NodeID,
	startOffset int,
	endID NodeID,
	endOffset int,
	operation RangeContentOperation,
) (NodeID, error) {
	if document == nil || document.store == nil {
		return InvalidNodeID, ErrInvalidDocument
	}
	if operation < RangeCloneContents || operation > RangeDeleteContents {
		return InvalidNodeID, NewException(InvalidStateError, ErrInvalidTree, "unknown Range contents operation %d", operation)
	}
	document.store.mutex.Lock()
	defer document.store.mutex.Unlock()

	startContainer, ok := document.store.resolveLocked(startID)
	if !ok {
		return InvalidNodeID, fmt.Errorf("%w: %d", ErrUnknownNode, startID)
	}
	endContainer, ok := document.store.resolveLocked(endID)
	if !ok {
		return InvalidNodeID, fmt.Errorf("%w: %d", ErrUnknownNode, endID)
	}
	start := boundaryPoint{container: startContainer, offset: startOffset}
	end := boundaryPoint{container: endContainer, offset: endOffset}
	if err := validateBoundaryPoint(start); err != nil {
		return InvalidNodeID, err
	}
	if err := validateBoundaryPoint(end); err != nil {
		return InvalidNodeID, err
	}
	if rangeRoot(startContainer) != rangeRoot(endContainer) {
		return InvalidNodeID, NewException(InvalidStateError, ErrInvalidTree, "Range boundaries do not share a tree")
	}
	order, err := compareBoundaryPoints(start, end)
	if err != nil {
		return InvalidNodeID, err
	}
	if order > 0 {
		return InvalidNodeID, NewException(InvalidStateError, ErrInvalidTree, "Range start follows its end")
	}

	var fragment *Node
	fragmentID := InvalidNodeID
	if operation != RangeDeleteContents {
		fragment = NewDocumentFragment()
		fragmentID = document.store.assignLocked(fragment)
	}
	if order == 0 {
		return fragmentID, nil
	}

	changedParents := make(map[*Node][]*Node)
	markParent := func(parent *Node) {
		if _, marked := changedParents[parent]; !marked {
			changedParents[parent] = append([]*Node(nil), parent.Children...)
		}
	}
	changed := false
	prependIndexed := func(parent, child *Node) error {
		ordered, collectErr := collectSubtree(child)
		if collectErr != nil {
			return collectErr
		}
		for _, node := range ordered {
			document.store.assignLocked(node)
		}
		if child.Parent != nil {
			child.Parent.removeChild(child)
		}
		child.Parent = parent
		parent.Children = append([]*Node{child}, parent.Children...)
		return nil
	}

	var processParent func(*Node, *Node) error
	processParent = func(parent, output *Node) error {
		children := append([]*Node(nil), parent.Children...)
		for index := len(children) - 1; index >= 0; index-- {
			child := children[index]
			before := boundaryPoint{container: parent, offset: index}
			after := boundaryPoint{container: parent, offset: index + 1}
			startBeforeOrAt, compareErr := compareBoundaryPoints(start, before)
			if compareErr != nil {
				return compareErr
			}
			afterBeforeOrAtEnd, compareErr := compareBoundaryPoints(after, end)
			if compareErr != nil {
				return compareErr
			}
			fullyContained := startBeforeOrAt <= 0 && afterBeforeOrAtEnd <= 0
			afterStart, compareErr := compareBoundaryPoints(after, start)
			if compareErr != nil {
				return compareErr
			}
			beforeEnd, compareErr := compareBoundaryPoints(before, end)
			if compareErr != nil {
				return compareErr
			}
			intersects := afterStart > 0 && beforeEnd < 0
			if !intersects {
				continue
			}

			if fullyContained {
				switch operation {
				case RangeCloneContents:
					if err := prependIndexed(output, cloneRangeNode(child, true)); err != nil {
						return err
					}
				case RangeExtractContents:
					markParent(parent)
					if err := prependIndexed(output, child); err != nil {
						return err
					}
					changed = true
				case RangeDeleteContents:
					markParent(parent)
					parent.removeChild(child)
					child.Parent = nil
					changed = true
				}
				continue
			}

			switch child.Type {
			case TextNode, CommentNode, ProcessingInstructionNode:
				units := utf16.Encode([]rune(child.Data))
				from := 0
				to := len(units)
				if start.container == child {
					from = start.offset
				}
				if end.container == child {
					to = end.offset
				}
				if from < 0 {
					from = 0
				}
				if to > len(units) {
					to = len(units)
				}
				if to < from {
					to = from
				}
				selected := string(utf16.Decode(units[from:to]))
				if output != nil && selected != "" {
					partial := cloneRangeNode(child, false)
					partial.Data = selected
					if err := prependIndexed(output, partial); err != nil {
						return err
					}
				}
				if operation != RangeCloneContents && from != to {
					oldValue := child.Data
					child.Data = string(utf16.Decode(append(append([]uint16(nil), units[:from]...), units[to:]...)))
					document.recordCharacterMutationLocked(child, oldValue)
					changed = true
				}
			default:
				var partial *Node
				if output != nil {
					partial = cloneRangeNode(child, false)
					document.store.assignLocked(partial)
					partial.Parent = output
					output.Children = append([]*Node{partial}, output.Children...)
				}
				if err := processParent(child, partial); err != nil {
					return err
				}
			}
		}
		return nil
	}

	if startContainer == endContainer && isCharacterData(startContainer) {
		units := utf16.Encode([]rune(startContainer.Data))
		selected := string(utf16.Decode(units[startOffset:endOffset]))
		if fragment != nil && selected != "" {
			partial := cloneRangeNode(startContainer, false)
			partial.Data = selected
			if err := prependIndexed(fragment, partial); err != nil {
				return InvalidNodeID, err
			}
		}
		if operation != RangeCloneContents && startOffset != endOffset {
			oldValue := startContainer.Data
			startContainer.Data = string(utf16.Decode(append(append([]uint16(nil), units[:startOffset]...), units[endOffset:]...)))
			document.recordCharacterMutationLocked(startContainer, oldValue)
			changed = true
		}
	} else {
		common := commonRangeAncestor(startContainer, endContainer)
		if common == nil {
			return InvalidNodeID, NewException(InvalidStateError, ErrInvalidTree, "Range has no common ancestor")
		}
		if err := processParent(common, fragment); err != nil {
			return InvalidNodeID, err
		}
	}

	for parent, before := range changedParents {
		document.recordChildMutationLocked(parent, before, parent.Children, nil)
	}
	if changed {
		document.version.Add(1)
	}
	return fragmentID, nil
}

func validateBoundaryPoint(point boundaryPoint) error {
	if point.container == nil {
		return NewException(InvalidStateError, ErrInvalidTree, "Range boundary has no container")
	}
	limit := len(point.container.Children)
	if isCharacterData(point.container) {
		limit = len(utf16.Encode([]rune(point.container.Data)))
	}
	if point.offset < 0 || point.offset > limit {
		return NewException(IndexSizeError, ErrInvalidTree, "Range boundary offset %d is outside 0..%d", point.offset, limit)
	}
	return nil
}

func isCharacterData(node *Node) bool {
	return node != nil && (node.Type == TextNode || node.Type == CommentNode || node.Type == ProcessingInstructionNode)
}

func rangeRoot(node *Node) *Node {
	for node != nil && node.Parent != nil {
		node = node.Parent
	}
	return node
}

func commonRangeAncestor(first, second *Node) *Node {
	ancestors := make(map[*Node]struct{})
	for node := first; node != nil; node = node.Parent {
		ancestors[node] = struct{}{}
	}
	for node := second; node != nil; node = node.Parent {
		if _, found := ancestors[node]; found {
			return node
		}
	}
	return nil
}

func compareBoundaryPoints(first, second boundaryPoint) (int, error) {
	if first.container == second.container {
		switch {
		case first.offset < second.offset:
			return -1, nil
		case first.offset > second.offset:
			return 1, nil
		default:
			return 0, nil
		}
	}
	if rangeRoot(first.container) != rangeRoot(second.container) {
		return 0, NewException(InvalidStateError, ErrInvalidTree, "cannot compare boundary points from different trees")
	}
	if child := childBelowAncestor(first.container, second.container); child != nil {
		if first.offset <= nodeIndex(first.container.Children, child) {
			return -1, nil
		}
		return 1, nil
	}
	if child := childBelowAncestor(second.container, first.container); child != nil {
		if second.offset <= nodeIndex(second.container.Children, child) {
			return 1, nil
		}
		return -1, nil
	}
	common := commonRangeAncestor(first.container, second.container)
	if common == nil {
		return 0, NewException(InvalidStateError, ErrInvalidTree, "boundary points have no common ancestor")
	}
	firstChild := childBelowAncestor(common, first.container)
	secondChild := childBelowAncestor(common, second.container)
	if nodeIndex(common.Children, firstChild) < nodeIndex(common.Children, secondChild) {
		return -1, nil
	}
	return 1, nil
}

func childBelowAncestor(ancestor, descendant *Node) *Node {
	if ancestor == nil || descendant == nil || ancestor == descendant {
		return nil
	}
	child := descendant
	for child.Parent != nil && child.Parent != ancestor {
		child = child.Parent
	}
	if child.Parent != ancestor {
		return nil
	}
	return child
}

func cloneRangeNode(node *Node, deep bool) *Node {
	clone := &Node{
		Type:         node.Type,
		Data:         node.Data,
		Target:       node.Target,
		NamespaceURI: node.NamespaceURI,
		Prefix:       node.Prefix,
		Attributes:   append([]Attribute(nil), node.Attributes...),
	}
	if node.Control != nil {
		state := *node.Control
		state.UserValidity = false
		state.UserInteracted = false
		clone.Control = &state
	}
	if node.TemplateContent != nil {
		clone.TemplateContent = NewDocumentFragment()
		if deep {
			for _, child := range node.TemplateContent.Children {
				clone.TemplateContent.AppendChild(cloneRangeNode(child, true))
			}
		}
	}
	if deep {
		for _, child := range node.Children {
			clone.AppendChild(cloneRangeNode(child, true))
		}
	}
	return clone
}
