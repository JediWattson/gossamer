package browser

import "testing"

func TestValidityPseudosRestyleControlsFormsAndFieldsets(t *testing.T) {
	t.Parallel()

	engine, page, targetID := computedStyleTestPage(t, `
		<html><head><style>
			#target { display:block; color:black }
			#target:invalid { color:red }
			#target:valid { color:green }
			#account, #group { display:block; width:10px }
			#account:invalid, #group:invalid { width:30px }
		</style></head><body>
			<form id="account"><fieldset id="group"><input id="target" required></fieldset></form>
		</body></html>
	`)
	defer engine.Close()

	generation := page.DocumentGeneration()
	host := &taskHost{page: page, generation: generation}
	target := NodeHandle{Document: generation, Node: targetID}
	formID, _ := page.document.ElementByID("account")
	groupID, _ := page.document.ElementByID("group")
	form := NodeHandle{Document: generation, Node: formID}
	group := NodeHandle{Document: generation, Node: groupID}

	assertSelectorStateProperty(t, page, target, "color", "rgb(255, 0, 0)")
	assertSelectorStateProperty(t, page, form, "width", "30px")
	assertSelectorStateProperty(t, page, group, "width", "30px")
	assertFormSelectorMatch(t, host, target, ":invalid", true)
	assertFormSelectorMatch(t, host, form, ":invalid", true)

	firstSnapshot := page.computedStyle.snapshot
	if err := host.SetFormValue(target, "ready"); err != nil {
		t.Fatal(err)
	}
	assertSelectorStateProperty(t, page, target, "color", "rgb(0, 128, 0)")
	assertSelectorStateProperty(t, page, form, "width", "10px")
	assertSelectorStateProperty(t, page, group, "width", "10px")
	assertFormSelectorMatch(t, host, target, ":valid", true)
	assertFormSelectorMatch(t, host, form, ":valid", true)
	if page.computedStyle.snapshot == firstSnapshot {
		t.Fatal("validity mutation did not replace the style snapshot")
	}
	if page.Frame() != nil {
		t.Fatal("validity style flush unexpectedly published a frame")
	}
	if !page.Dirty() {
		t.Fatal("validity mutation did not leave the page dirty")
	}
}
