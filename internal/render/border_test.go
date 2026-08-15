package render_test

import (
	"image"
	"image/color"
	"testing"

	"github.com/JediWattson/gossamer/internal/dom"
	"github.com/JediWattson/gossamer/internal/render"
)

func TestRenderSolidBorderShorthandAddsGeometryAndPaint(t *testing.T) {
	t.Parallel()

	document, body := boxModelDocument()
	bordered := dom.NewElement("div", dom.Attribute{
		Name:  "style",
		Value: "width: 20px; height: 12px; border: 4px solid #123456",
	})
	body.AppendChild(bordered)

	frame, err := render.Render(document, render.Viewport{Width: 80, Height: 60})
	if err != nil {
		t.Fatalf("Render() error = %v", err)
	}
	box := findBox(frame.Root, bordered)
	if box == nil {
		t.Fatal("bordered box not found")
	}
	assertNear(t, "border box width", box.Bounds.Width, 28)
	assertNear(t, "border box height", box.Bounds.Height, 20)
	assertNear(t, "border content x", box.ContentBounds.X, box.Bounds.X+4)
	assertNear(t, "border content y", box.ContentBounds.Y, box.Bounds.Y+4)
	assertNear(t, "border content width", box.ContentBounds.Width, 20)
	assertNear(t, "border content height", box.ContentBounds.Height, 12)

	wantBorder := color.NRGBA{R: 0x12, G: 0x34, B: 0x56, A: 0xff}
	if got := borderTestFillCommands(frame.DisplayList.Commands, wantBorder); len(got) != 4 {
		t.Errorf("border fill command count = %d, want four sides", len(got))
	}
	painted, err := render.Rasterize(frame)
	if err != nil {
		t.Fatalf("Rasterize() error = %v", err)
	}
	assertBorderPixel(t, painted, 1, 1, wantBorder)
	assertBorderPixel(t, painted, 26, 10, wantBorder)
	assertBorderPixel(t, painted, 10, 18, wantBorder)
	assertBorderPixel(t, painted, 10, 10, color.NRGBA{R: 0xff, G: 0xff, B: 0xff, A: 0xff})
}

func TestRenderBorderSideOverrideRemovesTopBorder(t *testing.T) {
	t.Parallel()

	document, body := boxModelDocument()
	bordered := dom.NewElement("div", dom.Attribute{
		Name:  "style",
		Value: "width: 20px; height: 20px; border: 3px solid #cc0000; border-top: 0",
	})
	body.AppendChild(bordered)

	frame, err := render.Render(document, render.Viewport{Width: 80, Height: 60})
	if err != nil {
		t.Fatalf("Render() error = %v", err)
	}
	box := findBox(frame.Root, bordered)
	if box == nil {
		t.Fatal("bordered box not found")
	}
	assertNear(t, "side override border width", box.Bounds.Width, 26)
	assertNear(t, "side override border height", box.Bounds.Height, 23)
	assertNear(t, "side override content x", box.ContentBounds.X, box.Bounds.X+3)
	assertNear(t, "side override content y", box.ContentBounds.Y, box.Bounds.Y)

	painted, err := render.Rasterize(frame)
	if err != nil {
		t.Fatalf("Rasterize() error = %v", err)
	}
	wantBorder := color.NRGBA{R: 0xcc, A: 0xff}
	assertBorderPixel(t, painted, 10, 0, color.NRGBA{R: 0xff, G: 0xff, B: 0xff, A: 0xff})
	assertBorderPixel(t, painted, 0, 0, wantBorder)
	assertBorderPixel(t, painted, 10, 22, wantBorder)
}

