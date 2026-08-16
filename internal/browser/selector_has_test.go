package browser

import "testing"

func TestHasSelectorRestylesAfterLiveDescendantMutation(t *testing.T) {
	t.Parallel()

	engine, page, target := computedStyleTestPage(t, `
		<html><head><style>
			#target { display:block; color:black; width:10px; height:10px }
			#target:has(> input:checked) { color:red }
			#target:has(+ .summary .badge) { height:30px }
		</style></head><body>
			<article id="target"><input id="toggle" type="checkbox" checked></article>
			<section class="summary"><span class="badge"></span></section>
		</body></html>
	`)
	defer engine.Close()

	generation := page.DocumentGeneration()
	host := &taskHost{page: page, generation: generation}
	card := NodeHandle{Document: generation, Node: target}
	toggleID, ok := page.document.ElementByID("toggle")
	if !ok {
		t.Fatal("toggle has no stable ID")
	}
	toggle := NodeHandle{Document: generation, Node: toggleID}
	root := NodeHandle{Document: generation, Node: page.document.RootID()}

	assertSelectorStateProperty(t, page, card, "color", "rgb(255, 0, 0)")
	assertSelectorStateProperty(t, page, card, "height", "30px")
	assertFormSelectorMatch(t, host, card, ":has(> input:checked)", true)
	assertFormSelectorMatch(t, host, card, ":has(+ .summary .badge)", true)
	matches, err := host.QuerySelector(root, "article:has(input:checked)", true)
	if err != nil {
		t.Fatal(err)
	}
	if len(matches) != 1 || matches[0] != card {
		t.Fatalf("querySelectorAll(:has()) = %#v, want card", matches)
	}

	firstSnapshot := page.computedStyle.snapshot
	if err := host.SetFormChecked(toggle, false); err != nil {
		t.Fatal(err)
	}
	assertSelectorStateProperty(t, page, card, "color", "rgb(0, 0, 0)")
	assertFormSelectorMatch(t, host, card, ":has(> input:checked)", false)
	if page.computedStyle.snapshot == firstSnapshot {
		t.Fatal("descendant-state mutation did not invalidate the :has() style snapshot")
	}
	if page.Frame() != nil || !page.Dirty() {
		t.Fatal(":has() style flush published a frame or cleared task-boundary dirtiness")
	}
}
