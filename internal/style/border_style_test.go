package style

import "testing"

func TestBorderLineStylesParseAndSerialize(t *testing.T) {
	t.Parallel()

	tests := []struct {
		keyword string
		value   BorderStyle
	}{
		{"none", BorderStyleNone},
		{"hidden", BorderStyleHidden},
		{"dotted", BorderStyleDotted},
		{"dashed", BorderStyleDashed},
		{"solid", BorderStyleSolid},
		{"double", BorderStyleDouble},
		{"groove", BorderStyleGroove},
		{"ridge", BorderStyleRidge},
		{"inset", BorderStyleInset},
		{"outset", BorderStyleOutset},
	}
	for _, test := range tests {
		t.Run(test.keyword, func(t *testing.T) {
			parsed, ok := parseBorderStyle(test.keyword)
			if !ok || parsed != test.value {
				t.Fatalf("parseBorderStyle(%q) = %v, %t; want %v, true", test.keyword, parsed, ok, test.value)
			}
			if serialized := serializeComputedBorderStyle(parsed); serialized != test.keyword {
				t.Fatalf("serializeComputedBorderStyle(%v) = %q, want %q", parsed, serialized, test.keyword)
			}
		})
	}
	if _, ok := parseBorderStyle("wavy"); ok {
		t.Fatal("unsupported wavy border style parsed")
	}
}

func TestBorderStyleShorthandRetainsEverySideKeyword(t *testing.T) {
	t.Parallel()

	parsed, ok := parseBorderStyles("dotted dashed double groove")
	if !ok {
		t.Fatal("four-value border-style shorthand did not parse")
	}
	want := [4]borderStyle{borderStyleDotted, borderStyleDashed, borderStyleDouble, borderStyleGroove}
	if parsed != want {
		t.Fatalf("border styles = %#v, want %#v", parsed, want)
	}
}
