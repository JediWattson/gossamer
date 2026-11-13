package render

import (
	"image"
	"image/color"
	"testing"

	"github.com/JediWattson/gossamer/internal/dom"
)

func TestPaintArtifactIdentityLifecycle(t *testing.T) {
	t.Parallel()

	frame, err := Render(radiusTestDocument("width:40px;height:30px;background:red"), Viewport{Width: 80, Height: 60})
	if err != nil {
		t.Fatal(err)
	}
	if frame.PaintArtifactID() == 0 {
		t.Fatal("Render() returned an unstamped paint artifact")
	}
	if got := ScrollDisplayList(frame, 0, 8).PaintArtifactID(); got != frame.PaintArtifactID() {
		t.Fatalf("ScrollDisplayList artifact = %d, want %d", got, frame.PaintArtifactID())
	}
	node := dom.NewElement("div")
	if got := TransformDisplayList(frame, map[*dom.Node]VisualTransform{node: {OffsetY: 8}}).PaintArtifactID(); got != frame.PaintArtifactID() {
		t.Fatalf("TransformDisplayList artifact = %d, want %d", got, frame.PaintArtifactID())
	}
	child, err := Render(radiusTestDocument("width:10px;height:10px;background:blue"), Viewport{Width: 20, Height: 20})
	if err != nil {
		t.Fatal(err)
	}
	embedded := EmbedFrame(frame, child, Rect{X: 4, Y: 5, Width: 20, Height: 20})
	if embedded.PaintArtifactID() == 0 || embedded.PaintArtifactID() == frame.PaintArtifactID() {
		t.Fatalf("EmbedFrame artifact = %d, parent = %d", embedded.PaintArtifactID(), frame.PaintArtifactID())
	}
}

func TestFrameRasterizerIncrementalRootScrollParity(t *testing.T) {
	t.Parallel()
	base, root, fixed := rootDamageFrame()
	oldFrame := TransformDisplayList(base, map[*dom.Node]VisualTransform{root: {}})
	newFrame := TransformDisplayList(base, map[*dom.Node]VisualTransform{root: {OffsetY: 8}, fixed: {}})
	for _, scale := range []float64{1, 1.5, 2} {
		assertRasterUpdateParity(t, oldFrame, newFrame, scale, true, "")
	}
}

func TestFrameRasterizerKeepsStationaryOpaqueFillInsideViewportClip(t *testing.T) {
	t.Parallel()
	content := dom.NewElement("section")
	viewport := Viewport{Width: 120, Height: 90}
	base := &Frame{
		Viewport: viewport, paintArtifact: newPaintArtifactID(),
		DisplayList: DisplayList{Viewport: viewport, Commands: []Command{
			{Kind: BeginClipCommand, Rect: Rect{Width: 120, Height: 90}},
			{Kind: FillRectCommand, Rect: Rect{Width: 120, Height: 90}, Color: color.NRGBA{R: 0x16, G: 0x20, B: 0x31, A: 0xff}},
			{Kind: FillRectCommand, Node: content, Rect: Rect{X: 8, Y: 6, Width: 96, Height: 130}, Color: color.NRGBA{R: 0x38, G: 0x62, B: 0x91, A: 0xff}},
			{Kind: DrawTextCommand, Node: content, Text: "clipped scrolling content", X: 14, BaselineY: 30, FontSize: 14, FontFamily: FontFamilySansSerif, Color: opaqueWhite, textBounds: Rect{X: 14, Y: 15, Width: 90, Height: 18}},
			{Kind: EndClipCommand},
		}},
	}
	oldFrame := TransformDisplayList(base, map[*dom.Node]VisualTransform{content: {}})
	newFrame := TransformDisplayList(base, map[*dom.Node]VisualTransform{content: {OffsetY: 8}})
	for _, scale := range []float64{1, 1.5, 2} {
		assertRasterUpdateParity(t, oldFrame, newFrame, scale, true, "")
	}
}

func TestFrameRasterizerKeepsStationaryUniformAlphaFill(t *testing.T) {
	t.Parallel()
	content := dom.NewElement("section")
	viewport := Viewport{Width: 120, Height: 90}
	base := &Frame{
		Viewport: viewport, paintArtifact: newPaintArtifactID(),
		DisplayList: DisplayList{Viewport: viewport, Commands: []Command{
			{Kind: FillRectCommand, Rect: Rect{Width: 120, Height: 90}, Color: color.NRGBA{R: 0x0d, G: 0x16, B: 0x26, A: 0xff}},
			{Kind: FillRectCommand, Node: content, Rect: Rect{X: 6, Y: 4, Width: 108, Height: 130}, Color: color.NRGBA{R: 0x35, G: 0x78, B: 0xa6, A: 0xff}},
			{Kind: FillRectCommand, Rect: Rect{Width: 120, Height: 90}, Color: color.NRGBA{R: 0xb4, G: 0x48, B: 0x72, A: 0x70}},
			{Kind: DrawTextCommand, Node: content, Text: "moving above alpha fill", X: 14, BaselineY: 34, FontSize: 13, FontFamily: FontFamilySansSerif, Color: opaqueWhite, textBounds: Rect{X: 14, Y: 19, Width: 91, Height: 17}},
		}},
	}
	oldFrame := TransformDisplayList(base, map[*dom.Node]VisualTransform{content: {}})
	newFrame := TransformDisplayList(base, map[*dom.Node]VisualTransform{content: {OffsetY: 8}})
	for _, scale := range []float64{1, 1.5, 2} {
		assertRasterUpdateParity(t, oldFrame, newFrame, scale, true, "")
	}
}

func TestFrameRasterizerRepairsOnlyStationaryOpaqueRoundedFillBoundary(t *testing.T) {
	t.Parallel()
	content := dom.NewElement("section")
	viewport := Viewport{Width: 120, Height: 90}
	radii := CornerRadii{
		TopLeft: CornerRadius{X: 13, Y: 11}, TopRight: CornerRadius{X: 9, Y: 15},
		BottomRight: CornerRadius{X: 15, Y: 10}, BottomLeft: CornerRadius{X: 11, Y: 14},
	}
	base := &Frame{
		Viewport: viewport, paintArtifact: newPaintArtifactID(),
		DisplayList: DisplayList{Viewport: viewport, Commands: []Command{
			{Kind: FillRectCommand, Rect: Rect{Width: 120, Height: 90}, Color: color.NRGBA{R: 0x11, G: 0x18, B: 0x28, A: 0xff}},
			{Kind: BeginClipCommand, Rect: Rect{Width: 120, Height: 90}},
			{Kind: FillRectCommand, Rect: Rect{X: 8, Y: 7, Width: 104, Height: 76}, Radii: radii, Color: color.NRGBA{R: 0x2b, G: 0x38, B: 0x50, A: 0xff}},
			{Kind: FillRectCommand, Node: content, Rect: Rect{X: 14, Y: 16, Width: 90, Height: 115}, Color: color.NRGBA{R: 0x48, G: 0x72, B: 0x9c, A: 0xd8}},
			{Kind: DrawTextCommand, Node: content, Text: "content over rounded panel", X: 18, BaselineY: 42, FontSize: 13, FontFamily: FontFamilySansSerif, Color: opaqueWhite, textBounds: Rect{X: 18, Y: 27, Width: 86, Height: 17}},
			{Kind: EndClipCommand},
		}},
	}
	oldFrame := TransformDisplayList(base, map[*dom.Node]VisualTransform{content: {}})
	newFrame := TransformDisplayList(base, map[*dom.Node]VisualTransform{content: {OffsetY: 8}})
	for _, scale := range []float64{1, 1.5, 2} {
		assertRasterUpdateParity(t, oldFrame, newFrame, scale, true, "")
	}
}

func TestFrameRasterizerRepairsOnlyStationaryTranslucentRoundedFillBoundary(t *testing.T) {
	t.Parallel()
	content := dom.NewElement("section")
	viewport := Viewport{Width: 120, Height: 90}
	radii := CornerRadii{
		TopLeft: CornerRadius{X: 13, Y: 11}, TopRight: CornerRadius{X: 9, Y: 15},
		BottomRight: CornerRadius{X: 15, Y: 10}, BottomLeft: CornerRadius{X: 11, Y: 14},
	}
	base := &Frame{
		Viewport: viewport, paintArtifact: newPaintArtifactID(),
		DisplayList: DisplayList{Viewport: viewport, Commands: []Command{
			{Kind: FillRectCommand, Rect: Rect{Width: 120, Height: 90}, Color: color.NRGBA{R: 0x11, G: 0x18, B: 0x28, A: 0xff}},
			{Kind: BeginClipCommand, Rect: Rect{Width: 120, Height: 90}},
			{Kind: FillRectCommand, Rect: Rect{X: 8, Y: 7, Width: 104, Height: 76}, Radii: radii, Color: color.NRGBA{R: 0x2b, G: 0x38, B: 0x50, A: 0x88}},
			{Kind: FillRectCommand, Node: content, Rect: Rect{X: 14, Y: 16, Width: 90, Height: 115}, Color: color.NRGBA{R: 0x48, G: 0x72, B: 0x9c, A: 0xd8}},
			{Kind: DrawTextCommand, Node: content, Text: "content over alpha panel", X: 18, BaselineY: 42, FontSize: 13, FontFamily: FontFamilySansSerif, Color: opaqueWhite, textBounds: Rect{X: 18, Y: 27, Width: 86, Height: 17}},
			{Kind: EndClipCommand},
		}},
	}
	oldFrame := TransformDisplayList(base, map[*dom.Node]VisualTransform{content: {}})
	newFrame := TransformDisplayList(base, map[*dom.Node]VisualTransform{content: {OffsetY: 8}})
	for _, scale := range []float64{1, 1.5, 2} {
		assertRasterUpdateParity(t, oldFrame, newFrame, scale, true, "")
	}
}

