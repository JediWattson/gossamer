package browser

import "testing"

func TestLiveMutabilityAndPlaceholderStateFeedsSelectors(t *testing.T) {
	t.Parallel()

	engine, page, targetID := computedStyleTestPage(t, `
		<html><head><style>
			#target { display:block; color:black; width:10px }
			#target:read-write { color:green }
			#target:read-only { color:red }
			#target:placeholder-shown { width:30px }
			#editable-child { display:block; background-color:white }
			#editable-child:read-write { background-color:blue }
		</style></head><body>
			<input id="target" placeholder="hint">
			<section id="editor" contenteditable="true"><span id="editable-child"></span></section>
		</body></html>
	`)
	defer engine.Close()

	generation := page.DocumentGeneration()
	host := &taskHost{page: page, generation: generation}
	target := NodeHandle{Document: generation, Node: targetID}
	childID, ok := page.document.ElementByID("editable-child")
	if !ok {
		t.Fatal("editable child has no stable ID")
	}
	child := NodeHandle{Document: generation, Node: childID}
	root := NodeHandle{Document: generation, Node: page.document.RootID()}

	assertSelectorStateProperty(t, page, target, "color", "rgb(0, 128, 0)")
	assertSelectorStateProperty(t, page, target, "width", "30px")
	assertSelectorStateProperty(t, page, child, "background-color", "rgb(0, 0, 255)")
	assertFormSelectorMatch(t, host, target, ":read-write:placeholder-shown", true)
	assertFormSelectorMatch(t, host, child, ":read-write", true)
	matches, err := host.QuerySelector(root, ":placeholder-shown", true)
	if err != nil {
		t.Fatal(err)
	}
	if len(matches) != 1 || matches[0] != target {
		t.Fatalf("initial :placeholder-shown query = %#v, want target", matches)
	}

	firstSnapshot := page.computedStyle.snapshot
	if err := host.SetFormValue(target, "typed"); err != nil {
		t.Fatal(err)
	}
	assertSelectorStateProperty(t, page, target, "width", "10px")
	assertFormSelectorMatch(t, host, target, ":placeholder-shown", false)
	if page.computedStyle.snapshot == firstSnapshot {
		t.Fatal("live value mutation did not replace the versioned style snapshot")
	}

	if err := host.SetAttribute(target, "readonly", ""); err != nil {
		t.Fatal(err)
	}
	if err := host.SetFormValue(target, ""); err != nil {
		t.Fatal(err)
	}
	assertSelectorStateProperty(t, page, target, "color", "rgb(255, 0, 0)")
	assertSelectorStateProperty(t, page, target, "width", "30px")
	assertFormSelectorMatch(t, host, target, ":read-only:placeholder-shown", true)

	if err := host.RemoveAttribute(target, "readonly"); err != nil {
		t.Fatal(err)
	}
	if err := host.SetAttribute(target, "type", "checkbox"); err != nil {
		t.Fatal(err)
	}
	assertSelectorStateProperty(t, page, target, "color", "rgb(255, 0, 0)")
	assertSelectorStateProperty(t, page, target, "width", "10px")
	assertFormSelectorMatch(t, host, target, ":read-only", true)
	assertFormSelectorMatch(t, host, target, ":placeholder-shown", false)

	if err := host.SetAttribute(child, "contenteditable", "false"); err != nil {
		t.Fatal(err)
	}
	assertSelectorStateProperty(t, page, child, "background-color", "rgb(255, 255, 255)")
	assertFormSelectorMatch(t, host, child, ":read-only", true)

	if page.Frame() != nil {
		t.Fatal("mutability style flush unexpectedly published a frame")
	}
	if !page.Dirty() {
		t.Fatal("mutability mutations did not leave the page dirty for task-boundary render")
	}
}
