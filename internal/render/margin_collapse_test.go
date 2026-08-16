package render_test

import (
	"testing"

	"github.com/JediWattson/gossamer/internal/dom"
	"github.com/JediWattson/gossamer/internal/render"
)

func TestBlockSiblingMarginsCollapseAsOneSignedGroup(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		firstBottom string
		secondTop   string
		wantSecondY float64
	}{
		{name: "positive uses largest", firstBottom: "20px", secondTop: "30px", wantSecondY: 40},
		{name: "mixed signs add extremes", firstBottom: "20px", secondTop: "-30px", wantSecondY: 0},
		{name: "negative uses most negative", firstBottom: "-10px", secondTop: "-30px", wantSecondY: -20},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			document, body := boxModelDocument()
			first := dom.NewElement("div", dom.Attribute{Name: "style", Value: "height:10px;margin-bottom:" + test.firstBottom})
			second := dom.NewElement("div", dom.Attribute{Name: "style", Value: "height:10px;margin-top:" + test.secondTop})
			body.AppendChild(first)
			body.AppendChild(second)

			frame, err := render.Render(document, render.Viewport{Width: 200, Height: 100})
			if err != nil {
				t.Fatal(err)
			}
			firstBox := findBox(frame.Root, first)
			secondBox := findBox(frame.Root, second)
			if firstBox == nil || secondBox == nil {
				t.Fatalf("boxes = first:%#v second:%#v", firstBox, secondBox)
			}
			assertNear(t, "first y", firstBox.Bounds.Y, 0)
			assertNear(t, "second y", secondBox.Bounds.Y, test.wantSecondY)
		})
	}
}

func TestEmptyBlockMarginsCollapseThroughAdjacentSiblings(t *testing.T) {
	t.Parallel()

	document, body := boxModelDocument()
	first := dom.NewElement("div", dom.Attribute{Name: "style", Value: "height:10px;margin-bottom:5px"})
	empty := dom.NewElement("div", dom.Attribute{Name: "style", Value: "margin-top:20px;margin-bottom:30px"})
	second := dom.NewElement("div", dom.Attribute{Name: "style", Value: "height:10px;margin-top:15px"})
	body.AppendChild(first)
	body.AppendChild(empty)
	body.AppendChild(second)

	frame, err := render.Render(document, render.Viewport{Width: 200, Height: 100})
	if err != nil {
		t.Fatal(err)
	}
	firstBox := findBox(frame.Root, first)
	emptyBox := findBox(frame.Root, empty)
	secondBox := findBox(frame.Root, second)
	if firstBox == nil || emptyBox == nil || secondBox == nil {
		t.Fatalf("boxes = first:%#v empty:%#v second:%#v", firstBox, emptyBox, secondBox)
	}
	assertNear(t, "first y", firstBox.Bounds.Y, 0)
	assertNear(t, "collapsed empty y", emptyBox.Bounds.Y, 40)
	assertNear(t, "collapsed empty height", emptyBox.Bounds.Height, 0)
	assertNear(t, "second y", secondBox.Bounds.Y, 40)
}

func TestSpecifiedZeroHeightStopsEmptyBlockMarginCollapseThrough(t *testing.T) {
	t.Parallel()

	document, body := boxModelDocument()
	first := dom.NewElement("div", dom.Attribute{Name: "style", Value: "height:10px;margin-bottom:5px"})
	empty := dom.NewElement("div", dom.Attribute{Name: "style", Value: "height:0;margin-top:20px;margin-bottom:30px"})
	second := dom.NewElement("div", dom.Attribute{Name: "style", Value: "height:10px;margin-top:15px"})
	body.AppendChild(first)
	body.AppendChild(empty)
	body.AppendChild(second)

	frame, err := render.Render(document, render.Viewport{Width: 200, Height: 100})
	if err != nil {
		t.Fatal(err)
	}
	emptyBox := findBox(frame.Root, empty)
	secondBox := findBox(frame.Root, second)
	if emptyBox == nil || secondBox == nil {
		t.Fatalf("boxes = empty:%#v second:%#v", emptyBox, secondBox)
	}
	assertNear(t, "specified empty y", emptyBox.Bounds.Y, 30)
	assertNear(t, "following y", secondBox.Bounds.Y, 60)
}

