package render_test

import (
	"fmt"
	"image"
	"math"
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
	previousFrame := mustFrameFromLayout(t, document, previousLayout)
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
	wantDamage := []render.Rect{{X: 0, Y: 0, Width: 120, Height: 30}}
	if !reflect.DeepEqual(reused.DamageRects(), wantDamage) {
		t.Fatalf("paint-only damage rects = %#v, want %#v", reused.DamageRects(), wantDamage)
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
	assertDamageRectsCoverChangedPixels(t, previousFrame, incrementalFrame, reused.DamageRects(), 2)
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

func TestReuseFrameWithLayoutSnapshotPreservesDisplayListAndArtifactForNeutralMutations(t *testing.T) {
	document, target := indexedLayoutDocument(t, `
		<html><body style="margin:0">
		<div id="target" style="width:120px;height:30px;background:#112233;color:#ddeeff">content</div>
		</body></html>
	`)
	viewport := render.Viewport{Width: 320, Height: 200}
	previousStyles := mustDocumentStyleSnapshot(t, document, viewport, render.Resources{})
	previousLayout := mustLayoutSnapshot(t, document, viewport, previousStyles)
	previousFrame := mustFrameFromLayout(t, document, previousLayout)

	sequence := document.MutationSequence()
	if err := document.SetAttribute(target, "data-unrelated", "changed"); err != nil {
		t.Fatal(err)
	}
	neutralStyles := mustDocumentStyleSnapshot(t, document, viewport, render.Resources{})
	neutralLayout, ok := reuseLayoutSnapshot(t, document, viewport, previousLayout, neutralStyles, sequence)
	if !ok {
		t.Fatal("neutral mutation did not reuse layout")
	}
	var reusedFrame *render.Frame
	var reusedPaint bool
	if err := document.WithReadView(func(view dom.ReadView) error {
		var err error
		reusedFrame, reusedPaint, err = render.ReuseFrameReadViewWithLayoutSnapshot(view, previousFrame, neutralLayout)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if !reusedPaint || reusedFrame == nil {
		t.Fatalf("ReuseFrameReadViewWithLayoutSnapshot() = (%p, %t), want reused frame", reusedFrame, reusedPaint)
	}
	if reusedFrame == previousFrame {
		t.Fatal("ReuseFrameReadViewWithLayoutSnapshot returned the previous frame header pointer")
	}
	if reusedFrame.PaintArtifactID() != previousFrame.PaintArtifactID() {
		t.Fatalf("PaintArtifactID = %d, want %d", reusedFrame.PaintArtifactID(), previousFrame.PaintArtifactID())
	}
	if reusedFrame.ComputedStyles != neutralStyles || reusedFrame.Layout != neutralLayout {
		t.Fatal("reused frame did not rebind to the current style/layout snapshots")
	}
	if len(reusedFrame.DisplayList.Commands) == 0 || &reusedFrame.DisplayList.Commands[0] != &previousFrame.DisplayList.Commands[0] {
		t.Fatal("reused frame did not share the immutable display-list backing array")
	}
	pointerStyles, err := render.ComputeStyleSnapshot(document.Root(), viewport, render.Resources{})
	if err != nil {
		t.Fatal(err)
	}
	pointerLayout, err := render.ComputeLayoutSnapshotWithStyleSnapshot(document.Root(), viewport, render.Resources{}, pointerStyles)
	if err != nil {
		t.Fatal(err)
	}
	pointerFrame, err := render.RenderWithLayoutSnapshot(document.Root(), pointerLayout)
	if err != nil {
		t.Fatal(err)
	}
	directReused, directOK, err := render.ReuseFrameWithLayoutSnapshot(document.Root(), pointerFrame, pointerLayout)
	if err != nil || !directOK || directReused != pointerFrame {
		t.Fatalf("ReuseFrameWithLayoutSnapshot() = (%#v, %t, %v)", directReused, directOK, err)
	}

	sequence = document.MutationSequence()
	if err := document.SetAttribute(target, "style", "width:120px;height:30px;background:#334455;color:#ddeeff"); err != nil {
		t.Fatal(err)
	}
	paintStyles := mustDocumentStyleSnapshot(t, document, viewport, render.Resources{})
	paintLayout, ok := reuseLayoutSnapshot(t, document, viewport, neutralLayout, paintStyles, sequence)
	if !ok {
		t.Fatal("paint-only mutation did not reuse layout")
	}
	if err := document.WithReadView(func(view dom.ReadView) error {
		_, reusedPaint, err := render.ReuseFrameReadViewWithLayoutSnapshot(view, reusedFrame, paintLayout)
		if err != nil {
			return err
		}
		if reusedPaint {
			t.Fatal("ReuseFrameReadViewWithLayoutSnapshot reused frame across paint-damaging mutation")
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}

func TestLayoutSnapshotDamageBoundsCoverTextInlineShadowAndTransformChanges(t *testing.T) {
	viewport := render.Viewport{Width: 320, Height: 220}

	t.Run("inline span color and underline stay bounded to block line", func(t *testing.T) {
		document, _ := indexedLayoutDocument(t, `
			<html><body style="margin:0">
			<div id="block" style="margin-top:20px;width:140px;height:30px">hello <span id="target" style="color:red">world</span></div>
			</body></html>
		`)
		spanID, ok := document.ElementByID("target")
		if !ok {
			t.Fatal("span not indexed")
		}
		previousStyles := mustDocumentStyleSnapshot(t, document, viewport, render.Resources{})
		previousLayout := mustLayoutSnapshot(t, document, viewport, previousStyles)
		previousFrame := mustFrameFromLayout(t, document, previousLayout)
		sequence := document.MutationSequence()
		if err := document.SetAttribute(spanID, "style", "color:blue;text-decoration-line:underline"); err != nil {
			t.Fatal(err)
		}
		currentStyles := mustDocumentStyleSnapshot(t, document, viewport, render.Resources{})
		reused, ok := reuseLayoutSnapshot(t, document, viewport, previousLayout, currentStyles, sequence)
		if !ok {
			t.Fatal("inline span paint change did not reuse layout")
		}
		damage := reused.DamageRects()
		if len(damage) != 1 || damage[0].Width >= float64(viewport.Width) || damage[0].Height >= float64(viewport.Height) {
			t.Fatalf("inline span damage fell back to viewport: %#v", damage)
		}
		incrementalFrame := mustFrameFromLayout(t, document, reused)
		assertDamageRectsCoverChangedPixels(t, previousFrame, incrementalFrame, damage, 2)
	})

	t.Run("outer box shadow and 2D transform expand conservative damage bounds", func(t *testing.T) {
		document, target := indexedLayoutDocument(t, `
			<html><body style="margin:0">
			<div id="target" style="margin:30px;width:80px;height:40px;background:#cc2233;box-shadow:10px 8px 6px 4px rgba(0,0,0,0.45);transform:translate(18px, 12px)">box</div>
			</body></html>
		`)
		previousStyles := mustDocumentStyleSnapshot(t, document, viewport, render.Resources{})
		previousLayout := mustLayoutSnapshot(t, document, viewport, previousStyles)
		previousFrame := mustFrameFromLayout(t, document, previousLayout)
		sequence := document.MutationSequence()
		if err := document.SetAttribute(target, "style", "margin:30px;width:80px;height:40px;background:#2266cc;box-shadow:-12px -6px 8px 3px rgba(0,0,0,0.55);transform:translate(-10px, 24px)"); err != nil {
			t.Fatal(err)
		}
		currentStyles := mustDocumentStyleSnapshot(t, document, viewport, render.Resources{})
		reused, ok := reuseLayoutSnapshot(t, document, viewport, previousLayout, currentStyles, sequence)
		if !ok {
			t.Fatal("box-shadow/transform change did not reuse layout")
		}
		damage := reused.DamageRects()
		if len(damage) != 1 || damage[0].Width >= float64(viewport.Width) || damage[0].Height >= float64(viewport.Height) {
			t.Fatalf("shadow/transform damage = %#v, want bounded union", damage)
		}
		incrementalFrame := mustFrameFromLayout(t, document, reused)
		assertDamageRectsCoverChangedPixels(t, previousFrame, incrementalFrame, damage, 2)
	})

	t.Run("disjoint multi-element paint mutations keep separate damage rects", func(t *testing.T) {
		document, first := indexedLayoutDocument(t, `
			<html><head><style>
			#target { width:60px; height:24px; background:#aa2255 }
			#second { width:70px; height:26px; background:#22aa55 }
			#wrap.flipped #target { background:#2255aa }
			#wrap.flipped #second { background:#aa5522 }
			</style></head>
			<body style="margin:0"><div id="wrap">
			<div id="target">first</div>
			<div style="height:90px"></div>
			<div id="second">second</div>
			</div></body></html>
		`)
		_ = first
		wrapID, ok := document.ElementByID("wrap")
		if !ok {
			t.Fatal("wrap not indexed")
		}
		previousStyles := mustDocumentStyleSnapshot(t, document, viewport, render.Resources{})
		previousLayout := mustLayoutSnapshot(t, document, viewport, previousStyles)
		previousFrame := mustFrameFromLayout(t, document, previousLayout)
		sequence := document.MutationSequence()
		if err := document.SetAttribute(wrapID, "class", "flipped"); err != nil {
			t.Fatal(err)
		}
		currentStyles := mustDocumentStyleSnapshot(t, document, viewport, render.Resources{})
		reused, ok := reuseLayoutSnapshot(t, document, viewport, previousLayout, currentStyles, sequence)
		if !ok {
			t.Fatal("multi-element paint change did not reuse layout")
		}
		wantDamage := []render.Rect{
			{X: 0, Y: 0, Width: 60, Height: 24},
			{X: 0, Y: 114, Width: 70, Height: 26},
		}
		if !reflect.DeepEqual(reused.DamageRects(), wantDamage) {
			t.Fatalf("disjoint damage rects = %#v, want %#v", reused.DamageRects(), wantDamage)
		}
		incrementalFrame := mustFrameFromLayout(t, document, reused)
		assertDamageRectsCoverChangedPixels(t, previousFrame, incrementalFrame, reused.DamageRects(), 2)
	})

	t.Run("display none subtree paint changes emit zero damage and reuse frame", func(t *testing.T) {
		document, target := indexedLayoutDocument(t, `
			<html><body style="margin:0">
			<div style="display:none"><div id="target" style="color:red;background:red">hidden</div></div>
			<div style="width:80px;height:24px;background:#224466">visible</div>
			</body></html>
		`)
		previousStyles := mustDocumentStyleSnapshot(t, document, viewport, render.Resources{})
		previousLayout := mustLayoutSnapshot(t, document, viewport, previousStyles)
		previousFrame := mustFrameFromLayout(t, document, previousLayout)
		sequence := document.MutationSequence()
		if err := document.SetAttribute(target, "style", "color:blue;background:green"); err != nil {
			t.Fatal(err)
		}
		currentStyles := mustDocumentStyleSnapshot(t, document, viewport, render.Resources{})
		reused, ok := reuseLayoutSnapshot(t, document, viewport, previousLayout, currentStyles, sequence)
		if !ok {
			t.Fatal("hidden subtree paint change did not reuse layout")
		}
		if len(reused.DamageRects()) != 0 {
			t.Fatalf("hidden subtree damage = %#v, want empty", reused.DamageRects())
		}
		var reusedFrame *render.Frame
		var reusedPaint bool
		if err := document.WithReadView(func(view dom.ReadView) error {
			var err error
			reusedFrame, reusedPaint, err = render.ReuseFrameReadViewWithLayoutSnapshot(view, previousFrame, reused)
			return err
		}); err != nil {
			t.Fatal(err)
		}
		if !reusedPaint || reusedFrame.PaintArtifactID() != previousFrame.PaintArtifactID() {
			t.Fatalf("ReuseFrameReadViewWithLayoutSnapshot on hidden paint mutation = (%#v, %t)", reusedFrame, reusedPaint)
		}
	})
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
		{
			name: "table row background color in separated borders",
			source: `<html><body><table style="border-spacing:6px"><tr id="target">
				<td style="width:40px;height:20px">cell</td></tr></table></body></html>`,
			mutate: func(t *testing.T, document *dom.Document, target dom.NodeID) {
				t.Helper()
				if err := document.SetAttribute(target, "style", "background-color:#336699"); err != nil {
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
		previousFrame := mustFrameFromLayout(t, document, previousLayout)
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
		var incrementalFrame *render.Frame
		if err := document.WithReadView(func(view dom.ReadView) error {
			var reusedPaint bool
			var err error
			incrementalFrame, reusedPaint, err = render.ReuseFrameReadViewWithLayoutSnapshot(view, previousFrame, reused)
			if err != nil {
				return err
			}
			if !reusedPaint {
				incrementalFrame, err = render.RenderReadViewWithLayoutSnapshot(view, reused)
			}
			return err
		}); err != nil {
			t.Fatal(err)
		}
		fullLayout := mustLayoutSnapshot(t, document, viewport, currentStyles)
		fullFrame := mustFrameFromLayout(t, document, fullLayout)
		if !reflect.DeepEqual(incrementalFrame.DisplayList, fullFrame.DisplayList) {
			t.Fatalf("operation %d incremental display list differs from full", operation%8)
		}
		assertDamageRectsCoverChangedPixels(t, previousFrame, incrementalFrame, reused.DamageRects(), 2)
		incrementalGeometry, incrementalOK := reused.GeometryID(target)
		fullGeometry, fullOK := fullLayout.GeometryID(target)
		if incrementalOK != fullOK || !reflect.DeepEqual(incrementalGeometry, fullGeometry) {
			t.Fatalf("operation %d incremental geometry differs from full", operation%8)
		}
	})
}

func assertDamageRectsCoverChangedPixels(
	t *testing.T,
	previousFrame *render.Frame,
	currentFrame *render.Frame,
	damageRects []render.Rect,
	scale float64,
) {
	t.Helper()
	if scale < 1 {
		scale = 1
	}
	previousCanvas, err := render.RasterizeAtScale(previousFrame, scale)
	if err != nil {
		t.Fatal(err)
	}
	currentCanvas, err := render.RasterizeAtScale(currentFrame, scale)
	if err != nil {
		t.Fatal(err)
	}
	if previousCanvas.Bounds() != currentCanvas.Bounds() {
		t.Fatalf("canvas bounds mismatch: %v vs %v", previousCanvas.Bounds(), currentCanvas.Bounds())
	}
	scaledDamage := make([]image.Rectangle, 0, len(damageRects))
	for _, rect := range damageRects {
		if rect.Width <= 0 || rect.Height <= 0 {
			continue
		}
		scaledDamage = append(scaledDamage, image.Rect(
			int(math.Floor(rect.X*scale)),
			int(math.Floor(rect.Y*scale)),
			int(math.Ceil((rect.X+rect.Width)*scale)),
			int(math.Ceil((rect.Y+rect.Height)*scale)),
		))
	}
	bounds := currentCanvas.Bounds()
	for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
		rowOffset := (y - bounds.Min.Y) * currentCanvas.Stride
		for x := bounds.Min.X; x < bounds.Max.X; x++ {
			idx := rowOffset + (x-bounds.Min.X)*4
			if previousCanvas.Pix[idx] == currentCanvas.Pix[idx] &&
				previousCanvas.Pix[idx+1] == currentCanvas.Pix[idx+1] &&
				previousCanvas.Pix[idx+2] == currentCanvas.Pix[idx+2] &&
				previousCanvas.Pix[idx+3] == currentCanvas.Pix[idx+3] {
				continue
			}
			covered := false
			point := image.Pt(x, y)
			for _, rect := range scaledDamage {
				if point.In(rect) {
					covered = true
					break
				}
			}
			if !covered {
				t.Fatalf("changed device pixel (%d,%d) prev=%v curr=%v at scale %.1f lies outside damage rects %#v (scaled %#v)",
					x, y, previousCanvas.RGBAAt(x, y), currentCanvas.RGBAAt(x, y), scale, damageRects, scaledDamage)
			}
		}
	}
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
