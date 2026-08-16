package css_test

import (
	"errors"
	"testing"

	"github.com/JediWattson/gossamer/internal/css"
	"github.com/JediWattson/gossamer/internal/dom"
)

func TestValidityPseudosMatchCandidatesAndLiveValues(t *testing.T) {
	t.Parallel()

	valid := parseOneSelector(t, ":valid")
	invalid := parseOneSelector(t, ":invalid")
	input := dom.NewElement("input", dom.Attribute{Name: "required", Value: ""})
	if valid.Matches(input) || !invalid.Matches(input) {
		t.Fatal("required empty input did not match only :invalid")
	}
	input.Control.Value = "ready"
	input.Control.ValueDirty = true
	if !valid.Matches(input) || invalid.Matches(input) {
		t.Fatal("dirty populated input did not match only :valid")
	}
	input.Attributes = append(input.Attributes, dom.Attribute{Name: "disabled", Value: ""})
	if valid.Matches(input) || invalid.Matches(input) {
		t.Fatal("barred disabled input matched a validity pseudo-class")
	}

	selectNode := dom.NewElement("select", dom.Attribute{Name: "required", Value: ""})
	empty := dom.NewElement("option", dom.Attribute{Name: "value", Value: ""})
	choice := dom.NewElement("option", dom.Attribute{Name: "value", Value: "ok"})
	selectNode.AppendChild(empty)
	selectNode.AppendChild(choice)
	if !invalid.Matches(selectNode) {
		t.Fatal("required select on an empty first option was not invalid")
	}
	empty.Control.SelectedDirty = true
	empty.Control.Selected = false
	choice.Control.SelectedDirty = true
	choice.Control.Selected = true
	if !valid.Matches(selectNode) {
		t.Fatal("required select with a live nonempty choice was not valid")
	}
}

func TestValidityPseudosAggregateFormOwnersAndFieldsetDescendants(t *testing.T) {
	t.Parallel()

	valid := parseOneSelector(t, ":valid")
	invalid := parseOneSelector(t, ":invalid")
	document := dom.NewDocument()
	html := dom.NewElement("html")
	body := dom.NewElement("body")
	form := dom.NewElement("form", dom.Attribute{Name: "id", Value: "account"})
	fieldset := dom.NewElement("fieldset")
	inside := dom.NewElement("input", dom.Attribute{Name: "required", Value: ""})
	external := dom.NewElement("input", dom.Attribute{Name: "form", Value: "account"}, dom.Attribute{Name: "required", Value: ""}, dom.Attribute{Name: "value", Value: "ready"})
	other := dom.NewElement("input", dom.Attribute{Name: "required", Value: ""})
	fieldset.AppendChild(inside)
	form.AppendChild(fieldset)
	body.AppendChild(form)
	body.AppendChild(external)
	body.AppendChild(other)
	html.AppendChild(body)
	document.AppendChild(html)

	if !invalid.Matches(form) || valid.Matches(form) || !invalid.Matches(fieldset) {
		t.Fatal("invalid descendant did not invalidate its form and fieldset")
	}
	inside.Control.Value = "ready"
	inside.Control.ValueDirty = true
	if !valid.Matches(form) || invalid.Matches(form) || !valid.Matches(fieldset) {
		t.Fatal("valid owned controls did not validate form and fieldset")
	}
	if !invalid.Matches(other) || invalid.Matches(form) {
		t.Fatal("unowned invalid control affected the form aggregate")
	}
}

func TestValidityPseudoGrammarAndOperationBudget(t *testing.T) {
	t.Parallel()

	selectors, err := css.ParseSelectorList(":valid, :invalid")
	if err != nil {
		t.Fatal(err)
	}
	for _, selector := range selectors {
		if got, want := selector.Specificity(), (css.Specificity{Classes: 1}); got != want {
			t.Fatalf("specificity = %#v, want %#v", got, want)
		}
	}
	if _, err := css.ParseSelectorList(":valid()"); !errors.Is(err, css.ErrInvalidSelector) {
		t.Fatalf(":valid() error = %v, want ErrInvalidSelector", err)
	}

	form := dom.NewElement("form")
	for range 128 {
		form.AppendChild(dom.NewElement("input"))
	}
	if selectors[0].MatchesWithContext(form, css.MatchContext{OperationLimit: 16}) {
		t.Fatal(":valid matched after exhausting its aggregate traversal budget")
	}
	if !selectors[0].MatchesWithContext(form, css.MatchContext{OperationLimit: 2_000}) {
		t.Fatal(":valid did not match with a sufficient aggregate budget")
	}
}