func TestFrameRasterizerRepairsRoundedFillInsideOpacityGroup(t *testing.T) {
	t.Parallel()
	content := dom.NewElement("section")
	fixed := dom.NewElement("aside")
	viewport := Viewport{Width: 120, Height: 100}
	radii := CornerRadii{
		TopLeft: CornerRadius{X: 14, Y: 12}, TopRight: CornerRadius{X: 10, Y: 16},
		BottomRight: CornerRadius{X: 16, Y: 11}, BottomLeft: CornerRadius{X: 12, Y: 15},
	}
	base := &Frame{
		Viewport: viewport, paintArtifact: newPaintArtifactID(),
		DisplayList: DisplayList{Viewport: viewport, Commands: []Command{
			{Kind: FillRectCommand, Rect: Rect{Width: 120, Height: 100}, Color: color.NRGBA{R: 0x09, G: 0x10, B: 0x1d, A: 0xff}},
			{Kind: BeginOpacityCommand, Opacity: 0.72},
			{Kind: FillRectCommand, Rect: Rect{X: 7, Y: 6, Width: 106, Height: 78}, Radii: radii, Color: color.NRGBA{R: 0x34, G: 0x42, B: 0x5c, A: 0xff}},
			{Kind: FillRectCommand, Node: content, Rect: Rect{X: 13, Y: 14, Width: 92, Height: 120}, Color: color.NRGBA{R: 0x45, G: 0x79, B: 0xa8, A: 0xd0}},
			{Kind: DrawTextCommand, Node: content, Text: "opacity-group scroll", X: 18, BaselineY: 38, FontSize: 13, FontFamily: FontFamilySansSerif, Color: opaqueWhite, textBounds: Rect{X: 18, Y: 23, Width: 82, Height: 17}},
			{Kind: FillEllipseCommand, Node: fixed, Rect: Rect{X: 91, Y: 11, Width: 13, Height: 13}, Color: color.NRGBA{R: 0xf0, G: 0xb0, B: 0x42, A: 0xff}},
			{Kind: EndOpacityCommand},
		}},
	}
	oldFrame := TransformDisplayList(base, map[*dom.Node]VisualTransform{content: {}, fixed: {}})
	newFrame := TransformDisplayList(base, map[*dom.Node]VisualTransform{content: {OffsetY: 8}, fixed: {}})
	for _, scale := range []float64{1, 1.5, 2} {
		assertRasterUpdateParity(t, oldFrame, newFrame, scale, true, "")
	}
}

func TestFrameRasterizerRepairsAsymmetricTranslucentRoundedFillStraightEdge(t *testing.T) {
	t.Parallel()
	content := dom.NewElement("section")
	viewport := Viewport{Width: 140, Height: 100}
	base := &Frame{
		Viewport: viewport, paintArtifact: newPaintArtifactID(),
		DisplayList: DisplayList{Viewport: viewport, Commands: []Command{
			{Kind: FillRectCommand, Rect: Rect{Width: 140, Height: 100}, Color: color.NRGBA{R: 0x0c, G: 0x17, B: 0x29, A: 0xff}},
			{Kind: FillRectCommand, Rect: Rect{X: 8.25, Y: 7.25, Width: 123.5, Height: 84.5}, Radii: CornerRadii{
				BottomRight: CornerRadius{X: 17.25, Y: 12.5}, BottomLeft: CornerRadius{X: 11.5, Y: 16.25},
			}, Color: color.NRGBA{R: 0x34, G: 0x52, B: 0x70, A: 0x88}},
			{Kind: FillRectCommand, Node: content, Rect: Rect{X: 14, Y: 15, Width: 110, Height: 125}, Color: color.NRGBA{R: 0x4b, G: 0x7c, B: 0xac, A: 0xd0}},
			{Kind: DrawTextCommand, Node: content, Text: "one pixel alpha scroll", X: 20, BaselineY: 42, FontSize: 13, FontFamily: FontFamilySansSerif, Color: opaqueWhite, textBounds: Rect{X: 20, Y: 27, Width: 96, Height: 17}},
		}},
	}
	oldFrame := TransformDisplayList(base, map[*dom.Node]VisualTransform{content: {}})
	newFrame := TransformDisplayList(base, map[*dom.Node]VisualTransform{content: {OffsetY: 0.5}})
	assertRasterUpdateParity(t, oldFrame, newFrame, 2, true, "")
}

func TestFrameRasterizerRepairsAsymmetricRoundedFillStraightEdgesInsideOpacityGroup(t *testing.T) {
	t.Parallel()
	content := dom.NewElement("section")
	viewport := Viewport{Width: 140, Height: 105}
	base := &Frame{
		Viewport: viewport, paintArtifact: newPaintArtifactID(),
		DisplayList: DisplayList{Viewport: viewport, Commands: []Command{
			{Kind: FillRectCommand, Rect: Rect{Width: 140, Height: 105}, Color: color.NRGBA{R: 0x08, G: 0x12, B: 0x21, A: 0xff}},
			{Kind: BeginOpacityCommand, Opacity: 0.68},
			{Kind: FillRectCommand, Rect: Rect{X: 7.25, Y: 6.25, Width: 125.5, Height: 91.5}, Radii: CornerRadii{
				TopRight: CornerRadius{X: 14.5, Y: 18.25}, BottomRight: CornerRadius{X: 19.25, Y: 13.5},
			}, Color: color.NRGBA{R: 0x3c, G: 0x4b, B: 0x68, A: 0xa8}},
			{Kind: FillRectCommand, Node: content, Rect: Rect{X: 13, Y: 14, Width: 114, Height: 130}, Color: color.NRGBA{R: 0x4a, G: 0x80, B: 0xb2, A: 0xd4}},
			{Kind: DrawTextCommand, Node: content, Text: "horizontal opacity scroll", X: 19, BaselineY: 39, FontSize: 13, FontFamily: FontFamilySansSerif, Color: opaqueWhite, textBounds: Rect{X: 19, Y: 24, Width: 102, Height: 17}},
			{Kind: EndOpacityCommand},
		}},
	}
	oldFrame := TransformDisplayList(base, map[*dom.Node]VisualTransform{content: {}})
	newFrame := TransformDisplayList(base, map[*dom.Node]VisualTransform{content: {OffsetX: 0.5}})
	assertRasterUpdateParity(t, oldFrame, newFrame, 2, true, "")
}

func TestFrameRasterizerIncrementalNestedScrollParity(t *testing.T) {
	t.Parallel()
	base, content, scrollClip := nestedDamageFrame()
	oldFrame := TransformDisplayList(base, map[*dom.Node]VisualTransform{
		content: {HasClip: true, Clip: scrollClip},
	})
	newFrame := TransformDisplayList(base, map[*dom.Node]VisualTransform{
		content: {OffsetX: 4, OffsetY: 8, HasClip: true, Clip: scrollClip},
	})
	for _, scale := range []float64{1, 1.5, 2} {
		assertRasterUpdateParity(t, oldFrame, newFrame, scale, true, "")
	}
}

func TestFrameRasterizerIncrementalStationaryRectangularClipResizeParity(t *testing.T) {
	t.Parallel()
	base, content, oldClip := rectangularClipResizeFrame(false)
	newClip := Rect{X: oldClip.X, Y: oldClip.Y, Width: oldClip.Width + 8, Height: oldClip.Height - 8}
	oldFrame := TransformDisplayList(base, map[*dom.Node]VisualTransform{
		content: {HasClip: true, Clip: oldClip},
	})
	newFrame := TransformDisplayList(base, map[*dom.Node]VisualTransform{
		content: {OffsetY: 4, HasClip: true, Clip: newClip},
	})

	for _, scale := range []float64{1, 1.5, 2} {
		assertRasterUpdateParity(t, oldFrame, newFrame, scale, true, "")
	}
}

func TestFrameRasterizerIncrementalMovingRectangularClipResizeParity(t *testing.T) {
	t.Parallel()
	base, content, oldClip := rectangularClipResizeFrame(true)
	oldFrame := TransformDisplayList(base, map[*dom.Node]VisualTransform{content: {}})
	newFrame := TransformDisplayList(base, map[*dom.Node]VisualTransform{content: {OffsetX: 4, OffsetY: 4}})
	for index := range newFrame.DisplayList.Commands {
		command := &newFrame.DisplayList.Commands[index]
		if command.Node == content && command.HasClip {
			command.Clip.Width = oldClip.Width + 8
			command.Clip.Height = oldClip.Height - 8
		}
	}

	for _, scale := range []float64{1, 1.5, 2} {
		assertRasterUpdateParity(t, oldFrame, newFrame, scale, true, "")
	}
}

func TestFrameRasterizerIncrementalIndependentRectangularClipMotionParity(t *testing.T) {
	t.Parallel()
	base, content, oldClip := rectangularClipResizeFrame(true)
	pattern := Command{
		Kind: DrawGradientCommand, Rect: Rect{X: 16, Y: 14, Width: 66, Height: 46},
		GradientKind: LinearGradient, GradientAngle: 1.1, GradientStopCount: 3,
		GradientStops: [maxPaintGradientStops]GradientStop{
			{Offset: 0, Color: color.NRGBA{R: 0x16, G: 0x38, B: 0x68, A: 0xff}},
			{Offset: 0.5, Color: color.NRGBA{R: 0xd0, G: 0x58, B: 0x78, A: 0xff}},
			{Offset: 1, Color: color.NRGBA{R: 0x50, G: 0xc0, B: 0x90, A: 0xff}},
		},
	}
	base.DisplayList.Commands = append(base.DisplayList.Commands, Command{})
	copy(base.DisplayList.Commands[2:], base.DisplayList.Commands[1:])
	base.DisplayList.Commands[1] = pattern
	for index := range base.DisplayList.Commands {
		if command := &base.DisplayList.Commands[index]; command.Node == content && command.Kind == FillRectCommand {
			command.Color.A = 0xc0
		}
	}

	oldFrame := TransformDisplayList(base, map[*dom.Node]VisualTransform{content: {}})
	newFrame := TransformDisplayList(base, map[*dom.Node]VisualTransform{content: {OffsetY: 8}})
	for index := range newFrame.DisplayList.Commands {
		command := &newFrame.DisplayList.Commands[index]
		if command.Node == content && command.HasClip {
			// The content moves eight CSS pixels while its hard clip moves only
			// four, matching sticky/intersection projections seen in live pages.
			command.Clip.Y = oldClip.Y - 4
			command.Clip.Width = oldClip.Width + 8
			command.Clip.Height = oldClip.Height - 8
		}
	}

	for _, scale := range []float64{1, 1.5, 2} {
		assertRasterUpdateParity(t, oldFrame, newFrame, scale, true, "")
	}
}

func TestFrameRasterizerRoundedClipIndependentMotionRemainsConservative(t *testing.T) {
	t.Parallel()
	base, content, oldClip := rectangularClipResizeFrame(true)
	for index := range base.DisplayList.Commands {
		command := &base.DisplayList.Commands[index]
		if command.Node != content || !command.HasClip {
			continue
		}
		command.RoundedClipCount = 1
		command.RoundedClips[0] = RoundedClip{
			Rect: oldClip,
			Radii: CornerRadii{
				TopLeft: CornerRadius{X: 4, Y: 4}, TopRight: CornerRadius{X: 4, Y: 4},
				BottomRight: CornerRadius{X: 4, Y: 4}, BottomLeft: CornerRadius{X: 4, Y: 4},
			},
		}
	}
	oldFrame := TransformDisplayList(base, map[*dom.Node]VisualTransform{content: {}})
	newFrame := TransformDisplayList(base, map[*dom.Node]VisualTransform{content: {OffsetY: 8}})
	for index := range newFrame.DisplayList.Commands {
		command := &newFrame.DisplayList.Commands[index]
		if command.Node != content || command.RoundedClipCount == 0 {
			continue
		}
		command.Clip.Y = oldClip.Y - 4
		command.RoundedClips[0].Rect.Y = oldClip.Y - 4
	}
	assertRasterUpdateParity(t, oldFrame, newFrame, 2, false, RasterFallbackClipMotion)
}

