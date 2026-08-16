package css_test

import (
	"errors"
	"testing"

	"github.com/JediWattson/gossamer/internal/css"
	"github.com/JediWattson/gossamer/internal/dom"
)

func TestIndeterminatePseudoMatchesCheckboxAndProgressState(t *testing.T) {
	t.Parallel()

	selector := parseOneSelector(t, ":indeterminate")
	checkbox := dom.NewElement("input", dom.Attribute{Name: "type", Value: "checkbox"})
	if selector.Matches(checkbox) {
		t.Fatal("ordinary checkbox matched :indeterminate")
	}
	checkbox.Control.Indeterminate = true
	if !selector.Matches(checkbox) {
		t.Fatal("live indeterminate checkbox did not match")
	}
	checkbox.Control.Checked = true
	checkbox.Control.CheckedDirty = true
	if !selector.Matches(checkbox) {
		t.Fatal("checkedness incorrectly cleared independent indeterminateness")
	}
	text := dom.NewElement("input")
	text.Control.Indeterminate = true
	if selector.Matches(text) {
		t.Fatal("indeterminate IDL state on text input matched the pseudo-class")
	}

	progress := dom.NewElement("progress")
	if !selector.Matches(progress) {
		t.Fatal("progress without value did not match :indeterminate")
	}
	progress.Attributes = append(progress.Attributes, dom.Attribute{Name: "value", Value: "invalid"})
	if selector.Matches(progress) {
		t.Fatal("progress with a value attribute matched :indeterminate")
	}
}

func TestIndeterminatePseudoUsesLiveRadioGroupCheckedness(t *testing.T) {
	t.Parallel()

	selector := parseOneSelector(t, ":indeterminate")
	document := dom.NewDocument()
	html := dom.NewElement("html")
	body := dom.NewElement("body")
	form := dom.NewElement("form", dom.Attribute{Name: "id", Value: "first"})
	first := dom.NewElement("input", dom.Attribute{Name: "type", Value: "radio"}, dom.Attribute{Name: "name", Value: "pick"})
	second := dom.NewElement("input", dom.Attribute{Name: "type", Value: "radio"}, dom.Attribute{Name: "name", Value: "pick"})
	otherForm := dom.NewElement("form", dom.Attribute{Name: "id", Value: "second"})
	otherOwner := dom.NewElement("input", dom.Attribute{Name: "type", Value: "radio"}, dom.Attribute{Name: "name", Value: "pick"})
	unnamed := dom.NewElement("input", dom.Attribute{Name: "type", Value: "radio"})
	form.AppendChild(first)
	form.AppendChild(second)
	otherForm.AppendChild(otherOwner)
	body.AppendChild(form)
	body.AppendChild(otherForm)
	body.AppendChild(unnamed)
	html.AppendChild(body)
	document.AppendChild(html)

	for _, radio := range []*dom.Node{first, second, otherOwner, unnamed} {
		if !selector.Matches(radio) {
			t.Fatalf("unchecked radio %p did not match :indeterminate", radio)
		}
	}
	second.Control.Checked = true
	second.Control.CheckedDirty = true
	if selector.Matches(first) || selector.Matches(second) {
		t.Fatal("radio group with a checked member remained indeterminate")
	}
	if !selector.Matches(otherOwner) || !selector.Matches(unnamed) {
		t.Fatal("checked radio affected a different group")
	}
}

func TestIndeterminatePseudoGrammarAndOperationBudget(t *testing.T) {
	t.Parallel()

	selectors, err := css.ParseSelectorList(":indeterminate")
	if err != nil {
		t.Fatal(err)
	}
	if got, want := selectors[0].Specificity(), (css.Specificity{Classes: 1}); got != want {
		t.Fatalf("specificity = %#v, want %#v", got, want)
	}
	if _, err := css.ParseSelectorList(":indeterminate()"); !errors.Is(err, css.ErrInvalidSelector) {
		t.Fatalf(":indeterminate() error = %v, want ErrInvalidSelector", err)
	}

	root := dom.NewDocument()
	form := dom.NewElement("form")
	target := dom.NewElement("input", dom.Attribute{Name: "type", Value: "radio"}, dom.Attribute{Name: "name", Value: "pick"})
	form.AppendChild(target)
	for range 128 {
		form.AppendChild(dom.NewElement("div"))
	}
	root.AppendChild(form)
	if selectors[0].MatchesWithContext(target, css.MatchContext{OperationLimit: 16}) {
		t.Fatal(":indeterminate matched after exhausting the radio-group scan budget")
	}
	if !selectors[0].MatchesWithContext(target, css.MatchContext{OperationLimit: 1_000}) {
		t.Fatal(":indeterminate did not match with a sufficient budget")
	}
}
