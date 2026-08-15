package dom

import "testing"

func TestFormControlStateCoordinatesRadioSelectOwnerAndReset(t *testing.T) {
	root := NewDocument()
	html := NewElement("html")
	body := NewElement("body")
	form := NewElement("form", Attribute{Name: "id", Value: "account"})
	firstRadio := NewElement("input",
		Attribute{Name: "id", Value: "first-radio"},
		Attribute{Name: "type", Value: "radio"},
		Attribute{Name: "name", Value: "choice"},
	)
	secondRadio := NewElement("input",
		Attribute{Name: "id", Value: "second-radio"},
		Attribute{Name: "type", Value: "radio"},
		Attribute{Name: "name", Value: "choice"},
		Attribute{Name: "checked", Value: ""},
	)
	textarea := NewElement("textarea", Attribute{Name: "id", Value: "notes"})
	textarea.AppendChild(NewText("default notes"))
	selectNode := NewElement("select", Attribute{Name: "id", Value: "kind"})
	firstOption := NewElement("option", Attribute{Name: "value", Value: "one"})
	firstOption.AppendChild(NewText("One"))
	secondOption := NewElement("option",
		Attribute{Name: "value", Value: "two"},
		Attribute{Name: "selected", Value: ""},
	)
	secondOption.AppendChild(NewText("Two"))
	selectNode.AppendChild(firstOption)
	selectNode.AppendChild(secondOption)
	button := NewElement("button", Attribute{Name: "name", Value: "save"})
	external := NewElement("input",
		Attribute{Name: "id", Value: "external"},
		Attribute{Name: "form", Value: "account"},
	)
	root.AppendChild(html)
	html.AppendChild(body)
	body.AppendChild(form)
	form.AppendChild(firstRadio)
	form.AppendChild(secondRadio)
	form.AppendChild(textarea)
	form.AppendChild(selectNode)
	form.AppendChild(button)
	body.AppendChild(external)
	document, err := IndexDocument(root)
	if err != nil {
		t.Fatalf("IndexDocument: %v", err)
	}

	formID := mustNodeID(t, document, form)
	firstRadioID := mustNodeID(t, document, firstRadio)
	secondRadioID := mustNodeID(t, document, secondRadio)
	textareaID := mustNodeID(t, document, textarea)
	selectID := mustNodeID(t, document, selectNode)
	firstOptionID := mustNodeID(t, document, firstOption)
	secondOptionID := mustNodeID(t, document, secondOption)
	buttonID := mustNodeID(t, document, button)
	externalID := mustNodeID(t, document, external)

	if value, err := document.FormValue(textareaID); err != nil || value != "default notes" {
		t.Fatalf("textarea default value = %q, %v", value, err)
	}
	if value, err := document.FormValue(selectID); err != nil || value != "two" {
		t.Fatalf("select default value = %q, %v", value, err)
	}
	if index, err := document.FormSelectedIndex(selectID); err != nil || index != 1 {
		t.Fatalf("selectedIndex = %d, %v", index, err)
	}
	if selected, err := document.FormSelected(firstOptionID); err != nil || selected {
		t.Fatalf("first option selected = %t, %v", selected, err)
	}
	if selected, err := document.FormSelected(secondOptionID); err != nil || !selected {
		t.Fatalf("second option selected = %t, %v", selected, err)
	}

	if err := document.SetFormSelected(firstOptionID, true); err != nil {
		t.Fatalf("SetFormSelected: %v", err)
	}
	if index, _ := document.FormSelectedIndex(selectID); index != 0 {
		t.Fatalf("selectedIndex after option selection = %d", index)
	}
	if value, _ := document.FormValue(selectID); value != "one" {
		t.Fatalf("select value after option selection = %q", value)
	}
	if err := document.SetFormValue(selectID, "two"); err != nil {
		t.Fatalf("SetFormValue select: %v", err)
	}
	if index, _ := document.FormSelectedIndex(selectID); index != 1 {
		t.Fatalf("selectedIndex after value assignment = %d", index)
	}

	if err := document.SetFormChecked(firstRadioID, true); err != nil {
		t.Fatalf("SetFormChecked first radio: %v", err)
	}
	if first, _ := document.FormChecked(firstRadioID); !first {
		t.Fatal("first radio did not become checked")
	}
	if second, _ := document.FormChecked(secondRadioID); second {
		t.Fatal("radio group did not clear the previous choice")
	}
	if err := document.SetFormValue(textareaID, "user notes"); err != nil {
		t.Fatalf("SetFormValue textarea: %v", err)
	}

	controls, err := document.FormControlNodes(formID, FormElementCollection)
	if err != nil {
		t.Fatalf("FormControlNodes: %v", err)
	}
	wantControls := []NodeID{firstRadioID, secondRadioID, textareaID, selectID, buttonID, externalID}
	if !equalNodeIDs(controls, wantControls) {
		t.Fatalf("form controls = %v, want %v", controls, wantControls)
	}
	options, err := document.FormControlNodes(selectID, SelectOptionCollection)
	if err != nil || !equalNodeIDs(options, []NodeID{firstOptionID, secondOptionID}) {
		t.Fatalf("select options = %v, %v", options, err)
	}
	if owner, found, err := document.FormOwner(externalID); err != nil || !found || owner != formID {
		t.Fatalf("external form owner = %d, %t, %v", owner, found, err)
	}

	if err := document.ResetForm(formID); err != nil {
		t.Fatalf("ResetForm: %v", err)
	}
	if first, _ := document.FormChecked(firstRadioID); first {
		t.Fatal("reset kept dirty first-radio state")
	}
	if second, _ := document.FormChecked(secondRadioID); !second {
		t.Fatal("reset did not restore checked markup default")
	}
	if value, _ := document.FormValue(textareaID); value != "default notes" {
		t.Fatalf("textarea after reset = %q", value)
	}
	if value, _ := document.FormValue(selectID); value != "two" {
		t.Fatalf("select after reset = %q", value)
	}
}
