package browser

import (
	"errors"
	"testing"

	"github.com/JediWattson/gossamer/internal/dom"
)

func TestInlineStyleCacheReusesUnchangedDeclarationsAndReplacesByStableGeneration(t *testing.T) {
	t.Parallel()

	engine, page, targetID := computedStyleTestPage(t, `<!doctype html><html><body><span id="target" style="color:#123456;color:#abcdef">target</span></body></html>`)
	defer engine.Close()
	host := &taskHost{page: page, generation: page.DocumentGeneration()}
	handle := NodeHandle{Document: page.DocumentGeneration(), Node: targetID}

	assertColor := func(want string) {
		t.Helper()
		got, found, err := host.ComputedStyleProperty(handle, "", "color")
		if err != nil || !found || got != want {
			t.Fatalf("computed color = %q, %t, %v; want %q, true, nil", got, found, err, want)
		}
	}
	assertColor("rgb(171, 205, 239)")
	if got := page.resources.inlineStyles.parseCount; got != 1 {
		t.Fatalf("initial inline parse count = %d, want 1", got)
	}
	first := page.resources.inlineStyles.entries[targetID].declarations
	assertColor("rgb(171, 205, 239)")
	if got := page.resources.inlineStyles.parseCount; got != 1 {
		t.Fatalf("repeat inline parse count = %d, want 1", got)
	}
	if err := host.SetAttribute(handle, "class", "unrelated"); err != nil {
		t.Fatal(err)
	}
	assertColor("rgb(171, 205, 239)")
	if got := page.resources.inlineStyles.parseCount; got != 1 {
		t.Fatalf("unrelated mutation inline parse count = %d, want 1", got)
	}
	if declarations := page.resources.inlineStyles.entries[targetID].declarations; len(declarations) == 0 || &declarations[0] != &first[0] {
		t.Fatal("unrelated mutation replaced the cached declaration slice")
	}

	if err := host.SetStyleProperty(handle, "color", "#010203", ""); err != nil {
		t.Fatal(err)
	}
	assertColor("rgb(1, 2, 3)")
	if got := page.resources.inlineStyles.parseCount; got != 2 {
		t.Fatalf("style mutation inline parse count = %d, want 2", got)
	}
	if declarations := page.resources.inlineStyles.entries[targetID].declarations; len(declarations) == 0 || &declarations[0] == &first[0] {
		t.Fatal("style mutation retained the stale declaration slice")
	}

	body := computedStyleTestElement(page.document.Root(), "body")
	bodyID, ok := page.document.ID(body)
	if !ok {
		t.Fatal("body stable ID missing")
	}
	target, ok := page.document.Resolve(targetID)
	if !ok {
		t.Fatal("target pointer missing before detach")
	}
	if err := page.document.RemoveChild(bodyID, targetID); err != nil {
		t.Fatal(err)
	}
	if got, found, err := host.ComputedStyleProperty(handle, "", "color"); err != nil || found || got != "" {
		t.Fatalf("detached computed color = %q, %t, %v; want empty, false, nil", got, found, err)
	}
	if _, retained := page.resources.inlineStyles.entries[targetID]; retained {
		t.Fatal("detached element retained an inline declaration cache entry")
	}
	if _, err := page.document.AppendChild(bodyID, target); err != nil {
		t.Fatal(err)
	}
	assertColor("rgb(1, 2, 3)")
	if got := page.resources.inlineStyles.parseCount; got != 3 {
		t.Fatalf("reconnected inline parse count = %d, want 3", got)
	}
	if page.Frame() != nil || !page.Dirty() {
		t.Fatal("inline cache reads published a frame or cleared dirtiness")
	}
}

func TestInlineStyleCacheStoresSafelyRecoveredPartialDeclarations(t *testing.T) {
	t.Parallel()

	engine, page, targetID := computedStyleTestPage(t, `<!doctype html><html><body><span id="target" style="color:#123456; broken:'unterminated">target</span></body></html>`)
	defer engine.Close()
	host := &taskHost{page: page, generation: page.DocumentGeneration()}
	handle := NodeHandle{Document: page.DocumentGeneration(), Node: targetID}
	got, found, err := host.ComputedStyleProperty(handle, "", "color")
	if err != nil || !found || got != "rgb(18, 52, 86)" {
		t.Fatalf("partially recovered inline color = %q, %t, %v", got, found, err)
	}
	entry, ok := page.resources.inlineStyles.entries[targetID]
	if !ok || len(entry.declarations) != 1 || entry.declarations[0].Declaration.Property != "color" {
		t.Fatalf("partially recovered cache entry = %#v, %t", entry, ok)
	}
}

func TestInlineStyleCacheRejectsExpiredReadView(t *testing.T) {
	t.Parallel()

	engine, page, _ := computedStyleTestPage(t, `<!doctype html><html><body><span id="target" style="color:red">target</span></body></html>`)
	defer engine.Close()
	var expired dom.ReadView
	if err := page.document.WithReadView(func(view dom.ReadView) error {
		expired = view
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	cache := newInlineStyleCache()
	if declarations, err := cache.declarationsForView(expired); !errors.Is(err, dom.ErrExpiredReadView) || declarations != nil {
		t.Fatalf("expired cache read = %#v, %v; want nil, ErrExpiredReadView", declarations, err)
	}
}
