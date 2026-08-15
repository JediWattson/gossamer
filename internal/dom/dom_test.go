package dom

import "testing"

func TestConstructors(t *testing.T) {
	t.Parallel()

	attributes := []Attribute{
		{Name: "id", Value: "main"},
		{Name: "hidden", Value: ""},
	}

	tests := []struct {
		name       string
		node       *Node
		nodeType   NodeType
		data       string
		attributes []Attribute
	}{
		{name: "document", node: NewDocument(), nodeType: DocumentNode},
		{name: "doctype", node: NewDoctype("html"), nodeType: DoctypeNode, data: "html"},
		{name: "element", node: NewElement("main", attributes...), nodeType: ElementNode, data: "main", attributes: attributes},
		{name: "text", node: NewText("hello"), nodeType: TextNode, data: "hello"},
		{name: "comment", node: NewComment("note"), nodeType: CommentNode, data: "note"},
		{name: "processing instruction", node: NewProcessingInstruction("build", "debug"), nodeType: ProcessingInstructionNode, data: "debug"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			if test.node.Type != test.nodeType {
				t.Errorf("Type = %d, want %d", test.node.Type, test.nodeType)
			}
			if test.node.Data != test.data {
				t.Errorf("Data = %q, want %q", test.node.Data, test.data)
			}
			if test.node.Parent != nil {
				t.Errorf("Parent = %p, want nil", test.node.Parent)
			}
			if len(test.node.Children) != 0 {
				t.Errorf("len(Children) = %d, want 0", len(test.node.Children))
			}
			if len(test.node.Attributes) != len(test.attributes) {
				t.Fatalf("len(Attributes) = %d, want %d", len(test.node.Attributes), len(test.attributes))
			}
			for index, want := range test.attributes {
				if got := test.node.Attributes[index]; got != want {
					t.Errorf("Attributes[%d] = %#v, want %#v", index, got, want)
				}
			}
		})
	}
}

func TestNewElementCopiesAttributes(t *testing.T) {
	t.Parallel()

	attributes := []Attribute{{Name: "class", Value: "before"}}
	element := NewElement("div", attributes...)
	attributes[0].Value = "after"

	if got := element.Attributes[0].Value; got != "before" {
		t.Errorf("attribute value = %q, want %q", got, "before")
	}
}

func TestAppendChildMaintainsTree(t *testing.T) {
	t.Parallel()

	parent := NewElement("main")
	first := NewText("first")
	second := NewElement("strong")

	parent.AppendChild(first)
	parent.AppendChild(second)

	if len(parent.Children) != 2 {
		t.Fatalf("len(Children) = %d, want 2", len(parent.Children))
	}
	if parent.Children[0] != first || parent.Children[1] != second {
		t.Errorf("Children = %p, want [%p %p]", parent.Children, first, second)
	}
	if first.Parent != parent {
		t.Errorf("first.Parent = %p, want %p", first.Parent, parent)
	}
	if second.Parent != parent {
		t.Errorf("second.Parent = %p, want %p", second.Parent, parent)
	}
}

func TestAppendChildMovesAttachedNode(t *testing.T) {
	t.Parallel()

	oldParent := NewElement("old")
	newParent := NewElement("new")
	child := NewText("child")
	oldParent.AppendChild(child)

	newParent.AppendChild(child)

	if len(oldParent.Children) != 0 {
		t.Errorf("len(oldParent.Children) = %d, want 0", len(oldParent.Children))
	}
	if len(newParent.Children) != 1 || newParent.Children[0] != child {
		t.Errorf("newParent.Children = %p, want [%p]", newParent.Children, child)
	}
	if child.Parent != newParent {
		t.Errorf("child.Parent = %p, want %p", child.Parent, newParent)
	}
}

func TestAppendChildMovesExistingChildToEnd(t *testing.T) {
	t.Parallel()

	parent := NewElement("div")
	first := NewText("first")
	second := NewText("second")
	parent.AppendChild(first)
	parent.AppendChild(second)

	parent.AppendChild(first)

	if len(parent.Children) != 2 {
		t.Fatalf("len(Children) = %d, want 2", len(parent.Children))
	}
	if parent.Children[0] != second || parent.Children[1] != first {
		t.Errorf("Children = %p, want [%p %p]", parent.Children, second, first)
	}
}

func TestAppendChildNilIsNoOp(t *testing.T) {
	t.Parallel()

	parent := NewElement("div")
	parent.AppendChild(nil)

	if len(parent.Children) != 0 {
		t.Errorf("len(Children) = %d, want 0", len(parent.Children))
	}
}

func TestAppendChildRejectsCycle(t *testing.T) {
	t.Parallel()

	parent := NewElement("parent")
	child := NewElement("child")
	parent.AppendChild(child)

	defer func() {
		if recover() == nil {
			t.Fatal("AppendChild() did not panic")
		}
	}()
	child.AppendChild(parent)
}
