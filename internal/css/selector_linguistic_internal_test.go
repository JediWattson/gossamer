package css

import "testing"

func TestExtlangRegistryIsCompleteAndCanonical(t *testing.T) {
	t.Parallel()

	if got, want := len(extlangPrefix), 258; got != want {
		t.Fatalf("extlang registry size = %d, want %d", got, want)
	}
	for subtag, prefix := range extlangPrefix {
		prefixed := prefix + "-" + subtag
		want := canonicalLanguageTag(prefixed)
		if got := canonicalLanguageTag(subtag); got != want {
			t.Errorf("canonicalLanguageTag(%q) = %q, want prefixed canonical form %q", subtag, got, want)
		}
	}
}

func TestExtendedLanguageFilteringSingletonBoundaries(t *testing.T) {
	t.Parallel()

	tests := []struct {
		rangeValue string
		tag        string
		want       bool
	}{
		{rangeValue: "de-DE", tag: "de-Latn-DE", want: true},
		{rangeValue: "*-CH", tag: "de-Latn-CH", want: true},
		{rangeValue: "en-US", tag: "en-a-foo-US", want: false},
		{rangeValue: "", tag: "", want: true},
		{rangeValue: "", tag: "und", want: false},
		{rangeValue: "*", tag: "und", want: true},
		{rangeValue: "*", tag: "", want: false},
	}
	for _, test := range tests {
		if got := extendedLanguageRangeMatches(test.rangeValue, test.tag); got != test.want {
			t.Errorf("extendedLanguageRangeMatches(%q, %q) = %t, want %t", test.rangeValue, test.tag, got, test.want)
		}
	}
}