func TestRenderBorderSideLonghandsUseIndependentWidthsAndColors(t *testing.T) {
	t.Parallel()

	document, body := boxModelDocument()
	bordered := dom.NewElement("div", dom.Attribute{
		Name: "style",
		Value: `width: 20px; height: 20px;
			border-top-width: 2px; border-top-style: solid; border-top-color: #ff0000;
			border-right-width: 3px; border-right-style: solid; border-right-color: #00ff00;
			border-bottom-width: 4px; border-bottom-style: solid; border-bottom-color: #0000ff;
			border-left-width: 5px; border-left-style: solid; border-left-color: #ffff00`,
	})
	body.AppendChild(bordered)

	frame, err := render.Render(document, render.Viewport{Width: 80, Height: 60})
	if err != nil {
		t.Fatalf("Render() error = %v", err)
	}
	box := findBox(frame.Root, bordered)
	if box == nil {
		t.Fatal("bordered box not found")
	}
	assertNear(t, "longhand border width", box.Bounds.Width, 28)
	assertNear(t, "longhand border height", box.Bounds.Height, 26)
	assertNear(t, "longhand content x", box.ContentBounds.X, box.Bounds.X+5)
	assertNear(t, "longhand content y", box.ContentBounds.Y, box.Bounds.Y+2)

	painted, err := render.Rasterize(frame)
	if err != nil {
		t.Fatalf("Rasterize() error = %v", err)
	}
	assertBorderPixel(t, painted, 10, 0, color.NRGBA{R: 0xff, A: 0xff})
	assertBorderPixel(t, painted, 27, 10, color.NRGBA{G: 0xff, A: 0xff})
	assertBorderPixel(t, painted, 10, 25, color.NRGBA{B: 0xff, A: 0xff})
	assertBorderPixel(t, painted, 0, 10, color.NRGBA{R: 0xff, G: 0xff, A: 0xff})
	assertBorderPixel(t, painted, 10, 10, color.NRGBA{R: 0xff, G: 0xff, B: 0xff, A: 0xff})
}

func TestRenderBackgroundPaintsUnderSolidBorder(t *testing.T) {
	t.Parallel()

	document, body := boxModelDocument()
	bordered := dom.NewElement("div", dom.Attribute{
		Name:  "style",
		Value: "width: 20px; height: 20px; background: #abcdef; border: 4px solid #102030",
	})
	body.AppendChild(bordered)

	frame, err := render.Render(document, render.Viewport{Width: 80, Height: 60})
	if err != nil {
		t.Fatalf("Render() error = %v", err)
	}
	box := findBox(frame.Root, bordered)
	if box == nil {
		t.Fatal("bordered box not found")
	}
	wantBackground := color.NRGBA{R: 0xab, G: 0xcd, B: 0xef, A: 0xff}
	wantBorder := color.NRGBA{R: 0x10, G: 0x20, B: 0x30, A: 0xff}
	backgroundIndex := commandIndex(frame.DisplayList.Commands, func(command render.Command) bool {
		return command.Kind == render.FillRectCommand && command.Color == wantBackground && command.Rect == box.Bounds
	})
	borderIndex := commandIndex(frame.DisplayList.Commands, func(command render.Command) bool {
		return command.Kind == render.FillRectCommand && command.Color == wantBorder
	})
	if backgroundIndex < 0 || borderIndex < 0 {
		t.Fatalf("paint command indexes = background:%d border:%d, want both present", backgroundIndex, borderIndex)
	}
	if backgroundIndex >= borderIndex {
		t.Errorf("paint command indexes = background:%d border:%d, want background below border", backgroundIndex, borderIndex)
	}

	painted, err := render.Rasterize(frame)
	if err != nil {
		t.Fatalf("Rasterize() error = %v", err)
	}
	assertBorderPixel(t, painted, 1, 1, wantBorder)
	assertBorderPixel(t, painted, 10, 10, wantBackground)
}

func TestRenderBorderParticipatesInMaxWidthAutoMarginsAndPadding(t *testing.T) {
	t.Parallel()

	document, body := boxModelDocument()
	bordered := dom.NewElement("main", dom.Attribute{
		Name:  "style",
		Value: "max-width: 200px; margin: 0 auto; padding: 10px 20px; border: 5px solid #334455",
	})
	bordered.AppendChild(dom.NewText("Inset content"))
	body.AppendChild(bordered)

	frame, err := render.Render(document, render.Viewport{Width: 400, Height: 120})
	if err != nil {
		t.Fatalf("Render() error = %v", err)
	}
	box := findBox(frame.Root, bordered)
	if box == nil {
		t.Fatal("bordered box not found")
	}
	assertNear(t, "centered border box x", box.Bounds.X, 75)
	assertNear(t, "centered border box width", box.Bounds.Width, 250)
	assertNear(t, "bordered content x", box.ContentBounds.X, 100)
	assertNear(t, "bordered content y", box.ContentBounds.Y, 15)
	assertNear(t, "bordered content width", box.ContentBounds.Width, 200)
	fragment := findTextFragment(collectTextFragments(frame.Root), "Inset content")
	if fragment == nil {
		t.Fatal("inset text fragment not found")
	}
	assertNear(t, "inset text x", fragment.X, box.ContentBounds.X)
}