func TestBlockChildMarginsCollapseThroughEligibleParentEdges(t *testing.T) {
	t.Parallel()

	document, body := boxModelDocument()
	parent := dom.NewElement("section")
	child := dom.NewElement("div", dom.Attribute{Name: "style", Value: "height:10px;margin-top:25px;margin-bottom:30px"})
	following := dom.NewElement("div", dom.Attribute{Name: "style", Value: "height:10px;margin-top:5px"})
	child.AppendChild(dom.NewText(""))
	parent.AppendChild(child)
	body.AppendChild(parent)
	body.AppendChild(following)

	frame, err := render.Render(document, render.Viewport{Width: 200, Height: 120})
	if err != nil {
		t.Fatal(err)
	}
	parentBox := findBox(frame.Root, parent)
	childBox := findBox(frame.Root, child)
	followingBox := findBox(frame.Root, following)
	if parentBox == nil || childBox == nil || followingBox == nil {
		t.Fatalf("boxes = parent:%#v child:%#v following:%#v", parentBox, childBox, followingBox)
	}
	// The first child's top margin moves the parent's border edge, and the last
	// child's bottom margin joins the following sibling's top margin.
	assertNear(t, "parent y", parentBox.Bounds.Y, 25)
	assertNear(t, "child y", childBox.Bounds.Y, 25)
	assertNear(t, "parent height", parentBox.Bounds.Height, 10)
	assertNear(t, "following y", followingBox.Bounds.Y, 65)
}

func TestPaddingAndFormattingRootsStopParentChildMarginCollapse(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		parentStyle string
		wantParentY float64
		wantChildY  float64
	}{
		{name: "padding", parentStyle: "padding-top:1px", wantParentY: 0, wantChildY: 26},
		{name: "flex formatting root", parentStyle: "display:flex;flex-direction:column", wantParentY: 0, wantChildY: 25},
		{name: "overflow hidden", parentStyle: "overflow:hidden", wantParentY: 0, wantChildY: 25},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			document, body := boxModelDocument()
			parent := dom.NewElement("section", dom.Attribute{Name: "style", Value: test.parentStyle})
			child := dom.NewElement("div", dom.Attribute{Name: "style", Value: "height:10px;margin-top:25px"})
			parent.AppendChild(child)
			body.AppendChild(parent)

			frame, err := render.Render(document, render.Viewport{Width: 200, Height: 100})
			if err != nil {
				t.Fatal(err)
			}
			parentBox := findBox(frame.Root, parent)
			childBox := findBox(frame.Root, child)
			if parentBox == nil || childBox == nil {
				t.Fatalf("boxes = parent:%#v child:%#v", parentBox, childBox)
			}
			assertNear(t, "parent y", parentBox.Bounds.Y, test.wantParentY)
			assertNear(t, "child y", childBox.Bounds.Y, test.wantChildY)
		})
	}
}

func TestBottomPaddingAndMinimumHeightContainLastChildMargin(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name             string
		parentStyle      string
		wantParentHeight float64
		wantFollowingY   float64
	}{
		{name: "bottom padding", parentStyle: "padding-bottom:1px", wantParentHeight: 36, wantFollowingY: 41},
		{name: "minimum height", parentStyle: "min-height:40px", wantParentHeight: 40, wantFollowingY: 45},
		{name: "specified height", parentStyle: "height:40px", wantParentHeight: 40, wantFollowingY: 45},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			document, body := boxModelDocument()
			parent := dom.NewElement("section", dom.Attribute{Name: "style", Value: test.parentStyle})
			child := dom.NewElement("div", dom.Attribute{Name: "style", Value: "height:10px;margin-bottom:25px"})
			following := dom.NewElement("div", dom.Attribute{Name: "style", Value: "height:10px;margin-top:5px"})
			parent.AppendChild(child)
			body.AppendChild(parent)
			body.AppendChild(following)

			frame, err := render.Render(document, render.Viewport{Width: 200, Height: 120})
			if err != nil {
				t.Fatal(err)
			}
			parentBox := findBox(frame.Root, parent)
			followingBox := findBox(frame.Root, following)
			if parentBox == nil || followingBox == nil {
				t.Fatalf("boxes = parent:%#v following:%#v", parentBox, followingBox)
			}
			assertNear(t, "parent height", parentBox.Bounds.Height, test.wantParentHeight)
			assertNear(t, "following y", followingBox.Bounds.Y, test.wantFollowingY)
		})
	}
}