func TestFrameRasterizerRoundedCommandClipResizeRemainsConservative(t *testing.T) {
	t.Parallel()
	base, content, oldClip := rectangularClipResizeFrame(true)
	for index := range base.DisplayList.Commands {
		command := &base.DisplayList.Commands[index]
		if command.Node != content || !command.HasClip {
			continue
		}
		command.RoundedClipCount = 1
		command.RoundedClips[0] = RoundedClip{
			Rect: oldClip,
			Radii: CornerRadii{
				TopLeft: CornerRadius{X: 4, Y: 4}, TopRight: CornerRadius{X: 4, Y: 4},
				BottomRight: CornerRadius{X: 4, Y: 4}, BottomLeft: CornerRadius{X: 4, Y: 4},
			},
		}
	}
	oldFrame := TransformDisplayList(base, map[*dom.Node]VisualTransform{content: {}})
	newFrame := TransformDisplayList(base, map[*dom.Node]VisualTransform{content: {OffsetY: 4}})
	for index := range newFrame.DisplayList.Commands {
		command := &newFrame.DisplayList.Commands[index]
		if command.Node == content && command.HasClip {
			command.Clip.Width = oldClip.Width + 8
		}
	}
	assertRasterUpdateParity(t, oldFrame, newFrame, 2, false, RasterFallbackReason("payload_fill_rect_clip_size"))
}

func TestFrameRasterizerRectangularClipResizeRetainsExcessiveDamageFallback(t *testing.T) {
	t.Parallel()
	base, content, _ := rectangularClipResizeFrame(false)
	oldClip := Rect{X: 12, Y: 10, Width: 12, Height: 10}
	newClip := Rect{X: 12, Y: 10, Width: 114, Height: 84}
	oldFrame := TransformDisplayList(base, map[*dom.Node]VisualTransform{
		content: {HasClip: true, Clip: oldClip},
	})
	newFrame := TransformDisplayList(base, map[*dom.Node]VisualTransform{
		content: {OffsetY: 4, HasClip: true, Clip: newClip},
	})
	assertRasterUpdateParity(t, oldFrame, newFrame, 1, false, RasterFallbackExcessiveDamage)
}

func TestFrameRasterizerIncrementalNestedStationaryClipsUseOuterScrollport(t *testing.T) {
	t.Parallel()
	outerContent := dom.NewElement("outer-content")
	innerContent := dom.NewElement("inner-content")
	viewport := Viewport{Width: 140, Height: 100}
	outerClip := Rect{X: 8, Y: 7, Width: 112, Height: 78}
	innerClip := Rect{X: 29, Y: 24, Width: 48, Height: 35}
	base := &Frame{
		Viewport: viewport, paintArtifact: newPaintArtifactID(),
		DisplayList: DisplayList{Viewport: viewport, Commands: []Command{
			{Kind: FillRectCommand, Rect: Rect{Width: 140, Height: 100}, Color: color.NRGBA{R: 0x12, G: 0x1d, B: 0x2a, A: 0xff}},
			{Kind: FillRectCommand, Node: outerContent, Rect: Rect{X: 8, Y: 7, Width: 112, Height: 150}, Color: color.NRGBA{R: 0x28, G: 0x57, B: 0x81, A: 0xff}},
			{Kind: FillRectCommand, Node: innerContent, Rect: Rect{X: 24, Y: 18, Width: 70, Height: 90}, Color: color.NRGBA{R: 0xe0, G: 0x71, B: 0x43, A: 0xd8}},
			{Kind: FillEllipseCommand, Node: innerContent, Rect: Rect{X: 36, Y: 49, Width: 24, Height: 18}, Color: color.NRGBA{R: 0xf3, G: 0xcd, B: 0x58, A: 0xc8}},
		}},
	}
	oldFrame := TransformDisplayList(base, map[*dom.Node]VisualTransform{
		outerContent: {HasClip: true, Clip: outerClip},
		innerContent: {HasClip: true, Clip: innerClip},
	})
	newFrame := TransformDisplayList(base, map[*dom.Node]VisualTransform{
		outerContent: {OffsetY: 5, HasClip: true, Clip: outerClip},
		innerContent: {OffsetY: 5, HasClip: true, Clip: innerClip},
	})

	rasterizer := NewFrameRasterizer()
	defer rasterizer.Close()
	previous, err := rasterizer.RasterizeAtScale(oldFrame, 2)
	if err != nil {
		t.Fatal(err)
	}
	plan, fallback := planScrollDamage(previous, oldFrame, newFrame, 2, Viewport{Width: 280, Height: 200})
	if fallback != "" {
		t.Fatalf("plan fallback = %q", fallback)
	}
	if want := scaledRasterRect(outerClip, 2); plan.clip != want {
		t.Fatalf("scroll clip = %v, want outer scrollport %v", plan.clip, want)
	}
	assertRasterUpdateParity(t, oldFrame, newFrame, 2, true, "")
}

func TestFrameRasterizerIncrementalRepairsOpaqueFillInsideRectangularGroup(t *testing.T) {
	t.Parallel()
	base, content, scrollClip := nestedDamageFrame()
	overlay := dom.NewElement("clipped-overlay")
	group := []Command{
		{Kind: BeginClipCommand, Node: overlay, Rect: Rect{X: 32, Y: 27, Width: 24, Height: 13}},
		{Kind: FillRectCommand, Node: overlay, Rect: Rect{X: -100, Y: -100, Width: 400, Height: 400}, Color: color.NRGBA{R: 0xe8, G: 0x65, B: 0x43, A: 0xff}},
		{Kind: EndClipCommand, Node: overlay},
	}
	base.DisplayList.Commands = append(base.DisplayList.Commands, group...)
	oldFrame := TransformDisplayList(base, map[*dom.Node]VisualTransform{content: {HasClip: true, Clip: scrollClip}})
	newFrame := TransformDisplayList(base, map[*dom.Node]VisualTransform{content: {OffsetY: 6, HasClip: true, Clip: scrollClip}})
	assertRasterUpdateParity(t, oldFrame, newFrame, 1, true, "")
}

func TestFrameRasterizerIncrementalTransparencyRepairsPatternedUnderlay(t *testing.T) {
	t.Parallel()
	base, content, scrollClip := nestedDamageFrame()
	// A stationary gradient under translucent moving content is deliberately
	// not translation invariant. Regional replay repairs the affected clip.
	base.DisplayList.Commands = append([]Command{{
		Kind: DrawGradientCommand, Rect: scrollClip,
		GradientKind: LinearGradient, GradientAngle: 0.9, GradientStopCount: 2,
		GradientStops: [maxPaintGradientStops]GradientStop{
			{Offset: 0, Color: color.NRGBA{R: 0x10, G: 0x50, B: 0x90, A: 0xff}},
			{Offset: 1, Color: color.NRGBA{R: 0xf0, G: 0x80, B: 0x30, A: 0xff}},
		},
	}}, base.DisplayList.Commands...)
	oldFrame := TransformDisplayList(base, map[*dom.Node]VisualTransform{content: {HasClip: true, Clip: scrollClip}})
	newFrame := TransformDisplayList(base, map[*dom.Node]VisualTransform{content: {OffsetY: 6, HasClip: true, Clip: scrollClip}})
	assertRasterUpdateParity(t, oldFrame, newFrame, 2, true, "")
}

func TestFrameRasterizerRetainsMovingPatternBelowTrailingOverlaySuffix(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		overlay func(*dom.Node) []Command
	}{
		{
			name: "translucent rounded overlay",
			overlay: func(overlay *dom.Node) []Command {
				return []Command{{
					Kind: FillRectCommand, Node: overlay,
					Rect: Rect{X: 7, Y: 6, Width: 114, Height: 84}, Radii: CornerRadii{
						TopLeft: CornerRadius{X: 15, Y: 12}, TopRight: CornerRadius{X: 11, Y: 17},
						BottomRight: CornerRadius{X: 17, Y: 10}, BottomLeft: CornerRadius{X: 12, Y: 16},
					},
					Color: color.NRGBA{R: 0x48, G: 0x2f, B: 0x67, A: 0x78},
				}}
			},
		},
		{
			name: "complete opacity group overlay",
			overlay: func(overlay *dom.Node) []Command {
				return []Command{
					{Kind: BeginOpacityCommand, Node: overlay, Opacity: 0.63},
					{Kind: FillRectCommand, Node: overlay, Rect: Rect{X: 7, Y: 6, Width: 114, Height: 84}, Radii: CornerRadii{
						TopLeft: CornerRadius{X: 15, Y: 12}, TopRight: CornerRadius{X: 11, Y: 17},
						BottomRight: CornerRadius{X: 17, Y: 10}, BottomLeft: CornerRadius{X: 12, Y: 16},
					}, Color: color.NRGBA{R: 0x43, G: 0x38, B: 0x68, A: 0xff}},
					{Kind: FillEllipseCommand, Node: overlay, Rect: Rect{X: 92.25, Y: 13.5, Width: 18.5, Height: 15.25}, Color: color.NRGBA{R: 0xf2, G: 0xb2, B: 0x48, A: 0xd0}},
					{Kind: EndOpacityCommand, Node: overlay},
				}
			},
		},
	}
	scales := []struct {
		name  string
		value float64
	}{{"1x", 1}, {"1.5x", 1.5}, {"2x", 2}}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			content := dom.NewElement("moving-pattern")
			overlay := dom.NewElement("stationary-overlay")
			viewport := Viewport{Width: 128, Height: 96}
			commands := []Command{
				{Kind: FillRectCommand, Rect: Rect{Width: 128, Height: 96}, Color: color.NRGBA{R: 0x0c, G: 0x17, B: 0x29, A: 0xff}},
				{Kind: FillRectCommand, Node: content, Rect: Rect{X: 4, Y: 3, Width: 120, Height: 150}, Color: color.NRGBA{R: 0x24, G: 0x55, B: 0x83, A: 0xff}},
				{Kind: DrawGradientCommand, Node: content, Rect: Rect{X: 9, Y: 11, Width: 109, Height: 118}, GradientKind: LinearGradient, GradientAngle: 0.74, GradientStopCount: 3,
					GradientStops: [maxPaintGradientStops]GradientStop{
						{Offset: 0, Color: color.NRGBA{R: 0x18, G: 0x79, B: 0xa6, A: 0xd8}},
						{Offset: 0.46, Color: color.NRGBA{R: 0xd0, G: 0x55, B: 0x82, A: 0xbc}},
						{Offset: 1, Color: color.NRGBA{R: 0xf0, G: 0xc4, B: 0x58, A: 0xd0}},
					}},
				{Kind: FillEllipseCommand, Node: content, Rect: Rect{X: 18.25, Y: 27.5, Width: 29.5, Height: 21.25}, Color: color.NRGBA{R: 0xf1, G: 0x6d, B: 0x4f, A: 0xd0}},
			}
			commands = append(commands, test.overlay(overlay)...)
			base := &Frame{
				Viewport: viewport, paintArtifact: newPaintArtifactID(),
				DisplayList: DisplayList{Viewport: viewport, Commands: commands},
			}
			oldFrame := TransformDisplayList(base, map[*dom.Node]VisualTransform{content: {}})
			middleFrame := TransformDisplayList(base, map[*dom.Node]VisualTransform{content: {OffsetY: 6}})
			newFrame := TransformDisplayList(base, map[*dom.Node]VisualTransform{content: {OffsetY: 12}})

			for _, scale := range scales {
				t.Run(scale.name, func(t *testing.T) {
					rasterizer := NewFrameRasterizer()
					defer rasterizer.Close()
					previous, err := rasterizer.RasterizeAtScale(oldFrame, scale.value)
					if err != nil {
						t.Fatal(err)
					}
					first, err := rasterizer.UpdateAtScale(previous, oldFrame, middleFrame, scale.value)
					if err != nil {
						t.Fatal(err)
					}
					assertRetainedUnderlayTelemetry(t, first, false)
					middleReference, err := RasterizeAtScale(middleFrame, scale.value)
					if err != nil {
						t.Fatal(err)
					}
					assertRGBAEqual(t, first.Canvas, middleReference)

					second, err := rasterizer.UpdateAtScale(first.Canvas, middleFrame, newFrame, scale.value)
					if err != nil {
						t.Fatal(err)
					}
					assertRetainedUnderlayTelemetry(t, second, true)
					newReference, err := RasterizeAtScale(newFrame, scale.value)
					if err != nil {
						t.Fatal(err)
					}
					assertRGBAEqual(t, second.Canvas, newReference)
				})
			}
		})
	}
}

