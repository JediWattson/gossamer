package render

import (
	"fmt"
	"strings"
	"testing"

	"github.com/JediWattson/gossamer/internal/dom"
	computed "github.com/JediWattson/gossamer/internal/style"
)

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
			got := resolveGridAxis(test.start, test.end, test.explicit)
			if got.definite != test.wantDefinite || got.start != test.wantStart || got.span != test.wantSpan {
				t.Fatalf("resolveGridAxis() = %#v, want definite=%t start=%d span=%d", got, test.wantDefinite, test.wantStart, test.wantSpan)
			}
		})
	}
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
