package css_test

import (
	"errors"
	"testing"

	"github.com/JediWattson/gossamer/internal/css"
	"github.com/JediWattson/gossamer/internal/dom"
)

func TestMutabilityPseudoClassesFollowHTMLState(t *testing.T) {
	t.Parallel()

	readWrite := parseOneSelector(t, ":read-write")
	readOnly := parseOneSelector(t, ":read-only")
	assertState := func(name string, node *dom.Node, wantReadWrite bool) {
		t.Helper()
		t.Run(name, func(t *testing.T) {
			if got := readWrite.Matches(node); got != wantReadWrite {
				t.Errorf(":read-write = %t, want %t", got, wantReadWrite)
			}
			if got := readOnly.Matches(node); got == wantReadWrite {
				t.Errorf(":read-only = %t, want %t", got, !wantReadWrite)
			}
		})
	}

	assertState("text input", dom.NewElement("input"), true)
	assertState("readonly text input", dom.NewElement("input", dom.Attribute{Name: "readonly", Value: ""}), false)
	assertState("number input", dom.NewElement("input", dom.Attribute{Name: "type", Value: "number"}), true)
	assertState("readonly number", dom.NewElement("input", dom.Attribute{Name: "type", Value: "number"}, dom.Attribute{Name: "readonly", Value: ""}), false)
	assertState("checkbox", dom.NewElement("input", dom.Attribute{Name: "type", Value: "checkbox"}), false)
	assertState("range", dom.NewElement("input", dom.Attribute{Name: "type", Value: "range"}), false)
	assertState("textarea", dom.NewElement("textarea"), true)
	assertState("readonly textarea", dom.NewElement("textarea", dom.Attribute{Name: "readonly", Value: ""}), false)
	assertState("ordinary element", dom.NewElement("div"), false)

	fieldset := dom.NewElement("fieldset", dom.Attribute{Name: "disabled", Value: ""})
	disabledInput := dom.NewElement("input")
	fieldset.AppendChild(disabledInput)
	assertState("disabled fieldset input", disabledInput, false)

	host := dom.NewElement("section", dom.Attribute{Name: "contenteditable", Value: "plaintext-only"})
	editable := dom.NewElement("p")
	invalidInherits := dom.NewElement("span", dom.Attribute{Name: "contenteditable", Value: "invalid"})
	nestedReadOnly := dom.NewElement("strong", dom.Attribute{Name: "contenteditable", Value: "false"})
	editable.AppendChild(invalidInherits)
	invalidInherits.AppendChild(nestedReadOnly)
	host.AppendChild(editable)
	assertState("editing host", host, true)
	assertState("editable descendant", editable, true)
	assertState("invalid value inherits", invalidInherits, true)
	assertState("false state boundary", nestedReadOnly, false)

	foreign := dom.NewElement("svg")
	foreign.NamespaceURI = dom.SVGNamespace
	host.AppendChild(foreign)
	assertState("foreign editable descendant", foreign, true)

	checkboxInHost := dom.NewElement("input", dom.Attribute{Name: "type", Value: "checkbox"})
	host.AppendChild(checkboxInHost)
	assertState("input applicability wins over editable ancestor", checkboxInHost, false)
}

func TestPlaceholderShownUsesApplicableTypeAndLiveValue(t *testing.T) {
	t.Parallel()

	selector := parseOneSelector(t, ":placeholder-shown")
	assertMatch := func(name string, node *dom.Node, want bool) {
		t.Helper()
		t.Run(name, func(t *testing.T) {
			if got := selector.Matches(node); got != want {
				t.Errorf(":placeholder-shown = %t, want %t", got, want)
			}
		})
	}

	empty := dom.NewElement("input", dom.Attribute{Name: "placeholder", Value: "hint"})
	assertMatch("empty text input", empty, true)
	assertMatch("empty placeholder text", dom.NewElement("input", dom.Attribute{Name: "placeholder", Value: ""}), true)
	assertMatch("default value", dom.NewElement("input", dom.Attribute{Name: "placeholder", Value: "hint"}, dom.Attribute{Name: "value", Value: "typed"}), false)
	assertMatch("number input", dom.NewElement("input", dom.Attribute{Name: "type", Value: "number"}, dom.Attribute{Name: "placeholder", Value: "0"}), true)
	assertMatch("date input", dom.NewElement("input", dom.Attribute{Name: "type", Value: "date"}, dom.Attribute{Name: "placeholder", Value: "date"}), false)
	assertMatch("no placeholder", dom.NewElement("input"), false)
	assertMatch("ordinary element", dom.NewElement("div", dom.Attribute{Name: "placeholder", Value: "hint"}), false)

	empty.Control.Value = "typed"
	empty.Control.ValueDirty = true
	assertMatch("dirty nonempty value", empty, false)
	empty.Control.Value = ""
	assertMatch("dirty empty value", empty, true)

	textarea := dom.NewElement("textarea", dom.Attribute{Name: "placeholder", Value: "hint"})
	assertMatch("empty textarea", textarea, true)
	textarea.AppendChild(dom.NewText("default"))
	assertMatch("textarea default value", textarea, false)
	textarea.Control.ValueDirty = true
	textarea.Control.Value = ""
	assertMatch("dirty empty textarea", textarea, true)
}

func TestMutabilityPseudoGrammarAndOperationBudget(t *testing.T) {
	t.Parallel()

	for _, source := range []string{":read-only", ":read-write", ":placeholder-shown"} {
		selectors, err := css.ParseSelectorList(source)
		if err != nil {
			t.Errorf("ParseSelectorList(%q) = %v", source, err)
			continue
		}
		if got, want := selectors[0].Specificity(), (css.Specificity{Classes: 1}); got != want {
			t.Errorf("Specificity(%q) = %#v, want %#v", source, got, want)
		}
	}
	for _, source := range []string{":read-only()", ":read-write(value)", ":placeholder-shown()"} {
		if _, err := css.ParseSelectorList(source); !errors.Is(err, css.ErrInvalidSelector) {
			t.Errorf("ParseSelectorList(%q) error = %v, want ErrInvalidSelector", source, err)
		}
	}

	host := dom.NewElement("div", dom.Attribute{Name: "contenteditable", Value: "true"})
	current := host
	for range 128 {
		child := dom.NewElement("div")
		current.AppendChild(child)
		current = child
	}
	readWrite := parseOneSelector(t, ":read-write")
	readOnly := parseOneSelector(t, ":read-only")
	if readWrite.MatchesWithContext(current, css.MatchContext{OperationLimit: 32}) {
		t.Fatal(":read-write matched after exhausting its ancestor budget")
	}
	if readOnly.MatchesWithContext(current, css.MatchContext{OperationLimit: 32}) {
		t.Fatal(":read-only turned an exhausted :read-write check into a match")
	}
	if !readWrite.MatchesWithContext(current, css.MatchContext{OperationLimit: 1_000}) {
		t.Fatal(":read-write did not match with a sufficient ancestor budget")
	}

	textarea := dom.NewElement("textarea", dom.Attribute{Name: "placeholder", Value: "hint"})
	current = textarea
	for range 128 {
		child := dom.NewElement("span")
		current.AppendChild(child)
		current = child
	}
	placeholder := parseOneSelector(t, ":placeholder-shown")
	if placeholder.MatchesWithContext(textarea, css.MatchContext{OperationLimit: 32}) {
		t.Fatal(":placeholder-shown matched after exhausting its textarea budget")
	}
	if !placeholder.MatchesWithContext(textarea, css.MatchContext{OperationLimit: 1_000}) {
		t.Fatal(":placeholder-shown did not match with a sufficient textarea budget")
	}
}
