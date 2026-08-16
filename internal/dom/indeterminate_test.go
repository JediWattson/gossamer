package dom

import (
	"errors"
	"testing"
)

func TestFormIndeterminateIsLiveVersionedInputState(t *testing.T) {
	t.Parallel()

	root := NewDocument()
	checkbox := NewElement("input", Attribute{Name: "type", Value: "checkbox"})
	text := NewElement("input")
	other := NewElement("div")
	root.AppendChild(checkbox)
	root.AppendChild(text)
	root.AppendChild(other)
	document, err := IndexDocument(root)
	if err != nil {
		t.Fatal(err)
	}
	checkboxID := mustNodeID(t, document, checkbox)
	textID := mustNodeID(t, document, text)
	otherID := mustNodeID(t, document, other)

	if got, err := document.FormIndeterminate(checkboxID); err != nil || got {
		t.Fatalf("initial checkbox indeterminate = %t, %v", got, err)
	}
	version := document.Version()
	if err := document.SetFormIndeterminate(checkboxID, true); err != nil {
		t.Fatal(err)
	}
	if got, err := document.FormIndeterminate(checkboxID); err != nil || !got {
		t.Fatalf("updated checkbox indeterminate = %t, %v", got, err)
	}
	if document.Version() != version+1 {
		t.Fatalf("version after mutation = %d, want %d", document.Version(), version+1)
	}
	if err := document.SetFormIndeterminate(checkboxID, true); err != nil {
		t.Fatal(err)
	}
	if document.Version() != version+1 {
		t.Fatal("no-op indeterminate assignment changed the document version")
	}
	if err := document.SetFormIndeterminate(textID, true); err != nil {
		t.Fatalf("indeterminate IDL state did not apply to a non-checkbox input: %v", err)
	}
	if got, _ := document.FormIndeterminate(textID); !got {
		t.Fatal("non-checkbox input did not retain its live IDL state")
	}
	if err := document.SetFormIndeterminate(otherID, true); !errors.Is(err, ErrWrongNodeKind) {
		t.Fatalf("non-input error = %v, want ErrWrongNodeKind", err)
	}
}

func TestUnnamedRadiosDoNotCoordinateCheckedness(t *testing.T) {
	t.Parallel()

	root := NewDocument()
	form := NewElement("form")
	first := NewElement("input", Attribute{Name: "type", Value: "radio"})
	second := NewElement("input", Attribute{Name: "type", Value: "radio"})
	form.AppendChild(first)
	form.AppendChild(second)
	root.AppendChild(form)
	document, err := IndexDocument(root)
	if err != nil {
		t.Fatal(err)
	}
	firstID := mustNodeID(t, document, first)
	secondID := mustNodeID(t, document, second)
	if err := document.SetFormChecked(firstID, true); err != nil {
		t.Fatal(err)
	}
	if err := document.SetFormChecked(secondID, true); err != nil {
		t.Fatal(err)
	}
	if firstChecked, _ := document.FormChecked(firstID); !firstChecked {
		t.Fatal("checking an unnamed radio cleared a separate unnamed radio")
	}
	if group, err := document.RadioGroupNodes(firstID); err != nil || len(group) != 1 || group[0] != firstID {
		t.Fatalf("unnamed radio group = %v, %v; want only the radio itself", group, err)
	}
}
