package browser

import (
	"slices"
	"testing"
)

func TestTaskHostComputedStyleReflectsAllCSSOMMutationWithoutPublishingFrame(t *testing.T) {
	t.Parallel()

	engine, page, target := computedStyleTestPage(t, `
		<html><head><style>
			#target { color: #123456; display: block; width: 80px; }
		</style></head><body><div id="target">text</div></body></html>
	`)
	defer engine.Close()

	host := &taskHost{page: page, generation: page.DocumentGeneration()}
	handle := NodeHandle{Document: page.DocumentGeneration(), Node: target}
	if value, found, err := host.ComputedStyleProperty(handle, "", "display"); err != nil || !found || value != "block" {
		t.Fatalf("initial display = %q, %t, %v; want block, true, nil", value, found, err)
	}
	if page.Frame() != nil || !page.Dirty() {
		t.Fatal("initial computed-style read changed frame publication or render dirtiness")
	}

	if err := host.SetStyleCSSText(handle, "--keep: #abcdef; all: initial"); err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		property string
		want     string
	}{
		{property: "color", want: "rgb(0, 0, 0)"},
		{property: "display", want: "inline"},
		{property: "width", want: "auto"},
		{property: "--keep", want: "#abcdef"},
	} {
		value, found, err := host.ComputedStyleProperty(handle, "", test.property)
		if err != nil || !found || value != test.want {
			t.Errorf("computed %s after all mutation = %q, %t, %v; want %q, true, nil", test.property, value, found, err, test.want)
		}
	}
	if value, found, err := host.ComputedStyleProperty(handle, "", "all"); err != nil || found || value != "" {
		t.Errorf("computed all shorthand = %q, %t, %v; want empty, false, nil", value, found, err)
	}
	names, err := host.ComputedStylePropertyNames(handle, "")
	if err != nil {
		t.Fatal(err)
	}
	if slices.Contains(names, "all") || !slices.Contains(names, "--keep") {
		t.Fatalf("computed property names after all mutation = %q", names)
	}
	if page.Frame() != nil || !page.Dirty() {
		t.Fatal("live all computed-style read changed frame publication or render dirtiness")
	}
}
