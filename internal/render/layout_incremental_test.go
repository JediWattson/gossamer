package render_test

import (
	"fmt"
	"reflect"
	"strings"
	"testing"

	"github.com/JediWattson/gossamer/internal/dom"
	htmlparser "github.com/JediWattson/gossamer/internal/html"
	"github.com/JediWattson/gossamer/internal/render"
	"github.com/JediWattson/gossamer/internal/style"
)

func TestLayoutSnapshotReusesGeometryForPaintOnlyStyleChange(t *testing.T) {
	document, target := indexedLayoutDocument(t, `
		<html><body style="margin:0">
		<div id="target" style="width:120px;height:30px;opacity:.5;background:red;color:red">paint me</div>
		</body></html>
	`)
	viewport := render.Viewport{Width: 320, Height: 200}
	previousStyles := mustDocumentStyleSnapshot(t, document, viewport, render.Resources{})
	previousLayout := mustLayoutSnapshot(t, document, viewport, previousStyles)
	previousGeometry, _ := previousLayout.GeometryID(target)
	sequence := document.MutationSequence()
	if err := document.SetAttribute(target, "style", "width:120px;height:30px;opacity:.5;background:blue;color:blue"); err != nil {
		t.Fatal(err)
	}
	currentStyles := mustDocumentStyleSnapshot(t, document, viewport, render.Resources{})

	reused, ok := reuseLayoutSnapshot(t, document, viewport, previousLayout, currentStyles, sequence)
	if !ok || !reused.ReusedLayout() {
		t.Fatal("paint-only style change did not reuse layout")
	}
	if reused.ComputedStyles() != currentStyles || reused.Version() != document.Version() {
		t.Fatal("reused layout did not advance to the current style/document snapshot")
	}
	if geometry, found := reused.GeometryID(target); !found || !reflect.DeepEqual(geometry, previousGeometry) {
		t.Fatalf("reused geometry = %#v, %t; want %#v", geometry, found, previousGeometry)
	}
	if len(reused.DamageRects()) == 0 {
		t.Fatal("paint-only reuse did not retain damage bounds")
	}
	if previousLayout.ReusedLayout() || len(previousLayout.DamageRects()) != 0 {
		t.Fatal("incremental reuse mutated the prior layout snapshot")
	}

	incrementalFrame := mustFrameFromLayout(t, document, reused)
	fullLayout := mustLayoutSnapshot(t, document, viewport, currentStyles)
	fullFrame := mustFrameFromLayout(t, document, fullLayout)
	if !reflect.DeepEqual(incrementalFrame.DisplayList, fullFrame.DisplayList) {
		t.Fatalf("incremental paint differs from full layout\n incremental=%#v\n full=%#v",
			incrementalFrame.DisplayList.Commands, fullFrame.DisplayList.Commands)
	}
}

var incrementalLayoutBenchmarkSnapshot *render.LayoutSnapshot

func BenchmarkLayoutSnapshotPaintReuse(b *testing.B) {
	for _, benchmark := range []struct {
		name string
		full bool
	}{{name: "reuse"}, {name: "full-layout", full: true}} {
		b.Run(benchmark.name, func(b *testing.B) {
			document, target := benchmarkLayoutDocument(b)
			viewport := render.Viewport{Width: 1024, Height: 768}
			styles := benchmarkStyleSnapshot(b, document, viewport)
			layout := benchmarkLayoutSnapshot(b, document, viewport, styles)
			sequence := document.MutationSequence()
			b.ReportAllocs()
			b.ResetTimer()
			for iteration := 0; iteration < b.N; iteration++ {
				b.StopTimer()
				color := "#123456"
				if iteration%2 == 0 {
					color = "#654321"
				}
				if err := document.SetAttribute(target, "style", "height:4px;background-color:"+color+";color:"+color); err != nil {
					b.Fatal(err)
				}
				var records []dom.MutationRecord
				var currentStyles *style.Snapshot
				if err := document.WithReadView(func(view dom.ReadView) error {
					access, err := view.Acquire()
					if err != nil {
						return err
					}
					var latest uint64
					records, latest, err = access.MutationRecordsSince(sequence)
					access.Close()
					if err != nil {
						return err
					}
					sequence = latest
					var reused bool
					currentStyles, reused, err = render.RestyleStyleSnapshotFromReadView(
						view, viewport, render.Resources{}, styles, records,
					)
					if err == nil && !reused {
						err = fmt.Errorf("style snapshot was not incrementally updated")
					}
					return err
				}); err != nil {
					b.Fatal(err)
				}
				styles = currentStyles
				b.StartTimer()
				if benchmark.full {
					layout = benchmarkLayoutSnapshot(b, document, viewport, styles)
				} else {
					var updated *render.LayoutSnapshot
					var reused bool
					err := document.WithReadView(func(view dom.ReadView) error {
						var err error
						updated, reused, err = render.ReuseLayoutSnapshotFromReadView(view, viewport, layout, styles, records)
						return err
					})
					if err != nil || !reused {
						b.Fatalf("layout reuse = %t, %v", reused, err)
					}
					layout = updated
				}
				b.StopTimer()
				incrementalLayoutBenchmarkSnapshot = layout
				b.StartTimer()
			}
		})
	}
}

