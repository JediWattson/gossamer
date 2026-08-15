package dom

import (
	"errors"
	"testing"
)

func TestMutationPlannerRejectsInvalidFinalDocumentAtomically(t *testing.T) {
	root := NewDocument()
	doctype := NewDoctype("html")
	html := NewElement("html")
	body := NewElement("body")
	root.AppendChild(doctype)
	root.AppendChild(html)
	html.AppendChild(body)
	document, err := IndexDocument(root)
	if err != nil {
		t.Fatalf("IndexDocument: %v", err)
	}
	otherID, err := document.CreateElement("main")
	if err != nil {
		t.Fatalf("CreateElement: %v", err)
	}
	before := document.Version()
	err = document.AppendNode(document.RootID(), otherID)
	if name, ok := ErrorExceptionName(err); !ok || name != HierarchyRequestError {
		t.Fatalf("second document element error = %v (%q, %t)", err, name, ok)
	}
	if !errors.Is(err, ErrInvalidTree) {
		t.Fatalf("second document element error does not wrap ErrInvalidTree: %v", err)
	}
	if document.Version() != before {
		t.Fatalf("failed mutation changed version from %d to %d", before, document.Version())
	}
	if node, _ := document.Resolve(otherID); node.Parent != nil {
		t.Fatal("failed document insertion attached the candidate")
	}
	if len(root.Children) != 2 || root.Children[0] != doctype || root.Children[1] != html {
		t.Fatalf("failed document insertion changed children: %#v", root.Children)
	}

	err = document.InsertBefore(document.RootID(), mustNodeID(t, document, doctype), InvalidNodeID)
	if name, ok := ErrorExceptionName(err); !ok || name != HierarchyRequestError {
		t.Fatalf("doctype-after-element error = %v (%q, %t)", err, name, ok)
	}
	if root.Children[0] != doctype || root.Children[1] != html {
		t.Fatal("failed doctype move was not atomic")
	}
}

func TestConvenienceMutationsUseOneStableAtomicPath(t *testing.T) {
	root := NewDocument()
	html := NewElement("html")
	body := NewElement("body")
	first := NewElement("p", Attribute{Name: "id", Value: "first"})
	second := NewElement("p", Attribute{Name: "id", Value: "second"})
	root.AppendChild(html)
	html.AppendChild(body)
	body.AppendChild(first)
	body.AppendChild(second)
	document, err := IndexDocument(root)
	if err != nil {
		t.Fatalf("IndexDocument: %v", err)
	}
	bodyID := mustNodeID(t, document, body)
	firstID := mustNodeID(t, document, first)
	secondID := mustNodeID(t, document, second)
	thirdID, _ := document.CreateElement("p")
	fourthID, _ := document.CreateElement("p")

	if err := document.Mutate(bodyID, MutationPrepend, []NodeID{thirdID}); err != nil {
		t.Fatalf("prepend: %v", err)
	}
	if err := document.Mutate(secondID, MutationBefore, []NodeID{fourthID, firstID}); err != nil {
		t.Fatalf("before: %v", err)
	}
	want := []NodeID{thirdID, fourthID, firstID, secondID}
	if got, _ := document.ChildNodes(bodyID, false); !equalNodeIDs(got, want) {
		t.Fatalf("children after prepend/before = %v, want %v", got, want)
	}

	if err := document.Mutate(firstID, MutationAfter, []NodeID{thirdID, thirdID}); err != nil {
		t.Fatalf("after duplicate: %v", err)
	}
	want = []NodeID{fourthID, firstID, thirdID, secondID}
	if got, _ := document.ChildNodes(bodyID, false); !equalNodeIDs(got, want) {
		t.Fatalf("children after duplicate move = %v, want %v", got, want)
	}

	if err := document.Mutate(firstID, MutationReplaceWith, []NodeID{secondID}); err != nil {
		t.Fatalf("replaceWith: %v", err)
	}
	want = []NodeID{fourthID, secondID, thirdID}
	if got, _ := document.ChildNodes(bodyID, false); !equalNodeIDs(got, want) {
		t.Fatalf("children after replaceWith = %v, want %v", got, want)
	}
	if first.Parent != nil {
		t.Fatal("replaceWith did not detach its receiver")
	}

	if err := document.Mutate(bodyID, MutationReplaceChildren, []NodeID{firstID}); err != nil {
		t.Fatalf("replaceChildren: %v", err)
	}
	if got, _ := document.ChildNodes(bodyID, false); !equalNodeIDs(got, []NodeID{firstID}) {
		t.Fatalf("children after replaceChildren = %v", got)
	}
	if err := document.Mutate(firstID, MutationRemove, nil); err != nil {
		t.Fatalf("remove: %v", err)
	}
	if got, _ := document.ChildNodes(bodyID, false); len(got) != 0 {
		t.Fatalf("children after remove = %v", got)
	}
	if _, ok := document.Resolve(firstID); !ok {
		t.Fatal("detached node lost stable identity")
	}
}

func TestMutationErrorsExposeDOMExceptionNames(t *testing.T) {
	root := NewDocument()
	html := NewElement("html")
	body := NewElement("body")
	root.AppendChild(html)
	html.AppendChild(body)
	document, err := IndexDocument(root)
	if err != nil {
		t.Fatalf("IndexDocument: %v", err)
	}
	bodyID := mustNodeID(t, document, body)
	htmlID := mustNodeID(t, document, html)

	err = document.RemoveChild(bodyID, htmlID)
	if name, ok := ErrorExceptionName(err); !ok || name != NotFoundError {
		t.Fatalf("remove wrong child error = %v (%q, %t)", err, name, ok)
	}
	err = document.AppendNode(bodyID, htmlID)
	if name, ok := ErrorExceptionName(err); !ok || name != HierarchyRequestError {
		t.Fatalf("cycle error = %v (%q, %t)", err, name, ok)
	}
	if _, err := document.CreateElement("bad name"); err == nil {
		t.Fatal("CreateElement accepted an invalid name")
	} else if name, ok := ErrorExceptionName(err); !ok || name != InvalidCharacterError {
		t.Fatalf("invalid name error = %v (%q, %t)", err, name, ok)
	}
}

func equalNodeIDs(first, second []NodeID) bool {
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
