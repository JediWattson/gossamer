package browser

import "testing"

func TestPageReusesLayoutForPaintOnlyAndNeutralMutations(t *testing.T) {
	engine, page, target := computedStyleTestPage(t, `
		<html><head><style>.paint { background-color:#123456; color:#654321 }</style></head>
		<body style="margin:0"><div id="target" style="height:1200px">content</div></body></html>
	`)
	defer engine.Close()
	if err := page.Render(); err != nil {
		t.Fatal(err)
	}
	initial := page.layout.snapshot
	initialFrame := page.frame
	initialRawPaint := page.paint.frame
	initialArtifact := initialFrame.PaintArtifactID()

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
	if !page.paint.incremental || page.paint.frame == initialRawPaint {
		t.Fatalf("neutral paint state = incremental %t, frame reused pointer=%t", page.paint.incremental, page.paint.frame == initialRawPaint)
	}
	if page.frame.PaintArtifactID() != initialArtifact {
		t.Fatalf("neutral mutation changed PaintArtifactID from %d to %d", initialArtifact, page.frame.PaintArtifactID())
	}
	if len(page.frame.DisplayList.Commands) == 0 || &page.frame.DisplayList.Commands[0] != &initialFrame.DisplayList.Commands[0] {
		t.Fatal("neutral mutation did not reuse display-list backing storage")
	}

	if _, err := page.ScrollViewport(0, 24); err != nil {
		t.Fatal(err)
	}
	if err := page.Render(); err != nil {
		t.Fatal(err)
	}
	scrolledFrame := page.frame
	if scrolledFrame.PaintArtifactID() != initialArtifact {
		t.Fatalf("scroll projection changed PaintArtifactID from %d to %d", initialArtifact, scrolledFrame.PaintArtifactID())
	}
	if err := page.document.SetAttribute(target, "data-unrelated", "two"); err != nil {
		t.Fatal(err)
	}
	page.dirty = true
	if err := page.Render(); err != nil {
		t.Fatal(err)
	}
	if !page.paint.incremental || page.frame.PaintArtifactID() != initialArtifact {
		t.Fatalf("scrolled neutral paint = incremental %t, artifact %d want %d",
			page.paint.incremental, page.frame.PaintArtifactID(), initialArtifact)
	}
	if len(page.frame.DisplayList.Commands) == 0 || &page.frame.DisplayList.Commands[0] != &scrolledFrame.DisplayList.Commands[0] {
		t.Fatal("scrolled neutral mutation reprojected display list despite unchanged scroll offsets")
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
	if page.paint.incremental {
		t.Fatal("paint-only selector mutation skipped display-list rebuild")
	}
	if page.frame.PaintArtifactID() == initialArtifact {
		t.Fatal("paint-only selector mutation kept stale PaintArtifactID")
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