func TestFrameRasterizerArtifactRefreshRetainsOverlayBoundaryAcrossNodeReplacement(t *testing.T) {
	t.Parallel()

	content := dom.NewElement("moving-content")
	overlayA := dom.NewElement("overlay-a")
	overlayB := dom.NewElement("overlay-b")
	viewport := Viewport{Width: 96, Height: 72}
	commands := func(overlay *dom.Node) []Command {
		return []Command{
			{Kind: FillRectCommand, Rect: Rect{Width: 96, Height: 72}, Color: color.NRGBA{R: 0x10, G: 0x18, B: 0x24, A: 0xff}},
			{Kind: FillRectCommand, Node: content, Rect: Rect{X: 4, Y: 2, Width: 88, Height: 120}, Color: color.NRGBA{R: 0x28, G: 0x64, B: 0x91, A: 0xff}},
			{Kind: FillRectCommand, Node: overlay, Rect: Rect{X: 5, Y: 5, Width: 86, Height: 18}, Radii: CornerRadii{
				TopLeft: CornerRadius{X: 9, Y: 8}, TopRight: CornerRadius{X: 8, Y: 9},
				BottomRight: CornerRadius{X: 10, Y: 7}, BottomLeft: CornerRadius{X: 7, Y: 10},
			}, Color: color.NRGBA{R: 0x52, G: 0x36, B: 0x75, A: 0x9a}},
		}
	}
	baseA := &Frame{
		Viewport: viewport, paintArtifact: newPaintArtifactID(),
		DisplayList: DisplayList{Viewport: viewport, Commands: commands(overlayA)},
	}
	baseB := &Frame{
		Viewport: viewport, paintArtifact: newPaintArtifactID(),
		DisplayList: DisplayList{Viewport: viewport, Commands: commands(overlayB)},
	}
	oldFrame := TransformDisplayList(baseA, map[*dom.Node]VisualTransform{content: {}})
	middleFrame := TransformDisplayList(baseA, map[*dom.Node]VisualTransform{content: {OffsetY: 4}})
	artifactFrame := TransformDisplayList(baseB, map[*dom.Node]VisualTransform{content: {OffsetY: 4}})
	newFrame := TransformDisplayList(baseB, map[*dom.Node]VisualTransform{content: {OffsetY: 8}})

	rasterizer := NewFrameRasterizer()
	defer rasterizer.Close()
	previous, err := rasterizer.RasterizeAtScale(oldFrame, 2)
	if err != nil {
		t.Fatal(err)
	}
	first, err := rasterizer.UpdateAtScale(previous, oldFrame, middleFrame, 2)
	if err != nil {
		t.Fatal(err)
	}
	assertRetainedUnderlayTelemetry(t, first, false)
	artifact, err := rasterizer.UpdateAtScale(first.Canvas, middleFrame, artifactFrame, 2)
	if err != nil {
		t.Fatal(err)
	}
	if artifact.Incremental || artifact.Fallback != RasterFallbackArtifact {
		t.Fatalf("artifact refresh = incremental %t fallback %q", artifact.Incremental, artifact.Fallback)
	}
	artifactReference, err := RasterizeAtScale(artifactFrame, 2)
	if err != nil {
		t.Fatal(err)
	}
	assertRGBAEqual(t, artifact.Canvas, artifactReference)

	second, err := rasterizer.UpdateAtScale(artifact.Canvas, artifactFrame, newFrame, 2)
	if err != nil {
		t.Fatal(err)
	}
	assertRetainedUnderlayTelemetry(t, second, true)
	newReference, err := RasterizeAtScale(newFrame, 2)
	if err != nil {
		t.Fatal(err)
	}
	assertRGBAEqual(t, second.Canvas, newReference)
}

func TestRetainedUnderlayBoundariesTreatStructuralOnlyGapAsEquivalent(t *testing.T) {
	t.Parallel()
	commands := []Command{
		{Kind: FillRectCommand},
		{Kind: BeginClipCommand},
		{Kind: EndClipCommand},
		{Kind: BeginOpacityCommand},
		{Kind: EndOpacityCommand},
		{Kind: FillRectCommand},
	}
	if !retainedUnderlayBoundariesEquivalent(commands, 1, 5) {
		t.Fatal("structural-only boundary gap should preserve identical prefix pixels")
	}
	if retainedUnderlayBoundariesEquivalent(commands, 1, 6) {
		t.Fatal("boundary gap containing a painted command must not be reused")
	}
	if retainedUnderlayBoundariesEquivalent(commands, 0, 1) {
		t.Fatal("zero boundary must not be reused")
	}
}

func TestFrameRasterizerReplaysDisjointOverlayRegionsInParallelWithExactParity(t *testing.T) {
	t.Parallel()

	content := dom.NewElement("moving-content")
	top := dom.NewElement("top-overlay")
	bottom := dom.NewElement("bottom-overlay")
	viewport := Viewport{Width: 120, Height: 96}
	base := &Frame{
		Viewport: viewport, paintArtifact: newPaintArtifactID(),
		DisplayList: DisplayList{Viewport: viewport, Commands: []Command{
			{Kind: BeginClipCommand, Rect: Rect{Width: 120, Height: 96}},
			{Kind: FillRectCommand, Rect: Rect{Width: 120, Height: 96}, Color: color.NRGBA{R: 0x0d, G: 0x17, B: 0x26, A: 0xff}},
			{Kind: DrawGradientCommand, Node: content, Rect: Rect{X: 3, Y: 2, Width: 114, Height: 142}, GradientKind: LinearGradient, GradientAngle: 0.67, GradientStopCount: 2,
				GradientStops: [maxPaintGradientStops]GradientStop{
					{Offset: 0, Color: color.NRGBA{R: 0x22, G: 0x77, B: 0xa1, A: 0xff}},
					{Offset: 1, Color: color.NRGBA{R: 0xcb, G: 0x4c, B: 0x77, A: 0xff}},
				}},
			{Kind: FillRectCommand, Node: top, Rect: Rect{X: 4.25, Y: 4.5, Width: 111.5, Height: 20.25}, Radii: CornerRadii{
				TopLeft: CornerRadius{X: 11, Y: 9}, TopRight: CornerRadius{X: 8, Y: 12},
				BottomRight: CornerRadius{X: 12, Y: 8}, BottomLeft: CornerRadius{X: 9, Y: 11},
			}, Color: color.NRGBA{R: 0x52, G: 0x39, B: 0x76, A: 0x99}},
			{Kind: BeginOpacityCommand, Node: bottom, Opacity: 0.61},
			{Kind: FillRectCommand, Node: bottom, Rect: Rect{X: 4.5, Y: 70.25, Width: 111, Height: 21.5}, Radii: CornerRadii{
				TopLeft: CornerRadius{X: 10, Y: 8}, TopRight: CornerRadius{X: 9, Y: 11},
				BottomRight: CornerRadius{X: 11, Y: 9}, BottomLeft: CornerRadius{X: 8, Y: 10},
			}, Color: color.NRGBA{R: 0x35, G: 0x49, B: 0x6c, A: 0xff}},
			{Kind: FillEllipseCommand, Node: bottom, Rect: Rect{X: 91.5, Y: 74.25, Width: 15.5, Height: 12.25}, Color: color.NRGBA{R: 0xf0, G: 0xb7, B: 0x4a, A: 0xd7}},
			{Kind: EndOpacityCommand, Node: bottom},
			{Kind: EndClipCommand},
		}},
	}
	oldFrame := TransformDisplayList(base, map[*dom.Node]VisualTransform{content: {}})
	middleFrame := TransformDisplayList(base, map[*dom.Node]VisualTransform{content: {OffsetY: 4}})
	newFrame := TransformDisplayList(base, map[*dom.Node]VisualTransform{content: {OffsetY: 8}})

	for _, scale := range []float64{1, 1.5, 2} {
		rasterizer := NewFrameRasterizer()
		previous, err := rasterizer.RasterizeAtScale(oldFrame, scale)
		if err != nil {
			rasterizer.Close()
			t.Fatal(err)
		}
		first, err := rasterizer.UpdateAtScale(previous, oldFrame, middleFrame, scale)
		if err != nil {
			rasterizer.Close()
			t.Fatal(err)
		}
		if first.OverlayRegions != 2 {
			rasterizer.Close()
			t.Fatalf("first overlay regions = %d, want 2: %#v", first.OverlayRegions, first)
		}
		middleReference, err := RasterizeAtScale(middleFrame, scale)
		if err != nil {
			rasterizer.Close()
			t.Fatal(err)
		}
		assertRGBAEqual(t, first.Canvas, middleReference)

		second, err := rasterizer.UpdateAtScale(first.Canvas, middleFrame, newFrame, scale)
		if err != nil {
			rasterizer.Close()
			t.Fatal(err)
		}
		if second.OverlayRegions != 2 || !second.UnderlayCacheHit {
			rasterizer.Close()
			t.Fatalf("second overlay telemetry = regions %d cache hit %t", second.OverlayRegions, second.UnderlayCacheHit)
		}
		newReference, err := RasterizeAtScale(newFrame, scale)
		if err != nil {
			rasterizer.Close()
			t.Fatal(err)
		}
		assertRGBAEqual(t, second.Canvas, newReference)
		if err := rasterizer.Close(); err != nil {
			t.Fatal(err)
		}
	}
}

