package style

import (
	"fmt"
	"reflect"
	"strings"
	"testing"

	"github.com/JediWattson/gossamer/internal/css"
	"github.com/JediWattson/gossamer/internal/dom"
)

func FuzzSnapshotMutationUpdateMatchesFull(fuzz *testing.F) {
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
		rebound, reused := incrementalStyleUpdate(t, document, snapshot, input, sequence)
		if reused {
			assertIncrementalSnapshotMatchesFull(t, document, input, rebound)
		}
	})
}

func TestSnapshotRestylesAttributeSubtreeAndPseudoStyles(t *testing.T) {
	document, owner, parentID, childID := incrementalSubtreeDocument(t)
	input := incrementalStyleInput(t, owner, `
		.on .item { color:#123456 }
		.on::before { content:"active"; color:inherit }
	`)
	snapshot := incrementalStyleSnapshot(t, document, input)
	before := snapshot.DumpExplanations()
	sequence := document.MutationSequence()
	if err := document.SetAttribute(parentID, "class", "on"); err != nil {
		t.Fatal(err)
	}
	updated, reused := incrementalStyleUpdate(t, document, snapshot, input, sequence)
	if !reused {
		t.Fatal("selector-dependent class mutation did not use subtree restyle")
	}
	assertIncrementalSnapshotMatchesFull(t, document, input, updated)
	child, ok := updated.LookupID(childID)
	if !ok || child.Color().R != 0x12 || child.Color().G != 0x34 || child.Color().B != 0x56 {
		t.Fatalf("restyled descendant color = %#v, %t", child.Color(), ok)
	}
	if _, ok := updated.byPseudoID[stablePseudoKey{id: parentID, pseudo: css.PseudoElementBefore}]; !ok {
		t.Fatal("restyled subtree did not add matching ::before style")
	}
	if got := snapshot.DumpExplanations(); got != before {
		t.Fatal("incremental restyle mutated the previous snapshot")
	}

	sequence = document.MutationSequence()
	if err := document.SetAttribute(parentID, "class", "off"); err != nil {
		t.Fatal(err)
	}
	updated, reused = incrementalStyleUpdate(t, document, updated, input, sequence)
	if !reused {
		t.Fatal("second class mutation did not use subtree restyle")
	}
	assertIncrementalSnapshotMatchesFull(t, document, input, updated)
	if _, ok := updated.byPseudoID[stablePseudoKey{id: parentID, pseudo: css.PseudoElementBefore}]; ok {
		t.Fatal("restyled subtree retained stale ::before style")
	}
}

func TestSnapshotRestylesSiblingAndNthFilterDependencies(t *testing.T) {
	for _, test := range []struct {
		name       string
		stylesheet string
		mutate     func(*testing.T, *dom.Document, incrementalSiblingNodes)
	}{
		{
			name:       "adjacent sibling",
			stylesheet: `.lead + .target { color:#123456 }`,
			mutate: func(t *testing.T, document *dom.Document, nodes incrementalSiblingNodes) {
				t.Helper()
				if err := document.SetAttribute(nodes.first, "class", "lead"); err != nil {
					t.Fatal(err)
				}
			},
		},
		{
			name:       "nth of selector",
			stylesheet: `li:nth-child(2 of .pick) { color:#123456 }`,
			mutate: func(t *testing.T, document *dom.Document, nodes incrementalSiblingNodes) {
				t.Helper()
				if err := document.SetAttribute(nodes.first, "class", "pick"); err != nil {
					t.Fatal(err)
				}
			},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			document, owner, nodes := incrementalSiblingDocument(t)
			input := incrementalStyleInput(t, owner, test.stylesheet)
			snapshot := incrementalStyleSnapshot(t, document, input)
			sequence := document.MutationSequence()
			test.mutate(t, document, nodes)
			updated, reused := incrementalStyleUpdate(t, document, snapshot, input, sequence)
			if !reused {
				t.Fatal("sibling-sensitive mutation did not use parent-subtree restyle")
			}
			assertIncrementalSnapshotMatchesFull(t, document, input, updated)
		})
	}
}

