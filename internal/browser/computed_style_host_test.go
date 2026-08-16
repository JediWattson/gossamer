package browser

import (
	"errors"
	"slices"
	"testing"

	"github.com/JediWattson/gossamer/internal/css"
	"github.com/JediWattson/gossamer/internal/dom"
	"github.com/JediWattson/gossamer/internal/render"
)

func TestTaskHostComputedStyleReadsCurrentCascadeWithoutPublishingFrame(t *testing.T) {
	t.Parallel()

	engine, page, target := computedStyleTestPage(t, `
		<html><head><link rel="stylesheet" href="theme.css"><style>
			.parent { color: #123456; --measure: 25vw; }
			.child { display: block; width: var(--measure); }
			.child.alternate { display: inline-block; }
			@media (min-width: 700px) { .child { font-size: 20px; } }
		</style></head><body class="parent">
			<div id="target" class="child" style="opacity: .5; --label: ready">text</div>
		</body></html>
	`)
	defer engine.Close()

	host := &taskHost{page: page, generation: page.DocumentGeneration()}
	handle := NodeHandle{Document: page.DocumentGeneration(), Node: target}

	for _, test := range []struct {
		property string
		want     string
	}{
		{property: "display", want: "block"},
		{property: "COLOR", want: "rgb(18, 52, 86)"},
		{property: "width", want: "200px"},
		{property: "font-size", want: "20px"},
		{property: "opacity", want: "0.5"},
		{property: "--measure", want: "25vw"},
		{property: "--label", want: "ready"},
	} {
		got, found, err := host.ComputedStyleProperty(handle, "", test.property)
		if err != nil {
			t.Fatalf("ComputedStyleProperty(%q): %v", test.property, err)
		}
		if !found || got != test.want {
			t.Errorf("ComputedStyleProperty(%q) = %q, %t; want %q, true", test.property, got, found, test.want)
		}
	}
	if got, found, err := host.ComputedStyleProperty(handle, "", "--LABEL"); err != nil || found || got != "" {
		t.Errorf("case-sensitive custom property = %q, %t, %v; want empty, false, nil", got, found, err)
	}
	if got, found, err := host.ComputedStyleProperty(handle, "", "not-a-property"); err != nil || found || got != "" {
		t.Errorf("unknown property = %q, %t, %v; want empty, false, nil", got, found, err)
	}
	if got, found, err := host.ComputedStyleProperty(handle, "", "WIDTH"); err != nil || !found || got != "200px" {
		t.Errorf("ASCII-insensitive resolved width = %q, %t, %v; want 200px, true, nil", got, found, err)
	}

	names, err := host.ComputedStylePropertyNames(handle, "")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"color", "display", "width", "--label", "--measure"} {
		if !slices.Contains(names, want) {
			t.Errorf("computed property names do not contain %q: %v", want, names)
		}
	}
	if page.Frame() != nil {
		t.Fatal("computed-style host reads unexpectedly published a frame")
	}
	if !page.Dirty() {
		t.Fatal("computed-style host reads unexpectedly cleared render dirtiness")
	}

	if err := host.SetStyleProperty(handle, "color", "#abcdef", ""); err != nil {
		t.Fatal(err)
	}
	got, found, err := host.ComputedStyleProperty(handle, "", "color")
	if err != nil {
		t.Fatal(err)
	}
	if !found || got != "rgb(171, 205, 239)" {
		t.Fatalf("live color after inline mutation = %q, %t; want rgb(171, 205, 239), true", got, found)
	}
	if page.Frame() != nil {
		t.Fatal("live computed-style reread unexpectedly published a frame")
	}

	if err := host.SetAttribute(handle, "class", "child alternate"); err != nil {
		t.Fatal(err)
	}
	got, found, err = host.ComputedStyleProperty(handle, "", "display")
	if err != nil {
		t.Fatal(err)
	}
	if !found || got != "inline-block" {
		t.Fatalf("display after class mutation = %q, %t; want inline-block, true", got, found)
	}

	styleNode := computedStyleTestElement(page.document.Root(), "style")
	styleID, ok := page.document.ID(styleNode)
	if !ok {
		t.Fatal("style element has no stable ID")
	}
	if err := host.SetTextContent(
		NodeHandle{Document: page.DocumentGeneration(), Node: styleID},
		`#target { background-color: #0a141e; }`,
	); err != nil {
		t.Fatal(err)
	}
	got, found, err = host.ComputedStyleProperty(handle, "", "background-color")
	if err != nil {
		t.Fatal(err)
	}
	if !found || got != "rgb(10, 20, 30)" {
		t.Fatalf("background after style text mutation = %q, %t; want rgb(10, 20, 30), true", got, found)
	}
	if page.Frame() != nil {
		t.Fatal("DOM mutations plus computed-style flush unexpectedly published a frame")
	}

	if err := page.SetViewport(render.Viewport{Width: 600, Height: 600}); err != nil {
		t.Fatal(err)
	}
	got, found, err = host.ComputedStyleProperty(handle, "", "font-size")
	if err != nil {
		t.Fatal(err)
	}
	if !found || got != "16px" {
		t.Fatalf("font-size after media invalidation = %q, %t; want 16px, true", got, found)
	}

	link := computedStyleTestElement(page.document.Root(), "link")
	stylesheet, err := css.Parse(`.child { color: #010203 !important; }`)
	if err != nil {
		t.Fatal(err)
	}
	if err := page.SetResources(render.Resources{
		Stylesheets: map[*dom.Node]css.Stylesheet{link: stylesheet},
	}); err != nil {
		t.Fatal(err)
	}
	got, found, err = host.ComputedStyleProperty(handle, "", "color")
	if err != nil {
		t.Fatal(err)
	}
	if !found || got != "rgb(1, 2, 3)" {
		t.Fatalf("color after external stylesheet invalidation = %q, %t; want rgb(1, 2, 3), true", got, found)
	}
	if page.Frame() != nil {
		t.Fatal("computed-style reads after input invalidation unexpectedly published a frame")
	}
}

