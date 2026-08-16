package render

import (
	"fmt"
	"strings"
	"testing"

	"github.com/JediWattson/gossamer/internal/dom"
	htmlparser "github.com/JediWattson/gossamer/internal/html"
	computed "github.com/JediWattson/gossamer/internal/style"
)

func TestHorizontalGridTextInsideVerticalGridReturnsToHorizontalPaint(t *testing.T) {
	t.Parallel()

	document, err := htmlparser.Parse(strings.NewReader(`<!doctype html><html><body style="margin:0"><section style="display:grid;writing-mode:vertical-rl;width:200px;height:200px;grid-template-columns:200px;grid-template-rows:200px"><div style="display:grid;writing-mode:horizontal-tb;width:160px;height:100px;grid-template-columns:60px 100px;grid-template-rows:50px"><i>A</i><i>B</i></div></section></body></html>`))
	if err != nil {
		t.Fatal(err)
	}
	frame, err := Render(document, Viewport{Width: 300, Height: 300})
	if err != nil {
		t.Fatal(err)
	}
	seen := 0
	for _, command := range frame.DisplayList.Commands {
		if command.Kind != DrawTextCommand || command.Text != "A" && command.Text != "B" {
			continue
		}
		seen++
		if command.textOrientation != textPaintHorizontal {
			t.Fatalf("horizontal descendant text %q orientation = %d", command.Text, command.textOrientation)
		}
	}
	if seen != 2 {
		t.Fatalf("horizontal descendant text commands = %d, want 2", seen)
	}
}

func TestResolveGridAxisHandlesLinesSpansAndReversedAreas(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		start, end   computed.GridLine
		explicit     int
		wantDefinite bool
		wantStart    int
		wantSpan     int
	}{
		{name: "auto", wantSpan: 1},
		{name: "positive line and span", start: gridLineForTest(computed.GridLineNumber, 2), end: gridLineForTest(computed.GridLineSpan, 3), explicit: 4, wantDefinite: true, wantStart: 1, wantSpan: 3},
		{name: "negative explicit lines", start: gridLineForTest(computed.GridLineNumber, -3), end: gridLineForTest(computed.GridLineNumber, -1), explicit: 4, wantDefinite: true, wantStart: 2, wantSpan: 2},
		{name: "reversed lines", start: gridLineForTest(computed.GridLineNumber, 4), end: gridLineForTest(computed.GridLineNumber, 2), explicit: 4, wantDefinite: true, wantStart: 1, wantSpan: 2},
		{name: "end anchored span", start: gridLineForTest(computed.GridLineSpan, 2), end: gridLineForTest(computed.GridLineNumber, 5), explicit: 4, wantDefinite: true, wantStart: 2, wantSpan: 2},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := resolveGridAxis(test.start, test.end, gridAxisDefinitionFromTemplate(computedGridTrackListForTest(test.explicit)))
			if got.definite != test.wantDefinite || got.start != test.wantStart || got.span != test.wantSpan {
				t.Fatalf("resolveGridAxis() = %#v, want definite=%t start=%d span=%d", got, test.wantDefinite, test.wantStart, test.wantSpan)
			}
		})
	}
}

func TestResolveGridAxisHandlesNamedOccurrencesAreasAndSpans(t *testing.T) {
	t.Parallel()

	template := computedGridTrackListFromCSS("[outer area-start x] 10px [x] 10px [x area-end] 10px [outer]")
	definition := gridAxisDefinitionFromTemplate(template)
	tests := []struct {
		name       string
		start, end string
		wantStart  int
		wantSpan   int
	}{
		{name: "first named line", start: "x", wantStart: 0, wantSpan: 1},
		{name: "second named line", start: "2 x", wantStart: 1, wantSpan: 1},
		{name: "negative named occurrence", start: "-1 x", wantStart: 2, wantSpan: 1},
		{name: "missing positive continues implicit", start: "2 missing", wantStart: 5, wantSpan: 1},
		{name: "missing negative continues implicit", start: "-2 missing", wantStart: -2, wantSpan: 1},
		{name: "name-only prefers area start edge", start: "area", wantStart: 0, wantSpan: 1},
		{name: "name-only prefers area end edge", start: "1", end: "area", wantStart: 0, wantSpan: 2},
		{name: "forward named span", start: "x", end: "span 2 x", wantStart: 0, wantSpan: 2},
		{name: "backward named span", start: "span 2 x", end: "3 x", wantStart: 0, wantSpan: 2},
		{name: "case-sensitive missing name", start: "X", wantStart: 4, wantSpan: 1},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			start := computedGridLineFromCSS(test.start)
			end := computed.GridLine{}
			if test.end != "" {
				end = computedGridLineFromCSS(test.end)
			}
			got := resolveGridAxis(start, end, definition)
			if !got.definite || got.start != test.wantStart || got.span != test.wantSpan {
				t.Fatalf("resolveGridAxis(%q,%q) = %#v, want start=%d span=%d", test.start, test.end, got, test.wantStart, test.wantSpan)
			}
		})
	}

	autoSpan := resolveGridAxis(computed.GridLine{}, computedGridLineFromCSS("span 2 x"), definition)
	model := gridLayoutModel{columnAxis: definition}
	_, gotSpan := model.itemSpansAt(gridLayoutItem{column: autoSpan, row: gridAxisPlacement{span: 1}}, 0, 0)
	if gotSpan != 1 {
		t.Fatalf("unanchored named span = %d, want conflict-resolved span 1", gotSpan)
	}
	twoSpans := resolveGridAxis(computedGridLineFromCSS("span 3"), computedGridLineFromCSS("span 2"), definition)
	if twoSpans.definite || twoSpans.span != 3 {
		t.Fatalf("two spans = %#v, want end span discarded and start span 3", twoSpans)
	}
	twoNamedSpans := resolveGridAxis(computedGridLineFromCSS("span 3 x"), computedGridLineFromCSS("span 2 x"), definition)
	if twoNamedSpans.definite || twoNamedSpans.span != 1 {
		t.Fatalf("two named spans = %#v, want end discarded then named-only span 1", twoNamedSpans)
	}
}