func TestRenderBorderFixedHeightAdvancesFollowingBlock(t *testing.T) {
	t.Parallel()

	document, body := boxModelDocument()
	bordered := dom.NewElement("div", dom.Attribute{
		Name:  "style",
		Value: "width: 100px; height: 50px; padding: 5px 7px; border: 3px solid #556677",
	})
	bordered.AppendChild(dom.NewText("Fixed"))
	following := dom.NewElement("div")
	following.AppendChild(dom.NewText("Following"))
	body.AppendChild(bordered)
	body.AppendChild(following)

	frame, err := render.Render(document, render.Viewport{Width: 240, Height: 140})
	if err != nil {
		t.Fatalf("Render() error = %v", err)
	}
	box := findBox(frame.Root, bordered)
	followingBox := findBox(frame.Root, following)
	if box == nil || followingBox == nil {
		t.Fatalf("layout boxes = bordered:%#v following:%#v, want both", box, followingBox)
	}
	assertNear(t, "fixed bordered content x", box.ContentBounds.X, box.Bounds.X+10)
	assertNear(t, "fixed bordered content y", box.ContentBounds.Y, box.Bounds.Y+8)
	assertNear(t, "fixed bordered content width", box.ContentBounds.Width, 100)
	assertNear(t, "fixed bordered content height", box.ContentBounds.Height, 50)
	assertNear(t, "fixed border box width", box.Bounds.Width, 120)
	assertNear(t, "fixed border box height", box.Bounds.Height, 66)
	assertNear(t, "following block y", followingBox.Bounds.Y, box.Bounds.Y+box.Bounds.Height)
}

func TestRenderInvalidHigherSpecificityBorderDoesNotMaskValidDeclaration(t *testing.T) {
	t.Parallel()

	document, body := boxModelDocument()
	borderTestAddStyle(document, `
		.target { border: 4px solid #123456; }
		#target { border: definitely-invalid; }
	`)
	target := dom.NewElement("div",
		dom.Attribute{Name: "id", Value: "target"},
		dom.Attribute{Name: "class", Value: "target"},
		dom.Attribute{Name: "style", Value: "width: 20px; height: 10px"},
	)
	body.AppendChild(target)

	frame, err := render.Render(document, render.Viewport{Width: 80, Height: 60})
	if err != nil {
		t.Fatalf("Render() error = %v", err)
	}
	box := findBox(frame.Root, target)
	if box == nil {
		t.Fatal("bordered target box not found")
	}
	borderTestAssertEdges(t, "valid lower border", box.Border, render.Edges{Top: 4, Right: 4, Bottom: 4, Left: 4})
	if got := len(borderTestFillCommands(frame.DisplayList.Commands, color.NRGBA{R: 0x12, G: 0x34, B: 0x56, A: 0xff})); got != 4 {
		t.Errorf("valid lower border fill count = %d, want 4", got)
	}
}

