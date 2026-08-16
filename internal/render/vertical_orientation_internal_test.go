package render

import (
	"math"
	"strings"
	"testing"

	"github.com/JediWattson/gossamer/internal/dom"
	htmlparser "github.com/JediWattson/gossamer/internal/html"
	computed "github.com/JediWattson/gossamer/internal/style"
)

func TestUnicodeVerticalOrientation17Classification(t *testing.T) {
	t.Parallel()
	if unicodeVerticalOrientationVersion != "17.0.0" ||
		unicodeVerticalOrientationSourceSHA256 != "dcef09c3fb24d356b042569c328ec341efc5b53447700d799f2fb4834c3cd3cd" ||
		unicodeGraphemeBreakSourceSHA256 != "d6b51d1d2ae5c33b451b7ed994b48f1f4dc62b2272a5831e7fd418514a6bae89" ||
		unicodeEmojiDataSourceSHA256 != "2cb2bb9455cda83e8481541ecf5b6dfda66a3bb89efa3fa7c5297eccf607b72b" ||
		unicodeDerivedCoreSourceSHA256 != "24c7fed1195c482faaefd5c1e7eb821c5ee1fb6de07ecdbaa64b56a99da22c08" {
		t.Fatalf("Unicode vertical-text provenance = %q/%q/%q/%q/%q", unicodeVerticalOrientationVersion, unicodeVerticalOrientationSourceSHA256, unicodeGraphemeBreakSourceSHA256, unicodeEmojiDataSourceSHA256, unicodeDerivedCoreSourceSHA256)
	}

	tests := []struct {
		name  string
		value rune
		want  unicodeVerticalOrientation
	}{
		{name: "Latin rotated", value: 'A', want: unicodeVerticalRotated},
		{name: "Han upright", value: '漢', want: unicodeVerticalUpright},
		{name: "copyright upright", value: '©', want: unicodeVerticalUpright},
		{name: "ideographic comma transformed upright", value: '、', want: unicodeVerticalTransformedUpright},
		{name: "corner quote transformed rotated", value: '「', want: unicodeVerticalTransformedRotated},
		{name: "supplementary Han upright", value: '\U00020000', want: unicodeVerticalUpright},
		{name: "private use upright", value: '\U000F0000', want: unicodeVerticalUpright},
		{name: "outside Unicode rotated", value: rune(0x110000), want: unicodeVerticalRotated},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := verticalOrientationForRune(test.value); got != test.want {
				t.Fatalf("verticalOrientationForRune(%U) = %d, want %d", test.value, got, test.want)
			}
		})
	}
	for index, item := range unicodeVerticalOrientationRanges {
		if item.first > item.last {
			t.Fatalf("range %d is reversed: %#v", index, item)
		}
		if index != 0 && unicodeVerticalOrientationRanges[index-1].last >= item.first {
			t.Fatalf("ranges %d and %d overlap or are unsorted", index-1, index)
		}
	}
	for index, item := range unicodeGraphemeBreakRanges {
		if item.first > item.last {
			t.Fatalf("grapheme range %d is reversed: %#v", index, item)
		}
		if index != 0 && unicodeGraphemeBreakRanges[index-1].last >= item.first {
			t.Fatalf("grapheme ranges %d and %d overlap or are unsorted", index-1, index)
		}
	}
}

func TestVerticalTextUnitsFollowUnicode17ExtendedGraphemeBoundaries(t *testing.T) {
	t.Parallel()

	source := "A\u0301👩🏽‍💻🇺🇸🇨🇦각A\u200d漢\u0600Bक्" + "क\n\u0301"
	want := []string{"A\u0301", "👩🏽‍💻", "🇺🇸", "🇨🇦", "각", "A\u200d", "漢", "\u0600B", "क्क", "\n", "\u0301"}
	got := splitVerticalTextUnits(source)
	if len(got) != len(want) {
		t.Fatalf("splitVerticalTextUnits(%q) = %q, want %q", source, got, want)
	}
	for index := range want {
		if got[index] != want[index] {
			t.Fatalf("unit %d = %q, want %q", index, got[index], want[index])
		}
	}
}

func TestVerticalTextRunsApplyMixedUprightAndSideways(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		mode computed.TextOrientation
		want []verticalTextRun
	}{
		{
			name: "mixed U Tu R Tr fallbacks", mode: computed.TextOrientationMixed,
			want: []verticalTextRun{
				{text: "A", orientation: textPaintSidewaysRight, units: 1},
				{text: "漢。", orientation: textPaintUpright, units: 2},
				{text: "「B", orientation: textPaintSidewaysRight, units: 2},
			},
		},
		{name: "upright", mode: computed.TextOrientationUpright, want: []verticalTextRun{{text: "A漢", orientation: textPaintUpright, units: 2}}},
		{name: "sideways", mode: computed.TextOrientationSideways, want: []verticalTextRun{{text: "A漢", orientation: textPaintSidewaysRight, units: 2}}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			source := "A漢。「B"
			if test.name != "mixed U Tu R Tr fallbacks" {
				source = "A漢"
			}
			got := verticalTextRuns(source, test.mode)
			if len(got) != len(test.want) {
				t.Fatalf("runs = %#v, want %#v", got, test.want)
			}
			for index := range test.want {
				if got[index] != test.want[index] {
					t.Fatalf("run %d = %#v, want %#v", index, got[index], test.want[index])
				}
			}
		})
	}
}

