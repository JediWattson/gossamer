package dom

import (
	"errors"
	"fmt"
)

// ExceptionName is one of the stable names exposed by the Web DOMException
// interface. The Go DOM keeps the underlying sentinel error so internal
// callers can continue using errors.Is while the V8 adapter receives the
// browser-facing exception name.
type ExceptionName string

const (
	HierarchyRequestError ExceptionName = "HierarchyRequestError"
	NotFoundError         ExceptionName = "NotFoundError"
	InvalidCharacterError ExceptionName = "InvalidCharacterError"
	NamespaceError        ExceptionName = "NamespaceError"
	SyntaxError           ExceptionName = "SyntaxError"
	InvalidStateError     ExceptionName = "InvalidStateError"
)

// Exception is a typed DOM failure. Cause preserves the lower-level package
// error used by existing Go callers and tests.
type Exception struct {
	Name    ExceptionName
	Message string
	Cause   error
}

func (exception *Exception) Error() string {
	if exception == nil {
		return ""
	}
	return exception.Message
}

func (exception *Exception) Unwrap() error {
	if exception == nil {
		return nil
	}
	return exception.Cause
}

// NewException creates a browser-facing DOM failure while retaining cause for
// Go error classification.
func NewException(name ExceptionName, cause error, format string, arguments ...any) error {
	return &Exception{Name: name, Message: fmt.Sprintf(format, arguments...), Cause: cause}
}

// ErrorExceptionName extracts the browser-facing DOMException name, if any.
func ErrorExceptionName(err error) (ExceptionName, bool) {
	var exception *Exception
	if !errors.As(err, &exception) || exception == nil || exception.Name == "" {
		return "", false
	}
	return exception.Name, true
}

// MutationOperation identifies the ParentNode and ChildNode convenience
// algorithms exposed through JavaScript.
type MutationOperation uint8

const (
	MutationAppend MutationOperation = iota + 1
	MutationPrepend
	MutationBefore
	MutationAfter
	MutationReplaceWith
	MutationReplaceChildren
	MutationRemove
)

// Mutate applies one convenience mutation atomically. All arguments must
// already belong to document. Duplicate nodes are collapsed to their final
// occurrence, matching the fragment conversion performed by the DOM standard.
func (document *Document) Mutate(receiverID NodeID, operation MutationOperation, nodeIDs []NodeID) error {
	if document == nil || document.store == nil {
		return ErrInvalidDocument
	}
	document.store.mutex.Lock()
	defer document.store.mutex.Unlock()

	receiver, ok := document.store.resolveLocked(receiverID)
	if !ok {
		return fmt.Errorf("%w: %d", ErrUnknownNode, receiverID)
	}
	nodes, err := document.resolveMutationNodesLocked(nodeIDs)
	if err != nil {
		return err
	}

	switch operation {
	case MutationAppend, MutationPrepend, MutationReplaceChildren:
		if err := requireParentNode(receiver, receiverID); err != nil {
			return err
		}
	case MutationBefore, MutationAfter, MutationReplaceWith, MutationRemove:
		if receiver.Parent == nil {
			return nil
		}
	default:
		return NewException(SyntaxError, ErrInvalidTree, "unknown DOM mutation operation %d", operation)
	}

	if operation == MutationRemove {
		return document.removeChildLocked(receiver.Parent, receiver)
	}

	parent := receiver
	if operation == MutationBefore || operation == MutationAfter || operation == MutationReplaceWith {
		parent = receiver.Parent
	}

	placement := placeAppend
	switch operation {
	case MutationAppend:
		placement = placeAppend
	case MutationPrepend:
		placement = placePrepend
	case MutationBefore:
		placement = placeBefore
	case MutationAfter:
		placement = placeAfter
	case MutationReplaceWith:
		placement = placeReplace
	case MutationReplaceChildren:
		placement = placeReplaceAll
	}
	return document.placeNodesLocked(parent, nodes, receiver, placement)
}

type childPlacement uint8

const (
	placeAppend childPlacement = iota + 1
	placePrepend
	placeBefore
	placeAfter
	placeReplace
	placeReplaceAll
)

// placeNodesLocked is the single mutation commit path. It constructs and
// validates the complete destination child list before changing any parent
// pointer, making hierarchy failures atomic.
func (document *Document) placeNodesLocked(parent *Node, nodes []*Node, anchor *Node, placement childPlacement) error {
	group := deduplicateNodes(nodes)
	remove := make(map[*Node]struct{}, len(group)+1)
	for _, node := range group {
		remove[node] = struct{}{}
	}
	if placement == placeReplace {
		remove[anchor] = struct{}{}
	}

	remaining := make([]*Node, 0, len(parent.Children))
	insertionIndex := 0
	anchorSeen := placement == placeAppend || placement == placePrepend || placement == placeReplaceAll
	for _, child := range parent.Children {
		isAnchor := child == anchor
		_, excluded := remove[child]
		if placement == placeReplaceAll {
			excluded = true
		}
		if isAnchor && !anchorSeen {
			switch placement {
			case placeBefore, placeReplace:
				insertionIndex = len(remaining)
				anchorSeen = true
			case placeAfter:
				if !excluded {
					remaining = append(remaining, child)
				}
				insertionIndex = len(remaining)
				anchorSeen = true
				continue
			}
		}
		if !excluded {
			remaining = append(remaining, child)
		}
	}
	if !anchorSeen {
		return NewException(NotFoundError, ErrInvalidTree, "mutation target is not a child of its parent")
	}
	if placement == placeAppend {
		insertionIndex = len(remaining)
	} else if placement == placePrepend || placement == placeReplaceAll {
		insertionIndex = 0
	}

	final := make([]*Node, 0, len(remaining)+len(group))
	final = append(final, remaining[:insertionIndex]...)
	final = append(final, group...)
	final = append(final, remaining[insertionIndex:]...)
	if err := validateFinalChildren(parent, final); err != nil {
		return err
	}
	if err := validateNoCycles(parent, group); err != nil {
		return err
	}
	if err := document.ensureMutationNodesIndexedLocked(group); err != nil {
		return err
	}

	changed := !sameNodeSlice(parent.Children, final)
	for _, node := range group {
		if node.Parent != parent {
			changed = true
		}
	}
	if !changed {
		return nil
	}
	document.commitChildrenLocked(parent, final)
	document.version.Add(1)
	return nil
}