func TestSnapshotRestylesInlineInheritanceEmptyTextAndNativeState(t *testing.T) {
	t.Run("inline inheritance", func(t *testing.T) {
		document, owner, parentID, childID := incrementalSubtreeDocument(t)
		input := incrementalStyleInput(t, owner, `.item { color:var(--tone) }`)
		snapshot := incrementalStyleSnapshot(t, document, input)
		sequence := document.MutationSequence()
		if err := document.SetAttribute(parentID, "style", "--tone:#123456"); err != nil {
			t.Fatal(err)
		}
		updated, reused := incrementalStyleUpdate(t, document, snapshot, input, sequence)
		if !reused {
			t.Fatal("inline style mutation did not use subtree restyle")
		}
		assertIncrementalSnapshotMatchesFull(t, document, input, updated)
		child, _ := updated.LookupID(childID)
		if child.Color().R != 0x12 || child.Color().G != 0x34 || child.Color().B != 0x56 {
			t.Fatalf("inherited custom-property color = %#v", child.Color())
		}
	})

	t.Run("empty text", func(t *testing.T) {
		document, owner, target, text := incrementalStyleDocument(t, "p")
		input := incrementalStyleInput(t, owner, `p:empty { color:#123456 }`)
		snapshot := incrementalStyleSnapshot(t, document, input)
		sequence := document.MutationSequence()
		if err := document.SetText(text, ""); err != nil {
			t.Fatal(err)
		}
		updated, reused := incrementalStyleUpdate(t, document, snapshot, input, sequence)
		if !reused {
			t.Fatal(":empty transition did not use parent-subtree restyle")
		}
		assertIncrementalSnapshotMatchesFull(t, document, input, updated)
		computed, _ := updated.LookupID(target)
		if computed.Color().R != 0x12 {
			t.Fatalf(":empty color = %#v", computed.Color())
		}
	})

	t.Run("checked state", func(t *testing.T) {
		document, owner, target, _ := incrementalStyleDocument(t, "input")
		if err := document.SetAttribute(target, "type", "checkbox"); err != nil {
			t.Fatal(err)
		}
		input := incrementalStyleInput(t, owner, `input:checked { color:#123456 }`)
		snapshot := incrementalStyleSnapshot(t, document, input)
		sequence := document.MutationSequence()
		if err := document.SetFormChecked(target, true); err != nil {
			t.Fatal(err)
		}
		updated, reused := incrementalStyleUpdate(t, document, snapshot, input, sequence)
		if !reused {
			t.Fatal(":checked state mutation did not use subtree restyle")
		}
		assertIncrementalSnapshotMatchesFull(t, document, input, updated)
	})

	t.Run("automatic directionality", func(t *testing.T) {
		document, owner, target, text := incrementalStyleDocument(t, "bdi")
		input := incrementalStyleInput(t, owner, `bdi:dir(rtl) { color:#123456 }`)
		snapshot := incrementalStyleSnapshot(t, document, input)
		sequence := document.MutationSequence()
		if err := document.SetText(text, "אבג"); err != nil {
			t.Fatal(err)
		}
		updated, reused := incrementalStyleUpdate(t, document, snapshot, input, sequence)
		if !reused {
			t.Fatal("automatic directionality text mutation did not use subtree restyle")
		}
		assertIncrementalSnapshotMatchesFull(t, document, input, updated)
		computed, _ := updated.LookupID(target)
		if computed.Color().R != 0x12 {
			t.Fatalf(":dir(rtl) color = %#v", computed.Color())
		}
	})
}

func TestSnapshotRestyleFallsBackForGlobalOrStructuralDependencies(t *testing.T) {
	for _, test := range []struct {
		name       string
		stylesheet string
		mutate     func(*testing.T, *dom.Document, dom.NodeID)
	}{
		{
			name:       "relational has",
			stylesheet: `body:has(.on) { color:red }`,
			mutate: func(t *testing.T, document *dom.Document, target dom.NodeID) {
				t.Helper()
				if err := document.SetAttribute(target, "class", "on"); err != nil {
					t.Fatal(err)
				}
			},
		},
		{
			name:       "target relocation",
			stylesheet: `:target { color:red }`,
			mutate: func(t *testing.T, document *dom.Document, target dom.NodeID) {
				t.Helper()
				if err := document.SetAttribute(target, "id", "destination"); err != nil {
					t.Fatal(err)
				}
			},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			document, owner, target, _ := incrementalStyleDocument(t, "div")
			input := incrementalStyleInput(t, owner, test.stylesheet)
			input.SelectorState.TargetID = "destination"
			snapshot := incrementalStyleSnapshot(t, document, input)
			sequence := document.MutationSequence()
			test.mutate(t, document, target)
			if _, reused := incrementalStyleUpdate(t, document, snapshot, input, sequence); reused {
				t.Fatal("global selector dependency incorrectly used subtree restyle")
			}
		})
	}
}

