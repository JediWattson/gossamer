package dom

import "testing"

func TestConstraintValidityUsesLiveValuesAndBarredControls(t *testing.T) {
	t.Parallel()

	required := NewElement("input", Attribute{Name: "required", Value: ""})
	if candidate, valid, complete := EvaluateConstraintValidity(required, nil); !candidate || valid || !complete {
		t.Fatalf("required empty input = candidate:%t valid:%t complete:%t", candidate, valid, complete)
	}
	required.Control.Value = "ready"
	required.Control.ValueDirty = true
	if candidate, valid, complete := EvaluateConstraintValidity(required, nil); !candidate || !valid || !complete {
		t.Fatalf("dirty populated input = candidate:%t valid:%t complete:%t", candidate, valid, complete)
	}

	for name, control := range map[string]*Node{
		"disabled": NewElement("input", Attribute{Name: "required", Value: ""}, Attribute{Name: "disabled", Value: ""}),
		"readonly": NewElement("input", Attribute{Name: "required", Value: ""}, Attribute{Name: "readonly", Value: ""}),
		"hidden":   NewElement("input", Attribute{Name: "type", Value: "hidden"}, Attribute{Name: "required", Value: ""}),
	} {
		if candidate, _, complete := EvaluateConstraintValidity(control, nil); candidate || !complete {
			t.Errorf("%s control = candidate:%t complete:%t, want barred", name, candidate, complete)
		}
	}

	email := NewElement("input", Attribute{Name: "type", Value: "email"}, Attribute{Name: "multiple", Value: ""}, Attribute{Name: "value", Value: "a@example.test, broken"})
	if candidate, valid, _ := EvaluateConstraintValidity(email, nil); !candidate || valid {
		t.Fatal("multiple email with a malformed address was valid")
	}
	email.Attributes[2].Value = "a@example.test, b@example.test"
	if _, valid, _ := EvaluateConstraintValidity(email, nil); !valid {
		t.Fatal("multiple valid email addresses were invalid")
	}
}

func TestConstraintValidityHonorsDisabledFieldsetLegendAndFormSubmission(t *testing.T) {
	t.Parallel()

	root := NewDocument()
	form := NewElement("form")
	fieldset := NewElement("fieldset", Attribute{Name: "disabled", Value: ""})
	legend := NewElement("legend")
	legendInput := NewElement("input", Attribute{Name: "required", Value: ""})
	disabledInput := NewElement("input", Attribute{Name: "required", Value: ""})
	legend.AppendChild(legendInput)
	fieldset.AppendChild(legend)
	fieldset.AppendChild(disabledInput)
	form.AppendChild(fieldset)
	root.AppendChild(form)
	document, err := IndexDocument(root)
	if err != nil {
		t.Fatal(err)
	}
	formID := mustNodeID(t, document, form)
	legendID := mustNodeID(t, document, legendInput)
	if valid, invalid, err := document.FormValidity(formID); err != nil || valid || len(invalid) != 1 || invalid[0] != legendID {
		t.Fatalf("fieldset form validity = %t, %v, %v", valid, invalid, err)
	}
	if err := document.SetFormValue(legendID, "ready"); err != nil {
		t.Fatal(err)
	}
	if valid, invalid, err := document.FormValidity(formID); err != nil || !valid || len(invalid) != 0 {
		t.Fatalf("updated fieldset form validity = %t, %v, %v", valid, invalid, err)
	}
}

func TestConstraintValidityUsesRadioGroupRequirednessAndSelectPlaceholder(t *testing.T) {
	t.Parallel()

	form := NewElement("form")
	first := NewElement("input", Attribute{Name: "type", Value: "radio"}, Attribute{Name: "name", Value: "pick"}, Attribute{Name: "required", Value: ""})
	second := NewElement("input", Attribute{Name: "type", Value: "radio"}, Attribute{Name: "name", Value: "pick"})
	form.AppendChild(first)
	form.AppendChild(second)
	for _, radio := range []*Node{first, second} {
		if candidate, valid, complete := EvaluateConstraintValidity(radio, nil); !candidate || valid || !complete {
			t.Fatalf("unchecked required-group radio = candidate:%t valid:%t complete:%t", candidate, valid, complete)
		}
	}
	second.Control.Checked = true
	second.Control.CheckedDirty = true
	for _, radio := range []*Node{first, second} {
		if _, valid, _ := EvaluateConstraintValidity(radio, nil); !valid {
			t.Fatal("checked group did not validate every radio member")
		}
	}

	selectNode := NewElement("select", Attribute{Name: "required", Value: ""})
	placeholder := NewElement("option", Attribute{Name: "value", Value: ""})
	emptyChoice := NewElement("option", Attribute{Name: "value", Value: ""})
	selectNode.AppendChild(placeholder)
	selectNode.AppendChild(emptyChoice)
	if _, valid, _ := EvaluateConstraintValidity(selectNode, nil); valid {
		t.Fatal("selected placeholder label option was valid")
	}
	placeholder.Control.SelectedDirty = true
	placeholder.Control.Selected = false
	emptyChoice.Control.SelectedDirty = true
	emptyChoice.Control.Selected = true
	if _, valid, _ := EvaluateConstraintValidity(selectNode, nil); !valid {
		t.Fatal("non-placeholder empty-valued option was invalid")
	}
}

func TestConstraintValidityFailsClosedWhenTraversalBudgetExpires(t *testing.T) {
	t.Parallel()

	fieldset := NewElement("fieldset", Attribute{Name: "disabled", Value: ""})
	for range 64 {
		fieldset.AppendChild(NewElement("div"))
	}
	target := NewElement("input", Attribute{Name: "required", Value: ""})
	fieldset.AppendChild(target)
	remaining := 1
	take := func() bool {
		remaining--
		return remaining >= 0
	}
	if _, _, complete := EvaluateConstraintValidity(target, take); complete {
		t.Fatal("constraint validation completed after its traversal budget expired")
	}
}
