package dom

import (
	"errors"
	"runtime"
	"slices"
	"testing"
	"time"
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

func TestDocumentIdentityDistinguishesEquivalentIndexedDocuments(t *testing.T) {
	t.Parallel()

	firstRoot, _, _, _, _ := identityFixture()
	secondRoot, _, _, _, _ := identityFixture()
	first, err := IndexDocument(firstRoot)
	if err != nil {
		t.Fatal(err)
	}
	second, err := IndexDocument(secondRoot)
	if err != nil {
		t.Fatal(err)
	}
	if first.Identity() == (DocumentIdentity{}) || second.Identity() == (DocumentIdentity{}) {
		t.Fatal("indexed document received a zero identity")
	}
	if first.Identity() == second.Identity() {
		t.Fatal("separate indexed documents received the same identity")
	}
	if err := first.WithReadView(func(view ReadView) error {
		if view.Identity() != first.Identity() {
			t.Fatalf("ReadView.Identity() = %#v, want document identity %#v", view.Identity(), first.Identity())
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}

func TestDocumentReadViewProvidesCoherentVersionAndIdentity(t *testing.T) {
	t.Parallel()

	root, html, body, paragraph, text := identityFixture()
	document, err := IndexDocument(root)
	if err != nil {
		t.Fatal(err)
	}
	wantVersion := document.Version()
	wantIDs := map[*Node]NodeID{root: 1, html: 2, body: 3, paragraph: 4, text: 5}

	err = document.WithReadView(func(view ReadView) error {
		if got := view.Root(); got != root {
			t.Fatalf("ReadView.Root() = %p, want %p", got, root)
		}
		if got := view.Version(); got != wantVersion {
			t.Fatalf("ReadView.Version() = %d, want %d", got, wantVersion)
		}
		for node, wantID := range wantIDs {
			id, ok := view.ID(node)
			if !ok || id != wantID {
				t.Fatalf("ReadView.ID(%p) = %d, %t; want %d, true", node, id, ok, wantID)
			}
			resolved, ok := view.Resolve(id)
			if !ok || resolved != node {
				t.Fatalf("ReadView.Resolve(%d) = %p, %t; want %p, true", id, resolved, ok, node)
			}
		}
		if id, ok := view.ID(nil); ok || id != InvalidNodeID {
			t.Fatalf("ReadView.ID(nil) = %d, %t; want 0, false", id, ok)
		}
		if node, ok := view.Resolve(999); ok || node != nil {
			t.Fatalf("ReadView.Resolve(999) = %p, %t; want nil, false", node, ok)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestDocumentReadViewBlocksMutationWhileIdentityLookupsRemainAvailable(t *testing.T) {
	t.Parallel()

	root, _, _, paragraph, _ := identityFixture()
	document, err := IndexDocument(root)
	if err != nil {
		t.Fatal(err)
	}
	paragraphID, ok := document.ID(paragraph)
	if !ok {
		t.Fatal("paragraph has no stable ID")
	}
	writerStarted := make(chan struct{})
	writerDone := make(chan error, 1)

	err = document.WithReadView(func(view ReadView) error {
		go func() {
			close(writerStarted)
			writerDone <- document.SetAttribute(paragraphID, "data-state", "after")
		}()
		<-writerStarted
		for range 32 {
			runtime.Gosched()
		}

		if id, found := view.ID(paragraph); !found || id != paragraphID {
			t.Fatalf("ReadView.ID(paragraph) = %d, %t; want %d, true", id, found, paragraphID)
		}
		if resolved, found := view.Resolve(paragraphID); !found || resolved != paragraph {
			t.Fatalf("ReadView.Resolve(%d) = %p, %t; want %p, true", paragraphID, resolved, found, paragraph)
		}
		select {
		case mutationErr := <-writerDone:
			t.Fatalf("mutation completed while ReadView callback held the read lock: %v", mutationErr)
		default:
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}

	select {
	case mutationErr := <-writerDone:
		if mutationErr != nil {
			t.Fatal(mutationErr)
		}
	case <-time.After(time.Second):
		t.Fatal("mutation remained blocked after ReadView callback returned")
	}
	if got := document.Version(); got != 2 {
		t.Fatalf("document version after mutation = %d, want 2", got)
	}
}

func TestDocumentReadViewRejectsInvalidReaders(t *testing.T) {
	t.Parallel()

	var nilDocument *Document
	if err := nilDocument.WithReadView(func(ReadView) error { return nil }); !errors.Is(err, ErrInvalidDocument) {
		t.Fatalf("nil Document.WithReadView() error = %v, want ErrInvalidDocument", err)
	}

	root, _, _, _, _ := identityFixture()
	document, err := IndexDocument(root)
	if err != nil {
		t.Fatal(err)
	}
	if err := document.WithReadView(nil); err == nil {
		t.Fatal("Document.WithReadView(nil) unexpectedly succeeded")
	}
}

func TestDocumentReadViewExpiresAfterCallbackAndPanic(t *testing.T) {
	t.Parallel()

	root, _, _, paragraph, _ := identityFixture()
	document, err := IndexDocument(root)
	if err != nil {
		t.Fatal(err)
	}
	paragraphID, ok := document.ID(paragraph)
	if !ok {
		t.Fatal("paragraph has no stable ID")
	}

	var returned ReadView
	if err := document.WithReadView(func(view ReadView) error {
		returned = view
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	assertExpiredReadView(t, returned, root, paragraphID)

	var panicked ReadView
	func() {
		defer func() {
			if recovered := recover(); recovered == nil {
				t.Fatal("WithReadView callback did not panic")
			}
		}()
		_ = document.WithReadView(func(view ReadView) error {
			panicked = view
			panic("expire the view")
		})
	}()
	assertExpiredReadView(t, panicked, root, paragraphID)
}

func TestAcquiredReadAccessKeepsDocumentLockedAcrossCallbackReturn(t *testing.T) {
	t.Parallel()

	root, _, _, paragraph, _ := identityFixture()
	document, err := IndexDocument(root)
	if err != nil {
		t.Fatal(err)
	}
	paragraphID, ok := document.ID(paragraph)
	if !ok {
		t.Fatal("paragraph has no stable ID")
	}

	accessReady := make(chan *ReadAccess, 1)
	callbackReturning := make(chan struct{})
	readDone := make(chan error, 1)
	go func() {
		readDone <- document.WithReadView(func(view ReadView) error {
			access, acquireErr := view.Acquire()
			if acquireErr != nil {
				return acquireErr
			}
			accessReady <- access
			close(callbackReturning)
			return nil
		})
	}()

	access := <-accessReady
	<-callbackReturning
	writerStarted := make(chan struct{})
	writerDone := make(chan error, 1)
	go func() {
		close(writerStarted)
		writerDone <- document.SetAttribute(paragraphID, "data-state", "after")
	}()
	<-writerStarted
	for range 32 {
		runtime.Gosched()
	}

	select {
	case err := <-readDone:
		t.Fatalf("WithReadView returned while an acquired access remained open: %v", err)
	default:
	}
	select {
	case err := <-writerDone:
		t.Fatalf("writer completed while an acquired access remained open: %v", err)
	default:
	}
	if resolved, found := access.Resolve(paragraphID); !found || resolved != paragraph {
		t.Fatalf("ReadAccess.Resolve(%d) = %p, %t; want %p, true", paragraphID, resolved, found, paragraph)
	}
	if id, found := access.ID(paragraph); !found || id != paragraphID {
		t.Fatalf("ReadAccess.ID(paragraph) = %d, %t; want %d, true", id, found, paragraphID)
	}

	access.Close()
	access.Close()
	if root := access.Root(); root != nil {
		t.Fatalf("closed ReadAccess.Root() = %p, want nil", root)
	}
	if _, found := access.Resolve(paragraphID); found {
		t.Fatal("closed ReadAccess.Resolve unexpectedly succeeded")
	}
	select {
	case err := <-readDone:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("WithReadView remained blocked after ReadAccess.Close")
	}
	select {
	case err := <-writerDone:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("writer remained blocked after ReadAccess.Close")
	}
}

func assertExpiredReadView(t *testing.T, view ReadView, node *Node, id NodeID) {
	t.Helper()
	if access, err := view.Acquire(); !errors.Is(err, ErrExpiredReadView) || access != nil {
		t.Fatalf("expired ReadView.Acquire() = %v, %v; want nil, ErrExpiredReadView", access, err)
	}
	if got := view.Identity(); got != (DocumentIdentity{}) {
		t.Fatalf("expired ReadView.Identity() = %#v, want zero", got)
	}
	if got := view.Version(); got != 0 {
		t.Fatalf("expired ReadView.Version() = %d, want 0", got)
	}
	if got := view.Root(); got != nil {
		t.Fatalf("expired ReadView.Root() = %p, want nil", got)
	}
	if got, found := view.ID(node); found || got != InvalidNodeID {
		t.Fatalf("expired ReadView.ID(node) = %d, %t; want 0, false", got, found)
	}
	if got, found := view.Resolve(id); found || got != nil {
		t.Fatalf("expired ReadView.Resolve(%d) = %p, %t; want nil, false", id, got, found)
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
	if names, err := document.AttributeNames(rowID); err != nil || !slices.Equal(names, []string{"id", "data-state"}) {
		t.Fatalf("AttributeNames = %#v, %v", names, err)
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

func TestDocumentFragmentInsertionReplacementAndClone(t *testing.T) {
	document, err := IndexDocument(NewDocument())
	if err != nil {
		t.Fatal(err)
	}
	parentID, err := document.CreateElement("main")
	if err != nil {
		t.Fatal(err)
	}
	if err := document.AppendNode(document.RootID(), parentID); err != nil {
		t.Fatal(err)
	}
	fragmentID, err := document.CreateDocumentFragment()
	if err != nil {
		t.Fatal(err)
	}
	firstID, _ := document.CreateElement("span")
	secondID, _ := document.CreateTextNode("two")
	if err := document.AppendNode(fragmentID, firstID); err != nil {
		t.Fatal(err)
	}
	if err := document.AppendNode(fragmentID, secondID); err != nil {
		t.Fatal(err)
	}
	if err := document.AppendNode(parentID, fragmentID); err != nil {
		t.Fatal(err)
	}
	if children, err := document.ChildNodes(parentID, false); err != nil || !slices.Equal(children, []NodeID{firstID, secondID}) {
		t.Fatalf("fragment insertion children = %#v, %v", children, err)
	}
	if children, err := document.ChildNodes(fragmentID, false); err != nil || len(children) != 0 {
		t.Fatalf("inserted fragment children = %#v, %v", children, err)
	}

	parsed := NewDocumentFragment()
	parsed.AppendChild(NewElement("strong"))
	parsed.AppendChild(NewText("tail"))
	if err := document.ReplaceChildrenFromFragment(parentID, parsed); err != nil {
		t.Fatal(err)
	}
	children, err := document.ChildNodes(parentID, false)
	if err != nil || len(children) != 2 {
		t.Fatalf("fragment replacement children = %#v, %v", children, err)
	}
	cloneID, err := document.CloneNode(children[0], true)
	if err != nil {
		t.Fatal(err)
	}
	clone, ok := document.Resolve(cloneID)
	if !ok || clone.Parent != nil || clone.Data != "strong" {
		t.Fatalf("clone = %#v, %t", clone, ok)
	}
}

func TestDocumentReclaimDropsDetachedNodesWithoutReusingIDs(t *testing.T) {
	document, err := IndexDocument(NewDocument())
	if err != nil {
		t.Fatal(err)
	}
	baseline := document.Store().LiveLen()
	parent, err := document.CreateElement("div")
	if err != nil {
		t.Fatal(err)
	}
	child, err := document.CreateTextNode("temporary")
	if err != nil {
		t.Fatal(err)
	}
	if err := document.AppendNode(parent, child); err != nil {
		t.Fatal(err)
	}
	highWater := document.Store().Len()
	if err := document.Reclaim([]NodeID{parent, child}); err != nil {
		t.Fatal(err)
	}
	if document.Store().LiveLen() != baseline || document.Store().Len() != highWater {
		t.Fatalf("store after reclaim = live:%d high-water:%d", document.Store().LiveLen(), document.Store().Len())
	}
	if _, ok := document.Resolve(parent); ok {
		t.Fatal("reclaimed parent still resolves")
	}
	next, err := document.CreateElement("span")
	if err != nil {
		t.Fatal(err)
	}
	if next <= child {
		t.Fatalf("NodeID was reused: next=%d child=%d", next, child)
	}
	if err := document.Reclaim([]NodeID{document.RootID()}); !errors.Is(err, ErrInvalidTree) {
		t.Fatalf("connected root reclaim = %v, want %v", err, ErrInvalidTree)
	}
}

func TestFormValueAndCheckedStateSeparateFromMarkupDefaults(t *testing.T) {
	document, err := IndexDocument(NewDocument())
	if err != nil {
		t.Fatal(err)
	}
	inputID, err := document.CreateElement("input")
	if err != nil {
		t.Fatal(err)
	}
	if err := document.SetAttribute(inputID, "value", "markup"); err != nil {
		t.Fatal(err)
	}
	if err := document.SetAttribute(inputID, "checked", ""); err != nil {
		t.Fatal(err)
	}
	if value, err := document.FormValue(inputID); err != nil || value != "markup" {
		t.Fatalf("initial value = %q, %v", value, err)
	}
	if checked, err := document.FormChecked(inputID); err != nil || !checked {
		t.Fatalf("initial checked = %t, %v", checked, err)
	}
	if err := document.SetFormValue(inputID, "user"); err != nil {
		t.Fatal(err)
	}
	if err := document.SetFormChecked(inputID, false); err != nil {
		t.Fatal(err)
	}
	if err := document.SetAttribute(inputID, "value", "new-default"); err != nil {
		t.Fatal(err)
	}
	if err := document.RemoveAttribute(inputID, "checked"); err != nil {
		t.Fatal(err)
	}
	if value, err := document.FormValue(inputID); err != nil || value != "user" {
		t.Fatalf("dirty value = %q, %v", value, err)
	}
	if checked, err := document.FormChecked(inputID); err != nil || checked {
		t.Fatalf("dirty checked = %t, %v", checked, err)
	}
	cloneID, err := document.CloneNode(inputID, true)
	if err != nil {
		t.Fatal(err)
	}
	if value, err := document.FormValue(cloneID); err != nil || value != "user" {
		t.Fatalf("cloned value = %q, %v", value, err)
	}
	if checked, err := document.FormChecked(cloneID); err != nil || checked {
		t.Fatalf("cloned checked = %t, %v", checked, err)
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
