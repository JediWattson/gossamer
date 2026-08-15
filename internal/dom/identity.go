package dom

import (
	"errors"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
)

// NodeID is stable logical identity within one Document. Zero is never a
// valid node ID.
type NodeID uint32

const InvalidNodeID NodeID = 0

var (
	ErrInvalidDocument = errors.New("dom: invalid document")
	ErrUnknownNode     = errors.New("dom: unknown node id")
	ErrInvalidTree     = errors.New("dom: invalid node tree")
	ErrWrongNodeKind   = errors.New("dom: wrong node kind")
)

// Document adds stable identity and mutation versioning around the existing
// pointer-backed DOM. The renderer may continue resolving IDs to *Node during
// migration, while browser/runtime-facing APIs use NodeID.
type Document struct {
	root  NodeID
	store *NodeStore

	version atomic.Uint64
}

// NodeStore resolves logical identity in both directions. Backing nodes may
// remain pointer based during the incremental migration.
type NodeStore struct {
	mutex sync.RWMutex
	nodes []*Node
	ids   map[*Node]NodeID
}

// IndexDocument assigns deterministic pre-order IDs to an existing DOM tree.
func IndexDocument(root *Node) (*Document, error) {
	if root == nil || root.Type != DocumentNode {
		return nil, fmt.Errorf("%w: root must be a document node", ErrInvalidDocument)
	}
	ordered, err := collectSubtree(root)
	if err != nil {
		return nil, err
	}
	store := &NodeStore{nodes: make([]*Node, 0, len(ordered)), ids: make(map[*Node]NodeID, len(ordered))}
	for _, node := range ordered {
		store.assignLocked(node)
	}
	document := &Document{root: store.ids[root], store: store}
	document.version.Store(1)
	return document, nil
}

func (document *Document) RootID() NodeID {
	if document == nil {
		return InvalidNodeID
	}
	return document.root
}

func (document *Document) Root() *Node {
	if document == nil {
		return nil
	}
	node, _ := document.store.Resolve(document.root)
	return node
}

// ReadRoot holds the store's read lock while callback traverses the current
// pointer-backed tree. Page rendering uses this transitional guard so stable-ID
// mutations cannot race the legacy renderer.
func (document *Document) ReadRoot(callback func(*Node) error) error {
	if document == nil || document.store == nil {
		return ErrInvalidDocument
	}
	if callback == nil {
		return fmt.Errorf("dom: nil document reader")
	}
	document.store.mutex.RLock()
	defer document.store.mutex.RUnlock()
	root, ok := document.store.resolveLocked(document.root)
	if !ok {
		return fmt.Errorf("%w: %d", ErrUnknownNode, document.root)
	}
	return callback(root)
}

func (document *Document) Store() *NodeStore {
	if document == nil {
		return nil
	}
	return document.store
}

func (document *Document) Resolve(id NodeID) (*Node, bool) {
	if document == nil {
		return nil, false
	}
	return document.store.Resolve(id)
}

func (document *Document) ID(node *Node) (NodeID, bool) {
	if document == nil {
		return InvalidNodeID, false
	}
	return document.store.ID(node)
}

// ElementByID returns the first connected element whose id attribute exactly
// matches value. Detached nodes remain addressable by NodeID but are excluded
// from document lookup.
func (document *Document) ElementByID(value string) (NodeID, bool) {
	if document == nil || document.store == nil {
		return InvalidNodeID, false
	}
	document.store.mutex.RLock()
	defer document.store.mutex.RUnlock()
	root, ok := document.store.resolveLocked(document.root)
	if !ok {
		return InvalidNodeID, false
	}
	stack := []*Node{root}
	for len(stack) != 0 {
		last := len(stack) - 1
		node := stack[last]
		stack = stack[:last]
		if node.Type == ElementNode {
			for _, attribute := range node.Attributes {
				if attribute.Name == "id" && attribute.Value == value {
					return document.store.ids[node], true
				}
			}
		}
		for index := len(node.Children) - 1; index >= 0; index-- {
			stack = append(stack, node.Children[index])
		}
	}
	return InvalidNodeID, false
}

