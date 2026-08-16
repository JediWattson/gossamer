package style

import (
	"fmt"
	"reflect"
	"strings"
	"testing"

	"github.com/JediWattson/gossamer/internal/css"
	"github.com/JediWattson/gossamer/internal/dom"
)

func FuzzSnapshotMutationRebindMatchesFull(fuzz *testing.F) {
	for _, source := range []string{
		`div[data-state] { color:red }`,
		`:empty, :dir(rtl), input:checked { display:none }`,
		`:is(.watched, #target):has(+ img) { width:10px }`,
	} {
		fuzz.Add([]byte(source), uint8(0), uint8(0))
	}
	fuzz.Fuzz(func(t *testing.T, rawCSS []byte, operation, attributeIndex uint8) {
		if len(rawCSS) > 2048 {
			rawCSS = rawCSS[:2048]
		}
		document, owner, nodes := incrementalFuzzDocument(t)
		stylesheet, _ := css.Parse(string(rawCSS))
		input := Input{
			Environment: Environment{Width: 800, Height: 600, InitialFontSize: 16, MediaType: "screen"},
			Stylesheets: map[*dom.Node]css.Stylesheet{owner: stylesheet},
		}
		snapshot := incrementalStyleSnapshot(t, document, input)
		sequence := document.MutationSequence()
		attributes := []string{
			"data-state", "class", "id", "title", "lang", "dir", "href", "width", "height", "style",
			"type", "value", "checked", "placeholder", "required", "disabled", "min", "max",
		}
		var err error
		switch operation % 6 {
		case 0:
			err = document.SetAttribute(nodes.div, attributes[int(attributeIndex)%len(attributes)], "changed")
		case 1:
			err = document.SetAttribute(nodes.image, attributes[int(attributeIndex)%len(attributes)], "23")
		case 2:
			err = document.SetText(nodes.text, "changed text")
		case 3:
			err = document.SetAttribute(nodes.detached, attributes[int(attributeIndex)%len(attributes)], "detached")
		case 4:
			err = document.SetFormChecked(nodes.input, true)
		case 5:
			err = document.SetFormValue(nodes.input, "typed")
		}
		if err != nil {
			t.Fatal(err)
		}
		rebound, reused := incrementalStyleRebind(t, document, snapshot, sequence)
		if reused {
			assertIncrementalSnapshotMatchesFull(t, document, input, rebound)
		}
	})
}

var incrementalBenchmarkSnapshot *Snapshot

func BenchmarkSnapshotMutationRebind(b *testing.B) {
	for _, benchmark := range []struct {
		name string
		full bool
	}{{name: "rebind"}, {name: "full-pass", full: true}} {
		b.Run(benchmark.name, func(b *testing.B) {
			document, owner, target := incrementalBenchmarkDocument(b)
			var source strings.Builder
			for index := 0; index < 800; index++ {
				fmt.Fprintf(&source, ".rule-%d { color:#123456; margin-left:%dpx }\n", index, index%31)
			}
			input := incrementalStyleInputForBenchmark(b, owner, source.String())
			snapshot := incrementalStyleSnapshotForBenchmark(b, document, input)
			sequence := document.MutationSequence()
			b.ReportAllocs()
			b.ResetTimer()
			for iteration := 0; iteration < b.N; iteration++ {
				if err := document.SetAttribute(target, "data-unrelated", fmt.Sprint(iteration)); err != nil {
					b.Fatal(err)
				}
				if benchmark.full {
					snapshot = incrementalStyleSnapshotForBenchmark(b, document, input)
				} else {
					var rebound *Snapshot
					var reused bool
					err := document.WithReadView(func(view dom.ReadView) error {
						access, err := view.Acquire()
						if err != nil {
							return err
						}
						records, latest, err := access.MutationRecordsSince(sequence)
						access.Close()
						if err != nil {
							return err
						}
						sequence = latest
						rebound, reused, err = snapshot.RebindReadViewAfterMutations(view, records)
						return err
					})
					if err != nil || !reused {
						b.Fatalf("rebind = %t, %v", reused, err)
					}
					snapshot = rebound
				}
				incrementalBenchmarkSnapshot = snapshot
			}
		})
	}
}

type incrementalFuzzNodes struct {
	div      dom.NodeID
	image    dom.NodeID
	text     dom.NodeID
	input    dom.NodeID
	detached dom.NodeID
}