func TestSnapshotRestyleRecompactsSupersededProvenance(t *testing.T) {
	document, owner, target, _ := incrementalSubtreeDocument(t)
	input := incrementalStyleInput(t, owner, `[data-state=on] { color:#123456 }`)
	snapshot := incrementalStyleSnapshot(t, document, input)
	original := snapshot
	originalDump := original.DumpExplanations()
	sequence := document.MutationSequence()
	for iteration := 0; iteration < 64; iteration++ {
		value := "off"
		if iteration%2 == 0 {
			value = "on"
		}
		if err := document.SetAttribute(target, "data-state", value); err != nil {
			t.Fatal(err)
		}
		updated, reused := incrementalStyleUpdate(t, document, snapshot, input, sequence)
		if !reused {
			t.Fatalf("iteration %d fell back to a full pass", iteration)
		}
		snapshot = updated
		sequence = document.MutationSequence()
	}
	full := incrementalStyleSnapshot(t, document, input)
	if got, want := snapshot.DumpExplanations(), full.DumpExplanations(); got != want {
		t.Fatal("repeated targeted restyles diverged from full provenance")
	}
	if len(snapshot.provenance.sources) != len(full.provenance.sources) ||
		len(snapshot.provenance.rollbacks) != len(full.provenance.rollbacks) {
		t.Fatalf("incremental provenance retained stale arena entries: sources %d/%d, rollbacks %d/%d",
			len(snapshot.provenance.sources), len(full.provenance.sources),
			len(snapshot.provenance.rollbacks), len(full.provenance.rollbacks))
	}
	if got := original.DumpExplanations(); got != originalDump {
		t.Fatal("repeated restyles mutated the original snapshot")
	}
}

func BenchmarkSnapshotMutationSubtreeRestyle(b *testing.B) {
	for _, benchmark := range []struct {
		name string
		full bool
	}{{name: "subtree-restyle"}, {name: "full-pass", full: true}} {
		b.Run(benchmark.name, func(b *testing.B) {
			document, owner, target := incrementalBenchmarkDocument(b)
			var source strings.Builder
			for index := 0; index < 800; index++ {
				fmt.Fprintf(&source, ".rule-%d { color:#123456; margin-left:%dpx }\n", index, index%31)
			}
			source.WriteString(`[data-state=on] { color:#654321 }`)
			input := incrementalStyleInputForBenchmark(b, owner, source.String())
			snapshot := incrementalStyleSnapshotForBenchmark(b, document, input)
			sequence := document.MutationSequence()
			b.ReportAllocs()
			b.ResetTimer()
			for iteration := 0; iteration < b.N; iteration++ {
				value := "off"
				if iteration%2 == 0 {
					value = "on"
				}
				if err := document.SetAttribute(target, "data-state", value); err != nil {
					b.Fatal(err)
				}
				if benchmark.full {
					snapshot = incrementalStyleSnapshotForBenchmark(b, document, input)
					sequence = document.MutationSequence()
				} else {
					var updated *Snapshot
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
						updated, reused, err = snapshot.RestyleReadViewAfterMutations(view, input, records)
						return err
					})
					if err != nil || !reused {
						b.Fatalf("subtree restyle = %t, %v", reused, err)
					}
					snapshot = updated
				}
				incrementalBenchmarkSnapshot = snapshot
			}
		})
	}
}

type incrementalSiblingNodes struct {
	first  dom.NodeID
	second dom.NodeID
	third  dom.NodeID
}

