package browser

import "testing"

func TestComputedWritingModeInheritanceAndMutationStayLive(t *testing.T) {
	t.Parallel()

	engine, page, parentID := computedStyleTestPage(t, `<!doctype html><html><body>
		<section id=target style="writing-mode:vertical-rl"><div id=child></div></section>
	</body></html>`)
	defer engine.Close()
	generation := page.DocumentGeneration()
	parent := NodeHandle{Document: generation, Node: parentID}
	child := NodeHandle{Document: generation, Node: mustPageElementID(t, page, "child")}

	assertResolvedProperty(t, page, parent, "writing-mode", "vertical-rl")
	assertResolvedProperty(t, page, child, "writing-mode", "vertical-rl")
	if page.Frame() != nil || !page.Dirty() {
		t.Fatal("computed writing-mode read published a frame or cleared dirtiness")
	}
	if err := page.document.SetAttribute(parentID, "style", "writing-mode:vertical-lr"); err != nil {
		t.Fatal(err)
	}
	assertResolvedProperty(t, page, parent, "writing-mode", "vertical-lr")
	assertResolvedProperty(t, page, child, "writing-mode", "vertical-lr")
	if page.Frame() != nil || !page.Dirty() {
		t.Fatal("live writing-mode read published a frame or cleared dirtiness")
	}
}
