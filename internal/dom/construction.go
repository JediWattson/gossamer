package dom

import (
	"fmt"
	"unicode/utf16"
)

// TemplateContent returns the inert DocumentFragment owned by an HTML
// template element.
func (document *Document) TemplateContent(id NodeID) (NodeID, error) {
	if document == nil || document.store == nil {
		return InvalidNodeID, ErrInvalidDocument
	}
	document.store.mutex.Lock()
	defer document.store.mutex.Unlock()
	node, ok := document.store.resolveLocked(id)
	if !ok {
		return InvalidNodeID, fmt.Errorf("%w: %d", ErrUnknownNode, id)
	}
	if node.Type != ElementNode || node.NamespaceURI != HTMLNamespace || node.Data != "template" {
		return InvalidNodeID, fmt.Errorf("%w: node %d is not a template", ErrWrongNodeKind, id)
	}
	if node.TemplateContent == nil {
		node.TemplateContent = NewDocumentFragment()
	}
	return document.store.assignLocked(node.TemplateContent), nil
}

// SplitText splits a text node at a UTF-16 code-unit offset.
func (document *Document) SplitText(id NodeID, offset int) (NodeID, error) {
	if document == nil || document.store == nil {
		return InvalidNodeID, ErrInvalidDocument
	}
	document.store.mutex.Lock()
	defer document.store.mutex.Unlock()
	node, ok := document.store.resolveLocked(id)
	if !ok {
		return InvalidNodeID, fmt.Errorf("%w: %d", ErrUnknownNode, id)
	}
	if node.Type != TextNode {
		return InvalidNodeID, fmt.Errorf("%w: node %d is not text", ErrWrongNodeKind, id)
	}
	units := utf16.Encode([]rune(node.Data))
	if offset < 0 || offset > len(units) {
		return InvalidNodeID, NewException(IndexSizeError, ErrInvalidTree, "text split offset %d is outside 0..%d", offset, len(units))
	}
	oldValue := node.Data
	node.Data = string(utf16.Decode(units[:offset]))
	remainder := NewText(string(utf16.Decode(units[offset:])))
	document.recordCharacterMutationLocked(node, oldValue)
	if node.Parent == nil {
		newID := document.store.assignLocked(remainder)
		document.version.Add(1)
		return newID, nil
	}
	parent := node.Parent
	if err := document.placeNodesLocked(parent, []*Node{remainder}, node, placeAfter); err != nil {
		return InvalidNodeID, err
	}
	return document.store.ids[remainder], nil
}

// Normalize merges adjacent text children and removes empty text nodes.
func (document *Document) Normalize(id NodeID) error {
	if document == nil || document.store == nil {
		return ErrInvalidDocument
	}
	document.store.mutex.Lock()
	defer document.store.mutex.Unlock()
	root, ok := document.store.resolveLocked(id)
	if !ok {
		return fmt.Errorf("%w: %d", ErrUnknownNode, id)
	}
	changed := false
	var normalizeParent func(*Node)
	normalizeParent = func(parent *Node) {
		before := append([]*Node(nil), parent.Children...)
		final := make([]*Node, 0, len(before))
		for index := 0; index < len(before); {
			child := before[index]
			if child.Type != TextNode {
				final = append(final, child)
				normalizeParent(child)
				index++
				continue
			}
			oldValue := child.Data
			merged := child.Data
			cursor := index + 1
			for cursor < len(before) && before[cursor].Type == TextNode {
				merged += before[cursor].Data
				cursor++
			}
			if merged != "" {
				child.Data = merged
				final = append(final, child)
			}
			if merged != oldValue {
				document.recordCharacterMutationLocked(child, oldValue)
			}
			if cursor-index > 1 || merged == "" {
				changed = true
			}
			index = cursor
		}
		if !sameNodeSlice(before, final) {
			for _, child := range before {
				if nodeIndex(final, child) < 0 {
					child.Parent = nil
				}
			}
			parent.Children = append(parent.Children[:0], final...)
			document.recordChildMutationLocked(parent, before, final, nil)
			changed = true
		}
		if parent.TemplateContent != nil {
			normalizeParent(parent.TemplateContent)
		}
	}
	normalizeParent(root)
	if changed {
		document.version.Add(1)
	}
	return nil
}

// AdoptNode detaches and returns a node already owned by this document.
func (document *Document) AdoptNode(id NodeID) (NodeID, error) {
	if document == nil || document.store == nil {
		return InvalidNodeID, ErrInvalidDocument
	}
	document.store.mutex.Lock()
	defer document.store.mutex.Unlock()
	node, ok := document.store.resolveLocked(id)
	if !ok {
		return InvalidNodeID, fmt.Errorf("%w: %d", ErrUnknownNode, id)
	}
	if node.Type == DocumentNode {
		return InvalidNodeID, NewException(NotFoundError, ErrWrongNodeKind, "a Document cannot be adopted")
	}
	if node.Parent != nil {
		if err := document.removeChildLocked(node.Parent, node); err != nil {
			return InvalidNodeID, err
		}
	}
	return id, nil
}
