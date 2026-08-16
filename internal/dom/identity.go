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

// DocumentIdentity is an opaque process-local token that distinguishes two
// indexed Documents even when their NodeIDs and mutation versions coincide.
// Its representation is private so callers can compare but not forge tokens.
type DocumentIdentity struct {
	value uint64
}

var nextDocumentIdentity atomic.Uint64

var (
	ErrInvalidDocument = errors.New("dom: invalid document")
	ErrUnknownNode     = errors.New("dom: unknown node id")
	ErrInvalidTree     = errors.New("dom: invalid node tree")
	ErrWrongNodeKind   = errors.New("dom: wrong node kind")
	ErrInvalidName     = errors.New("dom: invalid name")
	ErrNamespace       = errors.New("dom: invalid namespace")
	ErrExpiredReadView = errors.New("dom: read view has expired")
)

// Document adds stable identity and mutation versioning around the existing
// pointer-backed DOM. The renderer may continue resolving IDs to *Node during
// migration, while browser/runtime-facing APIs use NodeID.
type Document struct {
	identity DocumentIdentity
	root     NodeID
	store    *NodeStore

	version atomic.Uint64

	mutationSequence uint64
	mutations        []MutationRecord
}

// NodeStore resolves logical identity in both directions. Backing nodes may
// remain pointer based during the incremental migration.
type NodeStore struct {
	mutex sync.RWMutex
	nodes []*Node
	ids   map[*Node]NodeID
}

// ReadView is a callback-scoped capability for acquiring coherent raw DOM
// access. WithReadView revokes the capability before releasing the NodeStore
// read lock. An access acquired before revocation keeps that lock effective
// until the access is closed, even if it is used by another goroutine.
type ReadView struct {
	lease *readViewLease
}

type readViewLease struct {
	mutex    sync.RWMutex
	active   bool
	identity DocumentIdentity
	store    *NodeStore
	root     NodeID
	version  uint64
}

// ReadAccess keeps one coherent raw-node read alive. Close is idempotent and
// must be called; methods fail safely after Close. A separate method guard
// makes Close safe to race with readers of the same access.
type ReadAccess struct {
	mutex  sync.RWMutex
	lease  *readViewLease
	closed bool
}

// Acquire starts one coherent raw-node access. It fails after the callback
// supplied to WithReadView has returned or begun returning.
func (view ReadView) Acquire() (*ReadAccess, error) {
	if view.lease == nil {
		return nil, ErrExpiredReadView
	}
	view.lease.mutex.RLock()
	if !view.lease.active {
		view.lease.mutex.RUnlock()
		return nil, ErrExpiredReadView
	}
	return &ReadAccess{lease: view.lease}, nil
}

// Close ends access and permits WithReadView to release the document read
// lock. It may be called more than once.
func (access *ReadAccess) Close() {
	if access == nil {
		return
	}
	access.mutex.Lock()
	if !access.closed {
		access.closed = true
		lease := access.lease
		access.lease = nil
		if lease != nil {
			lease.mutex.RUnlock()
		}
	}
	access.mutex.Unlock()
}

// Identity returns the opaque indexed-Document identity for this access.
func (access *ReadAccess) Identity() DocumentIdentity {
	if access == nil {
		return DocumentIdentity{}
	}
	access.mutex.RLock()
	defer access.mutex.RUnlock()
	if access.closed || access.lease == nil {
		return DocumentIdentity{}
	}
	return access.lease.identity
}

// Root returns the document node for this coherent access.
func (access *ReadAccess) Root() *Node {
	if access == nil {
		return nil
	}
	access.mutex.RLock()
	defer access.mutex.RUnlock()
	if access.closed || access.lease == nil || access.lease.store == nil {
		return nil
	}
	root, _ := access.lease.store.resolveLocked(access.lease.root)
	return root
}

// Version returns the document mutation version captured for this access.
func (access *ReadAccess) Version() uint64 {
	if access == nil {
		return 0
	}
	access.mutex.RLock()
	defer access.mutex.RUnlock()
	if access.closed || access.lease == nil {
		return 0
	}
	return access.lease.version
}

// ID resolves a backing node to stable logical identity without reacquiring
// the NodeStore read lock.
func (access *ReadAccess) ID(node *Node) (NodeID, bool) {
	if access == nil || node == nil {
		return InvalidNodeID, false
	}
	access.mutex.RLock()
	defer access.mutex.RUnlock()
	if access.closed || access.lease == nil || access.lease.store == nil {
		return InvalidNodeID, false
	}
	id, ok := access.lease.store.ids[node]
	return id, ok
}

