package css_test

import (
	"errors"
	"testing"

	"github.com/JediWattson/gossamer/internal/css"
	"github.com/JediWattson/gossamer/internal/dom"
)

func TestDefaultPseudoMatchesMarkupDefaultsNotLiveSelection(t *testing.T) {
	t.Parallel()

	selector := parseOneSelector(t, ":default")
	checkbox := dom.NewElement("input",
		dom.Attribute{Name: "type", Value: "checkbox"},
		dom.Attribute{Name: "checked", Value: ""},
	)
	if !selector.Matches(checkbox) {
		t.Fatal("checked checkbox did not match :default")
	}
	checkbox.Control.Checked = false
	checkbox.Control.CheckedDirty = true
	if !selector.Matches(checkbox) {
		t.Fatal("dirty unchecked checkbox lost its checked-attribute default")
	}
	checkbox.Attributes = checkbox.Attributes[:1]
	checkbox.Control.Checked = true
	if selector.Matches(checkbox) {
		t.Fatal("dirty checked checkbox without a checked attribute matched :default")
	}

	option := dom.NewElement("option", dom.Attribute{Name: "selected", Value: ""})
	option.Control.SelectedDirty = true
	option.Control.Selected = false
	if !selector.Matches(option) {
		t.Fatal("dirty deselected option lost its selected-attribute default")
	}
	plainOption := dom.NewElement("option")
	plainOption.Control.SelectedDirty = true
	plainOption.Control.Selected = true
	if selector.Matches(plainOption) {
		t.Fatal("dirty selected option without a selected attribute matched :default")
	}

	if selector.Matches(dom.NewElement("input", dom.Attribute{Name: "type", Value: "text"}, dom.Attribute{Name: "checked", Value: ""})) {
		t.Fatal("checked attribute on an inapplicable input type matched :default")
	}
}

func TestDefaultPseudoChoosesFirstSubmitButtonForFormOwner(t *testing.T) {
	t.Parallel()

	selector := parseOneSelector(t, ":default")
	document := dom.NewDocument()
	html := dom.NewElement("html")
	body := dom.NewElement("body")
	form := dom.NewElement("form", dom.Attribute{Name: "id", Value: "checkout"})
	first := dom.NewElement("button")
	second := dom.NewElement("input", dom.Attribute{Name: "type", Value: "submit"})
	form.AppendChild(first)
	form.AppendChild(second)
	body.AppendChild(form)
	html.AppendChild(body)
	document.AppendChild(html)

	if !selector.Matches(first) || selector.Matches(second) {
		t.Fatal(":default did not choose the first submit button in tree order")
	}
	first.Attributes = append(first.Attributes, dom.Attribute{Name: "type", Value: "button"})
	if selector.Matches(first) || !selector.Matches(second) {
		t.Fatal(":default did not update after the first button stopped submitting")
	}
	first.Attributes[0].Value = "submit"
	first.Attributes = append(first.Attributes, dom.Attribute{Name: "disabled", Value: ""})
	if !selector.Matches(first) || selector.Matches(second) {
		t.Fatal("disabled first submit button was not retained as the form default")
	}

	selectNode := dom.NewElement("select")
	selectButton := dom.NewElement("button")
	selectNode.AppendChild(selectButton)
	form.Children = append([]*dom.Node{selectNode}, form.Children...)
	selectNode.Parent = form
	if selector.Matches(selectButton) || !selector.Matches(first) {
		t.Fatal("select appearance button participated in default submission")
	}
}

func TestDefaultPseudoHonorsExternalFormOwnersAndFirstMatchingID(t *testing.T) {
	t.Parallel()

	selector := parseOneSelector(t, ":default")
	document := dom.NewDocument()
	html := dom.NewElement("html")
	body := dom.NewElement("body")
	otherOwnerButton := dom.NewElement("button", dom.Attribute{Name: "form", Value: "other"})
	external := dom.NewElement("button", dom.Attribute{Name: "form", Value: "checkout"})
	shadowID := dom.NewElement("div", dom.Attribute{Name: "id", Value: "blocked"})
	blocked := dom.NewElement("button", dom.Attribute{Name: "form", Value: "blocked"})
	emptyAssociation := dom.NewElement("button", dom.Attribute{Name: "form", Value: ""})
	emptyForm := dom.NewElement("form")
	emptyForm.AppendChild(emptyAssociation)
	checkout := dom.NewElement("form", dom.Attribute{Name: "id", Value: "checkout"})
	other := dom.NewElement("form", dom.Attribute{Name: "id", Value: "other"})
	inside := dom.NewElement("button")
	checkout.AppendChild(inside)
	for _, node := range []*dom.Node{otherOwnerButton, external, shadowID, blocked, checkout, other, emptyForm} {
		body.AppendChild(node)
	}
	html.AppendChild(body)
	document.AppendChild(html)

	if !selector.Matches(external) || selector.Matches(inside) {
		t.Fatal("externally associated earlier button was not the checkout default")
	}
	if !selector.Matches(otherOwnerButton) {
		t.Fatal("button for a different form owner interfered with tree ordering")
	}
	if selector.Matches(blocked) {
		t.Fatal("form attribute resolved past the first non-form element with the ID")
	}
	if selector.Matches(emptyAssociation) {
		t.Fatal("connected empty form attribute fell back to an ancestor or empty ID")
	}

	detachedForm := dom.NewElement("form")
	detachedButton := dom.NewElement("button", dom.Attribute{Name: "form", Value: "elsewhere"})
	detachedForm.AppendChild(detachedButton)
	if !selector.Matches(detachedButton) {
		t.Fatal("disconnected button did not fall back to its ancestor form owner")
	}
}

func TestDefaultPseudoGrammarAndOperationBudget(t *testing.T) {
	t.Parallel()

	selectors, err := css.ParseSelectorList(":default")
	if err != nil {
		t.Fatal(err)
	}
	if got, want := selectors[0].Specificity(), (css.Specificity{Classes: 1}); got != want {
		t.Fatalf(":default specificity = %#v, want %#v", got, want)
	}
	if _, err := css.ParseSelectorList(":default()"); !errors.Is(err, css.ErrInvalidSelector) {
		t.Fatalf(":default() error = %v, want ErrInvalidSelector", err)
	}

	document := dom.NewDocument()
	html := dom.NewElement("html")
	body := dom.NewElement("body")
	form := dom.NewElement("form")
	for range 128 {
		body.AppendChild(dom.NewElement("div"))
	}
	target := dom.NewElement("button")
	form.AppendChild(target)
	body.AppendChild(form)
	html.AppendChild(body)
	document.AppendChild(html)
	selector := selectors[0]
	if selector.MatchesWithContext(target, css.MatchContext{OperationLimit: 32}) {
		t.Fatal(":default matched after exhausting its tree-order budget")
	}
	if !selector.MatchesWithContext(target, css.MatchContext{OperationLimit: 1_000}) {
		t.Fatal(":default did not match with a sufficient tree-order budget")
	}
}