func computedGridTrackListForTest(count int) computed.GridTrackList {
	if count == 0 {
		return computed.GridTrackList{}
	}
	return computedGridTrackListFromCSS(fmt.Sprintf("repeat(%d,1px)", count))
}

func computedGridTrackListFromCSS(source string) computed.GridTrackList {
	document := dom.NewDocument()
	html := dom.NewElement("html")
	body := dom.NewElement("body")
	target := dom.NewElement("div", dom.Attribute{Name: "style", Value: "grid-template-columns:" + source})
	body.AppendChild(target)
	html.AppendChild(body)
	document.AppendChild(html)
	snapshot := computed.Compute(document, computed.Input{Environment: computed.Environment{Width: 800, Height: 600, InitialFontSize: 16}})
	style, _ := snapshot.Lookup(target)
	return style.GridTemplateColumns()
}

func TestGridPlacementBudgetsFailClosed(t *testing.T) {
	t.Parallel()

	model := gridLayoutModel{columns: maxGridTracksPerAxis + 1, rows: 1}
	if err := model.checkBounds(); err == nil || !strings.Contains(err.Error(), "tracks per axis") {
		t.Fatalf("track bound error = %v", err)
	}
	model = gridLayoutModel{columns: 1001, rows: 1000}
	if err := model.checkBounds(); err == nil || !strings.Contains(err.Error(), "occupied cells") {
		t.Fatalf("cell bound error = %v", err)
	}
	model = gridLayoutModel{occupied: make(map[gridCell]struct{}), placementOps: maxGridPlacementOps}
	if _, err := model.areaFree(0, 0, 1, 1); err == nil || !strings.Contains(err.Error(), "placement operations") {
		t.Fatalf("placement operation error = %v", err)
	}
	if err := model.occupy(0, 0, 1024, 1024); err == nil || !strings.Contains(err.Error(), "occupied cells") {
		t.Fatalf("item cell bound error = %v", err)
	}
}

func TestGridAutoRepeatCountUsesOnePixelFloorAndTrackBudget(t *testing.T) {
	t.Parallel()

	template := computedGridTrackListFromCSS("repeat(auto-fill,0px)")
	available := 1_000_000_000.0
	expanded := resolveGridAutoRepeat(template, &available, false, 0, Viewport{Width: 800, Height: 600})
	if expanded.Len() != maxGridTracksPerAxis {
		t.Fatalf("zero-sized auto-repeat expanded to %d tracks, want bounded %d", expanded.Len(), maxGridTracksPerAxis)
	}
	if start, end, ok := expanded.AutoRepeatRange(); !ok || start != 0 || end != maxGridTracksPerAxis {
		t.Fatalf("bounded auto-repeat range = %d..%d, %t", start, end, ok)
	}
}

func TestGridAutoRepeatCountUsesFixedBreadthsPercentagesAndGaps(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		template  string
		available *float64
		gap       float64
		minimum   bool
		want      int
	}{
		{name: "single fixed", template: "repeat(auto-fill,100px)", available: float64Pointer(320), gap: 10, want: 3},
		{name: "pattern plus surrounding tracks", template: "20px repeat(auto-fill,50px 30px) 30px", available: float64Pointer(300), gap: 10, want: 6},
		{name: "percentage", template: "repeat(auto-fill,20%)", available: float64Pointer(500), want: 5},
		{name: "max floored by min", template: "repeat(auto-fill,minmax(100px,20%))", available: float64Pointer(400), want: 4},
		{name: "minimum chooses enough repetitions", template: "repeat(auto-fill,100px)", available: float64Pointer(320), gap: 10, minimum: true, want: 3},
		{name: "indefinite repeats once", template: "repeat(auto-fill,100px)", want: 1},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			template := computedGridTrackListFromCSS(test.template)
			expanded := resolveGridAutoRepeat(template, test.available, test.minimum, test.gap, Viewport{Width: 800, Height: 600})
			if expanded.Len() != test.want {
				t.Fatalf("resolved %q to %d tracks, want %d", test.template, expanded.Len(), test.want)
			}
		})
	}
}

func float64Pointer(value float64) *float64 { return &value }

func gridLineForTest(kind computed.GridLineKind, number int) computed.GridLine {
	switch kind {
	case computed.GridLineNumber:
		return computedGridLineFromCSS(fmt.Sprint(number))
	case computed.GridLineSpan:
		return computedGridLineFromCSS("span " + fmt.Sprint(number))
	default:
		return computed.GridLine{}
	}
}

func computedGridLineFromCSS(source string) computed.GridLine {
	// Use the production computed-value pipeline rather than relying on private
	// fields of style.GridLine from the renderer package.
	document := dom.NewDocument()
	html := dom.NewElement("html")
	body := dom.NewElement("body")
	target := dom.NewElement("div", dom.Attribute{Name: "style", Value: "grid-column-start:" + source})
	body.AppendChild(target)
	html.AppendChild(body)
	document.AppendChild(html)
	snapshot := computed.Compute(document, computed.Input{Environment: computed.Environment{Width: 800, Height: 600, InitialFontSize: 16}})
	style, _ := snapshot.Lookup(target)
	return style.GridColumnStart()
}
