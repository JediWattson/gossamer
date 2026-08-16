package css_test

import (
	"errors"
	"testing"

	"github.com/JediWattson/gossamer/internal/css"
	"github.com/JediWattson/gossamer/internal/dom"
)

func TestUserValidityPseudosRequireCommittedCandidateState(t *testing.T) {
	t.Parallel()

	userValid := parseOneSelector(t, ":user-valid")
	userInvalid := parseOneSelector(t, ":user-invalid")
	input := dom.NewElement("input", dom.Attribute{Name: "required", Value: ""})
	if userValid.Matches(input) || userInvalid.Matches(input) {
		t.Fatal("untouched input matched a user-validity pseudo-class")
	}
	input.Control.UserValidity = true
	if userValid.Matches(input) || !userInvalid.Matches(input) {
		t.Fatal("committed empty required input did not match only :user-invalid")
	}
	input.Control.Value = "ready"
	input.Control.ValueDirty = true
	if !userValid.Matches(input) || userInvalid.Matches(input) {
		t.Fatal("committed populated input did not match only :user-valid")
	}
	input.Attributes = append(input.Attributes, dom.Attribute{Name: "disabled", Value: ""})
	if userValid.Matches(input) || userInvalid.Matches(input) {
		t.Fatal("barred disabled input matched a user-validity pseudo-class")
	}
}

func TestUserValidityPseudosDoNotAggregateAndHonorBudget(t *testing.T) {
	t.Parallel()

	userInvalid := parseOneSelector(t, ":user-invalid")
	form := dom.NewElement("form")
	fieldset := dom.NewElement("fieldset")
	input := dom.NewElement("input", dom.Attribute{Name: "required", Value: ""})
	input.Control.UserValidity = true
	fieldset.AppendChild(input)
	form.AppendChild(fieldset)
	if userInvalid.Matches(form) || userInvalid.Matches(fieldset) || !userInvalid.Matches(input) {
		t.Fatal(":user-invalid aggregated beyond input/select/textarea controls")
	}
	if userInvalid.MatchesWithContext(input, css.MatchContext{OperationLimit: 1}) {
		t.Fatal(":user-invalid matched after exhausting the validity operation budget")
	}
	if !userInvalid.MatchesWithContext(input, css.MatchContext{OperationLimit: 64}) {
		t.Fatal(":user-invalid did not match with a sufficient operation budget")
	}
}

func TestUserValidityPseudoGrammar(t *testing.T) {
	t.Parallel()

	selectors, err := css.ParseSelectorList(":user-valid, :user-invalid")
	if err != nil {
		t.Fatal(err)
	}
	for _, selector := range selectors {
		if got, want := selector.Specificity(), (css.Specificity{Classes: 1}); got != want {
			t.Fatalf("specificity = %#v, want %#v", got, want)
		}
	}
	for _, invalid := range []string{":user-valid()", ":user-invalid()"} {
		if _, err := css.ParseSelectorList(invalid); !errors.Is(err, css.ErrInvalidSelector) {
			t.Fatalf("ParseSelectorList(%q) error = %v, want ErrInvalidSelector", invalid, err)
		}
	}
}
