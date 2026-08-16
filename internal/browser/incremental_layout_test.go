package browser

import "testing"

func TestPageReusesLayoutForPaintOnlyAndNeutralMutations(t *testing.T) {
	engine, page, target := computedStyleTestPage(t, `
		<html><head><style>.paint { background-color:#123456; color:#654321 }</style></head>
		<body><div id="target">content</div></body></html>
	`)
	defer engine.Close()
	if err := page.Render(); err != nil {
		t.Fatal(err)
	}
	initial := page.layout.snapshot

	if err := page.document.SetAttribute(target, "data-unrelated", "one"); err != nil {
		t.Fatal(err)
	}
	page.dirty = true
	if err := page.Render(); err != nil {
		t.Fatal(err)
	}
	if !page.layout.incremental || !page.layout.snapshot.ReusedLayout() || len(page.layout.snapshot.DamageRects()) != 0 {
		t.Fatalf("neutral layout state = incremental %t, reused %t, damage %#v",
			page.layout.incremental, page.layout.snapshot.ReusedLayout(), page.layout.snapshot.DamageRects())
	}
	if page.layout.snapshot == initial || page.layout.snapshot.Version() != page.document.Version() {
		t.Fatal("neutral mutation did not publish a current immutable layout header")
	}

	if err := page.document.SetAttribute(target, "class", "paint"); err != nil {
		t.Fatal(err)
	}
	page.dirty = true
	if err := page.Render(); err != nil {
		t.Fatal(err)
	}
	if !page.computedStyle.incremental || !page.layout.incremental || !page.layout.snapshot.ReusedLayout() {
		t.Fatal("paint-only selector mutation did not reuse targeted style and layout")
	}
	if len(page.layout.snapshot.DamageRects()) == 0 {
		t.Fatal("paint-only selector mutation has no repaint damage")
	}
	if page.frame == nil || page.frame.ComputedStyles != page.computedStyle.snapshot || page.frame.Layout != page.layout.snapshot {
		t.Fatal("published frame does not own the incremental style/layout snapshots")
	}
}

func TestPageFallsBackToFullLayoutForGeometryAndTextMutations(t *testing.T) {
	engine, page, target := computedStyleTestPage(t, `
		<html><head><style>.wide { width:200px }</style></head>
		<body><div id="target">content</div></body></html>
	`)
	defer engine.Close()
	if err := page.Render(); err != nil {
		t.Fatal(err)
	}
	if err := page.document.SetAttribute(target, "class", "wide"); err != nil {
		t.Fatal(err)
	}
	page.dirty = true
	if err := page.Render(); err != nil {
		t.Fatal(err)
	}
	if page.layout.incremental || page.layout.snapshot.ReusedLayout() {
		t.Fatal("geometry-affecting style mutation reused layout")
	}

	node, ok := page.document.Resolve(target)
	if !ok || len(node.Children) == 0 {
		t.Fatal("target text is unavailable")
	}
	text, ok := page.document.ID(node.Children[0])
	if !ok {
		t.Fatal("target text has no stable identity")
	}
	if err := page.document.SetText(text, "different content"); err != nil {
		t.Fatal(err)
	}
	page.dirty = true
	if err := page.Render(); err != nil {
		t.Fatal(err)
	}
	if page.layout.incremental || page.layout.snapshot.ReusedLayout() {
		t.Fatal("text mutation reused layout")
	}
}
