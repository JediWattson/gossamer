package browser

import "testing"

func TestLiveFormStateFeedsSelectorsAndComputedStyle(t *testing.T) {
	t.Parallel()

	engine, page, target := computedStyleTestPage(t, `
		<html><head><style>
			#target { display:block; color:black; background-color:white; width:10px }
			#target:checked { color:red }
			#target:enabled { background-color:green }
			#target:disabled { background-color:blue }
			#target:required { width:30px }
			#target:optional { width:20px }
			#choice { color:black }
			#choice:checked { color:blue }
		</style></head><body>
			<input id="target" type="checkbox" checked required>
			<select><option id="choice">first</option></select>
		</body></html>
	`)
	defer engine.Close()

	generation := page.DocumentGeneration()
	host := &taskHost{page: page, generation: generation}
	handle := NodeHandle{Document: generation, Node: target}
	choiceID, ok := page.document.ElementByID("choice")
	if !ok {
		t.Fatal("choice has no stable ID")
	}
	choice := NodeHandle{Document: generation, Node: choiceID}
	root := NodeHandle{Document: generation, Node: page.document.RootID()}

	assertSelectorStateProperty(t, page, handle, "color", "rgb(255, 0, 0)")
	assertSelectorStateProperty(t, page, handle, "background-color", "rgb(0, 128, 0)")
	assertSelectorStateProperty(t, page, handle, "width", "30px")
	assertFormSelectorMatch(t, host, handle, ":checked:enabled:required", true)
	assertFormSelectorMatch(t, host, handle, ":disabled, :optional", false)
	assertSelectorStateProperty(t, page, choice, "color", "rgb(0, 0, 255)")
	assertFormSelectorMatch(t, host, choice, ":checked:enabled", true)

	matches, err := host.QuerySelector(root, ":checked", true)
	if err != nil {
		t.Fatal(err)
	}
	if len(matches) != 2 || matches[0] != handle || matches[1] != choice {
		t.Fatalf("initial :checked query = %#v, want checkbox then option", matches)
	}

	firstSnapshot := page.computedStyle.snapshot
	if err := host.SetFormChecked(handle, false); err != nil {
		t.Fatal(err)
	}
	assertSelectorStateProperty(t, page, handle, "color", "rgb(0, 0, 0)")
	assertFormSelectorMatch(t, host, handle, ":checked", false)
	if page.computedStyle.snapshot == firstSnapshot {
		t.Fatal("checkedness mutation did not replace the versioned style snapshot")
	}

	if err := host.SetAttribute(handle, "disabled", ""); err != nil {
		t.Fatal(err)
	}
	assertSelectorStateProperty(t, page, handle, "background-color", "rgb(0, 0, 255)")
	assertFormSelectorMatch(t, host, handle, ":disabled", true)
	assertFormSelectorMatch(t, host, handle, ":enabled", false)

	if err := host.RemoveAttribute(handle, "required"); err != nil {
		t.Fatal(err)
	}
	assertSelectorStateProperty(t, page, handle, "width", "20px")
	assertFormSelectorMatch(t, host, handle, ":required", false)
	assertFormSelectorMatch(t, host, handle, ":optional", true)

	if err := host.SetFormSelected(choice, false); err != nil {
		t.Fatal(err)
	}
	assertSelectorStateProperty(t, page, choice, "color", "rgb(0, 0, 0)")
	assertFormSelectorMatch(t, host, choice, ":checked", false)
	if err := host.SetFormSelected(choice, true); err != nil {
		t.Fatal(err)
	}
	assertSelectorStateProperty(t, page, choice, "color", "rgb(0, 0, 255)")
	assertFormSelectorMatch(t, host, choice, ":checked", true)

	if page.Frame() != nil {
		t.Fatal("form-state style flush unexpectedly published a frame")
	}
	if !page.Dirty() {
		t.Fatal("form-state mutation did not leave the page dirty for task-boundary render")
	}
}

func assertFormSelectorMatch(t *testing.T, host *taskHost, handle NodeHandle, selector string, want bool) {
	t.Helper()
	got, err := host.MatchesSelector(handle, selector)
	if err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Fatalf("MatchesSelector(%q) = %t, want %t", selector, got, want)
	}
}
