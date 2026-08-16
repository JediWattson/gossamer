package render_test

import (
	"math"
	"sort"
	"testing"

	"github.com/JediWattson/gossamer/internal/dom"
	"github.com/JediWattson/gossamer/internal/render"
)

func TestTextAlignJustifyFillsSoftWrappedLinesButNotFinalLine(t *testing.T) {
	t.Parallel()

	document, body := boxModelDocument()
	container := dom.NewElement("div", dom.Attribute{Name: "style", Value: "width:80px;text-align:justify;font:10px monospace"})
	text := dom.NewText("one two three four five")
	container.AppendChild(text)
	body.AppendChild(container)

	frame, err := render.Render(document, render.Viewport{Width: 160, Height: 100})
	if err != nil {
		t.Fatal(err)
	}
	box := findBox(frame.Root, container)
	if box == nil {
		t.Fatal("justified container box missing")
	}
	lines := textFragmentLinesForNode(collectTextFragments(frame.Root), text)
	if len(lines) < 2 {
		t.Fatalf("justified lines = %#v, want at least two", lines)
	}
	first := lines[0]
	last := lines[len(lines)-1]
	if len(first) < 2 {
		t.Fatalf("first justified line fragments = %#v, want split inter-word fragments", first)
	}
	assertNear(t, "first justified x", first[0].X, box.ContentBounds.X)
	assertNear(t, "first justified edge", lineRight(first), box.ContentBounds.X+box.ContentBounds.Width)
	if lineRight(last) >= box.ContentBounds.X+box.ContentBounds.Width-0.01 {
		t.Fatalf("final line right = %.2f, want start-aligned before %.2f", lineRight(last), box.ContentBounds.X+box.ContentBounds.Width)
	}
	for index := 1; index < len(first); index++ {
		if first[index].X <= first[index-1].X+first[index-1].Width {
			t.Fatalf("justification opportunity %d did not expand: %#v", index, first)
		}
	}
}

func TestTextAlignJustifyDoesNotExpandForcedBreakLine(t *testing.T) {
	t.Parallel()

	document, body := boxModelDocument()
	container := dom.NewElement("div", dom.Attribute{Name: "style", Value: "width:120px;text-align:justify;font:10px monospace"})
	firstText := dom.NewText("one two")
	container.AppendChild(firstText)
	container.AppendChild(dom.NewElement("br"))
	container.AppendChild(dom.NewText("three four"))
	body.AppendChild(container)

	frame, err := render.Render(document, render.Viewport{Width: 160, Height: 100})
	if err != nil {
		t.Fatal(err)
	}
	box := findBox(frame.Root, container)
	lines := textFragmentLinesForNode(collectTextFragments(frame.Root), firstText)
	if box == nil || len(lines) != 1 {
		t.Fatalf("forced-break fixture = box:%#v lines:%#v", box, lines)
	}
	if len(lines[0]) != 1 {
		t.Fatalf("forced-break line was expanded into %d fragments: %#v", len(lines[0]), lines[0])
	}
	if lineRight(lines[0]) >= box.ContentBounds.X+box.ContentBounds.Width {
		t.Fatalf("forced-break line unexpectedly filled container: %#v", lines[0])
	}
}

func TestTextAlignJustifyExpandsSpaceBeforeAtomicInlineBox(t *testing.T) {
	t.Parallel()

	document, body := boxModelDocument()
	container := dom.NewElement("div", dom.Attribute{Name: "style", Value: "width:60px;text-align:justify;font:10px monospace"})
	container.AppendChild(dom.NewText("a "))
	atomic := dom.NewElement("span", dom.Attribute{Name: "style", Value: "display:inline-block;width:10px;height:10px;background:#123456"})
	container.AppendChild(atomic)
	container.AppendChild(dom.NewText(" xxxxxxxx"))
	body.AppendChild(container)

	frame, err := render.Render(document, render.Viewport{Width: 120, Height: 100})
	if err != nil {
		t.Fatal(err)
	}
	containerBox := findBox(frame.Root, container)
	atomicBox := findBox(frame.Root, atomic)
	if containerBox == nil || atomicBox == nil {
		t.Fatalf("atomic justification boxes = container:%#v atomic:%#v", containerBox, atomicBox)
	}
	assertNear(t, "justified atomic right", atomicBox.Bounds.X+atomicBox.Bounds.Width, containerBox.ContentBounds.X+containerBox.ContentBounds.Width)
	if hit := render.HitTest(frame, atomicBox.Bounds.X+1, atomicBox.Bounds.Y+1); hit != atomic {
		t.Fatalf("justified atomic hit = %p, want %p", hit, atomic)
	}
}

func TestTextAlignJustifyExpandsSpaceBeforeInlineReplacedBox(t *testing.T) {
	t.Parallel()

	document, body := boxModelDocument()
	container := dom.NewElement("div", dom.Attribute{Name: "style", Value: "width:60px;text-align:justify;font:10px monospace"})
	container.AppendChild(dom.NewText("a "))
	imageNode := dom.NewElement("img", dom.Attribute{Name: "style", Value: "width:10px;height:10px"})
	container.AppendChild(imageNode)
	container.AppendChild(dom.NewText(" xxxxxxxx"))
	body.AppendChild(container)

	frame, err := render.Render(document, render.Viewport{Width: 120, Height: 100})
	if err != nil {
		t.Fatal(err)
	}
	containerBox := findBox(frame.Root, container)
	geometry, ok := frame.Layout.Geometry(imageNode)
	if containerBox == nil || !ok {
		t.Fatalf("replaced justification geometry = container:%#v image:%#v/%t", containerBox, geometry, ok)
	}
	assertNear(t, "justified image right", geometry.Bounds.X+geometry.Bounds.Width, containerBox.ContentBounds.X+containerBox.ContentBounds.Width)
}

func textFragmentLinesForNode(fragments []render.TextFragment, node *dom.Node) [][]render.TextFragment {
	byBaseline := make(map[float64][]render.TextFragment)
	var baselines []float64
	for _, fragment := range fragments {
		if fragment.Node != node {
			continue
		}
		if _, exists := byBaseline[fragment.BaselineY]; !exists {
			baselines = append(baselines, fragment.BaselineY)
		}
		byBaseline[fragment.BaselineY] = append(byBaseline[fragment.BaselineY], fragment)
	}
	sort.Float64s(baselines)
	lines := make([][]render.TextFragment, 0, len(baselines))
	for _, baseline := range baselines {
		line := byBaseline[baseline]
		sort.Slice(line, func(left, right int) bool { return line[left].X < line[right].X })
		lines = append(lines, line)
	}
	return lines
}

func lineRight(line []render.TextFragment) float64 {
	right := math.Inf(-1)
	for _, fragment := range line {
		right = math.Max(right, fragment.X+fragment.Width)
	}
	return right
}
