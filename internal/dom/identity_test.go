package dom

import (
	"errors"
	"testing"
)

func TestIndexDocumentAssignsDeterministicStableIDs(t *testing.T) {
	t.Parallel()

	root, html, body, paragraph, text := identityFixture()
	document, err := IndexDocument(root)
	if err != nil {
		t.Fatal(err)
	}
	want := map[*Node]NodeID{root: 1, html: 2, body: 3, paragraph: 4, text: 5}
	for node, wantID := range want {
		id, ok := document.ID(node)
		if !ok || id != wantID {
			t.Fatalf("ID(%p) = %d, %t; want %d, true", node, id, ok, wantID)
		}
		resolved, ok := document.Resolve(id)
		if !ok || resolved != node {
			t.Fatalf("Resolve(%d) = %p, %t; want %p, true", id, resolved, ok, node)
		}
	}
	if document.RootID() != 1 || document.Root() != root || document.Store().Len() != 5 {
		t.Fatalf("document root/store = id:%d root:%p len:%d", document.RootID(), document.Root(), document.Store().Len())
	}
}

func TestDocumentMutationUsesStableIdentity(t *testing.T) {
	t.Parallel()

	root, _, body, paragraph, text := identityFixture()
	document, err := IndexDocument(root)
	if err != nil {
		t.Fatal(err)
	}
	paragraphID, _ := document.ID(paragraph)
	textID, _ := document.ID(text)
	initialVersion := document.Version()
	if err := document.SetText(textID, "after"); err != nil {
		t.Fatal(err)
	}
	if text.Data != "after" || document.Version() != initialVersion+1 {
		t.Fatalf("SetText result = data:%q version:%d", text.Data, document.Version())
	}

	secondParagraph := NewElement("p")
	secondText := NewText("second")
	secondParagraph.AppendChild(secondText)
	secondID, err := document.AppendChild(mustNodeID(t, document, body), secondParagraph)
	if err != nil {
		t.Fatal(err)
	}
	if secondID <= textID {
		t.Fatalf("new subtree id = %d, want after existing id %d", secondID, textID)
	}
	if movedID, err := document.AppendChild(mustNodeID(t, document, body), paragraph); err != nil || movedID != paragraphID {
		t.Fatalf("moving indexed subtree = id:%d error:%v, want id:%d", movedID, err, paragraphID)
	}
	if got, _ := document.ID(text); got != textID {
		t.Fatalf("text ID after mutations = %d, want stable %d", got, textID)
	}
}

func TestDocumentRejectsUnknownAndWrongKindMutations(t *testing.T) {
	t.Parallel()

	root, _, body, _, _ := identityFixture()
	document, err := IndexDocument(root)
	if err != nil {
		t.Fatal(err)
	}
	if err := document.SetText(999, "nope"); !errors.Is(err, ErrUnknownNode) {
		t.Fatalf("SetText(unknown) error = %v, want ErrUnknownNode", err)
	}
	if err := document.SetText(mustNodeID(t, document, body), "nope"); !errors.Is(err, ErrWrongNodeKind) {
		t.Fatalf("SetText(element) error = %v, want ErrWrongNodeKind", err)
	}
}

func identityFixture() (root, html, body, paragraph, text *Node) {
	root = NewDocument()
	html = NewElement("html")
	body = NewElement("body")
	paragraph = NewElement("p")
	text = NewText("before")
	root.AppendChild(html)
	html.AppendChild(body)
	body.AppendChild(paragraph)
	paragraph.AppendChild(text)
	return
}

func mustNodeID(t *testing.T, document *Document, node *Node) NodeID {
	t.Helper()
	id, ok := document.ID(node)
	if !ok {
		t.Fatalf("node %p has no ID", node)
	}
	return id
}
