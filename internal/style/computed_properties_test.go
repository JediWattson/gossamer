package style

import (
	"image/color"
	"slices"
	"testing"

	"github.com/JediWattson/gossamer/internal/css"
)

func TestComputedPropertyValueSerializesEverySupportedLonghand(t *testing.T) {
	computed := ComputedStyle{
		display:         DisplayBlock,
		color:           color.NRGBA{R: 0x12, G: 0x34, B: 0x56, A: 0xff},
		background:      color.NRGBA{R: 10, G: 20, B: 30, A: 128},
		hasBackground:   true,
		fontSize:        18.5,
		fontWeightValue: 725,
		lineHeight:      LineHeight{value: 21.25, absolute: true},
		textDecoration:  TextDecorationUnderline,
		textAlign:       AlignRight,
		listStyleType:   ListStyleSquare,
		opacity:         0.5,
		width:           Length{unit: LengthAuto},
		height:          Length{value: 12, unit: LengthPX},
		minWidth:        Length{value: 25, unit: LengthPercent},
		maxWidth:        Length{value: 75, unit: LengthVW},
		paddingTop:      Length{value: 4, unit: LengthVH},
		paddingRight:    Length{value: 3, unit: LengthVW},
		paddingBottom:   Length{value: 2.5, unit: LengthPercent},
		paddingLeft:     Length{value: 1.25, unit: LengthPX},
		borderTop: BorderSide{
			width:    Length{value: 1, unit: LengthPX},
			style:    BorderStyleSolid,
			color:    color.NRGBA{R: 255, A: 255},
			hasColor: true,
		},
		borderRight: BorderSide{
			width: Length{value: 2, unit: LengthVW},
			style: BorderStyleHidden,
		},
		borderBottom: BorderSide{
			width:    Length{value: 3, unit: LengthVH},
			style:    BorderStyleNone,
			color:    color.NRGBA{G: 128, A: 255},
			hasColor: true,
		},
		borderLeft: BorderSide{
			width: Length{value: 4.5, unit: LengthPX},
			style: BorderStyleSolid,
		},
		marginTop:    Length{value: -1.5, unit: LengthPX},
		marginRight:  Length{value: 10, unit: LengthPercent},
		marginBottom: Length{value: 2, unit: LengthVH},
		marginLeft:   Length{unit: LengthAuto},
	}

	want := map[string]string{
		"background-color":     "rgba(10, 20, 30, 0.5)",
		"border-bottom-color":  "rgb(0, 128, 0)",
		"border-bottom-style":  "none",
		"border-bottom-width":  "0px",
		"border-left-color":    "rgb(18, 52, 86)",
		"border-left-style":    "solid",
		"border-left-width":    "4.5px",
		"border-right-color":   "rgb(18, 52, 86)",
		"border-right-style":   "hidden",
		"border-right-width":   "0px",
		"border-top-color":     "rgb(255, 0, 0)",
		"border-top-style":     "solid",
		"border-top-width":     "1px",
		"color":                "rgb(18, 52, 86)",
		"display":              "block",
		"font-size":            "18.5px",
		"font-weight":          "725",
		"height":               "12px",
		"line-height":          "21.25px",
		"list-style-type":      "square",
		"margin-bottom":        "2vh",
		"margin-left":          "auto",
		"margin-right":         "10%",
		"margin-top":           "-1.5px",
		"max-width":            "75vw",
		"min-width":            "25%",
		"opacity":              "0.5",
		"padding-bottom":       "2.5%",
		"padding-left":         "1.25px",
		"padding-right":        "3vw",
		"padding-top":          "4vh",
		"text-align":           "right",
		"text-decoration-line": "underline",
		"width":                "auto",
	}

	for property, expected := range want {
		t.Run(property, func(t *testing.T) {
			got, ok := ComputedPropertyValue(computed, property)
			if !ok || got != expected {
				t.Fatalf("ComputedPropertyValue(%q) = %q, %t, want %q, true", property, got, ok, expected)
			}
		})
	}

	if got, ok := ComputedPropertyValue(computed, "CoLoR"); !ok || got != "rgb(18, 52, 86)" {
		t.Fatalf("ASCII-insensitive color lookup = %q, %t", got, ok)
	}
	for _, unsupported := range []string{"", " color", "margin", "background", "border-top", "COLOR\u212A"} {
		if got, ok := ComputedPropertyValue(computed, unsupported); ok || got != "" {
			t.Errorf("unsupported lookup %q = %q, %t, want empty, false", unsupported, got, ok)
		}
	}
}