func incrementalSubtreeDocument(t *testing.T) (*dom.Document, *dom.Node, dom.NodeID, dom.NodeID) {
	t.Helper()
	root := dom.NewDocument()
	html := dom.NewElement("html")
	head := dom.NewElement("head")
	owner := dom.NewElement("style")
	head.AppendChild(owner)
	body := dom.NewElement("body")
	parent := dom.NewElement("section", dom.Attribute{Name: "class", Value: "off"})
	child := dom.NewElement("span", dom.Attribute{Name: "class", Value: "item"})
	child.AppendChild(dom.NewText("content"))
	parent.AppendChild(child)
	body.AppendChild(parent)
	html.AppendChild(head)
	html.AppendChild(body)
	root.AppendChild(html)
	document, err := dom.IndexDocument(root)
	if err != nil {
		t.Fatal(err)
	}
	parentID, _ := document.ID(parent)
	childID, _ := document.ID(child)
	return document, owner, parentID, childID
}

func incrementalSiblingDocument(t *testing.T) (*dom.Document, *dom.Node, incrementalSiblingNodes) {
	t.Helper()
	root := dom.NewDocument()
	html := dom.NewElement("html")
	head := dom.NewElement("head")
	owner := dom.NewElement("style")
	head.AppendChild(owner)
	body := dom.NewElement("body")
	list := dom.NewElement("ul")
	first := dom.NewElement("li")
	second := dom.NewElement("li", dom.Attribute{Name: "class", Value: "target pick"})
	third := dom.NewElement("li", dom.Attribute{Name: "class", Value: "pick"})
	list.AppendChild(first)
	list.AppendChild(second)
	list.AppendChild(third)
	body.AppendChild(list)
	html.AppendChild(head)
	html.AppendChild(body)
	root.AppendChild(html)
	document, err := dom.IndexDocument(root)
	if err != nil {
		t.Fatal(err)
	}
	id := func(node *dom.Node) dom.NodeID {
		result, _ := document.ID(node)
		return result
	}
	return document, owner, incrementalSiblingNodes{first: id(first), second: id(second), third: id(third)}
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

func TestSnapshotMutationUpdateRejectsStylesheetSourceChanges(t *testing.T) {
	root := dom.NewDocument()
	html := dom.NewElement("html")
	head := dom.NewElement("head")
	owner := dom.NewElement("style")
	source := dom.NewText(`div { color:red }`)
	owner.AppendChild(source)
	head.AppendChild(owner)
	body := dom.NewElement("body")
	target := dom.NewElement("div")
	body.AppendChild(target)
	html.AppendChild(head)
	html.AppendChild(body)
	root.AppendChild(html)
	document, err := dom.IndexDocument(root)
	if err != nil {
		t.Fatal(err)
	}
	input := Input{Environment: Environment{Width: 800, Height: 600, InitialFontSize: 16, MediaType: "screen"}}
	snapshot := incrementalStyleSnapshot(t, document, input)
	sourceID, _ := document.ID(source)
	ownerID, _ := document.ID(owner)

	sequence := document.MutationSequence()
	if err := document.SetText(sourceID, `div { color:blue }`); err != nil {
		t.Fatal(err)
	}
	if _, reused := incrementalStyleUpdate(t, document, snapshot, input, sequence); reused {
		t.Fatal("embedded stylesheet text mutation used an incremental snapshot")
	}

	snapshot = incrementalStyleSnapshot(t, document, input)
	sequence = document.MutationSequence()
	if err := document.SetAttribute(ownerID, "media", "print"); err != nil {
		t.Fatal(err)
	}
	if _, reused := incrementalStyleUpdate(t, document, snapshot, input, sequence); reused {
		t.Fatal("stylesheet media mutation used an incremental snapshot")
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

func incrementalStyleUpdate(
	t *testing.T,
	document *dom.Document,
	snapshot *Snapshot,
	input Input,
	sequence uint64,
) (*Snapshot, bool) {
	t.Helper()
	var updated *Snapshot
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
		updated, reused, err = snapshot.RebindReadViewAfterMutations(view, records)
		if err != nil || reused {
			return err
		}
		updated, reused, err = snapshot.RestyleReadViewAfterMutations(view, input, records)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	return updated, reused
}

func assertIncrementalSnapshotMatchesFull(t *testing.T, document *dom.Document, input Input, incremental *Snapshot) {
	t.Helper()
	full := incrementalStyleSnapshot(t, document, input)
	if got, want := incremental.DumpExplanations(), full.DumpExplanations(); got != want {
		t.Fatalf("incremental snapshot differs from full pass\n--- incremental ---\n%s\n--- full ---\n%s", got, want)
	}
}