func incrementalFuzzDocument(t *testing.T) (*dom.Document, *dom.Node, incrementalFuzzNodes) {
	t.Helper()
	root := dom.NewDocument()
	html := dom.NewElement("html")
	head := dom.NewElement("head")
	owner := dom.NewElement("style")
	head.AppendChild(owner)
	body := dom.NewElement("body")
	div := dom.NewElement("div", dom.Attribute{Name: "id", Value: "target"}, dom.Attribute{Name: "class", Value: "watched"})
	paragraph := dom.NewElement("p")
	text := dom.NewText("before")
	paragraph.AppendChild(text)
	image := dom.NewElement("img")
	input := dom.NewElement("input", dom.Attribute{Name: "type", Value: "checkbox"})
	body.AppendChild(div)
	body.AppendChild(paragraph)
	body.AppendChild(image)
	body.AppendChild(input)
	html.AppendChild(head)
	html.AppendChild(body)
	root.AppendChild(html)
	document, err := dom.IndexDocument(root)
	if err != nil {
		t.Fatal(err)
	}
	detached, err := document.CreateElement("section")
	if err != nil {
		t.Fatal(err)
	}
	id := func(node *dom.Node) dom.NodeID {
		result, _ := document.ID(node)
		return result
	}
	return document, owner, incrementalFuzzNodes{div: id(div), image: id(image), text: id(text), input: id(input), detached: detached}
}

func incrementalBenchmarkDocument(b *testing.B) (*dom.Document, *dom.Node, dom.NodeID) {
	b.Helper()
	root := dom.NewDocument()
	html := dom.NewElement("html")
	head := dom.NewElement("head")
	owner := dom.NewElement("style")
	head.AppendChild(owner)
	body := dom.NewElement("body")
	var target *dom.Node
	for index := 0; index < 400; index++ {
		node := dom.NewElement("div", dom.Attribute{Name: "class", Value: fmt.Sprintf("rule-%d", index*2)})
		body.AppendChild(node)
		if index == 200 {
			target = node
		}
	}
	html.AppendChild(head)
	html.AppendChild(body)
	root.AppendChild(html)
	document, err := dom.IndexDocument(root)
	if err != nil {
		b.Fatal(err)
	}
	targetID, _ := document.ID(target)
	return document, owner, targetID
}

func incrementalStyleInputForBenchmark(b *testing.B, owner *dom.Node, source string) Input {
	b.Helper()
	stylesheet, err := css.Parse(source)
	if err != nil {
		b.Fatal(err)
	}
	return Input{
		Environment: Environment{Width: 1024, Height: 768, InitialFontSize: 16, MediaType: "screen"},
		Stylesheets: map[*dom.Node]css.Stylesheet{owner: stylesheet},
	}
}

func incrementalStyleSnapshotForBenchmark(b *testing.B, document *dom.Document, input Input) *Snapshot {
	b.Helper()
	var snapshot *Snapshot
	if err := document.WithReadView(func(view dom.ReadView) error {
		var err error
		snapshot, err = ComputeReadView(view, input)
		return err
	}); err != nil {
		b.Fatal(err)
	}
	return snapshot
}

func TestSnapshotRebindsStyleNeutralAttributeAndDetachedMutations(t *testing.T) {
	document, owner, target, _ := incrementalStyleDocument(t, "div")
	input := incrementalStyleInput(t, owner, `.watched[data-state] { color:red }`)
	snapshot := incrementalStyleSnapshot(t, document, input)
	sequence := document.MutationSequence()

	if err := document.SetAttribute(target, "data-unrelated", "changed"); err != nil {
		t.Fatal(err)
	}
	rebound, reused := incrementalStyleRebind(t, document, snapshot, sequence)
	if !reused {
		t.Fatal("unreferenced connected attribute forced a full style pass")
	}
	assertIncrementalSnapshotMatchesFull(t, document, input, rebound)
	if reflect.ValueOf(rebound.byID).Pointer() != reflect.ValueOf(snapshot.byID).Pointer() {
		t.Fatal("style-neutral rebind copied computed style storage")
	}

	detached, err := document.CreateElement("section")
	if err != nil {
		t.Fatal(err)
	}
	sequence = document.MutationSequence()
	if err := document.SetAttribute(detached, "data-state", "watched-but-detached"); err != nil {
		t.Fatal(err)
	}
	rebound, reused = incrementalStyleRebind(t, document, rebound, sequence)
	if !reused {
		t.Fatal("detached attribute mutation forced a full style pass")
	}
	assertIncrementalSnapshotMatchesFull(t, document, input, rebound)

	sequence = document.MutationSequence()
	if err := document.SetAttribute(target, "data-state", "watched"); err != nil {
		t.Fatal(err)
	}
	if _, reused = incrementalStyleRebind(t, document, rebound, sequence); reused {
		t.Fatal("referenced selector attribute reused stale styles")
	}
}

func TestSnapshotRebindCharacterDataUsesTextDependencies(t *testing.T) {
	document, owner, _, text := incrementalStyleDocument(t, "p")
	input := incrementalStyleInput(t, owner, `p:empty { color:red }`)
	snapshot := incrementalStyleSnapshot(t, document, input)
	sequence := document.MutationSequence()

	if err := document.SetText(text, "still nonempty"); err != nil {
		t.Fatal(err)
	}
	rebound, reused := incrementalStyleRebind(t, document, snapshot, sequence)
	if !reused {
		t.Fatal("nonempty-to-nonempty text mutation forced a full style pass")
	}
	assertIncrementalSnapshotMatchesFull(t, document, input, rebound)

	sequence = document.MutationSequence()
	if err := document.SetText(text, ""); err != nil {
		t.Fatal(err)
	}
	if _, reused = incrementalStyleRebind(t, document, rebound, sequence); reused {
		t.Fatal(":empty transition reused stale styles")
	}
}

