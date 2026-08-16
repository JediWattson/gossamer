package render_test

import (
	"image/color"
	"testing"

	"github.com/JediWattson/gossamer/internal/css"
	"github.com/JediWattson/gossamer/internal/dom"
	"github.com/JediWattson/gossamer/internal/render"
)

func TestGeneratedPseudoTextUsesPseudoStyleAndDOMOrder(t *testing.T) {
	t.Parallel()

	document, _ := pseudoRenderDocument(`
		#target { color:#0000ff }
		#target::before { content:"before"; color:#ff0000 }
		#target::after { content:"after"; color:#008000 }
	`, "middle")
	frame, err := render.Render(document, render.Viewport{Width: 320, Height: 160})
	if err != nil {
		t.Fatal(err)
	}

	var commands []render.Command
	for _, command := range frame.DisplayList.Commands {
		if command.Kind == render.DrawTextCommand {
			commands = append(commands, command)
		}
	}
	if len(commands) != 3 {
		t.Fatalf("target text commands = %#v, want before/middle/after", commands)
	}
	wantText := []string{"before", "middle", "after"}
	wantPseudo := []css.PseudoElement{css.PseudoElementBefore, css.PseudoElementNone, css.PseudoElementAfter}
	wantColor := []color.NRGBA{
		{R: 0xff, A: 0xff},
		{B: 0xff, A: 0xff},
		{G: 0x80, A: 0xff},
	}
	for index := range commands {
		if commands[index].Text != wantText[index] || commands[index].Pseudo != wantPseudo[index] || commands[index].Color != wantColor[index] {
			t.Errorf("command %d = %#v, want text=%q pseudo=%s color=%#v", index, commands[index], wantText[index], wantPseudo[index], wantColor[index])
		}
	}
}

func TestGeneratedBlockPseudoHasSeparateGeometryPaintAndOriginHitTarget(t *testing.T) {
	t.Parallel()

	document, target := pseudoRenderDocument(`
		#target::before {
			content:"";
			display:block;
			width:50%;
			height:12px;
			background-color:#ff0000;
		}
	`, "body")
	frame, err := render.Render(document, render.Viewport{Width: 320, Height: 180})
	if err != nil {
		t.Fatal(err)
	}
	pseudoGeometry, ok := frame.Layout.PseudoGeometry(target, css.PseudoElementBefore)
	if !ok {
		t.Fatal("::before block has no pseudo geometry")
	}
	if pseudoGeometry.ContentBounds.Height != 12 || pseudoGeometry.ContentBounds.Width <= 0 {
		t.Fatalf("pseudo geometry = %#v", pseudoGeometry)
	}
	principal, ok := frame.Layout.Geometry(target)
	if !ok {
		t.Fatal("origin has no principal geometry")
	}
	if principal.ContentBounds == pseudoGeometry.ContentBounds {
		t.Fatal("pseudo box overwrote principal element geometry")
	}

	painted := false
	for _, command := range frame.DisplayList.Commands {
		if command.Kind == render.FillRectCommand && command.Node == target && command.Pseudo == css.PseudoElementBefore && command.Color == (color.NRGBA{R: 0xff, A: 0xff}) {
			painted = true
			break
		}
	}
	if !painted {
		t.Fatal("pseudo background was not painted with pseudo ownership")
	}
	hit := render.HitTest(frame,
		pseudoGeometry.Bounds.X+pseudoGeometry.Bounds.Width/2,
		pseudoGeometry.Bounds.Y+pseudoGeometry.Bounds.Height/2,
	)
	if hit != target {
		t.Fatalf("pseudo hit target = %p, want originating element %p", hit, target)
	}
}

func TestGeneratedPseudoSuppressionForDisplayNoneAndReplacedElement(t *testing.T) {
	t.Parallel()

	document, target := pseudoRenderDocument(`#target::before { content:"hidden"; display:none }`, "body")
	frame, err := render.Render(document, render.Viewport{Width: 320, Height: 160})
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := frame.Layout.PseudoGeometry(target, css.PseudoElementBefore); ok {
		t.Fatal("display:none pseudo has geometry")
	}
	for _, command := range frame.DisplayList.Commands {
		if command.Node == target && command.Pseudo == css.PseudoElementBefore {
			t.Fatalf("display:none pseudo painted %#v", command)
		}
	}

	imageDocument := dom.NewDocument()
	html := dom.NewElement("html")
	head := dom.NewElement("head")
	styleElement := dom.NewElement("style")
	styleElement.AppendChild(dom.NewText(`img::before { content:"not generated"; display:block; height:10px }`))
	head.AppendChild(styleElement)
	body := dom.NewElement("body")
	image := dom.NewElement("img", dom.Attribute{Name: "style", Value: "width:10px;height:10px"})
	body.AppendChild(image)
	html.AppendChild(head)
	html.AppendChild(body)
	imageDocument.AppendChild(html)
	imageFrame, err := render.Render(imageDocument, render.Viewport{Width: 320, Height: 160})
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := imageFrame.Layout.PseudoGeometry(image, css.PseudoElementBefore); ok {
		t.Fatal("replaced img generated ::before geometry")
	}
}

func pseudoRenderDocument(source, text string) (*dom.Node, *dom.Node) {
	document := dom.NewDocument()
	html := dom.NewElement("html")
	head := dom.NewElement("head")
	styleElement := dom.NewElement("style")
	styleElement.AppendChild(dom.NewText(source))
	head.AppendChild(styleElement)
	body := dom.NewElement("body")
	target := dom.NewElement("div", dom.Attribute{Name: "id", Value: "target"})
	target.AppendChild(dom.NewText(text))
	body.AppendChild(target)
	html.AppendChild(head)
	html.AppendChild(body)
	document.AppendChild(html)
	return document, target
}
