package browser

import "testing"

func TestDefaultPseudoRestylesAfterMarkupAndButtonTypeMutations(t *testing.T) {
	t.Parallel()

	engine, page, checkboxID := computedStyleTestPage(t, `
		<html><head><style>
			#target { display:block; color:black }
			#target:default { color:green }
			button { display:block; width:10px }
			button:default { width:30px }
		</style></head><body>
			<input id="target" type="checkbox" checked>
			<form><button id="first">first</button><button id="second">second</button></form>
		</body></html>
	`)
	defer engine.Close()

	generation := page.DocumentGeneration()
	host := &taskHost{page: page, generation: generation}
	checkbox := NodeHandle{Document: generation, Node: checkboxID}
	firstID, ok := page.document.ElementByID("first")
	if !ok {
		t.Fatal("first button has no stable ID")
	}
	secondID, ok := page.document.ElementByID("second")
	if !ok {
		t.Fatal("second button has no stable ID")
	}
	first := NodeHandle{Document: generation, Node: firstID}
	second := NodeHandle{Document: generation, Node: secondID}

	assertSelectorStateProperty(t, page, checkbox, "color", "rgb(0, 128, 0)")
	assertSelectorStateProperty(t, page, first, "width", "30px")
	assertSelectorStateProperty(t, page, second, "width", "10px")
	assertFormSelectorMatch(t, host, checkbox, ":default:checked", true)
	assertFormSelectorMatch(t, host, first, ":default", true)

	firstSnapshot := page.computedStyle.snapshot
	if err := host.SetFormChecked(checkbox, false); err != nil {
		t.Fatal(err)
	}
	assertSelectorStateProperty(t, page, checkbox, "color", "rgb(0, 128, 0)")
	assertFormSelectorMatch(t, host, checkbox, ":default:not(:checked)", true)
	if page.computedStyle.snapshot == firstSnapshot {
		t.Fatal("checkedness mutation did not replace the versioned style snapshot")
	}

	if err := host.RemoveAttribute(checkbox, "checked"); err != nil {
		t.Fatal(err)
	}
	assertSelectorStateProperty(t, page, checkbox, "color", "rgb(0, 0, 0)")
	assertFormSelectorMatch(t, host, checkbox, ":default", false)

	if err := host.SetAttribute(first, "type", "button"); err != nil {
		t.Fatal(err)
	}
	assertSelectorStateProperty(t, page, first, "width", "10px")
	assertSelectorStateProperty(t, page, second, "width", "30px")
	assertFormSelectorMatch(t, host, first, ":default", false)
	assertFormSelectorMatch(t, host, second, ":default", true)

	if page.Frame() != nil {
		t.Fatal("default-state style flush unexpectedly published a frame")
	}
	if !page.Dirty() {
		t.Fatal("default-state mutations did not leave the page dirty")
	}
}