// Version changes after each effective mutation made through Document.
func (document *Document) Version() uint64 {
	if document == nil {
		return 0
	}
	return document.version.Load()
}

// Text returns text-node data while holding the identity store's read lock, so
// host reads cannot race stable-ID mutation.
func (document *Document) Text(id NodeID) (string, error) {
	if document == nil || document.store == nil {
		return "", ErrInvalidDocument
	}
	document.store.mutex.RLock()
	defer document.store.mutex.RUnlock()
	node, ok := document.store.resolveLocked(id)
	if !ok {
		return "", fmt.Errorf("%w: %d", ErrUnknownNode, id)
	}
	if node.Type != TextNode {
		return "", fmt.Errorf("%w: node %d is %d, want text", ErrWrongNodeKind, id, node.Type)
	}
	return node.Data, nil
}

// TextContent returns the concatenated descendant text for an element or the
// node data for text and comment nodes.
func (document *Document) TextContent(id NodeID) (string, error) {
	if document == nil || document.store == nil {
		return "", ErrInvalidDocument
	}
	document.store.mutex.RLock()
	defer document.store.mutex.RUnlock()
	node, ok := document.store.resolveLocked(id)
	if !ok {
		return "", fmt.Errorf("%w: %d", ErrUnknownNode, id)
	}
	var content strings.Builder
	stack := []*Node{node}
	for len(stack) != 0 {
		last := len(stack) - 1
		current := stack[last]
		stack = stack[:last]
		if current.Type == TextNode {
			content.WriteString(current.Data)
			continue
		}
		if current.Type == CommentNode && current == node {
			return current.Data, nil
		}
		for index := len(current.Children) - 1; index >= 0; index-- {
			stack = append(stack, current.Children[index])
		}
	}
	return content.String(), nil
}

// ClosestElement returns the nearest element ancestor, including id itself.
func (document *Document) ClosestElement(id NodeID) (NodeID, bool) {
	if document == nil || document.store == nil {
		return InvalidNodeID, false
	}
	document.store.mutex.RLock()
	defer document.store.mutex.RUnlock()
	node, ok := document.store.resolveLocked(id)
	if !ok {
		return InvalidNodeID, false
	}
	for node != nil && node.Type != ElementNode {
		node = node.Parent
	}
	if node == nil {
		return InvalidNodeID, false
	}
	element, ok := document.store.ids[node]
	return element, ok
}

// SetText changes a text node through stable identity.
func (document *Document) SetText(id NodeID, data string) error {
	if document == nil || document.store == nil {
		return ErrInvalidDocument
	}
	document.store.mutex.Lock()
	defer document.store.mutex.Unlock()
	node, ok := document.store.resolveLocked(id)
	if !ok {
		return fmt.Errorf("%w: %d", ErrUnknownNode, id)
	}
	if node.Type != TextNode {
		return fmt.Errorf("%w: node %d is %d, want text", ErrWrongNodeKind, id, node.Type)
	}
	if node.Data == data {
		return nil
	}
	node.Data = data
	document.version.Add(1)
	return nil
}

// SetTextContent replaces an element's children with one new text node, or
// updates the data of an existing text or comment node. Previously indexed
// descendants become detached but keep their stable IDs.
func (document *Document) SetTextContent(id NodeID, data string) error {
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
	case TextNode, CommentNode:
		if node.Data == data {
			return nil
		}
		node.Data = data
	case ElementNode:
		if len(node.Children) == 0 && data == "" {
			return nil
		}
		if len(node.Children) == 1 && node.Children[0].Type == TextNode && node.Children[0].Data == data {
			return nil
		}
		for _, child := range node.Children {
			child.Parent = nil
		}
		node.Children = nil
		if data != "" {
			child := NewText(data)
			child.Parent = node
			node.Children = []*Node{child}
			document.store.assignLocked(child)
		}
	default:
		return fmt.Errorf("%w: node %d is %d, want element or character data", ErrWrongNodeKind, id, node.Type)
	}
	document.version.Add(1)
	return nil
}

