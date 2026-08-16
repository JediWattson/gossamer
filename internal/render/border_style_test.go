package render_test

import (
	"image/color"
	"testing"

	"github.com/JediWattson/gossamer/internal/dom"
	"github.com/JediWattson/gossamer/internal/render"
)

func TestRenderEveryBorderLineStyleWithBoundedPaintCommands(t *testing.T) {
	t.Parallel()

	document, body := boxModelDocument()
	styles := []string{"dotted", "dashed", "solid", "double", "groove", "ridge", "inset", "outset"}
	nodes := make(map[string]*dom.Node, len(styles))
	for _, borderStyle := range styles {
		node := dom.NewElement("div", dom.Attribute{Name: "style", Value: "width:60px;height:10px;border-top:6px " + borderStyle + " #6480a0"})
		nodes[borderStyle] = node
		body.AppendChild(node)
	}
	frame, err := render.Render(document, render.Viewport{Width: 100, Height: 160})
	if err != nil {
		t.Fatal(err)
	}
	base := color.NRGBA{R: 0x64, G: 0x80, B: 0xa0, A: 0xff}
	for _, borderStyle := range styles {
		node := nodes[borderStyle]
		box := findBox(frame.Root, node)
		if box == nil || box.Border.Top != 6 {
			t.Fatalf("%s border box = %#v, want 6px top edge", borderStyle, box)
		}
		commands := borderCommandsForNode(frame.DisplayList.Commands, node)
		if len(commands) == 0 || len(commands) > 64 {
			t.Fatalf("%s paint command count = %d, want bounded nonempty output", borderStyle, len(commands))
		}
		switch borderStyle {
		case "dotted":
			if len(commands) < 2 || commands[0].Kind != render.FillEllipseCommand {
				t.Fatalf("dotted commands = %#v, want repeated ellipses", commands)
			}
		case "dashed":
			if len(commands) < 2 || commands[0].Kind != render.FillRectCommand || borderCommandExtent(commands) >= box.Bounds.Width {
				t.Fatalf("dashed commands = %#v, want separated rectangular dashes", commands)
			}
		case "double":
			if len(commands) != 2 || commands[0].Rect.Height != 2 || commands[1].Rect.Height != 2 {
				t.Fatalf("double commands = %#v, want two 2px lines", commands)
			}
		case "groove", "ridge":
			if len(commands) != 2 || commands[0].Color == commands[1].Color || commands[0].Color == base || commands[1].Color == base {
				t.Fatalf("%s commands = %#v, want two shaded halves", borderStyle, commands)
			}
		case "inset", "outset":
			if len(commands) != 1 || commands[0].Color == base {
				t.Fatalf("%s command = %#v, want one shaded edge", borderStyle, commands)
			}
		case "solid":
			if len(commands) != 1 || commands[0].Color != base {
				t.Fatalf("solid command = %#v", commands)
			}
		}
	}
}

func TestRasterizeDottedAndDoubleBordersPreservesGaps(t *testing.T) {
	t.Parallel()

	document, body := boxModelDocument()
	dotted := dom.NewElement("div", dom.Attribute{Name: "style", Value: "width:30px;height:4px;border-top:6px dotted #000"})
	double := dom.NewElement("div", dom.Attribute{Name: "style", Value: "width:30px;height:4px;border-top:6px double #000"})
	body.AppendChild(dotted)
	body.AppendChild(double)
	frame, err := render.Render(document, render.Viewport{Width: 50, Height: 30})
	if err != nil {
		t.Fatal(err)
	}
	painted, err := render.Rasterize(frame)
	if err != nil {
		t.Fatal(err)
	}
	dottedBox, doubleBox := findBox(frame.Root, dotted), findBox(frame.Root, double)
	if dottedBox == nil || doubleBox == nil {
		t.Fatal("border boxes missing")
	}
	black := color.NRGBA{A: 0xff}
	white := color.NRGBA{R: 0xff, G: 0xff, B: 0xff, A: 0xff}
	assertBorderPixel(t, painted, int(dottedBox.Bounds.X+3), int(dottedBox.Bounds.Y+3), black)
	assertBorderPixel(t, painted, int(dottedBox.Bounds.X+9), int(dottedBox.Bounds.Y), white)
	assertBorderPixel(t, painted, int(doubleBox.Bounds.X+10), int(doubleBox.Bounds.Y), black)
	assertBorderPixel(t, painted, int(doubleBox.Bounds.X+10), int(doubleBox.Bounds.Y+3), white)
	assertBorderPixel(t, painted, int(doubleBox.Bounds.X+10), int(doubleBox.Bounds.Y+5), black)
}

func borderCommandsForNode(commands []render.Command, node *dom.Node) []render.Command {
	var result []render.Command
	for _, command := range commands {
		if command.Node == node && (command.Kind == render.FillRectCommand || command.Kind == render.FillEllipseCommand) {
			result = append(result, command)
		}
	}
	return result
}

func borderCommandExtent(commands []render.Command) float64 {
	total := 0.0
	for _, command := range commands {
		total += command.Rect.Width
	}
	return total
}