func TestTaskHostComputedPseudoStyleAndHandleValidation(t *testing.T) {
	t.Parallel()

	engine, page, target := computedStyleTestPage(t, `<html><head><style>
		#target::before { content:"generated"; display:block; width:50%; height:10px; color:#ff0000 }
	</style></head><body><div id="target">text</div></body></html>`)
	defer engine.Close()

	host := &taskHost{page: page, generation: page.DocumentGeneration()}
	handle := NodeHandle{Document: page.DocumentGeneration(), Node: target}
	if got, found, err := host.ComputedStyleProperty(handle, "::before", "color"); err != nil || !found || got != "rgb(255, 0, 0)" {
		t.Fatalf("pseudo color = %q, %t, %v", got, found, err)
	}
	if got, found, err := host.ComputedStyleProperty(handle, ":before", "content"); err != nil || !found || got != `"generated"` {
		t.Fatalf("legacy pseudo content = %q, %t, %v", got, found, err)
	}
	if got, found, err := host.ComputedStyleProperty(handle, "::before", "height"); err != nil || !found || got != "10px" {
		t.Fatalf("layout-backed pseudo height = %q, %t, %v", got, found, err)
	}
	if got, found, err := host.ComputedStyleProperty(handle, "::marker", "color"); err != nil || found || got != "" {
		t.Fatalf("unsupported pseudo property = %q, %t, %v; want empty, false, nil", got, found, err)
	}
	names, err := host.ComputedStylePropertyNames(handle, "::before")
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Contains(names, "content") || !slices.Contains(names, "color") {
		t.Fatalf("pseudo names = %#v", names)
	}
	unsupportedNames, err := host.ComputedStylePropertyNames(handle, "::marker")
	if err != nil {
		t.Fatal(err)
	}
	if unsupportedNames == nil || len(unsupportedNames) != 0 {
		t.Fatalf("unsupported pseudo names = %#v, want non-nil empty list", unsupportedNames)
	}
	if page.Frame() != nil || !page.Dirty() {
		t.Fatal("pseudo computed-style read published a frame or cleared dirtiness")
	}
	styleNode := computedStyleTestElement(page.document.Root(), "style")
	styleID, ok := page.document.ID(styleNode)
	if !ok {
		t.Fatal("style element has no stable ID")
	}
	if err := host.SetTextContent(
		NodeHandle{Document: page.DocumentGeneration(), Node: styleID},
		`#target::before { content:"updated"; color:#008000 }`,
	); err != nil {
		t.Fatal(err)
	}
	if got, found, err := host.ComputedStyleProperty(handle, "::before", "content"); err != nil || !found || got != `"updated"` {
		t.Fatalf("live pseudo content = %q, %t, %v", got, found, err)
	}
	if got, found, err := host.ComputedStyleProperty(handle, "::before", "color"); err != nil || !found || got != "rgb(0, 128, 0)" {
		t.Fatalf("live pseudo color = %q, %t, %v", got, found, err)
	}
	if page.Frame() != nil {
		t.Fatal("live pseudo reread published a frame")
	}

	stale := NodeHandle{Document: page.DocumentGeneration() + 1, Node: target}
	if _, _, err := host.ComputedStyleProperty(stale, "::before", "color"); !errors.Is(err, ErrStaleNodeHandle) {
		t.Fatalf("stale pseudo property error = %v, want %v", err, ErrStaleNodeHandle)
	}
	if _, err := host.ComputedStylePropertyNames(stale, "::before"); !errors.Is(err, ErrStaleNodeHandle) {
		t.Fatalf("stale pseudo names error = %v, want %v", err, ErrStaleNodeHandle)
	}

	text := computedStyleTestText(page.document.Root())
	textID, ok := page.document.ID(text)
	if !ok {
		t.Fatal("text node has no stable ID")
	}
	textHandle := NodeHandle{Document: page.DocumentGeneration(), Node: textID}
	if _, _, err := host.ComputedStyleProperty(textHandle, "", "color"); !errors.Is(err, dom.ErrWrongNodeKind) {
		t.Fatalf("text computed property error = %v, want %v", err, dom.ErrWrongNodeKind)
	}
	if _, err := host.ComputedStylePropertyNames(textHandle, ""); !errors.Is(err, dom.ErrWrongNodeKind) {
		t.Fatalf("text computed names error = %v, want %v", err, dom.ErrWrongNodeKind)
	}
	if _, _, err := host.ComputedStyleProperty(textHandle, "::before", "color"); !errors.Is(err, dom.ErrWrongNodeKind) {
		t.Fatalf("text pseudo property error = %v, want %v", err, dom.ErrWrongNodeKind)
	}

	parent, ok, err := page.document.RelatedNode(target, dom.ParentNode)
	if err != nil || !ok {
		t.Fatalf("target parent = %d, %t, %v", parent, ok, err)
	}
	if err := page.document.RemoveChild(parent, target); err != nil {
		t.Fatal(err)
	}
	if got, found, err := host.ComputedStyleProperty(handle, "", "color"); err != nil || found || got != "" {
		t.Fatalf("detached computed property = %q, %t, %v; want empty, false, nil", got, found, err)
	}
	if got, found, err := host.ComputedStyleProperty(handle, "", "width"); err != nil || found || got != "" {
		t.Fatalf("detached resolved width = %q, %t, %v; want empty, false, nil", got, found, err)
	}
	if got, found, err := host.ComputedStyleProperty(handle, "::before", "content"); err != nil || found || got != "" {
		t.Fatalf("detached pseudo property = %q, %t, %v; want empty, false, nil", got, found, err)
	}
	names, err = host.ComputedStylePropertyNames(handle, "")
	if err != nil {
		t.Fatal(err)
	}
	if names == nil || len(names) != 0 {
		t.Fatalf("detached computed names = %#v, want non-nil empty list", names)
	}
}