// AppendChild attaches a subtree and assigns IDs only to previously unseen
// nodes. Moving an already indexed subtree preserves every existing ID.
func (document *Document) AppendChild(parentID NodeID, child *Node) (NodeID, error) {
	if document == nil || document.store == nil {
		return InvalidNodeID, ErrInvalidDocument
	}
	if child == nil {
		return InvalidNodeID, fmt.Errorf("%w: nil child", ErrInvalidTree)
	}
	document.store.mutex.Lock()
	defer document.store.mutex.Unlock()
	parent, ok := document.store.resolveLocked(parentID)
	if !ok {
		return InvalidNodeID, fmt.Errorf("%w: %d", ErrUnknownNode, parentID)
	}
	for ancestor := parent; ancestor != nil; ancestor = ancestor.Parent {
		if ancestor == child {
			return InvalidNodeID, fmt.Errorf("%w: append would create a cycle", ErrInvalidTree)
		}
	}
	ordered, err := collectSubtree(child)
	if err != nil {
		return InvalidNodeID, err
	}
	known := 0
	for _, node := range ordered {
		if _, exists := document.store.ids[node]; exists {
			known++
		}
	}
	if known != 0 && known != len(ordered) {
		return InvalidNodeID, fmt.Errorf("%w: subtree mixes indexed and unindexed nodes", ErrInvalidTree)
	}

	parent.AppendChild(child)
	if known == 0 {
		for _, node := range ordered {
			document.store.assignLocked(node)
		}
	}
	document.version.Add(1)
	return document.store.ids[child], nil
}

func (store *NodeStore) Resolve(id NodeID) (*Node, bool) {
	if store == nil {
		return nil, false
	}
	store.mutex.RLock()
	node, ok := store.resolveLocked(id)
	store.mutex.RUnlock()
	return node, ok
}

func (store *NodeStore) ID(node *Node) (NodeID, bool) {
	if store == nil || node == nil {
		return InvalidNodeID, false
	}
	store.mutex.RLock()
	id, ok := store.ids[node]
	store.mutex.RUnlock()
	return id, ok
}

func (store *NodeStore) Len() int {
	if store == nil {
		return 0
	}
	store.mutex.RLock()
	length := len(store.nodes)
	store.mutex.RUnlock()
	return length
}

func (store *NodeStore) resolveLocked(id NodeID) (*Node, bool) {
	if id == InvalidNodeID || uint64(id) > uint64(len(store.nodes)) {
		return nil, false
	}
	node := store.nodes[int(id)-1]
	return node, node != nil
}

func (store *NodeStore) assignLocked(node *Node) NodeID {
	if id, exists := store.ids[node]; exists {
		return id
	}
	id := NodeID(len(store.nodes) + 1)
	store.nodes = append(store.nodes, node)
	store.ids[node] = id
	return id
}

func collectSubtree(root *Node) ([]*Node, error) {
	if root == nil {
		return nil, fmt.Errorf("%w: nil root", ErrInvalidTree)
	}
	ordered := make([]*Node, 0)
	seen := make(map[*Node]struct{})
	var visit func(*Node) error
	visit = func(node *Node) error {
		if node == nil {
			return fmt.Errorf("%w: nil child", ErrInvalidTree)
		}
		if _, exists := seen[node]; exists {
			return fmt.Errorf("%w: repeated node", ErrInvalidTree)
		}
		seen[node] = struct{}{}
		ordered = append(ordered, node)
		for _, child := range node.Children {
			if child == nil || child.Parent != node {
				return fmt.Errorf("%w: inconsistent parent link", ErrInvalidTree)
			}
			if err := visit(child); err != nil {
				return err
			}
		}
		return nil
	}
	if err := visit(root); err != nil {
		return nil, err
	}
	return ordered, nil
}