func TestSnapshotRebindRejectsAutomaticDirectionalityTextMutation(t *testing.T) {
	document, owner, target, text := incrementalStyleDocument(t, "bdi")
	input := incrementalStyleInput(t, owner, `bdi { color:red }`)
	snapshot := incrementalStyleSnapshot(t, document, input)
	sequence := document.MutationSequence()
	if err := document.SetText(text, "אבג"); err != nil {
		t.Fatal(err)
	}
	if _, reused := incrementalStyleRebind(t, document, snapshot, sequence); reused {
		t.Fatalf("automatic directionality text mutation on node %d reused stale styles", target)
	}
}

func TestSnapshotRebindNativeStateUsesPseudoDependencies(t *testing.T) {
	for _, test := range []struct {
		name       string
		stylesheet string
		wantReused bool
	}{
		{name: "unobserved checked state", stylesheet: `input { color:red }`, wantReused: true},
		{name: "checked pseudo", stylesheet: `input:checked { color:red }`, wantReused: false},
	} {
		t.Run(test.name, func(t *testing.T) {
			document, owner, inputID, _ := incrementalStyleDocument(t, "input")
			if err := document.SetAttribute(inputID, "type", "checkbox"); err != nil {
				t.Fatal(err)
			}
			snapshot := incrementalStyleSnapshot(t, document, incrementalStyleInput(t, owner, test.stylesheet))
			sequence := document.MutationSequence()
			if err := document.SetFormChecked(inputID, true); err != nil {
				t.Fatal(err)
			}
			rebound, reused := incrementalStyleRebind(t, document, snapshot, sequence)
			if reused != test.wantReused {
				t.Fatalf("reused = %t, want %t", reused, test.wantReused)
			}
			if reused {
				assertIncrementalSnapshotMatchesFull(t, document, incrementalStyleInput(t, owner, test.stylesheet), rebound)
			}
		})
	}
}

func incrementalStyleDocument(t *testing.T, targetName string) (*dom.Document, *dom.Node, dom.NodeID, dom.NodeID) {
	t.Helper()
	root := dom.NewDocument()
	html := dom.NewElement("html")
	head := dom.NewElement("head")
	owner := dom.NewElement("style")
	head.AppendChild(owner)
	body := dom.NewElement("body")
	target := dom.NewElement(targetName, dom.Attribute{Name: "class", Value: "watched"})
	text := dom.NewText("before")
	if targetName != "input" {
		target.AppendChild(text)
	}
	body.AppendChild(target)
	html.AppendChild(head)
	html.AppendChild(body)
	root.AppendChild(html)
	document, err := dom.IndexDocument(root)
	if err != nil {
		t.Fatal(err)
	}
	targetID, _ := document.ID(target)
	textID, _ := document.ID(text)
	return document, owner, targetID, textID
}

func incrementalStyleInput(t *testing.T, owner *dom.Node, source string) Input {
	t.Helper()
	stylesheet, err := css.Parse(source)
	if err != nil {
		t.Fatal(err)
	}
	return Input{
		Environment: Environment{Width: 800, Height: 600, InitialFontSize: 16, MediaType: "screen"},
		Stylesheets: map[*dom.Node]css.Stylesheet{owner: stylesheet},
	}
}

func incrementalStyleSnapshot(t *testing.T, document *dom.Document, input Input) *Snapshot {
	t.Helper()
	var snapshot *Snapshot
	if err := document.WithReadView(func(view dom.ReadView) error {
		var err error
		snapshot, err = ComputeReadView(view, input)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	return snapshot
}

func incrementalStyleRebind(t *testing.T, document *dom.Document, snapshot *Snapshot, sequence uint64) (*Snapshot, bool) {
	t.Helper()
	var rebound *Snapshot
	var reused bool
	if err := document.WithReadView(func(view dom.ReadView) error {
		access, err := view.Acquire()
		if err != nil {
			return err
		}
		records, _, err := access.MutationRecordsSince(sequence)
		access.Close()
		if err != nil {
			return err
		}
		rebound, reused, err = snapshot.RebindReadViewAfterMutations(view, records)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	return rebound, reused
}

func assertIncrementalSnapshotMatchesFull(t *testing.T, document *dom.Document, input Input, incremental *Snapshot) {
	t.Helper()
	full := incrementalStyleSnapshot(t, document, input)
	if got, want := incremental.DumpExplanations(), full.DumpExplanations(); got != want {
		t.Fatalf("incremental snapshot differs from full pass\n--- incremental ---\n%s\n--- full ---\n%s", got, want)
	}
}
