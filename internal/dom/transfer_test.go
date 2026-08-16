package dom_test

import (
	"errors"
	"testing"

	"github.com/JediWattson/gossamer/internal/dom"
)

func TestCrossDocumentImportUsesFreshIdentityAndPreservesState(t *testing.T) {
	t.Parallel()

	sourceRoot := dom.NewDocument()
	container := dom.NewElement("section", dom.Attribute{Name: "id", Value: "source"})
	input := dom.NewElement("input", dom.Attribute{Name: "value", Value: "initial"})
	input.Control.Value = "dirty"
	input.Control.ValueDirty = true
	template := dom.NewElement("template")
	template.TemplateContent.AppendChild(dom.NewText("inside"))
	container.AppendChild(input)
	container.AppendChild(template)
	sourceRoot.AppendChild(container)
	source, err := dom.IndexDocument(sourceRoot)
	if err != nil {
		t.Fatal(err)
	}
	sourceID, _ := source.ID(container)
	data, err := source.ExportNode(sourceID, true)
	if err != nil {
		t.Fatal(err)
	}

	target, err := dom.IndexDocument(dom.NewDocument())
	if err != nil {
		t.Fatal(err)
	}
	importedID, err := target.ImportNode(data)
	if err != nil {
		t.Fatal(err)
	}
	imported, ok := target.Resolve(importedID)
	if !ok || imported == container || imported.Parent != nil || len(imported.Children) != 2 {
		t.Fatalf("imported root = %#v, found=%t", imported, ok)
	}
	if imported.Children[0].Control == nil || imported.Children[0].Control.Value != "dirty" || !imported.Children[0].Control.ValueDirty {
		t.Fatalf("imported control state = %#v", imported.Children[0].Control)
	}
	if imported.Children[1].TemplateContent == nil || len(imported.Children[1].TemplateContent.Children) != 1 || imported.Children[1].TemplateContent.Children[0].Data != "inside" {
		t.Fatalf("imported template = %#v", imported.Children[1].TemplateContent)
	}
	if resolved, _ := source.Resolve(sourceID); resolved != container || container.Parent != sourceRoot {
		t.Fatal("import mutated the source document")
	}
}

func TestCrossDocumentTakeRetiresSourceIDsBeforeImport(t *testing.T) {
	t.Parallel()

	sourceRoot := dom.NewDocument()
	parent := dom.NewElement("main")
	child := dom.NewElement("span")
	text := dom.NewText("move")
	child.AppendChild(text)
	parent.AppendChild(child)
	sourceRoot.AppendChild(parent)
	source, err := dom.IndexDocument(sourceRoot)
	if err != nil {
		t.Fatal(err)
	}
	childID, _ := source.ID(child)
	textID, _ := source.ID(text)
	data, retired, err := source.TakeNode(childID)
	if err != nil {
		t.Fatal(err)
	}
	if len(retired) != 2 || retired[0] != childID || retired[1] != textID {
		t.Fatalf("retired IDs = %#v", retired)
	}
	if _, ok := source.Resolve(childID); ok {
		t.Fatal("adopted source root still resolves")
	}
	if len(parent.Children) != 0 {
		t.Fatalf("source parent children = %#v", parent.Children)
	}
	if _, _, err := source.TakeNode(childID); !errors.Is(err, dom.ErrUnknownNode) {
		t.Fatalf("second take error = %v, want ErrUnknownNode", err)
	}

	target, err := dom.IndexDocument(dom.NewDocument())
	if err != nil {
		t.Fatal(err)
	}
	adoptedID, err := target.ImportNode(data)
	if err != nil {
		t.Fatal(err)
	}
	adopted, ok := target.Resolve(adoptedID)
	if !ok || adopted == child || len(adopted.Children) != 1 || adopted.Children[0].Data != "move" {
		t.Fatalf("adopted root = %#v, found=%t", adopted, ok)
	}
}
