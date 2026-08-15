package dom

import (
	"reflect"
	"testing"
)

func TestTemplateContentCloneSplitNormalizeAndAdopt(t *testing.T) {
	root := NewDocument()
	host := NewElement("main")
	template := NewElement("template")
	host.AppendChild(template)
	root.AppendChild(host)
	document, err := IndexDocument(root)
	if err != nil {
		t.Fatal(err)
	}
	templateID, _ := document.ID(template)
	contentID, err := document.TemplateContent(templateID)
	if err != nil {
		t.Fatal(err)
	}
	if again, err := document.TemplateContent(templateID); err != nil || again != contentID {
		t.Fatalf("TemplateContent SameObject identity = %d, %v; want %d", again, err, contentID)
	}
	if children, err := document.ChildNodes(templateID, false); err != nil || len(children) != 0 {
		t.Fatalf("template child nodes = %#v, %v", children, err)
	}

	firstID, err := document.AppendChild(contentID, NewText("A😀"))
	if err != nil {
		t.Fatal(err)
	}
	secondID, err := document.SplitText(firstID, 3)
	if err != nil {
		t.Fatal(err)
	}
	if first, _ := document.Text(firstID); first != "A😀" {
		t.Fatalf("split prefix = %q", first)
	}
	if second, _ := document.Text(secondID); second != "" {
		t.Fatalf("split suffix = %q", second)
	}
	thirdID, err := document.AppendChild(contentID, NewText("tail"))
	if err != nil {
		t.Fatal(err)
	}
	if err := document.Normalize(contentID); err != nil {
		t.Fatal(err)
	}
	if children, err := document.ChildNodes(contentID, false); err != nil || !reflect.DeepEqual(children, []NodeID{firstID}) {
		t.Fatalf("normalized template children = %#v, %v", children, err)
	}
	if text, _ := document.Text(firstID); text != "A😀tail" {
		t.Fatalf("normalized text = %q", text)
	}
	if snapshot, _ := document.Snapshot(thirdID); snapshot.Connected {
		t.Fatalf("merged text remained connected: %#v", snapshot)
	}

	cloneID, err := document.CloneNode(templateID, true)
	if err != nil {
		t.Fatal(err)
	}
	cloneContentID, err := document.TemplateContent(cloneID)
	if err != nil {
		t.Fatal(err)
	}
	cloneChildren, err := document.ChildNodes(cloneContentID, false)
	if err != nil || len(cloneChildren) != 1 || cloneChildren[0] == firstID {
		t.Fatalf("deep-cloned template content = %#v, %v", cloneChildren, err)
	}
	if text, _ := document.Text(cloneChildren[0]); text != "A😀tail" {
		t.Fatalf("cloned template text = %q", text)
	}

	hostID, _ := document.ID(host)
	adopted, err := document.AdoptNode(templateID)
	if err != nil || adopted != templateID {
		t.Fatalf("AdoptNode = %d, %v; want %d", adopted, err, templateID)
	}
	if children, err := document.ChildNodes(hostID, false); err != nil || len(children) != 0 {
		t.Fatalf("host children after adopt = %#v, %v", children, err)
	}
}
