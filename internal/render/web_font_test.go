package render_test

import (
	"testing"

	"github.com/JediWattson/gossamer/internal/css"
	"github.com/JediWattson/gossamer/internal/dom"
	"github.com/JediWattson/gossamer/internal/render"
	"golang.org/x/image/font/gofont/gobold"
	"golang.org/x/image/font/gofont/gomonobold"
)

func TestLoadedWebFontSelectionSurvivesLayoutAndRasterReplay(t *testing.T) {
	document, body := boxModelDocument()
	text := dom.NewText("web font replay")
	element := dom.NewElement("p", dom.Attribute{Name: "style", Value: `font-family: "Fixture Face", serif; font-weight: bold; font-size: 24px`})
	element.AppendChild(text)
	body.AppendChild(element)

	regularData := append([]byte(nil), gobold.TTF...)
	boldData := append([]byte(nil), gomonobold.TTF...)
	frame, err := render.RenderWithResources(document, render.Viewport{Width: 400, Height: 120}, render.Resources{Fonts: []render.WebFontFace{
		{Family: "Fixture Face", Style: render.FontStyleNormal, WeightMin: 400, WeightMax: 400, Source: "regular.ttf", Data: regularData},
		{Family: "fixture face", Style: render.FontStyleNormal, WeightMin: 700, WeightMax: 700, Source: "bold.ttf", Data: boldData},
	}})
	if err != nil {
		t.Fatalf("RenderWithResources() error = %v", err)
	}
	var command *render.Command
	for index := range frame.DisplayList.Commands {
		candidate := &frame.DisplayList.Commands[index]
		if candidate.Kind == render.DrawTextCommand && candidate.Node == text {
			command = candidate
			break
		}
	}
	if command == nil {
		t.Fatal("no text command for web-font text")
	}
	if command.WebFont != 2 {
		t.Fatalf("selected web font = %d, want second bold face", command.WebFont)
	}
	if len(frame.Fonts) != 2 {
		t.Fatalf("retained frame fonts = %d, want 2", len(frame.Fonts))
	}

	// The frame owns the exact bytes used during layout; replay must not depend
	// on caller-owned resource slices remaining unchanged.
	for index := range regularData {
		regularData[index] = 0
	}
	for index := range boldData {
		boldData[index] = 0
	}
	if _, err := render.Rasterize(frame); err != nil {
		t.Fatalf("Rasterize() with retained web font error = %v", err)
	}
}

func TestWebFontUnicodeRangesSelectTheSubsetContainingTheRun(t *testing.T) {
	document, body := boxModelDocument()
	text := dom.NewText("Latin text")
	element := dom.NewElement("p", dom.Attribute{Name: "style", Value: `font-family: "Subset Face", serif`})
	element.AppendChild(text)
	body.AppendChild(element)
	frame, err := render.RenderWithResources(document, render.Viewport{Width: 300, Height: 80}, render.Resources{Fonts: []render.WebFontFace{
		{Family: "Subset Face", WeightMin: 400, WeightMax: 400, Source: "cyrillic.ttf", Data: gobold.TTF, UnicodeRanges: []css.UnicodeRange{{Start: 0x400, End: 0x4ff}}},
		{Family: "Subset Face", WeightMin: 400, WeightMax: 400, Source: "latin.ttf", Data: gobold.TTF, UnicodeRanges: []css.UnicodeRange{{Start: 0x20, End: 0x7e}}},
	}})
	if err != nil {
		t.Fatal(err)
	}
	for _, command := range frame.DisplayList.Commands {
		if command.Kind == render.DrawTextCommand && command.Node == text {
			if command.WebFont != 2 {
				t.Fatalf("selected unicode subset = %d, want Latin face 2", command.WebFont)
			}
			return
		}
	}
	t.Fatal("no text command for unicode-range fixture")
}
