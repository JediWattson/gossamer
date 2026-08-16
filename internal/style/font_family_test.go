package style

import "testing"

func TestParseFontFamilyNormalizesListsAndSelectsBundledFallback(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		source     string
		serialized string
		selected   FontFamily
	}{
		{name: "generic", source: "monospace", serialized: "monospace", selected: FontFamilyMonospace},
		{name: "named available", source: `Go Mono, sans-serif`, serialized: `"Go Mono", sans-serif`, selected: FontFamilyMonospace},
		{name: "quoted fallback", source: `"Unavailable", monospace`, serialized: `"Unavailable", monospace`, selected: FontFamilyMonospace},
		{name: "quoted generic is a name", source: `"monospace", sans-serif`, serialized: `"monospace", sans-serif`, selected: FontFamilySansSerif},
		{name: "escaped name", source: `G\6f  Mono, serif`, serialized: `"Go Mono", serif`, selected: FontFamilyMonospace},
		{name: "system fallback", source: `Fancy Name, system-ui`, serialized: `"Fancy Name", system-ui`, selected: FontFamilySystemUI},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			serialized, selected, ok := parseFontFamily(test.source)
			if !ok || serialized != test.serialized || selected != test.selected {
				t.Fatalf("parseFontFamily(%q) = %q, %v, %t; want %q, %v, true", test.source, serialized, selected, ok, test.serialized, test.selected)
			}
		})
	}
}

func TestParseFontFamilyRejectsMalformedLists(t *testing.T) {
	t.Parallel()

	for _, source := range []string{"", ", monospace", "serif,", `""`, "serif fallback", "var(--family)", "Go/Mono"} {
		if serialized, selected, ok := parseFontFamily(source); ok {
			t.Errorf("parseFontFamily(%q) = %q, %v, true; want rejection", source, serialized, selected)
		}
	}
}

func TestFontShorthandRequiresAndReturnsAValidFamily(t *testing.T) {
	t.Parallel()

	_, _, _, _, family, ok := parseFontShorthand(`italic bold 18px/1.5 "Unavailable", monospace`, Viewport{Width: 320, Height: 200})
	if !ok {
		t.Fatal("valid font shorthand was rejected")
	}
	serialized, selected, ok := parseFontFamily(family)
	if !ok || serialized != `"Unavailable", monospace` || selected != FontFamilyMonospace {
		t.Fatalf("shorthand family = %q -> %q, %v, %t", family, serialized, selected, ok)
	}
	if _, _, _, _, _, ok := parseFontShorthand("18px serif,", Viewport{}); ok {
		t.Fatal("font shorthand accepted a trailing family comma")
	}
}
