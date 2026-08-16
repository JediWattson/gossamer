package browser

import "testing"

func TestIndeterminatePseudoRestylesFromLiveAndMarkupState(t *testing.T) {
	t.Parallel()

	engine, page, checkboxID := computedStyleTestPage(t, `
		<html><head><style>
			#target { display:block; color:black }
			#target:indeterminate { color:green }
			input[type=radio] { width:10px }
			input[type=radio]:indeterminate { width:30px }
			progress { display:block; height:10px }
			progress:indeterminate { height:30px }
		</style></head><body>
			<input id="target" type="checkbox">
			<input id="first" type="radio" name="pick">
			<input id="second" type="radio" name="pick">
			<progress id="progress"></progress>
		</body></html>
	`)
	defer engine.Close()

	generation := page.DocumentGeneration()
	host := &taskHost{page: page, generation: generation}
	checkbox := NodeHandle{Document: generation, Node: checkboxID}
	firstID, _ := page.document.ElementByID("first")
	secondID, _ := page.document.ElementByID("second")
	progressID, _ := page.document.ElementByID("progress")
	first := NodeHandle{Document: generation, Node: firstID}
	second := NodeHandle{Document: generation, Node: secondID}
	progress := NodeHandle{Document: generation, Node: progressID}

	assertSelectorStateProperty(t, page, checkbox, "color", "rgb(0, 0, 0)")
	assertSelectorStateProperty(t, page, first, "width", "30px")
	assertSelectorStateProperty(t, page, second, "width", "30px")
	assertSelectorStateProperty(t, page, progress, "height", "30px")

	firstSnapshot := page.computedStyle.snapshot
	if err := host.SetFormIndeterminate(checkbox, true); err != nil {
		t.Fatal(err)
	}
	assertSelectorStateProperty(t, page, checkbox, "color", "rgb(0, 128, 0)")
	assertFormSelectorMatch(t, host, checkbox, ":indeterminate", true)
	if page.computedStyle.snapshot == firstSnapshot {
		t.Fatal("indeterminate mutation did not replace the style snapshot")
	}

	if err := host.SetFormChecked(first, true); err != nil {
		t.Fatal(err)
	}
	assertSelectorStateProperty(t, page, first, "width", "10px")
	assertSelectorStateProperty(t, page, second, "width", "10px")
	if err := host.SetAttribute(progress, "value", "0.5"); err != nil {
		t.Fatal(err)
	}
	assertSelectorStateProperty(t, page, progress, "height", "10px")

	if page.Frame() != nil {
		t.Fatal("indeterminate style flush unexpectedly published a frame")
	}
	if !page.Dirty() {
		t.Fatal("indeterminate mutations did not leave the page dirty")
	}
}
