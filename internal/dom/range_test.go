package dom

import "testing"

func TestRangeContentsAcrossTextAndNestedContainers(t *testing.T) {
	build := func(t *testing.T) (*Document, NodeID, NodeID, NodeID, NodeID, NodeID) {
		t.Helper()
		root := NewDocument()
		container := NewElement("div")
		left := NewElement("p")
		start := NewText("ab")
		strong := NewElement("strong")
		strong.AppendChild(NewText("cd"))
		left.AppendChild(start)
		left.AppendChild(strong)
		right := NewElement("p")
		italic := NewElement("i")
		italic.AppendChild(NewText("ef"))
		end := NewText("gh")
		right.AppendChild(italic)
		right.AppendChild(end)
		container.AppendChild(left)
		container.AppendChild(right)
		root.AppendChild(container)
		document, err := IndexDocument(root)
		if err != nil {
			t.Fatal(err)
		}
		startID, _ := document.ID(start)
		endID, _ := document.ID(end)
		strongID, _ := document.ID(strong)
		italicID, _ := document.ID(italic)
		containerID, _ := document.ID(container)
		return document, startID, endID, strongID, italicID, containerID
	}

	document, startID, endID, strongID, italicID, containerID := build(t)
	fragmentID, err := document.RangeContents(startID, 1, endID, 1, RangeCloneContents)
	if err != nil {
		t.Fatal(err)
	}
	fragmentChildren, _ := document.ChildNodes(fragmentID, false)
	if len(fragmentChildren) != 2 {
		t.Fatalf("cloned fragment children = %#v", fragmentChildren)
	}
	leftClone, _ := document.Resolve(fragmentChildren[0])
	rightClone, _ := document.Resolve(fragmentChildren[1])
	if leftClone.Data != "p" || rightClone.Data != "p" ||
		len(leftClone.Children) != 2 || leftClone.Children[0].Data != "b" || leftClone.Children[1].Data != "strong" ||
		len(rightClone.Children) != 2 || rightClone.Children[0].Data != "i" || rightClone.Children[1].Data != "g" {
		t.Fatalf("nested cloned contents = %#v / %#v", leftClone, rightClone)
	}
	if clonedStrongID, _ := document.ID(leftClone.Children[1]); clonedStrongID == strongID {
		t.Fatal("cloneContents reused a fully-contained node")
	}
	if children, _ := document.ChildNodes(containerID, false); len(children) != 2 {
		t.Fatalf("cloneContents mutated source children = %#v", children)
	}

	document, startID, endID, strongID, italicID, containerID = build(t)
	fragmentID, err = document.RangeContents(startID, 1, endID, 1, RangeExtractContents)
	if err != nil {
		t.Fatal(err)
	}
	fragmentChildren, _ = document.ChildNodes(fragmentID, false)
	leftClone, _ = document.Resolve(fragmentChildren[0])
	rightClone, _ = document.Resolve(fragmentChildren[1])
	if movedStrongID, _ := document.ID(leftClone.Children[1]); movedStrongID != strongID {
		t.Fatalf("extractContents strong identity = %d, want %d", movedStrongID, strongID)
	}
	if movedItalicID, _ := document.ID(rightClone.Children[0]); movedItalicID != italicID {
		t.Fatalf("extractContents italic identity = %d, want %d", movedItalicID, italicID)
	}
	containerChildren, _ := document.ChildNodes(containerID, false)
	leftSource, _ := document.Resolve(containerChildren[0])
	rightSource, _ := document.Resolve(containerChildren[1])
	if len(leftSource.Children) != 1 || leftSource.Children[0].Data != "a" ||
		len(rightSource.Children) != 1 || rightSource.Children[0].Data != "h" {
		t.Fatalf("nested extracted source = %#v / %#v", leftSource, rightSource)
	}
}

func TestRangeDeleteContentsUsesUTF16Offsets(t *testing.T) {
	root := NewDocument()
	text := NewText("A😀BC")
	root.AppendChild(text)
	document, err := IndexDocument(root)
	if err != nil {
		t.Fatal(err)
	}
	textID, _ := document.ID(text)
	if fragment, err := document.RangeContents(textID, 1, textID, 3, RangeDeleteContents); err != nil || fragment != InvalidNodeID {
		t.Fatalf("RangeDeleteContents = %d, %v", fragment, err)
	}
	if value, _ := document.Text(textID); value != "ABC" {
		t.Fatalf("UTF-16 deleted text = %q", value)
	}
	if _, err := document.RangeContents(textID, 10, textID, 10, RangeCloneContents); err == nil {
		t.Fatal("out-of-bounds Range boundary succeeded")
	}
}
