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
	if got, err := document.Text(textID); err != nil || got != "after" {
		t.Fatalf("Text(%d) = %q, %v; want after", textID, got, err)
	}
	if got, ok := document.ClosestElement(textID); !ok || got != paragraphID {
		t.Fatalf("ClosestElement(%d) = %d, %t; want %d, true", textID, got, ok, paragraphID)
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

func TestDocumentElementLookupAndTextContentPreserveDetachedIdentity(t *testing.T) {
	t.Parallel()

	root, _, body, paragraph, text := identityFixture()
	paragraph.Attributes = []Attribute{{Name: "id", Value: "counter"}}
	nested := NewElement("strong")
	nestedText := NewText(" plus")
	nested.AppendChild(nestedText)
	paragraph.AppendChild(nested)
	document, err := IndexDocument(root)
	if err != nil {
		t.Fatal(err)
	}
	paragraphID := mustNodeID(t, document, paragraph)
	textID := mustNodeID(t, document, text)
	nestedID := mustNodeID(t, document, nested)
	nestedTextID := mustNodeID(t, document, nestedText)

	if got, ok := document.ElementByID("counter"); !ok || got != paragraphID {
		t.Fatalf("ElementByID(counter) = %d, %t; want %d, true", got, ok, paragraphID)
	}
	if got, err := document.TextContent(paragraphID); err != nil || got != "before plus" {
		t.Fatalf("TextContent(%d) = %q, %v; want before plus", paragraphID, got, err)
	}
	version := document.Version()
	if err := document.SetTextContent(paragraphID, "after"); err != nil {
		t.Fatal(err)
	}
	if document.Version() != version+1 {
		t.Fatalf("version = %d, want %d", document.Version(), version+1)
	}
	if got, err := document.TextContent(paragraphID); err != nil || got != "after" {
		t.Fatalf("TextContent after mutation = %q, %v; want after", got, err)
	}
	if text.Parent != nil || nested.Parent != nil {
		t.Fatalf("replaced descendants remain attached: text=%p nested=%p", text.Parent, nested.Parent)
	}
	if resolved, ok := document.Resolve(textID); !ok || resolved != text {
		t.Fatalf("detached text identity = %p, %t; want %p, true", resolved, ok, text)
	}
	if resolved, ok := document.Resolve(nestedTextID); !ok || resolved != nestedText {
		t.Fatalf("detached nested text identity = %p, %t; want %p, true", resolved, ok, nestedText)
	}
	if _, ok := document.ElementByID("counter"); !ok {
		t.Fatal("mutated element disappeared from connected lookup")
	}
	if resolved, ok := document.Resolve(nestedID); !ok || resolved != nested {
		t.Fatalf("detached element identity = %p, %t; want %p, true", resolved, ok, nested)
	}
	if got, err := document.TextContent(nestedID); err != nil || got != " plus" {
		t.Fatalf("detached TextContent = %q, %v; want space-plus", got, err)
	}
	if _, ok := document.ElementByID("missing"); ok {
		t.Fatal("ElementByID(missing) unexpectedly found a node")
	}
	if got, _ := document.TextContent(mustNodeID(t, document, body)); got != "after" {
		t.Fatalf("body TextContent = %q, want after", got)
	}
}

func TestDocumentIndexedNodeChurnPreservesIdentityAndOrder(t *testing.T) {
	t.Parallel()

	root, _, body, _, _ := identityFixture()
	document, err := IndexDocument(root)
	if err != nil {
		t.Fatal(err)
	}
	bodyID := mustNodeID(t, document, body)
	initialNodes := document.Store().Len()
	initialVersion := document.Version()
	rowID, err := document.CreateElement("DIV")
	if err != nil {
		t.Fatal(err)
	}
	leftID, err := document.CreateTextNode("left")
	if err != nil {
		t.Fatal(err)
	}
	rightID, err := document.CreateTextNode("right")
	if err != nil {
		t.Fatal(err)
	}
	if document.Version() != initialVersion || document.Store().Len() != initialNodes+3 {
		t.Fatalf("detached creation = version:%d nodes:%d", document.Version(), document.Store().Len())
	}
	if err := document.SetAttribute(rowID, "ID", "dynamic-row"); err != nil {
		t.Fatal(err)
	}
	if err := document.SetAttribute(rowID, "data-state", "first"); err != nil {
		t.Fatal(err)
	}
	if err := document.AppendNode(rowID, leftID); err != nil {
		t.Fatal(err)
	}
	if err := document.AppendNode(rowID, rightID); err != nil {
		t.Fatal(err)
	}
	if err := document.InsertBefore(rowID, rightID, leftID); err != nil {
		t.Fatal(err)
	}
	if err := document.AppendNode(bodyID, rowID); err != nil {
		t.Fatal(err)
	}
	if got, ok := document.ElementByID("dynamic-row"); !ok || got != rowID {
		t.Fatalf("connected dynamic row = %d, %t; want %d, true", got, ok, rowID)
	}
	row, _ := document.Resolve(rowID)
	if len(row.Children) != 2 || mustNodeID(t, document, row.Children[0]) != rightID || mustNodeID(t, document, row.Children[1]) != leftID {
		t.Fatalf("reordered children = %#v", row.Children)
	}
	if got, err := document.TextContent(rowID); err != nil || got != "rightleft" {
		t.Fatalf("row TextContent = %q, %v; want rightleft", got, err)
	}
	if err := document.RemoveChild(rowID, leftID); err != nil {
		t.Fatal(err)
	}
	if err := document.AppendNode(rowID, leftID); err != nil {
		t.Fatal(err)
	}
	if err := document.RemoveAttribute(rowID, "data-state"); err != nil {
		t.Fatal(err)
	}
	if _, found, err := document.GetAttribute(rowID, "data-state"); err != nil || found {
		t.Fatalf("removed attribute = found:%t error:%v", found, err)
	}
	if err := document.RemoveChild(bodyID, rowID); err != nil {
		t.Fatal(err)
	}
	if _, ok := document.ElementByID("dynamic-row"); ok {
		t.Fatal("detached dynamic row remained in connected ID lookup")
	}
	if resolved, ok := document.Resolve(rowID); !ok || resolved != row {
		t.Fatalf("detached row identity = %p, %t; want %p, true", resolved, ok, row)
	}
	if err := document.AppendNode(bodyID, rowID); err != nil {
		t.Fatal(err)
	}
	if resolved, ok := document.Resolve(leftID); !ok || resolved.Parent != row {
		t.Fatalf("reinserted child identity = %#v, %t", resolved, ok)
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
