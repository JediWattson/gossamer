// Package dom defines the document tree used by the browser engine.
package dom

// NodeType identifies the kind of data stored in a Node.
type NodeType uint8

const (
	DocumentNode NodeType = iota
	DoctypeNode
	ElementNode
	TextNode
	CommentNode
	ProcessingInstructionNode
)

// Attribute is a name-value pair on an element.
type Attribute struct {
	Name  string
	Value string
}

// Node is a node in a document tree. Data contains the doctype name, element
// name, text, comment text, or processing-instruction data according to Type.
type Node struct {
	Type       NodeType
	Data       string
	Target     string
	Attributes []Attribute
	Parent     *Node
	Children   []*Node
}

// NewDocument creates an empty document node.
func NewDocument() *Node {
	return &Node{Type: DocumentNode}
}

// NewDoctype creates a doctype node with the given name.
func NewDoctype(name string) *Node {
	return &Node{Type: DoctypeNode, Data: name}
}

// NewElement creates an element node. The node owns a copy of attributes.
func NewElement(name string, attributes ...Attribute) *Node {
	return &Node{
		Type:       ElementNode,
		Data:       name,
		Attributes: append([]Attribute(nil), attributes...),
	}
}

// NewText creates a text node.
func NewText(data string) *Node {
	return &Node{Type: TextNode, Data: data}
}

// NewComment creates a comment node.
func NewComment(data string) *Node {
	return &Node{Type: CommentNode, Data: data}
}

// NewProcessingInstruction creates a processing-instruction node.
func NewProcessingInstruction(target, data string) *Node {
	return &Node{Type: ProcessingInstructionNode, Target: target, Data: data}
}

// AppendChild adds child at the end of node's children and sets its parent.
// Appending an attached node moves it from its previous location.
func (node *Node) AppendChild(child *Node) {
	if node == nil {
		panic("dom: append child to nil node")
	}
	if child == nil {
		return
	}

	for ancestor := node; ancestor != nil; ancestor = ancestor.Parent {
		if ancestor == child {
			panic("dom: appending child would create a cycle")
		}
	}

	if child.Parent != nil {
		child.Parent.removeChild(child)
	}

	child.Parent = node
	node.Children = append(node.Children, child)
}

func (node *Node) removeChild(child *Node) {
	for index, candidate := range node.Children {
		if candidate != child {
			continue
		}

		copy(node.Children[index:], node.Children[index+1:])
		node.Children[len(node.Children)-1] = nil
		node.Children = node.Children[:len(node.Children)-1]
		return
	}
}