// Resolve resolves stable logical identity to its backing node without
// reacquiring the NodeStore read lock.
func (access *ReadAccess) Resolve(id NodeID) (*Node, bool) {
	if access == nil {
		return nil, false
	}
	access.mutex.RLock()
	defer access.mutex.RUnlock()
	if access.closed || access.lease == nil || access.lease.store == nil {
		return nil, false
	}
	return access.lease.store.resolveLocked(id)
}

// Identity returns the opaque identity while this view remains active. It
// returns the zero token after callback expiry.
func (view ReadView) Identity() DocumentIdentity {
	access, err := view.Acquire()
	if err != nil {
		return DocumentIdentity{}
	}
	defer access.Close()
	return access.Identity()
}

// Root performs one callback-scoped root lookup. Callers that traverse or
// retain the returned node must instead hold an explicit ReadAccess.
func (view ReadView) Root() *Node {
	access, err := view.Acquire()
	if err != nil {
		return nil
	}
	defer access.Close()
	return access.Root()
}

// Version returns the captured version while this view remains active. It
// returns zero after callback expiry.
func (view ReadView) Version() uint64 {
	access, err := view.Acquire()
	if err != nil {
		return 0
	}
	defer access.Close()
	return access.Version()
}

// ID performs one callback-scoped stable-identity lookup. Callers that need a
// sequence of raw-node operations must hold an explicit ReadAccess.
func (view ReadView) ID(node *Node) (NodeID, bool) {
	access, err := view.Acquire()
	if err != nil {
		return InvalidNodeID, false
	}
	defer access.Close()
	return access.ID(node)
}

// Resolve performs one callback-scoped backing-node lookup. Callers that use
// the returned node beyond this call must hold an explicit ReadAccess.
func (view ReadView) Resolve(id NodeID) (*Node, bool) {
	access, err := view.Acquire()
	if err != nil {
		return nil, false
	}
	defer access.Close()
	return access.Resolve(id)
}

// IdentitySnapshot is the stable-ID graph shape needed by the browser's
// semantic ownership ledger. Parent follows DOM parentage plus non-DOM
// ownership edges such as an HTMLTemplateElement retaining its content
// fragment. It is zero for the document root and detached ownership roots.
type IdentitySnapshot struct {
	ID     NodeID
	Parent NodeID
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
	document := &Document{
		identity: newDocumentIdentity(),
		root:     store.ids[root],
		store:    store,
	}
	document.version.Store(1)
	return document, nil
}

func newDocumentIdentity() DocumentIdentity {
	value := nextDocumentIdentity.Add(1)
	if value == 0 {
		value = nextDocumentIdentity.Add(1)
	}
	return DocumentIdentity{value: value}
}

