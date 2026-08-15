package dom

import (
	"fmt"
	"strings"
)

// NodeRelation identifies one stable traversal edge from a node.
type NodeRelation uint8

const (
	ParentNode NodeRelation = iota + 1
	ParentElement
	FirstChild
	LastChild
	PreviousSibling
	NextSibling
	FirstElementChild
	LastElementChild
	PreviousElementSibling
	NextElementSibling
	DocumentElement
	DocumentHead
	DocumentBody
)

// NodeSnapshot contains the immutable metadata needed to expose a DOM node to
// a JavaScript engine. Connected reports whether the node reaches this
// document's root through parent edges.
type NodeSnapshot struct {
	Type         NodeType
	Data         string
	Target       string
	NamespaceURI string
	Prefix       string
	Connected    bool
}

// Snapshot returns node metadata without exposing its backing pointer.
func (document *Document) Snapshot(id NodeID) (NodeSnapshot, error) {
	if document == nil || document.store == nil {
		return NodeSnapshot{}, ErrInvalidDocument
	}
	document.store.mutex.RLock()
	defer document.store.mutex.RUnlock()
	node, ok := document.store.resolveLocked(id)
	if !ok {
		return NodeSnapshot{}, fmt.Errorf("%w: %d", ErrUnknownNode, id)
	}
	root, ok := document.store.resolveLocked(document.root)
	if !ok {
		return NodeSnapshot{}, fmt.Errorf("%w: %d", ErrUnknownNode, document.root)
	}
	connected := false
	for ancestor := node; ancestor != nil; ancestor = ancestor.Parent {
		if ancestor == root {
			connected = true
			break
		}
	}
	return NodeSnapshot{
		Type:         node.Type,
		Data:         node.Data,
		Target:       node.Target,
		NamespaceURI: node.NamespaceURI,
		Prefix:       node.Prefix,
		Connected:    connected,
	}, nil
}

// RelatedNode resolves one traversal edge while preserving stable identity.
func (document *Document) RelatedNode(id NodeID, relation NodeRelation) (NodeID, bool, error) {
	if document == nil || document.store == nil {
		return InvalidNodeID, false, ErrInvalidDocument
	}
	document.store.mutex.RLock()
	defer document.store.mutex.RUnlock()
	node, ok := document.store.resolveLocked(id)
	if !ok {
		return InvalidNodeID, false, fmt.Errorf("%w: %d", ErrUnknownNode, id)
	}

	var related *Node
	switch relation {
	case ParentNode:
		related = node.Parent
	case ParentElement:
		if node.Parent != nil && node.Parent.Type == ElementNode {
			related = node.Parent
		}
	case FirstChild:
		if len(node.Children) != 0 {
			related = node.Children[0]
		}
	case LastChild:
		if len(node.Children) != 0 {
			related = node.Children[len(node.Children)-1]
		}
	case PreviousSibling:
		related = sibling(node, -1, false)
	case NextSibling:
		related = sibling(node, 1, false)
	case FirstElementChild:
		related = elementChild(node.Children, false)
	case LastElementChild:
		related = elementChild(node.Children, true)
	case PreviousElementSibling:
		related = sibling(node, -1, true)
	case NextElementSibling:
		related = sibling(node, 1, true)
	case DocumentElement:
		if node.Type == DocumentNode {
			related = elementChild(node.Children, false)
		}
	case DocumentHead, DocumentBody:
		if node.Type == DocumentNode {
			documentElement := elementChild(node.Children, false)
			if documentElement != nil {
				name := "head"
				if relation == DocumentBody {
					name = "body"
				}
				for _, child := range documentElement.Children {
					if child.Type == ElementNode && child.NamespaceURI == HTMLNamespace && strings.EqualFold(child.Data, name) {
						related = child
						break
					}
				}
			}
		}
	default:
		return InvalidNodeID, false, fmt.Errorf("dom: unknown node relation %d", relation)
	}
	if related == nil {
		return InvalidNodeID, false, nil
	}
	relatedID, ok := document.store.ids[related]
	if !ok {
		return InvalidNodeID, false, fmt.Errorf("%w: related node is not indexed", ErrInvalidTree)
	}
	return relatedID, true, nil
}

// ChildNodes returns direct children in tree order. elementsOnly filters out
// all non-element children.
func (document *Document) ChildNodes(id NodeID, elementsOnly bool) ([]NodeID, error) {
	if document == nil || document.store == nil {
		return nil, ErrInvalidDocument
	}
	document.store.mutex.RLock()
	defer document.store.mutex.RUnlock()
	node, ok := document.store.resolveLocked(id)
	if !ok {
		return nil, fmt.Errorf("%w: %d", ErrUnknownNode, id)
	}
	result := make([]NodeID, 0, len(node.Children))
	for _, child := range node.Children {
		if elementsOnly && child.Type != ElementNode {
			continue
		}
		childID, exists := document.store.ids[child]
		if !exists {
			return nil, fmt.Errorf("%w: child node is not indexed", ErrInvalidTree)
		}
		result = append(result, childID)
	}
	return result, nil
}