func TestFrameRasterizerRetainedTranslatedOverlayUsesConservativeReplayRegion(t *testing.T) {
	t.Parallel()

	content := dom.NewElement("moving-content")
	overlay := dom.NewElement("translated-overlay")
	viewport := Viewport{Width: 128, Height: 88}
	base := &Frame{
		Viewport: viewport, paintArtifact: newPaintArtifactID(),
		DisplayList: DisplayList{Viewport: viewport, Commands: []Command{
			{Kind: FillRectCommand, Rect: Rect{Width: 128, Height: 88}, Color: color.NRGBA{R: 0x0d, G: 0x18, B: 0x27, A: 0xff}},
			{Kind: DrawGradientCommand, Node: content, Rect: Rect{X: 3, Y: 2, Width: 122, Height: 130}, GradientKind: LinearGradient, GradientStopCount: 2,
				GradientStops: [maxPaintGradientStops]GradientStop{
					{Offset: 0, Color: color.NRGBA{R: 0x24, G: 0x69, B: 0x91, A: 0xff}},
					{Offset: 1, Color: color.NRGBA{R: 0xb9, G: 0x48, B: 0x76, A: 0xff}},
				}},
			{Kind: BeginTransformCommand, Node: overlay, Transform: AffineTransform{A: 0.98, B: 0.12, C: -0.12, D: 0.98, E: 82, F: 11}},
			{Kind: FillRectCommand, Node: overlay, Rect: Rect{Width: 34, Height: 22}, Radii: CornerRadii{
				TopLeft: CornerRadius{X: 8, Y: 7}, TopRight: CornerRadius{X: 7, Y: 8},
				BottomRight: CornerRadius{X: 9, Y: 6}, BottomLeft: CornerRadius{X: 6, Y: 9},
			}, Color: color.NRGBA{R: 0xef, G: 0xb6, B: 0x48, A: 0xc8}},
			{Kind: EndTransformCommand, Node: overlay},
		}},
	}
	oldFrame := TransformDisplayList(base, map[*dom.Node]VisualTransform{content: {}})
	middleFrame := TransformDisplayList(base, map[*dom.Node]VisualTransform{content: {OffsetY: 4}})
	newFrame := TransformDisplayList(base, map[*dom.Node]VisualTransform{content: {OffsetY: 8}})
	deviceBounds := image.Rect(0, 0, viewport.Width, viewport.Height)
	groups, transformOutputs := rasterGroupBoundsAndTransformOutputsAtScaleRange(
		base.DisplayList.Commands, deviceBounds, 1, 2, len(base.DisplayList.Commands),
	)
	regions := overlayPaintRegions(base.DisplayList.Commands, nil, 1, deviceBounds, groups, transformOutputs, 2, len(base.DisplayList.Commands))
	if len(regions) != 1 || regions[0] != transformOutputs[2] || regions[0] == deviceBounds {
		t.Fatalf("translated overlay regions = %v, want bounded output %v", regions, transformOutputs[2])
	}

	for _, scale := range []float64{1, 1.5, 2} {
		rasterizer := NewFrameRasterizer()
		previous, err := rasterizer.RasterizeAtScale(oldFrame, scale)
		if err != nil {
			rasterizer.Close()
			t.Fatal(err)
		}
		for _, step := range []struct{ oldFrame, newFrame *Frame }{{oldFrame, middleFrame}, {middleFrame, newFrame}} {
			update, err := rasterizer.UpdateAtScale(previous, step.oldFrame, step.newFrame, scale)
			if err != nil {
				rasterizer.Close()
				t.Fatal(err)
			}
			if !update.RetainedUnderlay || update.OverlayRegions != 1 {
				rasterizer.Close()
				t.Fatalf("translated overlay update = retained %t regions %d", update.RetainedUnderlay, update.OverlayRegions)
			}
			reference, err := RasterizeAtScale(step.newFrame, scale)
			if err != nil {
				rasterizer.Close()
				t.Fatal(err)
			}
			assertRGBAEqual(t, update.Canvas, reference)
			previous = update.Canvas
		}
		foundTransformSamples := false
		for key, entry := range rasterizer.overlayPaint.layers.entries {
			if key.kind == rasterTransformLayer && entry.rgba64 != nil {
				foundTransformSamples = true
				break
			}
		}
		if !foundTransformSamples {
			rasterizer.Close()
			t.Fatal("retained transformed overlay did not publish 16-bit samples")
		}
		if err := rasterizer.Close(); err != nil {
			t.Fatal(err)
		}
	}
}

func TestFrameRasterizerRetainedItalicTextReplaysExactGlyphInk(t *testing.T) {
	t.Parallel()

	content := dom.NewElement("moving-content")
	overlay := dom.NewElement("italic-overlay")
	viewport := Viewport{Width: 132, Height: 86}
	base := &Frame{
		Viewport: viewport, paintArtifact: newPaintArtifactID(),
		DisplayList: DisplayList{Viewport: viewport, Commands: []Command{
			{Kind: FillRectCommand, Rect: Rect{Width: 132, Height: 86}, Color: color.NRGBA{R: 0x0c, G: 0x17, B: 0x27, A: 0xff}},
			{Kind: FillRectCommand, Node: content, Rect: Rect{X: 3, Y: 2, Width: 126, Height: 132}, Color: color.NRGBA{R: 0x2d, G: 0x66, B: 0x91, A: 0xff}},
			{Kind: DrawTextCommand, Node: overlay, Text: "fff j", X: 72, BaselineY: 34, FontSize: 25,
				FontFamily: FontFamilySansSerif, FontStyle: FontStyleItalic, Color: color.NRGBA{R: 0xf8, G: 0xe6, B: 0xa2, A: 0xe3},
				textBounds: Rect{X: 74, Y: 12, Width: 48, Height: 25}},
		}},
	}
	oldFrame := TransformDisplayList(base, map[*dom.Node]VisualTransform{content: {}})
	middleFrame := TransformDisplayList(base, map[*dom.Node]VisualTransform{content: {OffsetY: 4}})
	newFrame := TransformDisplayList(base, map[*dom.Node]VisualTransform{content: {OffsetY: 8}})

	for _, scale := range []float64{1, 1.5, 2} {
		rasterizer := NewFrameRasterizer()
		previous, err := rasterizer.RasterizeAtScale(oldFrame, scale)
		if err != nil {
			rasterizer.Close()
			t.Fatal(err)
		}
		for _, step := range []struct{ oldFrame, newFrame *Frame }{{oldFrame, middleFrame}, {middleFrame, newFrame}} {
			update, err := rasterizer.UpdateAtScale(previous, step.oldFrame, step.newFrame, scale)
			if err != nil {
				rasterizer.Close()
				t.Fatal(err)
			}
			if !update.RetainedUnderlay || update.OverlayRegions != 1 {
				rasterizer.Close()
				t.Fatalf("italic overlay update = retained %t regions %d", update.RetainedUnderlay, update.OverlayRegions)
			}
			reference, err := RasterizeAtScale(step.newFrame, scale)
			if err != nil {
				rasterizer.Close()
				t.Fatal(err)
			}
			assertRGBAEqual(t, update.Canvas, reference)
			previous = update.Canvas
		}
		if err := rasterizer.Close(); err != nil {
			t.Fatal(err)
		}
	}
}

func TestFrameRasterizerInvalidatesWarmOpacityLayerBeforeFullFallback(t *testing.T) {
	t.Parallel()

	content := dom.NewElement("moving-content")
	descendant := dom.NewElement("independently-moving-descendant")
	overlay := dom.NewElement("stationary-opacity-group")
	viewport := Viewport{Width: 120, Height: 86}
	base := &Frame{
		Viewport: viewport, paintArtifact: newPaintArtifactID(),
		DisplayList: DisplayList{Viewport: viewport, Commands: []Command{
			{Kind: FillRectCommand, Rect: Rect{Width: 120, Height: 86}, Color: color.NRGBA{R: 0x10, G: 0x19, B: 0x29, A: 0xff}},
			{Kind: FillRectCommand, Node: content, Rect: Rect{X: 4, Y: 2, Width: 112, Height: 132}, Color: color.NRGBA{R: 0x2b, G: 0x67, B: 0x91, A: 0xff}},
			{Kind: BeginOpacityCommand, Node: overlay, Opacity: 0.64},
			{Kind: FillRectCommand, Node: overlay, Rect: Rect{X: 8, Y: 7, Width: 104, Height: 67}, Color: color.NRGBA{R: 0x47, G: 0x39, B: 0x69, A: 0xff}},
			{Kind: FillEllipseCommand, Node: descendant, Rect: Rect{X: 73, Y: 27, Width: 24, Height: 19}, Color: color.NRGBA{R: 0xf1, G: 0xb4, B: 0x45, A: 0xe0}},
			{Kind: EndOpacityCommand, Node: overlay},
		}},
	}
	oldFrame := TransformDisplayList(base, map[*dom.Node]VisualTransform{content: {}, descendant: {}})
	middleFrame := TransformDisplayList(base, map[*dom.Node]VisualTransform{content: {OffsetY: 6}, descendant: {}})
	newFrame := TransformDisplayList(base, map[*dom.Node]VisualTransform{content: {OffsetY: 12}, descendant: {OffsetX: 0.25}})

	for _, scale := range []float64{1, 1.5, 2} {
		rasterizer := NewFrameRasterizer()
		previous, err := rasterizer.RasterizeAtScale(oldFrame, scale)
		if err != nil {
			rasterizer.Close()
			t.Fatal(err)
		}
		warm, err := rasterizer.UpdateAtScale(previous, oldFrame, middleFrame, scale)
		if err != nil {
			rasterizer.Close()
			t.Fatal(err)
		}
		if !warm.RetainedUnderlay || rasterizer.overlayPaint.layers.bytes == 0 {
			rasterizer.Close()
			t.Fatalf("opacity fixture did not warm retained paint: %#v", warm)
		}
		fallback, err := rasterizer.UpdateAtScale(warm.Canvas, middleFrame, newFrame, scale)
		if err != nil {
			rasterizer.Close()
			t.Fatal(err)
		}
		if fallback.Incremental || fallback.Fallback != RasterFallbackFractional {
			rasterizer.Close()
			t.Fatalf("fallback = incremental %t reason %q, want %q", fallback.Incremental, fallback.Fallback, RasterFallbackFractional)
		}
		reference, err := RasterizeAtScale(newFrame, scale)
		if err != nil {
			rasterizer.Close()
			t.Fatal(err)
		}
		assertRGBAEqual(t, fallback.Canvas, reference)
		if err := rasterizer.Close(); err != nil {
			t.Fatal(err)
		}
	}
}