// Identity returns the opaque process-local identity of document.
func (document *Document) Identity() DocumentIdentity {
	if document == nil {
		return DocumentIdentity{}
	}
	return document.identity
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

// WithReadView holds the store's read lock while callback observes the current
// pointer-backed tree, stable identities, and mutation version. The supplied
// view is valid only for the duration of callback.
func (document *Document) WithReadView(callback func(ReadView) error) error {
	if document == nil || document.store == nil {
		return ErrInvalidDocument
	}
	if callback == nil {
		return fmt.Errorf("dom: nil document reader")
	}
	document.store.mutex.RLock()
	_, ok := document.store.resolveLocked(document.root)
	if !ok {
		document.store.mutex.RUnlock()
		return fmt.Errorf("%w: %d", ErrUnknownNode, document.root)
	}
	lease := &readViewLease{
		active:   true,
		identity: document.identity,
		store:    document.store,
		root:     document.root,
		version:  document.version.Load(),
	}
	defer func() {
		// Writer preference on this mutex also prevents a new Acquire from
		// slipping in once callback return has begun. Existing accesses keep the
		// NodeStore read lock effective until they close and release their reads.
		lease.mutex.Lock()
		lease.active = false
		lease.mutex.Unlock()
		document.store.mutex.RUnlock()
	}()
	return callback(ReadView{lease: lease})
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
	return document.WithReadView(func(view ReadView) error {
		return callback(view.Root())
	})
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

// IdentitySnapshots returns every currently retained node in stable-ID order.
// It exposes identity and tree edges without leaking backing pointers across
// the browser/runtime ownership boundary.
func (document *Document) IdentitySnapshots() []IdentitySnapshot {
	if document == nil || document.store == nil {
		return nil
	}
	document.store.mutex.RLock()
	defer document.store.mutex.RUnlock()
	semanticParents := make(map[*Node]*Node)
	for _, node := range document.store.nodes {
		if node != nil && node.TemplateContent != nil {
			semanticParents[node.TemplateContent] = node
		}
	}
	result := make([]IdentitySnapshot, 0, len(document.store.nodes))
	for index, node := range document.store.nodes {
		if node == nil {
			continue
		}
		parent := InvalidNodeID
		if node.Parent != nil {
			parent = document.store.ids[node.Parent]
		} else if semanticParent := semanticParents[node]; semanticParent != nil {
			parent = document.store.ids[semanticParent]
		}
		result = append(result, IdentitySnapshot{ID: NodeID(index + 1), Parent: parent})
	}
	return result
}

// Reclaim removes detached nodes from the logical identity store without
// reusing their IDs. Every supplied node must be outside the connected
// document tree; callers normally derive this set from final region releases.
func (document *Document) Reclaim(ids []NodeID) error {
	if document == nil || document.store == nil {
		return ErrInvalidDocument
	}
	if len(ids) == 0 {
		return nil
	}
	document.store.mutex.Lock()
	defer document.store.mutex.Unlock()

	reclaiming := make(map[*Node]struct{}, len(ids))
	nodes := make([]*Node, 0, len(ids))
	root, rootOK := document.store.resolveLocked(document.root)
	if !rootOK {
		return fmt.Errorf("%w: %d", ErrUnknownNode, document.root)
	}
	for _, id := range ids {
		node, ok := document.store.resolveLocked(id)
		if !ok {
			return fmt.Errorf("%w: %d", ErrUnknownNode, id)
		}
		for ancestor := node; ancestor != nil; ancestor = ancestor.Parent {
			if ancestor == root {
				return fmt.Errorf("%w: cannot reclaim connected node %d", ErrInvalidTree, id)
			}
		}
		if _, exists := reclaiming[node]; exists {
			continue
		}
		reclaiming[node] = struct{}{}
		nodes = append(nodes, node)
	}

	for _, node := range nodes {
		if node.Parent != nil {
			if _, parentReclaimed := reclaiming[node.Parent]; !parentReclaimed {
				node.Parent.removeChild(node)
			}
		}
		for _, child := range node.Children {
			if _, childReclaimed := reclaiming[child]; !childReclaimed {
				child.Parent = nil
			}
		}
	}
	for _, node := range nodes {
		id := document.store.ids[node]
		delete(document.store.ids, node)
		document.store.nodes[int(id)-1] = nil
		node.Parent = nil
		node.Children = nil
	}
	return nil
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

// CreateElement indexes a detached element in the document's lifetime store.
// It remains addressable by stable ID even if it is never connected.
func (document *Document) CreateElement(name string) (NodeID, error) {
	if document == nil || document.store == nil {
		return InvalidNodeID, ErrInvalidDocument
	}
	name = strings.ToLower(name)
	if !validDOMName(name) {
		return InvalidNodeID, NewException(InvalidCharacterError, ErrInvalidName, "invalid element name %q", name)
	}
	document.store.mutex.Lock()
	defer document.store.mutex.Unlock()
	return document.store.assignLocked(NewElement(name)), nil
}

// CreateElementNS indexes a detached namespace-aware element. Qualified names
// preserve their case; createElement remains the HTML-lowercasing entry point.
func (document *Document) CreateElementNS(namespaceURI, qualifiedName string) (NodeID, error) {
	if document == nil || document.store == nil {
		return InvalidNodeID, ErrInvalidDocument
	}
	prefix, localName, err := parseQualifiedName(qualifiedName)
	if err != nil {
		return InvalidNodeID, err
	}
	if prefix != "" && namespaceURI == "" {
		return InvalidNodeID, NewException(NamespaceError, ErrNamespace, "prefix %q requires a namespace", prefix)
	}
	if prefix == "xml" && namespaceURI != XMLNamespace {
		return InvalidNodeID, NewException(NamespaceError, ErrNamespace, "xml prefix requires %q", XMLNamespace)
	}
	usesXMLNS := qualifiedName == "xmlns" || prefix == "xmlns"
	if usesXMLNS != (namespaceURI == XMLNSNamespace) {
		return InvalidNodeID, NewException(NamespaceError, ErrNamespace, "xmlns name and namespace must agree")
	}
	document.store.mutex.Lock()
	defer document.store.mutex.Unlock()
	return document.store.assignLocked(newElementNS(namespaceURI, prefix, localName)), nil
}

// CreateTextNode indexes detached character data in the document's lifetime
// store. Creation alone does not invalidate the connected tree.
func (document *Document) CreateTextNode(data string) (NodeID, error) {
	if document == nil || document.store == nil {
		return InvalidNodeID, ErrInvalidDocument
	}
	document.store.mutex.Lock()
	defer document.store.mutex.Unlock()
	return document.store.assignLocked(NewText(data)), nil
}

// CreateDocumentFragment indexes an empty detached fragment in the document's
// lifetime store. Inserting the fragment moves its children rather than the
// fragment node itself.
func (document *Document) CreateDocumentFragment() (NodeID, error) {
	if document == nil || document.store == nil {
		return InvalidNodeID, ErrInvalidDocument
	}
	document.store.mutex.Lock()
	defer document.store.mutex.Unlock()
	return document.store.assignLocked(NewDocumentFragment()), nil
}

// CloneNode creates a detached copy with fresh stable IDs. Parent edges are
// rebuilt inside the copied subtree and never alias the source tree.
func (document *Document) CloneNode(id NodeID, deep bool) (NodeID, error) {
	if document == nil || document.store == nil {
		return InvalidNodeID, ErrInvalidDocument
	}
	document.store.mutex.Lock()
	defer document.store.mutex.Unlock()
	source, ok := document.store.resolveLocked(id)
	if !ok {
		return InvalidNodeID, fmt.Errorf("%w: %d", ErrUnknownNode, id)
	}
	if source.Type == DocumentNode {
		return InvalidNodeID, fmt.Errorf("%w: document cloning is unsupported", ErrWrongNodeKind)
	}
	var clone func(*Node) *Node
	clone = func(node *Node) *Node {
		copy := &Node{
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
			copy.Control = &state
		}
		if node.TemplateContent != nil {
			copy.TemplateContent = &Node{Type: DocumentFragmentNode}
			if deep {
				for _, child := range node.TemplateContent.Children {
					copy.TemplateContent.AppendChild(clone(child))
				}
			}
		}
		if deep {
			for _, child := range node.Children {
				copy.AppendChild(clone(child))
			}
		}
		return copy
	}
	root := clone(source)
	ordered, err := collectSubtree(root)
	if err != nil {
		return InvalidNodeID, err
	}
	for _, node := range ordered {
		document.store.assignLocked(node)
	}
	return document.store.ids[root], nil
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
	oldValue := node.Data
	node.Data = data
	document.recordCharacterMutationLocked(node, oldValue)
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
		oldValue := node.Data
		node.Data = data
		document.recordCharacterMutationLocked(node, oldValue)
	case ElementNode, DocumentFragmentNode:
		if len(node.Children) == 0 && data == "" {
			return nil
		}
		if len(node.Children) == 1 && node.Children[0].Type == TextNode && node.Children[0].Data == data {
			return nil
		}
		before := append([]*Node(nil), node.Children...)
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
		document.recordChildMutationLocked(node, before, node.Children, nil)
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

// AppendNode moves an indexed node to the end of an indexed parent's children.
func (document *Document) AppendNode(parentID, childID NodeID) error {
	return document.InsertBefore(parentID, childID, InvalidNodeID)
}

// InsertBefore moves an indexed node immediately before referenceID. A zero
// reference appends. Every node stays in the same document lifetime store.
func (document *Document) InsertBefore(parentID, childID, referenceID NodeID) error {
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
	var reference *Node
	if referenceID != InvalidNodeID {
		reference, ok = document.store.resolveLocked(referenceID)
		if !ok {
			return fmt.Errorf("%w: %d", ErrUnknownNode, referenceID)
		}
		if reference.Parent != parent {
			return NewException(NotFoundError, ErrInvalidTree, "reference node %d is not a child of node %d", referenceID, parentID)
		}
	}
	nodes := []*Node{child}
	if child.Type == DocumentFragmentNode {
		nodes = append([]*Node(nil), child.Children...)
	}
	placement := placeAppend
	if reference != nil {
		placement = placeBefore
	}
	return document.placeNodesLocked(parent, nodes, reference, placement)
}

// ReplaceChildrenFromFragment replaces all children of an element or fragment
// with an unexposed parsed fragment. New nodes receive fresh stable IDs in
// tree order while displaced nodes remain indexed and detached for wrappers.
func (document *Document) ReplaceChildrenFromFragment(parentID NodeID, fragment *Node) error {
	if document == nil || document.store == nil {
		return ErrInvalidDocument
	}
	document.store.mutex.Lock()
	defer document.store.mutex.Unlock()
	parent, ok := document.store.resolveLocked(parentID)
	if !ok {
		return fmt.Errorf("%w: %d", ErrUnknownNode, parentID)
	}
	if fragment == nil || fragment.Type != DocumentFragmentNode {
		return NewException(HierarchyRequestError, ErrWrongNodeKind, "replacement source is not a document fragment")
	}
	return document.placeNodesLocked(parent, append([]*Node(nil), fragment.Children...), parent, placeReplaceAll)
}

// InsertFragment inserts a parsed fragment before referenceID, or appends it
// when referenceID is zero. The fragment object itself is never inserted.
func (document *Document) InsertFragment(parentID, referenceID NodeID, fragment *Node) error {
	if document == nil || document.store == nil {
		return ErrInvalidDocument
	}
	document.store.mutex.Lock()
	defer document.store.mutex.Unlock()
	parent, ok := document.store.resolveLocked(parentID)
	if !ok {
		return fmt.Errorf("%w: %d", ErrUnknownNode, parentID)
	}
	var reference *Node
	if referenceID != InvalidNodeID {
		var found bool
		reference, found = document.store.resolveLocked(referenceID)
		if !found {
			return fmt.Errorf("%w: %d", ErrUnknownNode, referenceID)
		}
		if reference.Parent != parent {
			return NewException(NotFoundError, ErrInvalidTree, "reference node %d is not a child of node %d", referenceID, parentID)
		}
	}
	if fragment == nil || fragment.Type != DocumentFragmentNode {
		return NewException(HierarchyRequestError, ErrWrongNodeKind, "insertion source is not a document fragment")
	}
	placement := placeAppend
	if reference != nil {
		placement = placeBefore
	}
	return document.placeNodesLocked(parent, append([]*Node(nil), fragment.Children...), reference, placement)
}

// RemoveChild detaches a direct child while preserving both stable IDs.
func (document *Document) RemoveChild(parentID, childID NodeID) error {
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
	if child.Parent != parent {
		return NewException(NotFoundError, ErrInvalidTree, "node %d is not a child of node %d", childID, parentID)
	}
	return document.removeChildLocked(parent, child)
}

func (document *Document) GetAttribute(id NodeID, name string) (string, bool, error) {
	if document == nil || document.store == nil {
		return "", false, ErrInvalidDocument
	}
	document.store.mutex.RLock()
	defer document.store.mutex.RUnlock()
	node, ok := document.store.resolveLocked(id)
	if !ok {
		return "", false, fmt.Errorf("%w: %d", ErrUnknownNode, id)
	}
	if node.Type != ElementNode {
		return "", false, fmt.Errorf("%w: node %d is not an element", ErrWrongNodeKind, id)
	}
	name = strings.ToLower(name)
	for _, attribute := range node.Attributes {
		if attribute.Name == name {
			return attribute.Value, true, nil
		}
	}
	return "", false, nil
}

// AttributeNames returns the element's attribute names in insertion order.
// The returned slice is a snapshot; facade objects can call this method again
// whenever they need a live view.
func (document *Document) AttributeNames(id NodeID) ([]string, error) {
	if document == nil || document.store == nil {
		return nil, ErrInvalidDocument
	}
	document.store.mutex.RLock()
	defer document.store.mutex.RUnlock()
	node, ok := document.store.resolveLocked(id)
	if !ok {
		return nil, fmt.Errorf("%w: %d", ErrUnknownNode, id)
	}
	if node.Type != ElementNode {
		return nil, fmt.Errorf("%w: node %d is not an element", ErrWrongNodeKind, id)
	}
	names := make([]string, len(node.Attributes))
	for index, attribute := range node.Attributes {
		names[index] = attribute.Name
	}
	return names, nil
}

func (document *Document) SetAttribute(id NodeID, name, value string) error {
	if document == nil || document.store == nil {
		return ErrInvalidDocument
	}
	name = strings.ToLower(name)
	if !validDOMName(name) {
		return NewException(InvalidCharacterError, ErrInvalidName, "invalid attribute name %q", name)
	}
	document.store.mutex.Lock()
	defer document.store.mutex.Unlock()
	node, ok := document.store.resolveLocked(id)
	if !ok {
		return fmt.Errorf("%w: %d", ErrUnknownNode, id)
	}
	if node.Type != ElementNode {
		return fmt.Errorf("%w: node %d is not an element", ErrWrongNodeKind, id)
	}
	for index := range node.Attributes {
		if node.Attributes[index].Name != name {
			continue
		}
		if node.Attributes[index].Value == value {
			return nil
		}
		oldValue := node.Attributes[index].Value
		node.Attributes[index].Value = value
		document.recordAttributeMutationLocked(node, name, oldValue, true)
		document.version.Add(1)
		return nil
	}
	node.Attributes = append(node.Attributes, Attribute{Name: name, Value: value})
	document.recordAttributeMutationLocked(node, name, "", false)
	document.version.Add(1)
	return nil
}

func (document *Document) RemoveAttribute(id NodeID, name string) error {
	if document == nil || document.store == nil {
		return ErrInvalidDocument
	}
	document.store.mutex.Lock()
	defer document.store.mutex.Unlock()
	node, ok := document.store.resolveLocked(id)
	if !ok {
		return fmt.Errorf("%w: %d", ErrUnknownNode, id)
	}
	if node.Type != ElementNode {
		return fmt.Errorf("%w: node %d is not an element", ErrWrongNodeKind, id)
	}
	name = strings.ToLower(name)
	for index := range node.Attributes {
		if node.Attributes[index].Name != name {
			continue
		}
		oldValue := node.Attributes[index].Value
		copy(node.Attributes[index:], node.Attributes[index+1:])
		node.Attributes[len(node.Attributes)-1] = Attribute{}
		node.Attributes = node.Attributes[:len(node.Attributes)-1]
		document.recordAttributeMutationLocked(node, name, oldValue, true)
		document.version.Add(1)
		return nil
	}
	return nil
}

func attributeValue(node *Node, name string) (string, bool) {
	for _, attribute := range node.Attributes {
		if attribute.Name == name {
			return attribute.Value, true
		}
	}
	return "", false
}

func descendantText(node *Node) string {
	var result strings.Builder
	var visit func(*Node)
	visit = func(current *Node) {
		if current.Type == TextNode {
			result.WriteString(current.Data)
			return
		}
		for _, child := range current.Children {
			visit(child)
		}
	}
	visit(node)
	return result.String()
}

func childIndex(parent, child *Node) int {
	for index, candidate := range parent.Children {
		if candidate == child {
			return index
		}
	}
	return len(parent.Children)
}

func validDOMName(name string) bool {
	if name == "" {
		return false
	}
	for _, character := range name {
		if character <= ' ' || strings.ContainsRune("<>/='\"", character) {
			return false
		}
	}
	return true
}

func parseQualifiedName(name string) (prefix, localName string, err error) {
	if !validDOMName(name) {
		return "", "", NewException(InvalidCharacterError, ErrInvalidName, "invalid qualified name %q", name)
	}
	if strings.Count(name, ":") > 1 {
		return "", "", NewException(NamespaceError, ErrInvalidName, "invalid qualified name %q", name)
	}
	prefix, localName, found := strings.Cut(name, ":")
	if !found {
		return "", name, nil
	}
	if prefix == "" || localName == "" || !validDOMName(prefix) || !validDOMName(localName) {
		return "", "", NewException(NamespaceError, ErrInvalidName, "invalid qualified name %q", name)
	}
	return prefix, localName, nil
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

// LiveLen reports retained nodes rather than the monotonic NodeID high-water
// mark returned by Len.
func (store *NodeStore) LiveLen() int {
	if store == nil {
		return 0
	}
	store.mutex.RLock()
	length := len(store.ids)
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
		if node.TemplateContent != nil {
			if node.TemplateContent.Parent != nil {
				return fmt.Errorf("%w: template content has a parent", ErrInvalidTree)
			}
			if err := visit(node.TemplateContent); err != nil {
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
