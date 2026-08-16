package browser

import "testing"

func TestUserValidityPseudosTrackCommittedInteractionAndSubmission(t *testing.T) {
	t.Parallel()

	engine, page, targetID := computedStyleTestPage(t, `
		<html><head><style>
			#target { display:block; color:black }
			#target:user-invalid { color:red }
			#target:user-valid { color:green }
		</style></head><body>
			<form id="account"><input id="target" required></form>
		</body></html>
	`)
	defer engine.Close()

	generation := page.DocumentGeneration()
	host := &taskHost{page: page, generation: generation}
	target := NodeHandle{Document: generation, Node: targetID}
	formID, ok := page.document.ElementByID("account")
	if !ok {
		t.Fatal("form has no stable ID")
	}
	form := NodeHandle{Document: generation, Node: formID}

	assertUserValidityState(t, page, host, target, false, false, "rgb(0, 0, 0)")

	// Script value changes do not count as user interaction.
	if err := host.SetFormValue(target, "scripted"); err != nil {
		t.Fatal(err)
	}
	assertUserValidityState(t, page, host, target, false, false, "rgb(0, 0, 0)")

	// Native text editing is pending until the edit is committed (for example,
	// by change or blur).
	if err := host.SetFormSelection(target, 0, len("scripted"), "none"); err != nil {
		t.Fatal(err)
	}
	if err := host.ReplaceFormSelection(target, "ready", "insertText"); err != nil {
		t.Fatal(err)
	}
	assertUserValidityState(t, page, host, target, false, false, "rgb(0, 0, 0)")
	if err := host.commitPendingUserValidity(target); err != nil {
		t.Fatal(err)
	}
	assertUserValidityState(t, page, host, target, true, false, "rgb(0, 128, 0)")

	if err := host.ResetForm(form); err != nil {
		t.Fatal(err)
	}
	assertUserValidityState(t, page, host, target, false, false, "rgb(0, 0, 0)")

	// The request-submit path marks every owned control before constraint
	// validation, including invalid controls.
	if err := host.MarkFormUserValidityForSubmission(form); err != nil {
		t.Fatal(err)
	}
	assertUserValidityState(t, page, host, target, false, true, "rgb(255, 0, 0)")

	if page.Frame() != nil {
		t.Fatal("user-validity style flush unexpectedly published a frame")
	}
	if !page.Dirty() {
		t.Fatal("user-validity mutation did not leave the page dirty")
	}
}

func assertUserValidityState(
	t *testing.T,
	page *Page,
	host *taskHost,
	handle NodeHandle,
	valid, invalid bool,
	color string,
) {
	t.Helper()
	assertFormSelectorMatch(t, host, handle, ":user-valid", valid)
	assertFormSelectorMatch(t, host, handle, ":user-invalid", invalid)
	assertSelectorStateProperty(t, page, handle, "color", color)
}
