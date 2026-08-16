package render

import (
	"reflect"
	"testing"

	"github.com/JediWattson/gossamer/internal/dom"
)

func TestDisplayListTransformsPreserveAndIntersectLayoutClips(t *testing.T) {
	t.Parallel()

	node := dom.NewElement("div")
	frame := &Frame{DisplayList: DisplayList{Commands: []Command{{
		Kind: FillRectCommand, Node: node,
		Rect:    Rect{X: 20, Y: 30, Width: 100, Height: 60},
		HasClip: true, Clip: Rect{X: 25, Y: 35, Width: 50, Height: 40},
	}}}}

	scrolled := ScrollDisplayList(frame, 10, 5)
	if got, want := scrolled.DisplayList.Commands[0].Clip, (Rect{X: 15, Y: 30, Width: 50, Height: 40}); got != want {
		t.Fatalf("root-scrolled clip = %#v, want %#v", got, want)
	}
	if frame.DisplayList.Commands[0].Clip != (Rect{X: 25, Y: 35, Width: 50, Height: 40}) {
		t.Fatal("root scroll mutated the source display list")
	}

	transformed := TransformDisplayList(frame, map[*dom.Node]VisualTransform{
		node: {OffsetX: 5, OffsetY: 10, HasClip: true, Clip: Rect{X: 30, Y: 20, Width: 30, Height: 80}},
	})
	command := transformed.DisplayList.Commands[0]
	if got, want := command.Rect, (Rect{X: 15, Y: 20, Width: 100, Height: 60}); got != want {
		t.Fatalf("transformed rect = %#v, want %#v", got, want)
	}
	if got, want := command.Clip, (Rect{X: 30, Y: 25, Width: 30, Height: 40}); got != want {
		t.Fatalf("intersected clip = %#v, want %#v", got, want)
	}
	if !command.HasClip || !reflect.DeepEqual(frame.DisplayList.Commands[0].Clip, Rect{X: 25, Y: 35, Width: 50, Height: 40}) {
		t.Fatal("visual transform lost the clip or mutated its source")
	}
}
