package css_test

import (
	"testing"

	"github.com/JediWattson/gossamer/internal/dom"
)

func TestCheckedPseudoUsesLiveInputAndOptionState(t *testing.T) {
	t.Parallel()

	checked := parseOneSelector(t, ":checked")
	checkbox := dom.NewElement("input",
		dom.Attribute{Name: "type", Value: "checkbox"},
		dom.Attribute{Name: "checked", Value: ""},
	)
	if !checked.Matches(checkbox) {
		t.Fatal("checked checkbox content state did not match :checked")
	}
	checkbox.Control.Checked = false
	checkbox.Control.CheckedDirty = true
	if checked.Matches(checkbox) {
		t.Fatal("dirty unchecked checkbox still matched its checked content attribute")
	}
	checkbox.Control.Checked = true
	if !checked.Matches(checkbox) {
		t.Fatal("dirty checked checkbox did not match :checked")
	}

	text := dom.NewElement("input", dom.Attribute{Name: "checked", Value: ""})
	if checked.Matches(text) {
		t.Fatal("text input matched :checked from an inapplicable attribute")
	}

	selectNode := dom.NewElement("select")
	first := dom.NewElement("option")
	second := dom.NewElement("option", dom.Attribute{Name: "disabled", Value: ""})
	third := dom.NewElement("option")
	selectNode.AppendChild(first)
	selectNode.AppendChild(second)
	selectNode.AppendChild(third)
	if !checked.Matches(first) || checked.Matches(second) || checked.Matches(third) {
		t.Fatal("single-select implicit selectedness did not choose the first enabled option")
	}
	for _, option := range []*dom.Node{first, second, third} {
		option.Control.SelectedDirty = true
		option.Control.Selected = option == third
	}
	if checked.Matches(first) || checked.Matches(second) || !checked.Matches(third) {
		t.Fatal("dirty option selectedness did not override implicit selection")
	}

	multiple := dom.NewElement("select", dom.Attribute{Name: "multiple", Value: ""})
	selected := dom.NewElement("option", dom.Attribute{Name: "selected", Value: ""})
	multiple.AppendChild(selected)
	if !checked.Matches(selected) {
		t.Fatal("selected option in a multiple select did not match :checked")
	}
	selected.Control.SelectedDirty = true
	selected.Control.Selected = false
	if checked.Matches(selected) {
		t.Fatal("dirty deselected option still matched its selected content attribute")
	}
}

func TestEnabledAndDisabledPseudosFollowHTMLDisabledness(t *testing.T) {
	t.Parallel()

	enabled := parseOneSelector(t, ":enabled")
	disabled := parseOneSelector(t, ":disabled")
	assertState := func(name string, node *dom.Node, wantEnabled, wantDisabled bool) {
		t.Helper()
		t.Run(name, func(t *testing.T) {
			t.Helper()
			if got := enabled.Matches(node); got != wantEnabled {
				t.Errorf(":enabled = %t, want %t", got, wantEnabled)
			}
			if got := disabled.Matches(node); got != wantDisabled {
				t.Errorf(":disabled = %t, want %t", got, wantDisabled)
			}
		})
	}

	fieldset := dom.NewElement("fieldset", dom.Attribute{Name: "disabled", Value: ""})
	firstLegend := dom.NewElement("legend")
	escaped := dom.NewElement("input")
	firstLegend.AppendChild(escaped)
	secondLegend := dom.NewElement("legend")
	notEscaped := dom.NewElement("button")
	secondLegend.AppendChild(notEscaped)
	descendant := dom.NewElement("textarea")
	nestedFieldset := dom.NewElement("fieldset")
	nestedControl := dom.NewElement("select")
	nestedFieldset.AppendChild(nestedControl)
	fieldset.AppendChild(dom.NewText("caption gap"))
	fieldset.AppendChild(firstLegend)
	fieldset.AppendChild(secondLegend)
	fieldset.AppendChild(descendant)
	fieldset.AppendChild(nestedFieldset)

	assertState("disabled fieldset", fieldset, false, true)
	assertState("first legend exception", escaped, true, false)
	assertState("second legend descendant", notEscaped, false, true)
	assertState("fieldset descendant", descendant, false, true)
	assertState("nested fieldset", nestedFieldset, false, true)
	assertState("nested fieldset control", nestedControl, false, true)

	selectNode := dom.NewElement("select", dom.Attribute{Name: "disabled", Value: ""})
	group := dom.NewElement("optgroup")
	option := dom.NewElement("option")
	group.AppendChild(option)
	selectNode.AppendChild(group)
	assertState("disabled select", selectNode, false, true)
	assertState("optgroup in disabled select", group, false, true)
	assertState("option in disabled select", option, false, true)

	enabledSelect := dom.NewElement("select")
	disabledGroup := dom.NewElement("optgroup", dom.Attribute{Name: "disabled", Value: ""})
	groupOption := dom.NewElement("option")
	disabledGroup.AppendChild(groupOption)
	enabledSelect.AppendChild(disabledGroup)
	assertState("disabled optgroup", disabledGroup, false, true)
	assertState("option in disabled optgroup", groupOption, false, true)

	plainOption := dom.NewElement("option")
	assertState("detached option", plainOption, true, false)
	assertState("plain button", dom.NewElement("button"), true, false)
	assertState("disabled button", dom.NewElement("button", dom.Attribute{Name: "disabled", Value: ""}), false, true)
	assertState("non-control", dom.NewElement("div", dom.Attribute{Name: "disabled", Value: ""}), false, false)
}

func TestRequiredAndOptionalPseudosRespectInputTypeApplicability(t *testing.T) {
	t.Parallel()

	required := parseOneSelector(t, ":required")
	optional := parseOneSelector(t, ":optional")
	assertState := func(name string, node *dom.Node, wantRequired, wantOptional bool) {
		t.Helper()
		t.Run(name, func(t *testing.T) {
			if got := required.Matches(node); got != wantRequired {
				t.Errorf(":required = %t, want %t", got, wantRequired)
			}
			if got := optional.Matches(node); got != wantOptional {
				t.Errorf(":optional = %t, want %t", got, wantOptional)
			}
		})
	}

	assertState("required text", dom.NewElement("input", dom.Attribute{Name: "required", Value: ""}), true, false)
	assertState("optional email", dom.NewElement("input", dom.Attribute{Name: "type", Value: "email"}), false, true)
	assertState("required checkbox", dom.NewElement("input", dom.Attribute{Name: "type", Value: "checkbox"}, dom.Attribute{Name: "required", Value: ""}), true, false)
	assertState("required select", dom.NewElement("select", dom.Attribute{Name: "required", Value: ""}), true, false)
	assertState("optional textarea", dom.NewElement("textarea"), false, true)

	for _, typeName := range []string{"hidden", "range", "color", "submit", "image", "reset", "button"} {
		assertState("inapplicable "+typeName, dom.NewElement("input",
			dom.Attribute{Name: "type", Value: typeName},
			dom.Attribute{Name: "required", Value: ""},
		), false, false)
	}
	assertState("non-control", dom.NewElement("div", dom.Attribute{Name: "required", Value: ""}), false, false)
	nonHTML := dom.NewElement("input", dom.Attribute{Name: "required", Value: ""})
	nonHTML.NamespaceURI = dom.SVGNamespace
	assertState("foreign namespace", nonHTML, false, false)
}
