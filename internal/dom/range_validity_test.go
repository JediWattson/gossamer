package dom

import "testing"

func TestRangeValidityCoversNumericLimitsAndBarredControls(t *testing.T) {
	t.Parallel()

	assert := func(name string, node *Node, participates, inRange bool) {
		t.Helper()
		t.Run(name, func(t *testing.T) {
			gotParticipates, gotInRange, complete := EvaluateRangeValidity(node, nil)
			if !complete || gotParticipates != participates || gotInRange != inRange {
				t.Fatalf("range state = participates:%t in:%t complete:%t; want %t, %t, true", gotParticipates, gotInRange, complete, participates, inRange)
			}
		})
	}

	assert("number in range", NewElement("input", Attribute{Name: "type", Value: "number"}, Attribute{Name: "min", Value: "5"}, Attribute{Name: "max", Value: "10"}, Attribute{Name: "value", Value: "7"}), true, true)
	assert("number underflow", NewElement("input", Attribute{Name: "type", Value: "number"}, Attribute{Name: "min", Value: "5"}, Attribute{Name: "value", Value: "4"}), true, false)
	assert("number overflow", NewElement("input", Attribute{Name: "type", Value: "number"}, Attribute{Name: "max", Value: "10"}, Attribute{Name: "value", Value: "11"}), true, false)
	assert("empty number", NewElement("input", Attribute{Name: "type", Value: "number"}, Attribute{Name: "min", Value: "5"}), true, true)
	assert("number without limits", NewElement("input", Attribute{Name: "type", Value: "number"}, Attribute{Name: "value", Value: "7"}), false, false)
	assert("invalid limits", NewElement("input", Attribute{Name: "type", Value: "number"}, Attribute{Name: "min", Value: "bad"}, Attribute{Name: "value", Value: "7"}), false, false)
	assert("disabled number", NewElement("input", Attribute{Name: "type", Value: "number"}, Attribute{Name: "min", Value: "5"}, Attribute{Name: "disabled", Value: ""}), false, false)
	assert("readonly number", NewElement("input", Attribute{Name: "type", Value: "number"}, Attribute{Name: "min", Value: "5"}, Attribute{Name: "readonly", Value: ""}), false, false)
	assert("range defaults", NewElement("input", Attribute{Name: "type", Value: "range"}), true, true)
	assert("range sanitizes overflow", NewElement("input", Attribute{Name: "type", Value: "range"}, Attribute{Name: "value", Value: "200"}), true, true)
}

func TestRangeValidityParsesHTMLDateAndTimeStates(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name     string
		typeName string
		min      string
		max      string
		value    string
		inRange  bool
	}{
		{name: "leap date", typeName: "date", min: "2024-02-29", max: "2024-03-02", value: "2024-03-01", inRange: true},
		{name: "date underflow", typeName: "date", min: "2024-02-29", value: "2024-02-28", inRange: false},
		{name: "month overflow", typeName: "month", max: "2026-08", value: "2026-09", inRange: false},
		{name: "ISO week 53", typeName: "week", min: "2020-W53", max: "2021-W01", value: "2020-W53", inRange: true},
		{name: "reversed time late", typeName: "time", min: "22:00", max: "02:00", value: "23:30", inRange: true},
		{name: "reversed time early", typeName: "time", min: "22:00", max: "02:00", value: "01:30", inRange: true},
		{name: "reversed time gap", typeName: "time", min: "22:00", max: "02:00", value: "12:00", inRange: false},
		{name: "local datetime overflow", typeName: "datetime-local", max: "2026-08-16T12:30", value: "2026-08-16T12:31", inRange: false},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			attributes := []Attribute{{Name: "type", Value: test.typeName}, {Name: "value", Value: test.value}}
			if test.min != "" {
				attributes = append(attributes, Attribute{Name: "min", Value: test.min})
			}
			if test.max != "" {
				attributes = append(attributes, Attribute{Name: "max", Value: test.max})
			}
			participates, inRange, complete := EvaluateRangeValidity(NewElement("input", attributes...), nil)
			if !participates || !complete || inRange != test.inRange {
				t.Fatalf("range state = participates:%t in:%t complete:%t, want true, %t, true", participates, inRange, complete, test.inRange)
			}
			candidate, valid, validityComplete := EvaluateConstraintValidity(NewElement("input", attributes...), nil)
			if !candidate || !validityComplete || valid != test.inRange {
				t.Fatalf("constraint state = candidate:%t valid:%t complete:%t, want true, %t, true", candidate, valid, validityComplete, test.inRange)
			}
		})
	}
}

func TestRangeValidityFailsClosedWhenCandidateScanExhausts(t *testing.T) {
	t.Parallel()

	fieldset := NewElement("fieldset", Attribute{Name: "disabled", Value: ""})
	for range 64 {
		fieldset.AppendChild(NewElement("div"))
	}
	target := NewElement("input", Attribute{Name: "type", Value: "number"}, Attribute{Name: "min", Value: "1"}, Attribute{Name: "value", Value: "2"})
	fieldset.AppendChild(target)
	remaining := 1
	if _, _, complete := EvaluateRangeValidity(target, func() bool {
		remaining--
		return remaining >= 0
	}); complete {
		t.Fatal("range validation completed after its traversal budget expired")
	}
}
