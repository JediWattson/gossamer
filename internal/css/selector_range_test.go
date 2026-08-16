package css_test

import (
	"errors"
	"testing"

	"github.com/JediWattson/gossamer/internal/css"
	"github.com/JediWattson/gossamer/internal/dom"
)

func TestRangePseudosUseLiveTypedInputValues(t *testing.T) {
	t.Parallel()

	inRange := parseOneSelector(t, ":in-range")
	outOfRange := parseOneSelector(t, ":out-of-range")
	number := dom.NewElement("input",
		dom.Attribute{Name: "type", Value: "number"},
		dom.Attribute{Name: "min", Value: "5"},
		dom.Attribute{Name: "max", Value: "10"},
		dom.Attribute{Name: "value", Value: "7"},
	)
	if !inRange.Matches(number) || outOfRange.Matches(number) {
		t.Fatal("numeric input did not initially match only :in-range")
	}
	number.Control.Value = "11"
	number.Control.ValueDirty = true
	if inRange.Matches(number) || !outOfRange.Matches(number) {
		t.Fatal("dirty overflow did not match only :out-of-range")
	}
	number.Control.Value = ""
	if !inRange.Matches(number) || outOfRange.Matches(number) {
		t.Fatal("empty limited input suffered range overflow or underflow")
	}
	plain := dom.NewElement("input", dom.Attribute{Name: "type", Value: "number"})
	if inRange.Matches(plain) || outOfRange.Matches(plain) {
		t.Fatal("input without range limitations matched a range pseudo-class")
	}
}

func TestRangePseudoGrammarAndSpecificity(t *testing.T) {
	t.Parallel()

	selectors, err := css.ParseSelectorList(":in-range, :out-of-range")
	if err != nil {
		t.Fatal(err)
	}
	for _, selector := range selectors {
		if got, want := selector.Specificity(), (css.Specificity{Classes: 1}); got != want {
			t.Fatalf("specificity = %#v, want %#v", got, want)
		}
	}
	if _, err := css.ParseSelectorList(":in-range()"); !errors.Is(err, css.ErrInvalidSelector) {
		t.Fatalf(":in-range() error = %v, want ErrInvalidSelector", err)
	}
}