func benchmarkLayoutDocument(b *testing.B) (*dom.Document, dom.NodeID) {
	b.Helper()
	var source strings.Builder
	source.WriteString(`<html><body style="margin:0">`)
	for index := 0; index < 300; index++ {
		id := ""
		if index == 150 {
			id = ` id="target"`
		}
		fmt.Fprintf(&source, `<div%s style="height:4px">row %d</div>`, id, index)
	}
	source.WriteString(`</body></html>`)
	root, err := htmlparser.Parse(strings.NewReader(source.String()))
	if err != nil {
		b.Fatal(err)
	}
	document, err := dom.IndexDocument(root)
	if err != nil {
		b.Fatal(err)
	}
	target, ok := document.ElementByID("target")
	if !ok {
		b.Fatal("target element is unavailable")
	}
	return document, target
}

func benchmarkStyleSnapshot(b *testing.B, document *dom.Document, viewport render.Viewport) *style.Snapshot {
	b.Helper()
	snapshot, err := render.ComputeDocumentStyleSnapshot(document, viewport, render.Resources{})
	if err != nil {
		b.Fatal(err)
	}
	return snapshot
}

func benchmarkLayoutSnapshot(b *testing.B, document *dom.Document, viewport render.Viewport, styles *style.Snapshot) *render.LayoutSnapshot {
	b.Helper()
	var layout *render.LayoutSnapshot
	if err := document.WithReadView(func(view dom.ReadView) error {
		var err error
		layout, err = render.ComputeLayoutSnapshotFromReadView(view, viewport, render.Resources{}, styles)
		return err
	}); err != nil {
		b.Fatal(err)
	}
	return layout
}

func TestLayoutSnapshotRebindsNeutralMutationWithoutPaintDamage(t *testing.T) {
	document, target := indexedLayoutDocument(t, `<html><body><div id="target">content</div></body></html>`)
	viewport := render.Viewport{Width: 320, Height: 200}
	previousStyles := mustDocumentStyleSnapshot(t, document, viewport, render.Resources{})
	previousLayout := mustLayoutSnapshot(t, document, viewport, previousStyles)
	sequence := document.MutationSequence()
	if err := document.SetAttribute(target, "data-unrelated", "changed"); err != nil {
		t.Fatal(err)
	}
	currentStyles := mustDocumentStyleSnapshot(t, document, viewport, render.Resources{})
	reused, ok := reuseLayoutSnapshot(t, document, viewport, previousLayout, currentStyles, sequence)
	if !ok || !reused.ReusedLayout() || len(reused.DamageRects()) != 0 {
		t.Fatalf("neutral layout reuse = %t, reused=%t, damage=%#v", ok, reused != nil && reused.ReusedLayout(), reused.DamageRects())
	}
}

func TestLayoutSnapshotFallsBackForGeometryAndDirectDOMInputs(t *testing.T) {
	for _, test := range []struct {
		name   string
		source string
		mutate func(*testing.T, *dom.Document, dom.NodeID)
	}{
		{
			name:   "computed width",
			source: `<html><body><div id="target" style="width:10px">content</div></body></html>`,
			mutate: func(t *testing.T, document *dom.Document, target dom.NodeID) {
				t.Helper()
				if err := document.SetAttribute(target, "style", "width:20px"); err != nil {
					t.Fatal(err)
				}
			},
		},
		{
			name: "generated attr content",
			source: `<html><head><style>#target::before { content:attr(data-label) }</style></head>
				<body><div id="target" data-label="before"></div></body></html>`,
			mutate: func(t *testing.T, document *dom.Document, target dom.NodeID) {
				t.Helper()
				if err := document.SetAttribute(target, "data-label", "after"); err != nil {
					t.Fatal(err)
				}
			},
		},
		{
			name:   "list numbering attribute",
			source: `<html><body><ol id="target" start="1"><li>item</li></ol></body></html>`,
			mutate: func(t *testing.T, document *dom.Document, target dom.NodeID) {
				t.Helper()
				if err := document.SetAttribute(target, "start", "5"); err != nil {
					t.Fatal(err)
				}
			},
		},
		{
			name: "collapsed border color",
			source: `<html><body><table style="border-collapse:collapse"><tr>
				<td id="target" style="border:2px solid red">cell</td></tr></table></body></html>`,
			mutate: func(t *testing.T, document *dom.Document, target dom.NodeID) {
				t.Helper()
				if err := document.SetAttribute(target, "style", "border:2px solid blue"); err != nil {
					t.Fatal(err)
				}
			},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			document, target := indexedLayoutDocument(t, test.source)
			viewport := render.Viewport{Width: 320, Height: 200}
			previousStyles := mustDocumentStyleSnapshot(t, document, viewport, render.Resources{})
			previousLayout := mustLayoutSnapshot(t, document, viewport, previousStyles)
			sequence := document.MutationSequence()
			test.mutate(t, document, target)
			currentStyles := mustDocumentStyleSnapshot(t, document, viewport, render.Resources{})
			if reused, ok := reuseLayoutSnapshot(t, document, viewport, previousLayout, currentStyles, sequence); ok || reused != nil {
				t.Fatalf("geometry-affecting mutation reused layout: %#v", reused)
			}
		})
	}
}