func TestRenderBorderShorthandAndSideParticipateInCascade(t *testing.T) {
	t.Parallel()

	red := color.NRGBA{R: 0xa0, A: 0xff}
	blue := color.NRGBA{B: 0xb0, A: 0xff}
	tests := []struct {
		name      string
		css       string
		wantTop   float64
		wantColor color.NRGBA
	}{
		{
			name:    "higher specificity shorthand beats side",
			css:     `#target { border: 6px solid #a00000; } .target { border-top: 1px solid #0000b0; }`,
			wantTop: 6, wantColor: red,
		},
		{
			name:    "higher specificity side beats shorthand",
			css:     `#target { border-top: 1px solid #0000b0; } .target { border: 6px solid #a00000; }`,
			wantTop: 1, wantColor: blue,
		},
		{
			name:    "important shorthand beats more specific side",
			css:     `.target { border: 6px solid #a00000 !important; } #target { border-top: 1px solid #0000b0; }`,
			wantTop: 6, wantColor: red,
		},
		{
			name:    "important side beats more specific shorthand",
			css:     `.target { border-top: 1px solid #0000b0 !important; } #target { border: 6px solid #a00000; }`,
			wantTop: 1, wantColor: blue,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			document, body := boxModelDocument()
			borderTestAddStyle(document, test.css)
			target := dom.NewElement("div",
				dom.Attribute{Name: "id", Value: "target"},
				dom.Attribute{Name: "class", Value: "target"},
				dom.Attribute{Name: "style", Value: "width: 20px; height: 10px"},
			)
			body.AppendChild(target)
			frame, err := render.Render(document, render.Viewport{Width: 80, Height: 60})
			if err != nil {
				t.Fatalf("Render() error = %v", err)
			}
			box := findBox(frame.Root, target)
			if box == nil {
				t.Fatal("bordered target box not found")
			}
			assertNear(t, "cascaded top border width", box.Border.Top, test.wantTop)
			assertNear(t, "unaffected right border width", box.Border.Right, 6)
			borderTestAssertFillRect(t, frame.DisplayList.Commands, test.wantColor, render.Rect{
				X: box.Bounds.X, Y: box.Bounds.Y, Width: box.Bounds.Width, Height: test.wantTop,
			})
		})
	}
}

func TestRenderBorderWidthAndColorShorthandsExpandOneToFourSides(t *testing.T) {
	t.Parallel()

	red := color.NRGBA{R: 0x11, A: 0xff}
	green := color.NRGBA{G: 0x22, A: 0xff}
	blue := color.NRGBA{B: 0x33, A: 0xff}
	yellow := color.NRGBA{R: 0x44, G: 0x44, A: 0xff}
	tests := []struct {
		name, widths, colors string
		wantEdges            render.Edges
		wantColors           [4]color.NRGBA
	}{
		{"one", "1px", "#110000", render.Edges{1, 1, 1, 1}, [4]color.NRGBA{red, red, red, red}},
		{"two", "1px 2px", "#110000 #002200", render.Edges{1, 2, 1, 2}, [4]color.NRGBA{red, green, red, green}},
		{"three", "1px 2px 3px", "#110000 #002200 #000033", render.Edges{1, 2, 3, 2}, [4]color.NRGBA{red, green, blue, green}},
		{"four", "1px 2px 3px 4px", "#110000 #002200 #000033 #444400", render.Edges{1, 2, 3, 4}, [4]color.NRGBA{red, green, blue, yellow}},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			document, body := boxModelDocument()
			target := dom.NewElement("div", dom.Attribute{Name: "style", Value: "width:20px;height:20px;border-style:solid;border-width:" + test.widths + ";border-color:" + test.colors})
			body.AppendChild(target)
			frame, err := render.Render(document, render.Viewport{Width: 80, Height: 80})
			if err != nil {
				t.Fatalf("Render() error = %v", err)
			}
			box := findBox(frame.Root, target)
			if box == nil {
				t.Fatal("bordered target box not found")
			}
			borderTestAssertEdges(t, "expanded border", box.Border, test.wantEdges)
			borderTestAssertSideFills(t, frame.DisplayList.Commands, box, test.wantColors)
		})
	}
}

func TestRenderBorderStyleShorthandExpandsOneToFourSides(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name, styles string
		want         render.Edges
	}{
		{"one", "solid", render.Edges{2, 2, 2, 2}},
		{"two", "solid none", render.Edges{2, 0, 2, 0}},
		{"three", "none solid solid", render.Edges{0, 2, 2, 2}},
		{"four", "solid none hidden solid", render.Edges{2, 0, 0, 2}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			document, body := boxModelDocument()
			target := dom.NewElement("div", dom.Attribute{Name: "style", Value: "width:20px;height:20px;border-width:2px;border-color:#334455;border-style:" + test.styles})
			body.AppendChild(target)
			frame, err := render.Render(document, render.Viewport{Width: 80, Height: 80})
			if err != nil {
				t.Fatalf("Render() error = %v", err)
			}
			box := findBox(frame.Root, target)
			if box == nil {
				t.Fatal("bordered target box not found")
			}
			borderTestAssertEdges(t, "expanded border styles", box.Border, test.want)
		})
	}
}

