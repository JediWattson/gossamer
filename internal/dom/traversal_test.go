package dom

import (
	"reflect"
	"testing"
)

func TestStableTraversalAndReplacement(t *testing.T) {
	root := NewDocument()
	html := NewElement("html")
	body := NewElement("body")
	first := NewElement("span")
	text := NewText("middle")
	last := NewElement("strong")
	root.AppendChild(html)
	html.AppendChild(body)
	body.AppendChild(first)
	body.AppendChild(text)
	body.AppendChild(last)

	document, err := IndexDocument(root)
	if err != nil {
		t.Fatal(err)
	}
	bodyID, _ := document.ID(body)
	firstID, _ := document.ID(first)
	textID, _ := document.ID(text)
	lastID, _ := document.ID(last)

	if got, err := document.ChildNodes(bodyID, false); err != nil || !reflect.DeepEqual(got, []NodeID{firstID, textID, lastID}) {
		t.Fatalf("ChildNodes(all) = %#v, %v", got, err)
	}
	if got, err := document.ChildNodes(bodyID, true); err != nil || !reflect.DeepEqual(got, []NodeID{firstID, lastID}) {
		t.Fatalf("ChildNodes(elements) = %#v, %v", got, err)
	}
	if got, found, err := document.RelatedNode(firstID, NextElementSibling); err != nil || !found || got != lastID {
		t.Fatalf("NextElementSibling = %d, %t, %v", got, found, err)
	}
	if contains, err := document.Contains(bodyID, textID); err != nil || !contains {
		t.Fatalf("Contains() = %t, %v", contains, err)
	}
	if snapshot, err := document.Snapshot(lastID); err != nil || !snapshot.Connected || snapshot.Data != "strong" {
		t.Fatalf("Snapshot() = %#v, %v", snapshot, err)
	}

	replacementID, err := document.CreateElement("em")
	if err != nil {
		t.Fatal(err)
	}
	if err := document.ReplaceChild(bodyID, replacementID, textID); err != nil {
		t.Fatal(err)
	}
	if got, err := document.ChildNodes(bodyID, false); err != nil || !reflect.DeepEqual(got, []NodeID{firstID, replacementID, lastID}) {
		t.Fatalf("children after replacement = %#v, %v", got, err)
	}
	if snapshot, err := document.Snapshot(textID); err != nil || snapshot.Connected {
		t.Fatalf("replaced node snapshot = %#v, %v", snapshot, err)
	}
}

func TestNodeValueAndHasAttribute(t *testing.T) {
	root := NewDocument()
	element := NewElement("div", Attribute{Name: "hidden", Value: ""})
	text := NewText("before")
	root.AppendChild(element)
	element.AppendChild(text)
	document, err := IndexDocument(root)
	if err != nil {
		t.Fatal(err)
	}
	elementID, _ := document.ID(element)
	textID, _ := document.ID(text)

	if found, err := document.HasAttribute(elementID, "HIDDEN"); err != nil || !found {
		t.Fatalf("HasAttribute(hidden) = %t, %v", found, err)
	}
	if value, nonNull, err := document.NodeValue(elementID); err != nil || nonNull || value != "" {
		t.Fatalf("element NodeValue = %q, %t, %v", value, nonNull, err)
	}
	if err := document.SetNodeValue(textID, "after"); err != nil {
		t.Fatal(err)
	}
	if value, nonNull, err := document.NodeValue(textID); err != nil || !nonNull || value != "after" {
		t.Fatalf("text NodeValue = %q, %t, %v", value, nonNull, err)
	}
}
