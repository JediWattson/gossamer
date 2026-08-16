package browser

import (
	"fmt"
	"testing"

	"github.com/JediWattson/gossamer/internal/dom"
)

func TestPageReusesStyleStorageForProvenStyleNeutralMutations(t *testing.T) {
	engine, page, target := computedStyleTestPage(t, `
		<html><head><style>.watched[data-state] { color:red } p:empty { display:none }</style></head>
		<body><p id="target" class="watched">before</p></body></html>
	`)
	defer engine.Close()
	handle := NodeHandle{Document: page.DocumentGeneration(), Node: target}
	if _, err := page.ComputedStyle(handle); err != nil {
		t.Fatal(err)
	}
	initial := page.computedStyle.snapshot

	if err := page.document.SetAttribute(target, "data-unrelated", "one"); err != nil {
		t.Fatal(err)
	}
	page.dirty = true
	if _, err := page.ComputedStyle(handle); err != nil {
		t.Fatal(err)
	}
	if !page.computedStyle.incremental || page.computedStyle.snapshot == initial {
		t.Fatal("style-neutral attribute did not publish a rebound snapshot header")
	}
	if page.computedStyle.snapshot.Version() != page.document.Version() {
		t.Fatal("rebound snapshot did not advance to the coherent document version")
	}
	if page.Frame() != nil || !page.Dirty() {
		t.Fatal("incremental computed-style read published a frame or cleared dirtiness")
	}

	targetNode, ok := page.document.Resolve(target)
	if !ok || len(targetNode.Children) != 1 || targetNode.Children[0].Type != dom.TextNode {
		t.Fatal("target text node is unavailable")
	}
	text := targetNode.Children[0]
	textID, ok := page.document.ID(text)
	if !ok {
		t.Fatal("target text has no stable ID")
	}
	if err := page.document.SetText(textID, "after"); err != nil {
		t.Fatal(err)
	}
	page.dirty = true
	if _, err := page.ComputedStyle(handle); err != nil {
		t.Fatal(err)
	}
	if !page.computedStyle.incremental {
		t.Fatal("nonempty text replacement forced a full style pass")
	}

	if err := page.document.SetAttribute(target, "data-state", "matched"); err != nil {
		t.Fatal(err)
	}
	page.dirty = true
	if _, err := page.ComputedStyle(handle); err != nil {
		t.Fatal(err)
	}
	if page.computedStyle.incremental {
		t.Fatal("selector-dependent attribute reused stale style storage")
	}
}

func TestPageMutationJournalGapFallsBackToFullStylePass(t *testing.T) {
	engine, page, target := computedStyleTestPage(t, `<html><body><div id="target"></div></body></html>`)
	defer engine.Close()
	handle := NodeHandle{Document: page.DocumentGeneration(), Node: target}
	if _, err := page.ComputedStyle(handle); err != nil {
		t.Fatal(err)
	}
	detached, err := page.document.CreateElement("div")
	if err != nil {
		t.Fatal(err)
	}
	for index := 0; index <= 4096; index++ {
		if err := page.document.SetAttribute(detached, "data-sequence", fmt.Sprint(index)); err != nil {
			t.Fatal(err)
		}
	}
	page.dirty = true
	if _, err := page.ComputedStyle(handle); err != nil {
		t.Fatal(err)
	}
	if page.computedStyle.incremental {
		t.Fatal("truncated mutation journal incorrectly reused style storage")
	}
	if page.computedStyle.mutationSequence != page.document.MutationSequence() {
		t.Fatal("full fallback did not advance the browser mutation cursor")
	}
}