func TestRenderCurrentColorAndTransparentBordersRetainGeometry(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name, border string
		wantColor    color.NRGBA
		wantFills    int
	}{
		{"currentColor", "currentColor", color.NRGBA{R: 0x24, G: 0x68, B: 0xac, A: 0xff}, 4},
		{"transparent", "transparent", color.NRGBA{}, 0},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			document, body := boxModelDocument()
			target := dom.NewElement("div", dom.Attribute{Name: "style", Value: "color:#2468ac;width:20px;height:10px;border:3px solid " + test.border})
			body.AppendChild(target)
			frame, err := render.Render(document, render.Viewport{Width: 80, Height: 60})
			if err != nil {
				t.Fatalf("Render() error = %v", err)
			}
			box := findBox(frame.Root, target)
			if box == nil {
				t.Fatal("bordered target box not found")
			}
			borderTestAssertEdges(t, "colored border geometry", box.Border, render.Edges{3, 3, 3, 3})
			if got := len(borderTestFillCommands(frame.DisplayList.Commands, test.wantColor)); got != test.wantFills {
				t.Errorf("border fill count = %d, want %d", got, test.wantFills)
			}
		})
	}
}

func TestRenderTopAndBottomBordersStopParentChildMarginCollapse(t *testing.T) {
	t.Parallel()

	document, body := boxModelDocument()
	parent := dom.NewElement("div", dom.Attribute{Name: "style", Value: "width:100px;border-top:2px solid;border-bottom:3px solid"})
	child := dom.NewElement("div", dom.Attribute{Name: "style", Value: "height:10px;margin:20px 0 24px"})
	parent.AppendChild(child)
	body.AppendChild(parent)
	frame, err := render.Render(document, render.Viewport{Width: 160, Height: 100})
	if err != nil {
		t.Fatalf("Render() error = %v", err)
	}
	parentBox := findBox(frame.Root, parent)
	childBox := findBox(frame.Root, child)
	if parentBox == nil || childBox == nil {
		t.Fatalf("layout boxes = parent:%#v child:%#v, want both", parentBox, childBox)
	}
	assertNear(t, "child top margin inside top border", childBox.Bounds.Y, parentBox.ContentBounds.Y+20)
	assertNear(t, "child bottom margin inside bottom border", parentBox.Bounds.Y+parentBox.Bounds.Height, childBox.Bounds.Y+childBox.Bounds.Height+24+3)
}

func TestRenderBlockImageBordersWrapDecodedAndMissingContent(t *testing.T) {
	t.Parallel()

	decoded := image.NewNRGBA(image.Rect(0, 0, 20, 10))
	tests := []struct {
		name      string
		image     image.Image
		wantDraws int
	}{
		{"decoded", decoded, 1},
		{"missing", nil, 0},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			document, body := boxModelDocument()
			imageNode := dom.NewElement("img",
				dom.Attribute{Name: "src", Value: test.name + ".png"},
				dom.Attribute{Name: "style", Value: "display:block;width:20px;height:10px;padding:2px;border:3px solid #765432"},
			)
			body.AppendChild(imageNode)
			resources := render.Resources{}
			if test.image != nil {
				resources.Images = map[*dom.Node]image.Image{imageNode: test.image}
			}
			frame, err := render.RenderWithResources(document, render.Viewport{Width: 100, Height: 60}, resources)
			if err != nil {
				t.Fatalf("RenderWithResources() error = %v", err)
			}
			box := findBox(frame.Root, imageNode)
			if box == nil {
				t.Fatal("block image box not found")
			}
			borderTestAssertEdges(t, "block image border", box.Border, render.Edges{3, 3, 3, 3})
			assertNear(t, "block image outer width", box.Bounds.Width, 30)
			assertNear(t, "block image outer height", box.Bounds.Height, 20)
			var draws []render.Command
			for _, command := range frame.DisplayList.Commands {
				if command.Kind == render.DrawImageCommand {
					draws = append(draws, command)
				}
			}
			if len(draws) != test.wantDraws {
				t.Fatalf("image draw count = %d, want %d", len(draws), test.wantDraws)
			}
			if len(draws) == 1 && draws[0].Rect != box.ContentBounds {
				t.Errorf("image draw rect = %#v, want content bounds %#v", draws[0].Rect, box.ContentBounds)
			}
			if got := len(borderTestFillCommands(frame.DisplayList.Commands, color.NRGBA{R: 0x76, G: 0x54, B: 0x32, A: 0xff})); got != 4 {
				t.Errorf("block image border fill count = %d, want 4", got)
			}
		})
	}
}

