package dom

import (
	"errors"
	"testing"
)

func TestFormUserValidityTracksCommitSubmissionResetAndClone(t *testing.T) {
	t.Parallel()

	root := NewDocument()
	html := NewElement("html")
	body := NewElement("body")
	form := NewElement("form", Attribute{Name: "id", Value: "account"})
	input := NewElement("input", Attribute{Name: "required", Value: ""})
	textarea := NewElement("textarea")
	selectNode := NewElement("select")
	selectNode.AppendChild(NewElement("option", Attribute{Name: "value", Value: "ready"}))
	button := NewElement("button")
	external := NewElement("input", Attribute{Name: "form", Value: "account"})
	other := NewElement("div")
	form.AppendChild(input)
	form.AppendChild(textarea)
	form.AppendChild(selectNode)
	form.AppendChild(button)
	body.AppendChild(form)
	body.AppendChild(external)
	body.AppendChild(other)
	html.AppendChild(body)
	root.AppendChild(html)
	document, err := IndexDocument(root)
	if err != nil {
		t.Fatal(err)
	}
	formID := mustNodeID(t, document, form)
	inputID := mustNodeID(t, document, input)
	textareaID := mustNodeID(t, document, textarea)
	selectID := mustNodeID(t, document, selectNode)
	buttonID := mustNodeID(t, document, button)
	externalID := mustNodeID(t, document, external)
	otherID := mustNodeID(t, document, other)

	if got, err := document.FormUserValidity(inputID); err != nil || got {
		t.Fatalf("initial user validity = %t, %v", got, err)
	}
	if err := document.SetFormValue(inputID, "programmatic"); err != nil {
		t.Fatal(err)
	}
	if got, _ := document.FormUserValidity(inputID); got {
		t.Fatal("programmatic value assignment set user validity")
	}
	if err := document.SetFormSelection(inputID, 0, 12, "none"); err != nil {
		t.Fatal(err)
	}
	if err := document.ReplaceFormSelection(inputID, "typed", "insertText"); err != nil {
		t.Fatal(err)
	}
	if got, _ := document.FormUserValidity(inputID); got {
		t.Fatal("uncommitted native edit set user validity")
	}
	if err := document.CommitFormUserInteraction(inputID); err != nil {
		t.Fatal(err)
	}
	if got, _ := document.FormUserValidity(inputID); !got {
		t.Fatal("committed native edit did not set user validity")
	}

	cloneID, err := document.CloneNode(inputID, false)
	if err != nil {
		t.Fatal(err)
	}
	if got, _ := document.FormUserValidity(cloneID); got {
		t.Fatal("cloned control retained source user validity")
	}

	if err := document.MarkFormUserValidityForSubmission(formID); err != nil {
		t.Fatal(err)
	}
	for _, id := range []NodeID{inputID, textareaID, selectID, externalID} {
		if got, err := document.FormUserValidity(id); err != nil || !got {
			t.Errorf("submission user validity for %d = %t, %v", id, got, err)
		}
	}
	if _, err := document.FormUserValidity(buttonID); !errors.Is(err, ErrWrongNodeKind) {
		t.Fatalf("button user-validity error = %v, want ErrWrongNodeKind", err)
	}
	if _, err := document.FormUserValidity(otherID); !errors.Is(err, ErrWrongNodeKind) {
		t.Fatalf("div user-validity error = %v, want ErrWrongNodeKind", err)
	}

	if err := document.ResetForm(formID); err != nil {
		t.Fatal(err)
	}
	for _, id := range []NodeID{inputID, textareaID, selectID, externalID} {
		if got, err := document.FormUserValidity(id); err != nil || got {
			t.Errorf("reset user validity for %d = %t, %v", id, got, err)
		}
	}
}

func TestFormUserValidityMutationsAreVersionedAndNoOpSafe(t *testing.T) {
	t.Parallel()

	root := NewDocument()
	input := NewElement("input")
	root.AppendChild(input)
	document, err := IndexDocument(root)
	if err != nil {
		t.Fatal(err)
	}
	inputID := mustNodeID(t, document, input)
	version := document.Version()
	if err := document.SetFormUserValidity(inputID, true); err != nil {
		t.Fatal(err)
	}
	if document.Version() != version+1 {
		t.Fatalf("version after user-validity mutation = %d, want %d", document.Version(), version+1)
	}
	if err := document.SetFormUserValidity(inputID, true); err != nil {
		t.Fatal(err)
	}
	if document.Version() != version+1 {
		t.Fatal("no-op user-validity assignment changed the document version")
	}
}