func TestCollapsedPercentageMarginsUseContainingContentWidth(t *testing.T) {
	t.Parallel()

	document, body := boxModelDocument()
	parent := dom.NewElement("section", dom.Attribute{Name: "style", Value: "width:100px;padding-top:1px"})
	first := dom.NewElement("div", dom.Attribute{Name: "style", Value: "height:10px;margin-bottom:10%"})
	second := dom.NewElement("div", dom.Attribute{Name: "style", Value: "height:10px;margin-top:20%"})
	parent.AppendChild(first)
	parent.AppendChild(second)
	body.AppendChild(parent)

	frame, err := render.Render(document, render.Viewport{Width: 200, Height: 100})
	if err != nil {
		t.Fatal(err)
	}
	firstBox := findBox(frame.Root, first)
	secondBox := findBox(frame.Root, second)
	if firstBox == nil || secondBox == nil {
		t.Fatalf("boxes = first:%#v second:%#v", firstBox, secondBox)
	}
	assertNear(t, "first y", firstBox.Bounds.Y, 1)
	// Both vertical percentages use the 100px containing content width, so
	// 10px and 20px collapse to a 20px gap.
	assertNear(t, "second y", secondBox.Bounds.Y, 31)
}

func TestFlexItemEstablishesIndependentMarginFormattingContext(t *testing.T) {
	t.Parallel()

	document, body := boxModelDocument()
	container := dom.NewElement("div", dom.Attribute{Name: "style", Value: "display:flex"})
	item := dom.NewElement("section")
	child := dom.NewElement("div", dom.Attribute{Name: "style", Value: "height:10px;margin-top:25px"})
	item.AppendChild(child)
	container.AppendChild(item)
	body.AppendChild(container)

	frame, err := render.Render(document, render.Viewport{Width: 200, Height: 100})
	if err != nil {
		t.Fatal(err)
	}
	itemBox := findBox(frame.Root, item)
	childBox := findBox(frame.Root, child)
	if itemBox == nil || childBox == nil {
		t.Fatalf("boxes = item:%#v child:%#v", itemBox, childBox)
	}
	assertNear(t, "flex item y", itemBox.Bounds.Y, 0)
	assertNear(t, "flex child y", childBox.Bounds.Y, 25)
	assertNear(t, "flex item content height", itemBox.ContentBounds.Height, 35)
}

func TestNegativeCollapsedMarginPreservesFlowPaintAndHitOrder(t *testing.T) {
	t.Parallel()

	document, body := boxModelDocument()
	first := dom.NewElement("div", dom.Attribute{Name: "style", Value: "height:20px;background:#ff0000"})
	second := dom.NewElement("div", dom.Attribute{Name: "style", Value: "height:20px;margin-top:-10px;background:#0000ff"})
	body.AppendChild(first)
	body.AppendChild(second)

	frame, err := render.Render(document, render.Viewport{Width: 100, Height: 60})
	if err != nil {
		t.Fatal(err)
	}
	firstBox := findBox(frame.Root, first)
	secondBox := findBox(frame.Root, second)
	if firstBox == nil || secondBox == nil {
		t.Fatalf("boxes = first:%#v second:%#v", firstBox, secondBox)
	}
	assertNear(t, "overlapping second y", secondBox.Bounds.Y, 10)
	if hit := render.HitTest(frame, 5, 15); hit != second {
		t.Fatalf("overlap hit = %p, want later sibling %p", hit, second)
	}
	firstPaint, secondPaint := -1, -1
	for index, command := range frame.DisplayList.Commands {
		if command.Kind != render.FillRectCommand {
			continue
		}
		if command.Node == first {
			firstPaint = index
		}
		if command.Node == second {
			secondPaint = index
		}
	}
	if firstPaint < 0 || secondPaint <= firstPaint {
		t.Fatalf("flow paint order = first:%d second:%d", firstPaint, secondPaint)
	}
}
