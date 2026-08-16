package dom

import "testing"

func TestFormValidityAndSuccessfulControlEntryList(t *testing.T) {
	t.Parallel()

	root := NewDocument()
	form := NewElement("form",
		Attribute{Name: "action", Value: "/search"},
		Attribute{Name: "method", Value: "post"},
	)
	required := NewElement("input",
		Attribute{Name: "name", Value: "query"},
		Attribute{Name: "required", Value: ""},
		Attribute{Name: "minlength", Value: "3"},
	)
	checked := NewElement("input",
		Attribute{Name: "type", Value: "checkbox"},
		Attribute{Name: "name", Value: "enabled"},
		Attribute{Name: "checked", Value: ""},
	)
	unchecked := NewElement("input",
		Attribute{Name: "type", Value: "checkbox"},
		Attribute{Name: "name", Value: "ignored"},
	)
	selectNode := NewElement("select", Attribute{Name: "name", Value: "color"})
	red := NewElement("option", Attribute{Name: "value", Value: "red"})
	blue := NewElement("option", Attribute{Name: "value", Value: "blue"}, Attribute{Name: "selected", Value: ""})
	selectNode.AppendChild(red)
	selectNode.AppendChild(blue)
	submit := NewElement("button", Attribute{Name: "name", Value: "commit"}, Attribute{Name: "value", Value: "yes"})
	form.AppendChild(required)
	form.AppendChild(checked)
	form.AppendChild(unchecked)
	form.AppendChild(selectNode)
	form.AppendChild(submit)
	root.AppendChild(form)
	document, err := IndexDocument(root)
	if err != nil {
		t.Fatal(err)
	}
	formID, _ := document.ID(form)
	requiredID, _ := document.ID(required)
	submitID, _ := document.ID(submit)
	valid, invalid, err := document.FormValidity(formID)
	if err != nil {
		t.Fatal(err)
	}
	if valid || len(invalid) != 1 || invalid[0] != requiredID {
		t.Fatalf("initial validity = %t, %#v", valid, invalid)
	}
	if err := document.SetFormValue(requiredID, "go!"); err != nil {
		t.Fatal(err)
	}
	valid, invalid, err = document.FormValidity(formID)
	if err != nil || !valid || len(invalid) != 0 {
		t.Fatalf("updated validity = %t, %#v, %v", valid, invalid, err)
	}
	submission, err := document.PrepareFormSubmission(formID, submitID)
	if err != nil {
		t.Fatal(err)
	}
	if submission.Action != "/search" || submission.Method != "post" || submission.Enctype != "" {
		t.Fatalf("submission attributes = %#v", submission)
	}
	want := []FormEntry{
		{Name: "query", Value: "go!"},
		{Name: "enabled", Value: "on"},
		{Name: "color", Value: "blue"},
		{Name: "commit", Value: "yes"},
	}
	if len(submission.Entries) != len(want) {
		t.Fatalf("entries = %#v, want %#v", submission.Entries, want)
	}
	for index := range want {
		if submission.Entries[index] != want[index] {
			t.Fatalf("entry %d = %#v, want %#v", index, submission.Entries[index], want[index])
		}
	}
}

func TestFormValidityCoversRequiredGroupsAndTypeMismatch(t *testing.T) {
	t.Parallel()

	root := NewDocument()
	form := NewElement("form")
	email := NewElement("input", Attribute{Name: "type", Value: "email"}, Attribute{Name: "value", Value: "not-an-email"})
	firstRadio := NewElement("input", Attribute{Name: "type", Value: "radio"}, Attribute{Name: "name", Value: "choice"}, Attribute{Name: "required", Value: ""})
	secondRadio := NewElement("input", Attribute{Name: "type", Value: "radio"}, Attribute{Name: "name", Value: "choice"})
	form.AppendChild(email)
	form.AppendChild(firstRadio)
	form.AppendChild(secondRadio)
	root.AppendChild(form)
	document, err := IndexDocument(root)
	if err != nil {
		t.Fatal(err)
	}
	formID, _ := document.ID(form)
	emailID, _ := document.ID(email)
	firstID, _ := document.ID(firstRadio)
	secondID, _ := document.ID(secondRadio)
	valid, invalid, err := document.FormValidity(formID)
	if err != nil {
		t.Fatal(err)
	}
	if valid || len(invalid) != 3 || invalid[0] != emailID || invalid[1] != firstID || invalid[2] != secondID {
		t.Fatalf("validity = %t, %#v", valid, invalid)
	}
	if err := document.SetFormValue(emailID, "user@example.test"); err != nil {
		t.Fatal(err)
	}
	if err := document.SetFormChecked(secondID, true); err != nil {
		t.Fatal(err)
	}
	valid, invalid, err = document.FormValidity(formID)
	if err != nil || !valid || len(invalid) != 0 {
		t.Fatalf("updated validity = %t, %#v, %v", valid, invalid, err)
	}
}