func (document *Document) ensureMutationNodesIndexedLocked(nodes []*Node) error {
	var ordered []*Node
	known := 0
	for _, root := range nodes {
		subtree, err := collectSubtree(root)
		if err != nil {
			return err
		}
		ordered = append(ordered, subtree...)
		for _, node := range subtree {
			if _, exists := document.store.ids[node]; exists {
				known++
			}
		}
	}
	if known != 0 && known != len(ordered) {
		return NewException(HierarchyRequestError, ErrInvalidTree, "mutation mixes indexed and unindexed nodes")
	}
	if known == 0 {
		for _, node := range ordered {
			document.store.assignLocked(node)
		}
	}
	return nil
}

func (document *Document) removeChildLocked(parent, child *Node) error {
	if parent == nil || child == nil || child.Parent != parent {
		return NewException(NotFoundError, ErrInvalidTree, "the node to remove is not a child of this node")
	}
	parent.removeChild(child)
	child.Parent = nil
	document.version.Add(1)
	return nil
}

func (document *Document) resolveMutationNodesLocked(ids []NodeID) ([]*Node, error) {
	nodes := make([]*Node, len(ids))
	for index, id := range ids {
		node, ok := document.store.resolveLocked(id)
		if !ok {
			return nil, fmt.Errorf("%w: %d", ErrUnknownNode, id)
		}
		nodes[index] = node
	}
	return nodes, nil
}

func deduplicateNodes(nodes []*Node) []*Node {
	last := make(map[*Node]int, len(nodes))
	for index, node := range nodes {
		last[node] = index
	}
	result := make([]*Node, 0, len(last))
	for index, node := range nodes {
		if last[node] == index {
			result = append(result, node)
		}
	}
	return result
}

func requireParentNode(node *Node, id NodeID) error {
	if node == nil || (node.Type != DocumentNode && node.Type != DocumentFragmentNode && node.Type != ElementNode) {
		return NewException(HierarchyRequestError, ErrWrongNodeKind, "node %d cannot have children", id)
	}
	return nil
}

func validateNoCycles(parent *Node, nodes []*Node) error {
	for _, node := range nodes {
		for ancestor := parent; ancestor != nil; ancestor = ancestor.Parent {
			if ancestor == node {
				return NewException(HierarchyRequestError, ErrInvalidTree, "mutation would create a cycle")
			}
		}
	}
	return nil
}

func validateFinalChildren(parent *Node, children []*Node) error {
	if err := requireParentNode(parent, InvalidNodeID); err != nil {
		return err
	}
	if parent.Type != DocumentNode {
		for _, child := range children {
			switch child.Type {
			case ElementNode, TextNode, CommentNode, ProcessingInstructionNode:
			default:
				return NewException(HierarchyRequestError, ErrInvalidTree, "node type %d cannot be inserted here", child.Type)
			}
		}
		return nil
	}

	elements := 0
	doctypes := 0
	elementIndex := -1
	doctypeIndex := -1
	for index, child := range children {
		switch child.Type {
		case ElementNode:
			elements++
			elementIndex = index
		case DoctypeNode:
			doctypes++
			doctypeIndex = index
		case CommentNode, ProcessingInstructionNode:
		case TextNode, DocumentNode, DocumentFragmentNode:
			return NewException(HierarchyRequestError, ErrInvalidTree, "node type %d cannot be inserted into a document", child.Type)
		default:
			return NewException(HierarchyRequestError, ErrInvalidTree, "unknown node type %d", child.Type)
		}
	}
	if elements > 1 {
		return NewException(HierarchyRequestError, ErrInvalidTree, "a document cannot contain more than one document element")
	}
	if doctypes > 1 {
		return NewException(HierarchyRequestError, ErrInvalidTree, "a document cannot contain more than one doctype")
	}
	if doctypeIndex >= 0 && elementIndex >= 0 && doctypeIndex > elementIndex {
		return NewException(HierarchyRequestError, ErrInvalidTree, "a document doctype must precede the document element")
	}
	return nil
}

func (document *Document) commitChildrenLocked(parent *Node, final []*Node) {
	keep := make(map[*Node]struct{}, len(final))
	for _, node := range final {
		keep[node] = struct{}{}
		if node.Parent != nil && node.Parent != parent {
			node.Parent.removeChild(node)
		}
	}
	for _, child := range parent.Children {
		if _, retained := keep[child]; !retained {
			child.Parent = nil
		}
	}
	parent.Children = append(parent.Children[:0], final...)
	for _, child := range parent.Children {
		child.Parent = parent
	}
}

func sameNodeSlice(first, second []*Node) bool {
	if len(first) != len(second) {
		return false
	}
	for index := range first {
		if first[index] != second[index] {
			return false
		}
	}
	return true
}