func TestRenderBorderAndPaddingDoNotMoveOutsideListMarkerAnchor(t *testing.T) {
	t.Parallel()

	document, body := markerTestDocument()
	for _, decoration := range []string{"", "padding-left:30px;border-left:6px solid #123456"} {
		list := dom.NewElement("ul", dom.Attribute{Name: "style", Value: "margin:0"})
		item := dom.NewElement("li", dom.Attribute{Name: "style", Value: decoration})
		item.AppendChild(dom.NewText("Anchored"))
		list.AppendChild(item)
		body.AppendChild(list)
	}
	frame, err := render.Render(document, render.Viewport{Width: 280, Height: 140})
	if err != nil {
		t.Fatalf("Render() error = %v", err)
	}
	markers := markerTestCommands(frame.DisplayList.Commands, "•")
	items := markerTestCommands(frame.DisplayList.Commands, "Anchored")
	if len(markers) != 2 || len(items) != 2 {
		t.Fatalf("command counts = markers:%d items:%d, want 2 each", len(markers), len(items))
	}
	assertNear(t, "bordered outside marker anchor", markers[1].X, markers[0].X)
	assertNear(t, "border and padding content shift", items[1].X-items[0].X, 36)
}

func borderTestFillCommands(commands []render.Command, want color.NRGBA) []render.Command {
	var matches []render.Command
	for _, command := range commands {
		if command.Kind == render.FillRectCommand && command.Color == want {
			matches = append(matches, command)
		}
	}
	return matches
}

func borderTestAddStyle(document *dom.Node, source string) {
	style := dom.NewElement("style")
	style.AppendChild(dom.NewText(source))
	document.Children[0].Children[0].AppendChild(style)
}

func borderTestAssertEdges(t *testing.T, name string, got, want render.Edges) {
	t.Helper()
	assertNear(t, name+" top", got.Top, want.Top)
	assertNear(t, name+" right", got.Right, want.Right)
	assertNear(t, name+" bottom", got.Bottom, want.Bottom)
	assertNear(t, name+" left", got.Left, want.Left)
}

func borderTestAssertSideFills(t *testing.T, commands []render.Command, box *render.Box, colors [4]color.NRGBA) {
	t.Helper()
	edges := box.Border
	borderTestAssertFillRect(t, commands, colors[0], render.Rect{X: box.Bounds.X, Y: box.Bounds.Y, Width: box.Bounds.Width, Height: edges.Top})
	borderTestAssertFillRect(t, commands, colors[1], render.Rect{X: box.Bounds.X + box.Bounds.Width - edges.Right, Y: box.Bounds.Y, Width: edges.Right, Height: box.Bounds.Height})
	borderTestAssertFillRect(t, commands, colors[2], render.Rect{X: box.Bounds.X, Y: box.Bounds.Y + box.Bounds.Height - edges.Bottom, Width: box.Bounds.Width, Height: edges.Bottom})
	borderTestAssertFillRect(t, commands, colors[3], render.Rect{X: box.Bounds.X, Y: box.Bounds.Y, Width: edges.Left, Height: box.Bounds.Height})
}

func borderTestAssertFillRect(t *testing.T, commands []render.Command, wantColor color.NRGBA, wantRect render.Rect) {
	t.Helper()
	index := commandIndex(commands, func(command render.Command) bool {
		return command.Kind == render.FillRectCommand && command.Color == wantColor &&
			near(command.Rect.X, wantRect.X) && near(command.Rect.Y, wantRect.Y) &&
			near(command.Rect.Width, wantRect.Width) && near(command.Rect.Height, wantRect.Height)
	})
	if index < 0 {
		t.Errorf("border fill not found: color=%#v rect=%#v", wantColor, wantRect)
	}
}

func assertBorderPixel(t *testing.T, painted interface{ At(int, int) color.Color }, x, y int, want color.NRGBA) {
	t.Helper()
	got := color.NRGBAModel.Convert(painted.At(x, y)).(color.NRGBA)
	if got != want {
		t.Errorf("pixel (%d,%d) = %#v, want %#v", x, y, got, want)
	}
}