func TestComputedPropertyValueSerializesInitialAndAlternateEnums(t *testing.T) {
	tests := []struct {
		name     string
		computed ComputedStyle
		property string
		want     string
	}{
		{name: "transparent background", property: "background-color", want: "rgba(0, 0, 0, 0)"},
		{name: "normal line height", computed: ComputedStyle{lineHeight: LineHeight{normal: true}}, property: "line-height", want: "normal"},
		{name: "explicit unitless line height", computed: ComputedStyle{lineHeight: LineHeight{value: 1.2}}, property: "line-height", want: "1.2"},
		{name: "negative zero", computed: ComputedStyle{opacity: -0.0}, property: "opacity", want: "0"},
		{name: "number precision", computed: ComputedStyle{opacity: 1.0 / 3.0}, property: "opacity", want: "0.3333333333333333"},
		{name: "small nonzero number", computed: ComputedStyle{opacity: 0.0000001}, property: "opacity", want: "0.0000001"},
		{name: "normal font weight", computed: ComputedStyle{fontWeightValue: 400}, property: "font-weight", want: "400"},
		{name: "inline display", property: "display", want: "inline"},
		{name: "inline block display", computed: ComputedStyle{display: DisplayInlineBlock}, property: "display", want: "inline-block"},
		{name: "list item display", computed: ComputedStyle{display: DisplayListItem}, property: "display", want: "list-item"},
		{name: "none display", computed: ComputedStyle{display: DisplayNone}, property: "display", want: "none"},
		{name: "center align", computed: ComputedStyle{textAlign: AlignCenter}, property: "text-align", want: "center"},
		{name: "left align", property: "text-align", want: "left"},
		{name: "start align", computed: ComputedStyle{textAlign: AlignStart}, property: "text-align", want: "start"},
		{name: "end align", computed: ComputedStyle{textAlign: AlignEnd}, property: "text-align", want: "end"},
		{name: "justify align", computed: ComputedStyle{textAlign: AlignJustify}, property: "text-align", want: "justify"},
		{name: "circle list", computed: ComputedStyle{listStyleType: ListStyleCircle}, property: "list-style-type", want: "circle"},
		{name: "decimal list", computed: ComputedStyle{listStyleType: ListStyleDecimal}, property: "list-style-type", want: "decimal"},
		{name: "none list", computed: ComputedStyle{listStyleType: ListStyleNone}, property: "list-style-type", want: "none"},
		{name: "disc list", property: "list-style-type", want: "disc"},
		{name: "no decoration", property: "text-decoration-line", want: "none"},
		{name: "auto max width", computed: ComputedStyle{maxWidth: Length{unit: LengthAuto}}, property: "max-width", want: "none"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, ok := ComputedPropertyValue(test.computed, test.property)
			if !ok || got != test.want {
				t.Fatalf("ComputedPropertyValue(%q) = %q, %t, want %q, true", test.property, got, ok, test.want)
			}
		})
	}
}

func TestComputedColorAlphaSerializationPreservesEightBitValues(t *testing.T) {
	for _, test := range []struct {
		alpha uint8
		want  string
	}{
		{alpha: 128, want: "rgba(1, 2, 3, 0.5)"},
		{alpha: 236, want: "rgba(1, 2, 3, 0.9254901960784314)"},
		{alpha: 1, want: "rgba(1, 2, 3, 0.00392156862745098)"},
	} {
		got := serializeComputedColor(color.NRGBA{R: 1, G: 2, B: 3, A: test.alpha})
		if got != test.want {
			t.Errorf("alpha %d serialization = %q, want %q", test.alpha, got, test.want)
		}
	}
}

func TestComputedPropertyValueUsesResolvedBorderAndMaxWidths(t *testing.T) {
	computed := cssInitialStyle(Environment{})
	for _, property := range []string{
		"border-top-width",
		"border-right-width",
		"border-bottom-width",
		"border-left-width",
	} {
		if got, ok := ComputedPropertyValue(computed, property); !ok || got != "0px" {
			t.Errorf("initial %s = %q, %t, want 0px, true", property, got, ok)
		}
	}
	if got, ok := ComputedPropertyValue(computed, "max-width"); !ok || got != "none" {
		t.Errorf("initial max-width = %q, %t, want none, true", got, ok)
	}
}

func TestComputedPropertiesCustomLookupAndDeterministicEnumeration(t *testing.T) {
	parent := css.ResolveCustomProperties(css.CustomProperties{}, map[string]string{
		"--base": "red",
		"--gone": "old",
	})
	properties := css.ResolveCustomProperties(parent, map[string]string{
		"--Theme": "warm",
		"--alias": "var(--base)",
		"--empty": "",
		"--gone":  "initial",
	})
	computed := ComputedStyle{customProperties: properties}

	for _, test := range []struct {
		name   string
		want   string
		wantOK bool
	}{
		{name: "--Theme", want: "warm", wantOK: true},
		{name: "--alias", want: "red", wantOK: true},
		{name: "--base", want: "red", wantOK: true},
		{name: "--empty", want: "", wantOK: true},
		{name: "--theme", want: "", wantOK: false},
		{name: "--gone", want: "", wantOK: false},
		{name: "--missing", want: "", wantOK: false},
	} {
		got, ok := ComputedPropertyValue(computed, test.name)
		if got != test.want || ok != test.wantOK {
			t.Errorf("ComputedPropertyValue(%q) = %q, %t, want %q, %t", test.name, got, ok, test.want, test.wantOK)
		}
	}

	wantNames := append([]string(nil), computedPropertyNames[:]...)
	wantNames = append(wantNames, "--Theme", "--alias", "--base", "--empty")
	gotNames := ComputedPropertyNames(computed)
	if !slices.Equal(gotNames, wantNames) {
		t.Fatalf("ComputedPropertyNames() = %q, want %q", gotNames, wantNames)
	}
	gotNames[0] = "mutated"
	if next := ComputedPropertyNames(computed); !slices.Equal(next, wantNames) {
		t.Fatalf("ComputedPropertyNames() after caller mutation = %q, want %q", next, wantNames)
	}
}

func TestComputedPropertyRegistryIsSortedAndComplete(t *testing.T) {
	if !slices.IsSorted(computedPropertyNames[:]) {
		t.Fatalf("computed property registry is not sorted: %q", computedPropertyNames)
	}
	computed := cssInitialStyle(Environment{})
	for _, property := range computedPropertyNames {
		if _, ok := ComputedPropertyValue(computed, property); !ok {
			t.Errorf("enumerated property %q has no serializer", property)
		}
	}
	if got := ComputedPropertyNames(ComputedStyle{}); !slices.Equal(got, computedPropertyNames[:]) {
		t.Fatalf("zero-value property names = %q, want %q", got, computedPropertyNames)
	}
}
