package browser

import "testing"

func TestRangePseudosRestyleFromLiveValueAndLimitMutations(t *testing.T) {
	t.Parallel()

	engine, page, targetID := computedStyleTestPage(t, `
		<html><head><style>
			#target { display:block; color:black }
			#target:in-range { color:green }
			#target:out-of-range { color:red }
		</style></head><body>
			<input id="target" type="number" min="5" max="10" value="7">
		</body></html>
	`)
	defer engine.Close()

	generation := page.DocumentGeneration()
	host := &taskHost{page: page, generation: generation}
	target := NodeHandle{Document: generation, Node: targetID}

	assertSelectorStateProperty(t, page, target, "color", "rgb(0, 128, 0)")
	assertFormSelectorMatch(t, host, target, ":in-range", true)
	firstSnapshot := page.computedStyle.snapshot
	if err := host.SetFormValue(target, "11"); err != nil {
		t.Fatal(err)
	}
	assertSelectorStateProperty(t, page, target, "color", "rgb(255, 0, 0)")
	assertFormSelectorMatch(t, host, target, ":out-of-range", true)
	if page.computedStyle.snapshot == firstSnapshot {
		t.Fatal("range value mutation did not replace the style snapshot")
	}
	if err := host.SetAttribute(target, "max", "12"); err != nil {
		t.Fatal(err)
	}
	assertSelectorStateProperty(t, page, target, "color", "rgb(0, 128, 0)")
	if err := host.RemoveAttribute(target, "min"); err != nil {
		t.Fatal(err)
	}
	if err := host.RemoveAttribute(target, "max"); err != nil {
		t.Fatal(err)
	}
	assertSelectorStateProperty(t, page, target, "color", "rgb(0, 0, 0)")
	assertFormSelectorMatch(t, host, target, ":in-range, :out-of-range", false)

	if page.Frame() != nil {
		t.Fatal("range-state style flush unexpectedly published a frame")
	}
	if !page.Dirty() {
		t.Fatal("range-state mutations did not leave the page dirty")
	}
}