func TestOverlayPaintRegionsUsesExactGlyphInkAndCapsDisplayListScans(t *testing.T) {
	t.Parallel()

	clip := image.Rect(0, 0, 256, 128)
	fonts, err := newFontBook(nil)
	if err != nil {
		t.Fatal(err)
	}
	defer fonts.Close()
	textCommand := Command{
		Kind: DrawTextCommand, Text: "fff j", X: 40, BaselineY: 42, FontSize: 28,
		FontStyle: FontStyleItalic, textBounds: Rect{X: 42, Y: 18, Width: 45, Height: 28},
	}
	wantInk, exact := fonts.horizontalTextInkBounds(textCommand)
	if !exact || wantInk.Empty() {
		t.Fatal("fixture did not produce exact italic glyph ink")
	}
	regions := overlayPaintRegions([]Command{textCommand}, fonts, 1, clip, nil, nil, 0, 1)
	if len(regions) != 1 || regions[0] != wantInk.Intersect(clip) {
		t.Fatalf("text overlay regions = %v, want exact glyph ink %v", regions, wantInk.Intersect(clip))
	}

	commands := make([]Command, maxOverlayRegions+1)
	wideClip := image.Rect(0, 0, (maxOverlayRegions+1)*3, 3)
	for index := range commands {
		commands[index] = Command{Kind: FillRectCommand, Rect: Rect{X: float64(index * 3), Width: 1, Height: 1}, Color: opaqueWhite}
	}
	regions = overlayPaintRegions(commands, fonts, 1, wideClip, nil, nil, 0, len(commands))
	if len(regions) != 1 || regions[0] != wideClip {
		t.Fatalf("oversized overlay decomposition = %v, want one bounded replay %v", regions, wideClip)
	}
}

func TestEffectLayerCacheEvictsLeastRecentlyUsedEntryWithinSharedBudget(t *testing.T) {
	t.Parallel()

	cache := effectLayerCache{}
	boundsA := image.Rect(0, 0, 768, 1024)
	boundsB := image.Rect(1, 0, 769, 1024)
	boundsC := image.Rect(2, 0, 770, 1024)
	cache.putOpacity(1, 2, 1, boundsA, 128, image.NewRGBA(boundsA))
	cache.putOpacity(3, 4, 1, boundsB, 128, image.NewRGBA(boundsB))
	if _, ok := cache.getOpacity(1, 2, 1, boundsA, 128); !ok {
		t.Fatal("first effect entry was not cached")
	}
	cache.putOpacity(5, 6, 1, boundsC, 128, image.NewRGBA(boundsC))
	if _, ok := cache.getOpacity(3, 4, 1, boundsB, 128); ok {
		t.Fatal("least-recently-used effect entry survived budget eviction")
	}
	if _, ok := cache.getOpacity(1, 2, 1, boundsA, 128); !ok {
		t.Fatal("recently touched effect entry was evicted")
	}
	if _, ok := cache.getOpacity(5, 6, 1, boundsC, 128); !ok {
		t.Fatal("new effect entry was not cached")
	}
	if cache.bytes > maxEffectLayerCacheBytes {
		t.Fatalf("effect cache bytes = %d, cap %d", cache.bytes, maxEffectLayerCacheBytes)
	}
}

func TestFrameRasterizerClearsRetainedUnderlayAfterOverlayReplayError(t *testing.T) {
	t.Parallel()

	content := dom.NewElement("moving-content")
	overlay := dom.NewElement("stationary-overlay")
	viewport := Viewport{Width: 64, Height: 48}
	base := &Frame{
		Viewport: viewport, paintArtifact: newPaintArtifactID(),
		DisplayList: DisplayList{Viewport: viewport, Commands: []Command{
			{Kind: FillRectCommand, Node: content, Rect: Rect{Width: 64, Height: 72}, Color: opaqueWhite},
			{Kind: CommandKind(255)},
			{Kind: FillRectCommand, Node: overlay, Rect: Rect{X: 4, Y: 4, Width: 18, Height: 12}, Color: opaqueWhite},
		}},
	}
	oldFrame := TransformDisplayList(base, map[*dom.Node]VisualTransform{content: {}})
	newFrame := TransformDisplayList(base, map[*dom.Node]VisualTransform{content: {OffsetY: 4}})
	previous := image.NewRGBA(image.Rect(0, 0, viewport.Width, viewport.Height))
	rasterizer := NewFrameRasterizer()
	defer rasterizer.Close()
	if _, err := rasterizer.UpdateAtScale(previous, oldFrame, newFrame, 1); err == nil {
		t.Fatal("invalid overlay replay error = nil")
	}
	if rasterizer.scrollBase != nil || rasterizer.scrollBaseFrame != nil {
		t.Fatal("failed overlay replay retained a partially advanced scrolling base")
	}
}

func TestFrameRasterizerDoesNotSplitTrailingRoundedGroupWithMovingDescendant(t *testing.T) {
	t.Parallel()

	content := dom.NewElement("moving-pattern")
	descendant := dom.NewElement("moving-overlay-descendant")
	overlay := dom.NewElement("stationary-overlay-group")
	viewport := Viewport{Width: 128, Height: 96}
	radii := CornerRadii{
		TopLeft: CornerRadius{X: 14, Y: 12}, TopRight: CornerRadius{X: 10, Y: 16},
		BottomRight: CornerRadius{X: 16, Y: 11}, BottomLeft: CornerRadius{X: 12, Y: 15},
	}
	base := &Frame{
		Viewport: viewport, paintArtifact: newPaintArtifactID(),
		DisplayList: DisplayList{Viewport: viewport, Commands: []Command{
			{Kind: FillRectCommand, Rect: Rect{Width: 128, Height: 96}, Color: color.NRGBA{R: 0x0c, G: 0x17, B: 0x29, A: 0xff}},
			{Kind: FillRectCommand, Node: content, Rect: Rect{X: 4, Y: 3, Width: 120, Height: 150}, Color: color.NRGBA{R: 0x24, G: 0x55, B: 0x83, A: 0xff}},
			{Kind: DrawGradientCommand, Node: content, Rect: Rect{X: 9, Y: 11, Width: 109, Height: 118}, GradientKind: LinearGradient, GradientAngle: 0.74, GradientStopCount: 2,
				GradientStops: [maxPaintGradientStops]GradientStop{
					{Offset: 0, Color: color.NRGBA{R: 0x18, G: 0x79, B: 0xa6, A: 0xd8}},
					{Offset: 1, Color: color.NRGBA{R: 0xd0, G: 0x55, B: 0x82, A: 0xbc}},
				}},
			{Kind: BeginOpacityCommand, Node: overlay, Opacity: 0.66},
			{Kind: BeginClipCommand, Node: overlay, Rect: Rect{X: 7, Y: 6, Width: 114, Height: 84}, Radii: radii},
			{Kind: FillRectCommand, Node: overlay, Rect: Rect{X: 7, Y: 6, Width: 114, Height: 84}, Color: color.NRGBA{R: 0x43, G: 0x38, B: 0x68, A: 0xff}},
			{Kind: FillEllipseCommand, Node: descendant, Rect: Rect{X: 76.25, Y: 25.5, Width: 27.5, Height: 20.25}, Color: color.NRGBA{R: 0xf2, G: 0xb2, B: 0x48, A: 0xd8}},
			{Kind: EndClipCommand, Node: overlay},
			{Kind: EndOpacityCommand, Node: overlay},
		}},
	}
	oldFrame := TransformDisplayList(base, map[*dom.Node]VisualTransform{content: {}, descendant: {}})
	newFrame := TransformDisplayList(base, map[*dom.Node]VisualTransform{content: {OffsetY: 6}, descendant: {OffsetY: 6}})
	for _, scale := range []float64{1, 1.5, 2} {
		rasterizer := NewFrameRasterizer()
		previous, err := rasterizer.RasterizeAtScale(oldFrame, scale)
		if err != nil {
			rasterizer.Close()
			t.Fatal(err)
		}
		reference, err := RasterizeAtScale(newFrame, scale)
		if err != nil {
			rasterizer.Close()
			t.Fatal(err)
		}
		update, err := rasterizer.UpdateAtScale(previous, oldFrame, newFrame, scale)
		rasterizer.Close()
		if err != nil {
			t.Fatal(err)
		}
		if update.RetainedUnderlay || update.UnderlayCacheHit || update.OverlaySuffixCommands != 0 || update.UnderlayCopyPixels != 0 {
			t.Fatalf("unsafe group used retained underlay: %#v", update)
		}
		if update.Incremental || update.Fallback != RasterFallbackUnsafeGroup {
			t.Fatalf("unsafe group update = incremental %t fallback %q, want full %q", update.Incremental, update.Fallback, RasterFallbackUnsafeGroup)
		}
		assertRGBAEqual(t, update.Canvas, reference)
	}
}

func assertRetainedUnderlayTelemetry(t *testing.T, update RasterUpdate, wantHit bool) {
	t.Helper()
	wantMiss := !wantHit
	if !update.Incremental || update.Fallback != "" || !update.RetainedUnderlay || update.UnderlayCacheHit != wantHit ||
		(update.UnderlayCacheMiss != "") != wantMiss || update.OverlaySuffixCommands <= 0 || update.OverlayRegions <= 0 ||
		update.UnderlayCopyPixels <= 0 || update.UnderlayCopyPixels != update.PlannedScrollClipArea {
		t.Fatalf("retained underlay update = %#v, want cache hit %t with nonzero suffix/copy telemetry", update, wantHit)
	}
}