func TestVerticalTextOrientationCreatesPhysicalRunsAndHitGeometry(t *testing.T) {
	t.Parallel()

	document, err := htmlparser.Parse(strings.NewReader(`<!doctype html><html><body style="margin:0"><section style="writing-mode:vertical-rl;width:220px;height:260px;font-size:20px;line-height:24px"><div>A漢B</div><div style="text-orientation:upright">CD</div><div style="text-orientation:sideways">字E</div><div style="writing-mode:horizontal-tb;text-orientation:upright">FG</div></section></body></html>`))
	if err != nil {
		t.Fatal(err)
	}
	frame, err := Render(document, Viewport{Width: 320, Height: 320})
	if err != nil {
		t.Fatal(err)
	}
	commands := make(map[string]Command)
	for _, command := range frame.DisplayList.Commands {
		if command.Kind == DrawTextCommand {
			commands[command.Text] = command
		}
	}
	for text, want := range map[string]textPaintOrientation{
		"A":  textPaintSidewaysRight,
		"漢":  textPaintUpright,
		"B":  textPaintSidewaysRight,
		"CD": textPaintUpright,
		"字E": textPaintSidewaysRight,
		"FG": textPaintHorizontal,
	} {
		command, ok := commands[text]
		if !ok {
			t.Fatalf("missing DrawText command %q; commands=%v", text, mapsKeys(commands))
		}
		if command.textOrientation != want {
			t.Fatalf("command %q orientation = %d, want %d", text, command.textOrientation, want)
		}
	}
	if !(commands["A"].textBounds.Y < commands["漢"].textBounds.Y && commands["漢"].textBounds.Y < commands["B"].textBounds.Y) {
		t.Fatalf("mixed run order = A:%#v Han:%#v B:%#v", commands["A"].textBounds, commands["漢"].textBounds, commands["B"].textBounds)
	}
	if got := commands["漢"].textBounds.Height; math.Abs(got-20) > 0.01 {
		t.Fatalf("upright Han advance = %g, want one 20px em", got)
	}
	if got := commands["CD"].textBounds.Height; math.Abs(got-40) > 0.01 {
		t.Fatalf("upright Latin advance = %g, want two 20px ems", got)
	}
	upright := commands["CD"]
	if hit := HitTest(frame, upright.textBounds.X+upright.textBounds.Width/2, upright.textBounds.Y+upright.textBounds.Height/2); hit != upright.Node {
		t.Fatalf("upright run HitTest = %#v, want text node %#v", hit, upright.Node)
	}
}

func TestUprightTextForcesUsedDirectionLTRWithoutChangingComputedDirection(t *testing.T) {
	t.Parallel()

	document, err := htmlparser.Parse(strings.NewReader(`<!doctype html><html><body style="margin:0"><section style="writing-mode:vertical-rl;direction:rtl;text-orientation:upright;text-align:start;width:30px;height:200px;font-size:20px">A</section></body></html>`))
	if err != nil {
		t.Fatal(err)
	}
	frame, err := Render(document, Viewport{Width: 100, Height: 240})
	if err != nil {
		t.Fatal(err)
	}
	var text Command
	for _, command := range frame.DisplayList.Commands {
		if command.Kind == DrawTextCommand && command.Text == "A" {
			text = command
			break
		}
	}
	if text.textOrientation != textPaintUpright {
		t.Fatalf("upright text orientation = %d", text.textOrientation)
	}
	if text.textBounds.Y > 30 {
		t.Fatalf("upright rtl text-align:start y = %g, want top/start from forced ltr used direction", text.textBounds.Y)
	}
}

func TestVerticalTextOrientationFeedsIntrinsicAtomicInlineSizing(t *testing.T) {
	t.Parallel()

	document, err := htmlparser.Parse(strings.NewReader(`<!doctype html><html><body style="margin:0"><span id="upright" style="display:inline-block;writing-mode:vertical-rl;text-orientation:upright;font-size:20px;line-height:24px">AB</span><span id="sideways" style="display:inline-block;writing-mode:vertical-rl;text-orientation:sideways;font-size:20px;line-height:24px">AB</span></body></html>`))
	if err != nil {
		t.Fatal(err)
	}
	frame, err := Render(document, Viewport{Width: 300, Height: 200})
	if err != nil {
		t.Fatal(err)
	}
	upright := findInternalBoxForNode(frame.Root, findInternalElementByID(document, "upright"))
	sideways := findInternalBoxForNode(frame.Root, findInternalElementByID(document, "sideways"))
	if upright == nil || sideways == nil {
		t.Fatalf("atomic inline boxes = upright:%#v sideways:%#v", upright, sideways)
	}
	if math.Abs(upright.ContentBounds.Height-40) > 0.01 {
		t.Fatalf("upright intrinsic inline extent = %g, want two 20px ems", upright.ContentBounds.Height)
	}
	if math.Abs(upright.ContentBounds.Width-24) > 0.01 {
		t.Fatalf("upright intrinsic block extent = %g, want 24px line height", upright.ContentBounds.Width)
	}
	if sideways.ContentBounds.Height >= upright.ContentBounds.Height {
		t.Fatalf("sideways/upright intrinsic inline extents = %g/%g, want horizontal glyph advance below two upright ems", sideways.ContentBounds.Height, upright.ContentBounds.Height)
	}
}

func mapsKeys(values map[string]Command) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	return keys
}

func findInternalElementByID(root *dom.Node, id string) *dom.Node {
	if root == nil {
		return nil
	}
	for _, attribute := range root.Attributes {
		if attribute.Name == "id" && attribute.Value == id {
			return root
		}
	}
	for _, child := range root.Children {
		if found := findInternalElementByID(child, id); found != nil {
			return found
		}
	}
	return nil
}

func findInternalBoxForNode(root *Box, node *dom.Node) *Box {
	if root == nil || node == nil {
		return nil
	}
	if root.Node == node {
		return root
	}
	for _, child := range root.Children {
		if found := findInternalBoxForNode(child, node); found != nil {
			return found
		}
	}
	return nil
}