func FuzzLayoutSnapshotReuseMatchesFull(fuzz *testing.F) {
	for operation := uint8(0); operation < 8; operation++ {
		fuzz.Add(operation, uint8(0x12), uint8(0x34), uint8(0x56))
	}
	fuzz.Fuzz(func(t *testing.T, operation, red, green, blue uint8) {
		document, target := indexedLayoutDocument(t, `
			<html><head><style>#target::before { content:attr(data-label) }</style></head>
			<body style="margin:0"><div id="target" style="width:120px;height:30px;border:2px solid red">text</div></body></html>
		`)
		viewport := render.Viewport{Width: 320, Height: 200}
		previousStyles := mustDocumentStyleSnapshot(t, document, viewport, render.Resources{})
		previousLayout := mustLayoutSnapshot(t, document, viewport, previousStyles)
		sequence := document.MutationSequence()
		value := fmt.Sprintf("#%02x%02x%02x", red, green, blue)
		switch operation % 8 {
		case 0:
			if err := document.SetAttribute(target, "style", "width:120px;height:30px;border:2px solid red;color:"+value); err != nil {
				t.Fatal(err)
			}
		case 1:
			if err := document.SetAttribute(target, "style", "width:121px;height:30px;border:2px solid red"); err != nil {
				t.Fatal(err)
			}
		case 2:
			if err := document.SetAttribute(target, "data-unrelated", value); err != nil {
				t.Fatal(err)
			}
		case 3:
			if err := document.SetAttribute(target, "data-label", value); err != nil {
				t.Fatal(err)
			}
		case 4:
			node, _ := document.Resolve(target)
			text, _ := document.ID(node.Children[0])
			if err := document.SetText(text, value); err != nil {
				t.Fatal(err)
			}
		case 5:
			if err := document.SetAttribute(target, "style", "width:120px;height:30px;border:2px solid "+value); err != nil {
				t.Fatal(err)
			}
		case 6:
			if err := document.SetAttribute(target, "style", "width:120px;height:30px;border:2px solid red;opacity:.5"); err != nil {
				t.Fatal(err)
			}
		case 7:
			if err := document.SetAttribute(target, "style", "width:120px;height:30px;border:2px solid red;text-decoration-line:underline"); err != nil {
				t.Fatal(err)
			}
		}
		currentStyles := mustDocumentStyleSnapshot(t, document, viewport, render.Resources{})
		reused, ok := reuseLayoutSnapshot(t, document, viewport, previousLayout, currentStyles, sequence)
		if !ok {
			return
		}
		incrementalFrame := mustFrameFromLayout(t, document, reused)
		fullLayout := mustLayoutSnapshot(t, document, viewport, currentStyles)
		fullFrame := mustFrameFromLayout(t, document, fullLayout)
		if !reflect.DeepEqual(incrementalFrame.DisplayList, fullFrame.DisplayList) {
			t.Fatalf("operation %d incremental display list differs from full", operation%8)
		}
		incrementalGeometry, incrementalOK := reused.GeometryID(target)
		fullGeometry, fullOK := fullLayout.GeometryID(target)
		if incrementalOK != fullOK || !reflect.DeepEqual(incrementalGeometry, fullGeometry) {
			t.Fatalf("operation %d incremental geometry differs from full", operation%8)
		}
	})
}

func mustLayoutSnapshot(t *testing.T, document *dom.Document, viewport render.Viewport, styles *style.Snapshot) *render.LayoutSnapshot {
	t.Helper()
	var layout *render.LayoutSnapshot
	if err := document.WithReadView(func(view dom.ReadView) error {
		var err error
		layout, err = render.ComputeLayoutSnapshotFromReadView(view, viewport, render.Resources{}, styles)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	return layout
}

func reuseLayoutSnapshot(
	t *testing.T,
	document *dom.Document,
	viewport render.Viewport,
	previous *render.LayoutSnapshot,
	styles *style.Snapshot,
	sequence uint64,
) (*render.LayoutSnapshot, bool) {
	t.Helper()
	var layout *render.LayoutSnapshot
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
		layout, reused, err = render.ReuseLayoutSnapshotFromReadView(view, viewport, previous, styles, records)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	return layout, reused
}

func mustFrameFromLayout(t *testing.T, document *dom.Document, layout *render.LayoutSnapshot) *render.Frame {
	t.Helper()
	var frame *render.Frame
	if err := document.WithReadView(func(view dom.ReadView) error {
		var err error
		frame, err = render.RenderReadViewWithLayoutSnapshot(view, layout)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	return frame
}