func TestFrameRasterizerIncrementalAllowsUnrelatedRoundedAndTransformGroups(t *testing.T) {
	t.Parallel()
	base, content, scrollClip := nestedDamageFrame()
	oldFrame := TransformDisplayList(base, map[*dom.Node]VisualTransform{content: {HasClip: true, Clip: scrollClip}})
	newFrame := TransformDisplayList(base, map[*dom.Node]VisualTransform{content: {OffsetY: 7, HasClip: true, Clip: scrollClip}})
	assertRasterUpdateParity(t, oldFrame, newFrame, 2, true, "")
}

func TestFrameRasterizerIncrementalRepairsStationaryTransformedOverlay(t *testing.T) {
	t.Parallel()
	base, content, scrollClip := nestedDamageFrame()
	overlay := dom.NewElement("overlay")
	commands := []Command{
		{Kind: BeginTransformCommand, Node: overlay, Transform: AffineTransform{A: 0.98, B: 0.18, C: -0.18, D: 0.98, E: -28, F: -10}},
		{Kind: FillRectCommand, Node: overlay, Rect: Rect{X: 86, Y: 30, Width: 24, Height: 16}, Color: color.NRGBA{R: 0xf0, G: 0x48, B: 0x70, A: 0xd0}},
		{Kind: EndTransformCommand, Node: overlay},
	}
	updatedCommands := make([]Command, 0, len(base.DisplayList.Commands)+len(commands))
	updatedCommands = append(updatedCommands, base.DisplayList.Commands[:1]...)
	updatedCommands = append(updatedCommands, commands...)
	updatedCommands = append(updatedCommands, base.DisplayList.Commands[1:]...)
	base.DisplayList.Commands = updatedCommands
	oldFrame := TransformDisplayList(base, map[*dom.Node]VisualTransform{content: {HasClip: true, Clip: scrollClip}})
	newFrame := TransformDisplayList(base, map[*dom.Node]VisualTransform{content: {OffsetY: 7, HasClip: true, Clip: scrollClip}})
	assertRasterUpdateParity(t, oldFrame, newFrame, 2, true, "")
}

func TestFrameRasterizerIncrementalLargeViewportVectorParity(t *testing.T) {
	t.Parallel()
	content := dom.NewElement("content")
	fixed := dom.NewElement("fixed")
	viewport := Viewport{Width: 700, Height: 600}
	radii := CornerRadii{TopLeft: CornerRadius{X: 7.25, Y: 6.5}, TopRight: CornerRadius{X: 5.75, Y: 8.25}, BottomRight: CornerRadius{X: 8.5, Y: 4.75}, BottomLeft: CornerRadius{X: 4.25, Y: 7.5}}
	base := &Frame{
		Viewport: viewport, paintArtifact: newPaintArtifactID(),
		DisplayList: DisplayList{Viewport: viewport, Commands: []Command{
			{Kind: FillRectCommand, Rect: Rect{Width: 700, Height: 600}, Color: color.NRGBA{R: 0x12, G: 0x20, B: 0x34, A: 0xff}},
			{Kind: FillRectCommand, Node: content, Rect: Rect{X: 20, Y: 15, Width: 640, Height: 900}, Color: color.NRGBA{R: 0x28, G: 0x48, B: 0x70, A: 0xff}},
			{Kind: FillRectCommand, Node: fixed, Rect: Rect{X: 330.25, Y: 250.5, Width: 37.5, Height: 24.25}, Radii: radii, Color: color.NRGBA{R: 0xe0, G: 0x60, B: 0x80, A: 0xd0}},
			{Kind: FillEllipseCommand, Node: fixed, Rect: Rect{X: 374.375, Y: 248.75, Width: 23.5, Height: 27.25}, Color: color.NRGBA{R: 0xf0, G: 0xc0, B: 0x50, A: 0xc8}},
		}},
	}
	oldFrame := TransformDisplayList(base, map[*dom.Node]VisualTransform{content: {}})
	newFrame := TransformDisplayList(base, map[*dom.Node]VisualTransform{content: {OffsetY: 3}, fixed: {}})
	assertRasterUpdateParity(t, oldFrame, newFrame, 1, true, "")
}

func TestFrameRasterizerIncrementalFallbacksRemainExact(t *testing.T) {
	t.Parallel()

	base, content, scrollClip := nestedDamageFrame()
	tests := []struct {
		name   string
		old    *Frame
		new    *Frame
		scale  float64
		reason RasterFallbackReason
	}{
		{
			name:  "fractional device delta",
			old:   TransformDisplayList(base, map[*dom.Node]VisualTransform{content: {HasClip: true, Clip: scrollClip}}),
			new:   TransformDisplayList(base, map[*dom.Node]VisualTransform{content: {OffsetY: 0.25, HasClip: true, Clip: scrollClip}}),
			scale: 2, reason: RasterFallbackFractional,
		},
		{
			name:  "changed artifact",
			old:   TransformDisplayList(base, map[*dom.Node]VisualTransform{content: {HasClip: true, Clip: scrollClip}}),
			new:   changedPaintArtifact(TransformDisplayList(base, map[*dom.Node]VisualTransform{content: {OffsetY: 4, HasClip: true, Clip: scrollClip}})),
			scale: 2, reason: RasterFallbackArtifact,
		},
		{
			name:  "stationary rounded scroll clip",
			old:   roundedScrollProjection(base, content, scrollClip, 0),
			new:   roundedScrollProjection(base, content, scrollClip, 5),
			scale: 2, reason: RasterFallbackUnsafeGroup,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			assertRasterUpdateParity(t, test.old, test.new, test.scale, false, test.reason)
		})
	}
}

func BenchmarkFrameRasterizerNestedScrollDamage(b *testing.B) {
	base, content, scrollClip := nestedDamageFrame()
	for index := 0; index < 240; index++ {
		base.DisplayList.Commands = append(base.DisplayList.Commands, Command{
			Kind: FillRectCommand, Node: content,
			Rect:  Rect{X: 14 + float64(index%8)*8, Y: 10 + float64(index)*3, Width: 7, Height: 2},
			Color: color.NRGBA{R: uint8(index), G: 0x88, B: 0xc0, A: 0xff},
		})
	}
	oldFrame := TransformDisplayList(base, map[*dom.Node]VisualTransform{content: {HasClip: true, Clip: scrollClip}})
	newFrame := TransformDisplayList(base, map[*dom.Node]VisualTransform{content: {OffsetY: 4, HasClip: true, Clip: scrollClip}})

	b.Run("full", func(b *testing.B) {
		b.ReportAllocs()
		for range b.N {
			if _, err := RasterizeAtScale(newFrame, 2); err != nil {
				b.Fatal(err)
			}
		}
	})
	b.Run("incremental alternating", func(b *testing.B) {
		rasterizer := NewFrameRasterizer()
		defer rasterizer.Close()
		canvas, err := rasterizer.RasterizeAtScale(oldFrame, 2)
		if err != nil {
			b.Fatal(err)
		}
		viewport, err := scaledViewport(newFrame.DisplayList.Viewport, 2)
		if err != nil {
			b.Fatal(err)
		}
		plan, fallback := planScrollDamage(canvas, oldFrame, newFrame, 2, viewport)
		if fallback != "" {
			b.Fatalf("benchmark plan fallback = %q", fallback)
		}
		old, next := oldFrame, newFrame
		b.ReportAllocs()
		b.ResetTimer()
		b.ReportMetric(float64(plan.damagePixels), "damage_px")
		b.ReportMetric(float64(len(plan.damage)), "damage_rects")
		b.ReportMetric(float64(rectangleArea(plan.clip)), "scroll_clip_px")
		if len(plan.damage) > 0 {
			union := plan.damage[0]
			for _, rectangle := range plan.damage[1:] {
				union = union.Union(rectangle)
			}
			b.ReportMetric(float64(rectangleArea(union)), "damage_union_px")
		}
		for range b.N {
			update, err := rasterizer.UpdateAtScale(canvas, old, next, 2)
			if err != nil {
				b.Fatal(err)
			}
			if !update.Incremental {
				b.Fatalf("fallback = %q", update.Fallback)
			}
			canvas, old, next = update.Canvas, next, old
		}
	})
}

func assertRasterUpdateParity(t *testing.T, oldFrame, newFrame *Frame, scale float64, wantIncremental bool, wantFallback RasterFallbackReason) {
	t.Helper()
	rasterizer := NewFrameRasterizer()
	defer rasterizer.Close()
	previous, err := rasterizer.RasterizeAtScale(oldFrame, scale)
	if err != nil {
		t.Fatal(err)
	}
	reference, err := RasterizeAtScale(newFrame, scale)
	if err != nil {
		t.Fatal(err)
	}
	update, err := rasterizer.UpdateAtScale(previous, oldFrame, newFrame, scale)
	if err != nil {
		t.Fatal(err)
	}
	if update.Incremental != wantIncremental || update.Fallback != wantFallback {
		t.Fatalf("update = incremental %t fallback %q damage %d/%d planned %#v, want %t %q",
			update.Incremental, update.Fallback, update.DamagePixels, update.DamageRects, update, wantIncremental, wantFallback)
	}
	if wantIncremental && update.Canvas != previous {
		t.Fatal("incremental update replaced the retained framebuffer")
	}
	if wantIncremental && (update.PlannedDamagePixels != update.DamagePixels ||
		update.PlannedDamageRects != update.DamageRects || update.PlannedScrollClipArea <= 0 ||
		update.PlannedRawDamageRects <= 0 || update.PlannedScrollCommands <= 0 || update.PlannedMovingCommands <= 0 ||
		update.PlannedDamageSource == "" || update.PlannedSourceDamagePixels <= 0 || update.PlannedSourceDamageRects <= 0 ||
		update.PlannedExactDamagePixels != 0 || update.PlannedExactDamageRects != 0) {
		t.Fatalf("incremental plan telemetry = pixels %d/%d rects %d/%d clip %d raw %d exact %d/%d commands %d/%d",
			update.PlannedDamagePixels, update.DamagePixels,
			update.PlannedDamageRects, update.DamageRects, update.PlannedScrollClipArea,
			update.PlannedRawDamageRects, update.PlannedExactDamagePixels, update.PlannedExactDamageRects,
			update.PlannedScrollCommands, update.PlannedMovingCommands)
	}
	if !wantIncremental && update.Canvas == previous {
		t.Fatal("full fallback mutated the retained framebuffer")
	}
	if wantFallback == RasterFallbackExcessiveDamage &&
		(update.PlannedDamagePixels <= 0 || update.PlannedDamageRects <= 0 || update.PlannedScrollClipArea <= 0 ||
			update.PlannedRawDamageRects <= 0 || update.PlannedExactDamagePixels <= 0 || update.PlannedExactDamageRects <= 0 ||
			update.PlannedExactDamagePixels > update.PlannedDamagePixels || update.PlannedDamageSource == "" ||
			update.PlannedSourceDamagePixels <= 0 || update.PlannedSourceDamageRects <= 0 ||
			update.PlannedScrollCommands <= 0 || update.PlannedMovingCommands <= 0) {
		t.Fatalf("excessive fallback lost rejected plan telemetry: %#v", update)
	}
	assertRGBAEqual(t, update.Canvas, reference)
}