// Contains reports whether other is the node itself or one of its descendants.
func (document *Document) Contains(id, other NodeID) (bool, error) {
	if document == nil || document.store == nil {
		return false, ErrInvalidDocument
	}
	document.store.mutex.RLock()
	defer document.store.mutex.RUnlock()
	node, ok := document.store.resolveLocked(id)
	if !ok {
		return false, fmt.Errorf("%w: %d", ErrUnknownNode, id)
	}
	otherNode, ok := document.store.resolveLocked(other)
	if !ok {
		return false, fmt.Errorf("%w: %d", ErrUnknownNode, other)
	}
	for ancestor := otherNode; ancestor != nil; ancestor = ancestor.Parent {
		if ancestor == node {
			return true, nil
		}
	}
	return false, nil
}

// ReplaceChild replaces a direct child with another indexed node. The removed
// subtree remains indexed and detached so existing wrappers retain identity.
func (document *Document) ReplaceChild(parentID, childID, replacedID NodeID) error {
	if document == nil || document.store == nil {
		return ErrInvalidDocument
	}
	document.store.mutex.Lock()
	defer document.store.mutex.Unlock()
	parent, ok := document.store.resolveLocked(parentID)
	if !ok {
		return fmt.Errorf("%w: %d", ErrUnknownNode, parentID)
	}
	child, ok := document.store.resolveLocked(childID)
	if !ok {
		return fmt.Errorf("%w: %d", ErrUnknownNode, childID)
	}
	replaced, ok := document.store.resolveLocked(replacedID)
	if !ok {
		return fmt.Errorf("%w: %d", ErrUnknownNode, replacedID)
	}
	if replaced.Parent != parent {
		return fmt.Errorf("%w: node %d is not a child of %d", ErrInvalidTree, replacedID, parentID)
	}
	if child == replaced {
		return nil
	}
	if parent.Type != ElementNode && parent.Type != DocumentNode && parent.Type != DocumentFragmentNode {
		return fmt.Errorf("%w: node %d cannot have children", ErrWrongNodeKind, parentID)
	}
	for ancestor := parent; ancestor != nil; ancestor = ancestor.Parent {
		if ancestor == child {
			return fmt.Errorf("%w: replacement would create a cycle", ErrInvalidTree)
		}
	}
	if child.Type == DocumentFragmentNode {
		children := append([]*Node(nil), child.Children...)
		for _, candidate := range children {
			for ancestor := parent; ancestor != nil; ancestor = ancestor.Parent {
				if ancestor == candidate {
					return fmt.Errorf("%w: fragment replacement would create a cycle", ErrInvalidTree)
				}
			}
		}
		index := childIndex(parent, replaced)
		if index == len(parent.Children) {
			return fmt.Errorf("%w: replacement node disappeared", ErrInvalidTree)
		}
		updated := make([]*Node, 0, len(parent.Children)-1+len(children))
		updated = append(updated, parent.Children[:index]...)
		updated = append(updated, children...)
		updated = append(updated, parent.Children[index+1:]...)
		parent.Children = updated
		child.Children = nil
		for _, candidate := range children {
			candidate.Parent = parent
		}
		replaced.Parent = nil
		document.version.Add(1)
		return nil
	}
	if child.Parent != nil {
		child.Parent.removeChild(child)
	}
	index := childIndex(parent, replaced)
	if index == len(parent.Children) {
		return fmt.Errorf("%w: replacement node disappeared", ErrInvalidTree)
	}
	parent.Children[index] = child
	child.Parent = parent
	replaced.Parent = nil
	document.version.Add(1)
	return nil
}

// NodeValue implements the nullable character-data view used by JavaScript.
func (document *Document) NodeValue(id NodeID) (string, bool, error) {
	if document == nil || document.store == nil {
		return "", false, ErrInvalidDocument
	}
	document.store.mutex.RLock()
	defer document.store.mutex.RUnlock()
	node, ok := document.store.resolveLocked(id)
	if !ok {
		return "", false, fmt.Errorf("%w: %d", ErrUnknownNode, id)
	}
	switch node.Type {
	case TextNode, CommentNode, ProcessingInstructionNode:
		return node.Data, true, nil
	default:
		return "", false, nil
	}
}

// SetNodeValue updates character data and is a no-op for other node kinds.
func (document *Document) SetNodeValue(id NodeID, value string) error {
	if document == nil || document.store == nil {
		return ErrInvalidDocument
	}
	document.store.mutex.Lock()
	defer document.store.mutex.Unlock()
	node, ok := document.store.resolveLocked(id)
	if !ok {
		return fmt.Errorf("%w: %d", ErrUnknownNode, id)
	}
	switch node.Type {
	case TextNode, CommentNode, ProcessingInstructionNode:
		if node.Data != value {
			node.Data = value
			document.version.Add(1)
		}
	}
	return nil
}

// HasAttribute reports whether an element contains the named content
// attribute, including an explicitly empty attribute.
func (document *Document) HasAttribute(id NodeID, name string) (bool, error) {
	_, found, err := document.GetAttribute(id, strings.ToLower(name))
	return found, err
}

func elementChild(children []*Node, reverse bool) *Node {
	if reverse {
		for index := len(children) - 1; index >= 0; index-- {
			if children[index].Type == ElementNode {
				return children[index]
			}
		}
		return nil
	}
	for _, child := range children {
		if child.Type == ElementNode {
			return child
		}
	}
	return nil
}

func sibling(node *Node, direction int, elementsOnly bool) *Node {
	if node.Parent == nil {
		return nil
	}
	index := childIndex(node.Parent, node)
	for index += direction; index >= 0 && index < len(node.Parent.Children); index += direction {
		candidate := node.Parent.Children[index]
		if !elementsOnly || candidate.Type == ElementNode {
			return candidate
		}
	}
	return nil
}
