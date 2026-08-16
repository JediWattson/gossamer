package dom

import (
	"encoding/json"
	"fmt"
)

const (
	maxTransferNodes = 100_000
	maxTransferDepth = 1_024
)

type wireNode struct {
	Type            NodeType      `json:"type"`
	Data            string        `json:"data,omitempty"`
	Target          string        `json:"target,omitempty"`
	NamespaceURI    string        `json:"namespaceURI,omitempty"`
	Prefix          string        `json:"prefix,omitempty"`
	Attributes      []Attribute   `json:"attributes,omitempty"`
	Control         *ControlState `json:"control,omitempty"`
	Children        []wireNode    `json:"children,omitempty"`
	TemplateContent *wireNode     `json:"templateContent,omitempty"`
}

// ExportNode serializes one detached snapshot suitable for crossing a Realm
// queue. The encoding contains no Go pointers or source-document NodeIDs.
func (document *Document) ExportNode(id NodeID, deep bool) ([]byte, error) {
	if document == nil || document.store == nil {
		return nil, ErrInvalidDocument
	}
	document.store.mutex.RLock()
	defer document.store.mutex.RUnlock()
	node, ok := document.store.resolveLocked(id)
	if !ok {
		return nil, fmt.Errorf("%w: %d", ErrUnknownNode, id)
	}
	if node.Type == DocumentNode {
		return nil, NewException(NotFoundError, ErrWrongNodeKind, "a Document cannot cross a document boundary")
	}
	count := 0
	wire, err := encodeWireNode(node, deep, 0, &count)
	if err != nil {
		return nil, err
	}
	return json.Marshal(wire)
}

// TakeNode is the adoption-side export. It serializes the complete subtree,
// detaches it from the source tree, and retires every source NodeID. The old
// IDs are returned so browser ownership records can be reconciled.
func (document *Document) TakeNode(id NodeID) ([]byte, []NodeID, error) {
	if document == nil || document.store == nil {
		return nil, nil, ErrInvalidDocument
	}
	document.store.mutex.Lock()
	defer document.store.mutex.Unlock()
	node, ok := document.store.resolveLocked(id)
	if !ok {
		return nil, nil, fmt.Errorf("%w: %d", ErrUnknownNode, id)
	}
	if node.Type == DocumentNode {
		return nil, nil, NewException(NotFoundError, ErrWrongNodeKind, "a Document cannot be adopted")
	}
	count := 0
	wire, err := encodeWireNode(node, true, 0, &count)
	if err != nil {
		return nil, nil, err
	}
	data, err := json.Marshal(wire)
	if err != nil {
		return nil, nil, err
	}
	if node.Parent != nil {
		if err := document.removeChildLocked(node.Parent, node); err != nil {
			return nil, nil, err
		}
	}
	ordered, err := collectSubtree(node)
	if err != nil {
		return nil, nil, err
	}
	retired := make([]NodeID, 0, len(ordered))
	for _, current := range ordered {
		currentID := document.store.ids[current]
		if currentID == InvalidNodeID {
			return nil, nil, fmt.Errorf("%w: adopted subtree has an unindexed node", ErrInvalidTree)
		}
		retired = append(retired, currentID)
		delete(document.store.ids, current)
		document.store.nodes[int(currentID)-1] = nil
	}
	node.Parent = nil
	return data, retired, nil
}

// ImportNode installs a serialized node snapshot with fresh NodeIDs. It is
// detached; insertion remains a separate ordinary DOM mutation.
func (document *Document) ImportNode(data []byte) (NodeID, error) {
	if document == nil || document.store == nil {
		return InvalidNodeID, ErrInvalidDocument
	}
	var wire wireNode
	if err := json.Unmarshal(data, &wire); err != nil {
		return InvalidNodeID, fmt.Errorf("dom: decode transferred node: %w", err)
	}
	count := 0
	node, err := decodeWireNode(wire, 0, &count)
	if err != nil {
		return InvalidNodeID, err
	}
	if node.Type == DocumentNode {
		return InvalidNodeID, NewException(NotFoundError, ErrWrongNodeKind, "a Document cannot be imported")
	}
	ordered, err := collectSubtree(node)
	if err != nil {
		return InvalidNodeID, err
	}
	document.store.mutex.Lock()
	defer document.store.mutex.Unlock()
	for _, current := range ordered {
		document.store.assignLocked(current)
	}
	return document.store.ids[node], nil
}

func encodeWireNode(node *Node, deep bool, depth int, count *int) (wireNode, error) {
	if node == nil || depth > maxTransferDepth || *count >= maxTransferNodes {
		return wireNode{}, fmt.Errorf("%w: transferred subtree exceeds limits", ErrInvalidTree)
	}
	(*count)++
	wire := wireNode{
		Type:         node.Type,
		Data:         node.Data,
		Target:       node.Target,
		NamespaceURI: node.NamespaceURI,
		Prefix:       node.Prefix,
		Attributes:   append([]Attribute(nil), node.Attributes...),
	}
	if node.Control != nil {
		control := *node.Control
		wire.Control = &control
	}
	if deep {
		wire.Children = make([]wireNode, 0, len(node.Children))
		for _, child := range node.Children {
			encoded, err := encodeWireNode(child, true, depth+1, count)
			if err != nil {
				return wireNode{}, err
			}
			wire.Children = append(wire.Children, encoded)
		}
	}
	if node.TemplateContent != nil {
		encoded, err := encodeWireNode(node.TemplateContent, deep, depth+1, count)
		if err != nil {
			return wireNode{}, err
		}
		wire.TemplateContent = &encoded
	}
	return wire, nil
}

func decodeWireNode(wire wireNode, depth int, count *int) (*Node, error) {
	if depth > maxTransferDepth || *count >= maxTransferNodes || wire.Type == DocumentNode || wire.Type > DocumentFragmentNode {
		return nil, fmt.Errorf("%w: invalid transferred subtree", ErrInvalidTree)
	}
	(*count)++
	node := &Node{
		Type:         wire.Type,
		Data:         wire.Data,
		Target:       wire.Target,
		NamespaceURI: wire.NamespaceURI,
		Prefix:       wire.Prefix,
		Attributes:   append([]Attribute(nil), wire.Attributes...),
	}
	if wire.Control != nil {
		control := *wire.Control
		node.Control = &control
	}
	for _, childWire := range wire.Children {
		child, err := decodeWireNode(childWire, depth+1, count)
		if err != nil {
			return nil, err
		}
		node.AppendChild(child)
	}
	if wire.TemplateContent != nil {
		content, err := decodeWireNode(*wire.TemplateContent, depth+1, count)
		if err != nil {
			return nil, err
		}
		if content.Type != DocumentFragmentNode || content.Parent != nil {
			return nil, fmt.Errorf("%w: invalid template content", ErrInvalidTree)
		}
		node.TemplateContent = content
	}
	return node, nil
}