func TestExactDamageRectanglesPreserveLShapedUnion(t *testing.T) {
	got := exactDamageRectangles([]image.Rectangle{
		image.Rect(0, 0, 4, 12),
		image.Rect(0, 0, 12, 4),
	}, image.Rect(0, 0, 12, 12))
	area := 0
	for index, rectangle := range got {
		area += rectangleArea(rectangle)
		for other := index + 1; other < len(got); other++ {
			if rectangle.Overlaps(got[other]) {
				t.Fatalf("exact union rectangles overlap: %v and %v", rectangle, got[other])
			}
		}
	}
	if area != 80 {
		t.Fatalf("exact L-shaped union area = %d, want 80 (%v)", area, got)
	}
}

func rootDamageFrame() (*Frame, *dom.Node, *dom.Node) {
	root := dom.NewElement("main")
	fixed := dom.NewElement("header")
	viewport := Viewport{Width: 120, Height: 90}
	return &Frame{
		Viewport: viewport, paintArtifact: newPaintArtifactID(),
		DisplayList: DisplayList{Viewport: viewport, Commands: []Command{
			{Kind: FillRectCommand, Rect: Rect{Width: 120, Height: 90}, Color: color.NRGBA{R: 0x12, G: 0x1a, B: 0x2a, A: 0xff}},
			{Kind: FillRectCommand, Node: root, Rect: Rect{X: 8, Y: 8, Width: 85, Height: 120}, Color: color.NRGBA{R: 0x35, G: 0x67, B: 0x91, A: 0xff}},
			{Kind: DrawTextCommand, Node: root, Text: "scrolling root", X: 14, BaselineY: 35, FontSize: 14, FontFamily: FontFamilySansSerif, Color: opaqueWhite, textBounds: Rect{X: 14, Y: 20, Width: 90, Height: 18}},
			{Kind: FillRectCommand, Node: fixed, Rect: Rect{X: 5, Y: 4, Width: 55, Height: 13}, Color: color.NRGBA{R: 0xe0, G: 0x70, B: 0x40, A: 0xff}},
		}},
	}, root, fixed
}

func nestedDamageFrame() (*Frame, *dom.Node, Rect) {
	content := dom.NewElement("section")
	scroller := dom.NewElement("div")
	fixed := dom.NewElement("aside")
	outside := dom.NewElement("nav")
	viewport := Viewport{Width: 120, Height: 90}
	clip := Rect{X: 10, Y: 8, Width: 72, Height: 60}
	sourceImage := image.NewRGBA(image.Rect(0, 0, 3, 3))
	for y := 0; y < 3; y++ {
		for x := 0; x < 3; x++ {
			sourceImage.SetRGBA(x, y, color.RGBA{R: uint8(40 + x*60), G: uint8(30 + y*70), B: 0xb0, A: 0xff})
		}
	}
	radii := CornerRadii{TopLeft: CornerRadius{X: 4, Y: 4}, TopRight: CornerRadius{X: 4, Y: 4}, BottomRight: CornerRadius{X: 4, Y: 4}, BottomLeft: CornerRadius{X: 4, Y: 4}}
	return &Frame{
		Viewport: viewport, paintArtifact: newPaintArtifactID(),
		DisplayList: DisplayList{Viewport: viewport, Commands: []Command{
			{Kind: FillRectCommand, Rect: Rect{Width: 120, Height: 90}, Color: color.NRGBA{R: 0x0e, G: 0x16, B: 0x25, A: 0xff}},
			// Unrelated rounded and transformed UI outside the scrollport must not
			// poison an otherwise safe rectangular scroll update.
			{Kind: BeginClipCommand, Node: outside, Rect: Rect{X: 92, Y: 5, Width: 22, Height: 20}, Radii: radii},
			{Kind: BeginTransformCommand, Node: outside, Transform: AffineTransform{A: 1, D: 1, E: 2}},
			{Kind: FillRectCommand, Node: outside, Rect: Rect{X: 92, Y: 5, Width: 20, Height: 18}, Color: color.NRGBA{R: 0x70, G: 0x30, B: 0x90, A: 0xff}},
			{Kind: EndTransformCommand, Node: outside},
			{Kind: EndClipCommand, Node: outside},
			{Kind: FillRectCommand, Node: scroller, Rect: clip, Color: color.NRGBA{R: 0x1d, G: 0x2c, B: 0x40, A: 0xff}},
			{Kind: BeginClipCommand, Node: scroller, Rect: clip},
			{Kind: BeginOpacityCommand, Node: content, Opacity: 0.82},
			{Kind: FillRectCommand, Node: content, Rect: Rect{X: 13, Y: 10, Width: 62, Height: 24}, Radii: radii, Color: color.NRGBA{R: 0x38, G: 0x70, B: 0xa8, A: 0xe0}},
			{Kind: FillEllipseCommand, Node: content, Rect: Rect{X: 18, Y: 39, Width: 16, Height: 13}, Color: color.NRGBA{R: 0xf0, G: 0xc0, B: 0x50, A: 0xc8}},
			{Kind: DrawGradientCommand, Node: content, Rect: Rect{X: 38, Y: 38, Width: 31, Height: 17}, GradientKind: LinearGradient, GradientAngle: 0.4, GradientStopCount: 2,
				GradientStops: [maxPaintGradientStops]GradientStop{{Offset: 0, Color: color.NRGBA{R: 0x50, G: 0x90, B: 0xe0, A: 0xff}}, {Offset: 1, Color: color.NRGBA{R: 0xd0, G: 0x50, B: 0x90, A: 0xff}}}},
			{Kind: BeginTransformCommand, Node: content, Transform: AffineTransform{A: 1, D: 1, E: 1.5, F: 0.5}},
			{Kind: DrawImageCommand, Node: content, Rect: Rect{X: 57, Y: 13, Width: 12, Height: 11}, Image: sourceImage, Opacity: 0.75},
			{Kind: EndTransformCommand, Node: content},
			{Kind: DrawBoxShadowCommand, Node: content, Rect: Rect{X: 22, Y: 57, Width: 25, Height: 8}, Radii: radii, Color: color.NRGBA{A: 0x90}, ShadowOffsetX: 1, ShadowOffsetY: 1, ShadowBlur: 2},
			{Kind: DrawTextCommand, Node: content, Text: "messages", X: 16, BaselineY: 30, FontSize: 12, FontFamily: FontFamilySansSerif, Color: opaqueWhite, textBounds: Rect{X: 16, Y: 18, Width: 55, Height: 15}},
			{Kind: EndOpacityCommand, Node: content},
			{Kind: EndClipCommand, Node: scroller},
			{Kind: FillRectCommand, Node: fixed, Rect: Rect{X: 19, Y: 12, Width: 30, Height: 9}, Color: color.NRGBA{R: 0xe8, G: 0x64, B: 0x54, A: 0xff}},
			{Kind: DrawTextCommand, Node: fixed, Text: "fixed", X: 22, BaselineY: 20, FontSize: 8, FontFamily: FontFamilyMonospace, Color: opaqueWhite, textBounds: Rect{X: 22, Y: 12, Width: 25, Height: 10}},
		}},
	}, content, clip
}

func rectangularClipResizeFrame(commandOwnsClip bool) (*Frame, *dom.Node, Rect) {
	content := dom.NewElement("resizing-clip-content")
	fixed := dom.NewElement("fixed-overlay")
	viewport := Viewport{Width: 128, Height: 96}
	clip := Rect{X: 12, Y: 10, Width: 80, Height: 64}
	commands := []Command{
		{Kind: FillRectCommand, Rect: Rect{Width: 128, Height: 96}, Color: color.NRGBA{R: 0x13, G: 0x1d, B: 0x2a, A: 0xff}},
		{Kind: FillRectCommand, Node: content, Rect: Rect{X: 4, Y: 3, Width: 112, Height: 130}, Color: color.NRGBA{R: 0x2b, G: 0x67, B: 0x92, A: 0xff}},
		{Kind: DrawGradientCommand, Node: content, Rect: Rect{X: 18, Y: 18, Width: 68, Height: 48}, GradientKind: LinearGradient, GradientAngle: 0.6, GradientStopCount: 2,
			GradientStops: [maxPaintGradientStops]GradientStop{
				{Offset: 0, Color: color.NRGBA{R: 0xf0, G: 0x74, B: 0x58, A: 0xe0}},
				{Offset: 1, Color: color.NRGBA{R: 0x64, G: 0xd0, B: 0x98, A: 0xc0}},
			}},
		{Kind: FillRectCommand, Node: fixed, Rect: Rect{X: 42, Y: 22, Width: 24, Height: 17}, Color: color.NRGBA{R: 0xf0, G: 0xd0, B: 0x58, A: 0xd8}},
	}
	if commandOwnsClip {
		for index := range commands {
			if commands[index].Node == content {
				commands[index].HasClip = true
				commands[index].Clip = clip
			}
		}
	}
	return &Frame{
		Viewport: viewport, paintArtifact: newPaintArtifactID(),
		DisplayList: DisplayList{Viewport: viewport, Commands: commands},
	}, content, clip
}

func changedPaintArtifact(frame *Frame) *Frame {
	result := *frame
	result.paintArtifact = newPaintArtifactID()
	result.DisplayList = frame.DisplayList
	result.DisplayList.Commands = append([]Command(nil), frame.DisplayList.Commands...)
	result.DisplayList.Commands[0].Color.R ^= 0xff
	return &result
}

func roundedScrollProjection(base *Frame, content *dom.Node, clip Rect, offset float64) *Frame {
	copyFrame := *base
	copyFrame.DisplayList = base.DisplayList
	copyFrame.DisplayList.Commands = append([]Command(nil), base.DisplayList.Commands...)
	for index := range copyFrame.DisplayList.Commands {
		command := &copyFrame.DisplayList.Commands[index]
		if command.Kind == BeginClipCommand && command.Rect == clip {
			command.Radii = CornerRadii{TopLeft: CornerRadius{X: 6, Y: 6}, TopRight: CornerRadius{X: 6, Y: 6}, BottomRight: CornerRadius{X: 6, Y: 6}, BottomLeft: CornerRadius{X: 6, Y: 6}}
			break
		}
	}
	return TransformDisplayList(&copyFrame, map[*dom.Node]VisualTransform{content: {OffsetY: offset, HasClip: true, Clip: clip}})
}
