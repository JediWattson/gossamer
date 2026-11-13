package style

import "testing"

func TestAspectRatioParsingAndSerialization(t *testing.T) {
	t.Parallel()

	cases := []struct {
		source string
		valid  bool
		want   string
		ratio  float64
		has    bool
	}{
		{source: "auto", valid: true, want: "auto"},
		{source: "1", valid: true, want: "1 / 1", ratio: 1, has: true},
		{source: "16 / 9", valid: true, want: "16 / 9", ratio: 16.0 / 9.0, has: true},
		{source: "auto 4/3", valid: true, want: "auto 4 / 3", ratio: 4.0 / 3.0, has: true},
		{source: "3 / 2 auto", valid: true, want: "auto 3 / 2", ratio: 1.5, has: true},
		{source: "0 / 1", valid: true, want: "0 / 1"},
		{source: "-1", valid: false},
		{source: "16 / -9", valid: false},
		{source: "auto auto", valid: false},
		{source: "16 / 9 / 2", valid: false},
	}

	for _, tc := range cases {
		parsed, ok := parseAspectRatio(tc.source)
		if ok != tc.valid {
			t.Fatalf("parseAspectRatio(%q) ok = %v, want %v", tc.source, ok, tc.valid)
		}
		if !ok {
			continue
		}
		if got := serializeAspectRatio(parsed); got != tc.want {
			t.Fatalf("serializeAspectRatio(%q) = %q, want %q", tc.source, got, tc.want)
		}
		gotRatio, gotHas := parsed.Preferred(false)
		if gotHas != tc.has || (tc.has && (gotRatio < tc.ratio-1e-6 || gotRatio > tc.ratio+1e-6)) {
			t.Fatalf("Preferred(false) for %q = (%v, %v), want (%v, %v)", tc.source, gotRatio, gotHas, tc.ratio, tc.has)
		}
	}

	autoPreferred := AspectRatio{Auto: true, HasRatio: true, Numerator: 16, Denominator: 9}
	if _, ok := autoPreferred.Preferred(true); ok {
		t.Fatal("expected auto <ratio> to defer to natural aspect ratio when present")
	}
	if r, ok := autoPreferred.Preferred(false); !ok || r < 1.77 || r > 1.78 {
		t.Fatalf("expected auto <ratio> to apply without natural ratio, got (%v, %v)", r, ok)
	}
}
