package render_test

import (
	"image/color"
	"slices"
	"testing"

	"github.com/JediWattson/gossamer/internal/dom"
	"github.com/JediWattson/gossamer/internal/render"
)

func TestVisibilitySuppressesPaintWithoutRemovingGeometry(t *testing.T) {
	t.Parallel()

	document, body := boxModelDocument()
	parentText := dom.NewText("hidden parent")
	childText := dom.NewText("visible child")
	parent := dom.NewElement("div", dom.Attribute{Name: "style", Value: "visibility:hidden;width:120px;height:60px;background:red;border:2px solid red"})
	child := dom.NewElement("div", dom.Attribute{Name: "style", Value: "visibility:visible;width:80px;height:20px;background:green"})
	parent.AppendChild(parentText)
	child.AppendChild(childText)
	parent.AppendChild(child)
	body.AppendChild(parent)

	frame, err := render.Render(document, render.Viewport{Width: 240, Height: 160})
	if err != nil {
		t.Fatal(err)
	}
	parentBox := findBox(frame.Root, parent)
	childBox := findBox(frame.Root, child)
	if parentBox == nil || childBox == nil || parentBox.Bounds.Width == 0 || childBox.Bounds.Width == 0 {
		t.Fatalf("visibility removed layout geometry: parent=%#v child=%#v", parentBox, childBox)
	}
	for _, command := range frame.DisplayList.Commands {
		if command.Node == parent || command.Node == parentText {
			t.Errorf("hidden parent emitted paint command %#v", command)
		}
	}
	if commandIndex(frame.DisplayList.Commands, func(command render.Command) bool {
		return command.Node == child && command.Kind == render.FillRectCommand && command.Color == (color.NRGBA{G: 128, A: 255})
	}) < 0 {
		t.Error("visible descendant did not override inherited hidden visibility")
	}
	paintedChildText := ""
	for _, command := range frame.DisplayList.Commands {
		if command.Node == childText && command.Kind == render.DrawTextCommand {
			if paintedChildText != "" {
				paintedChildText += " "
			}
			paintedChildText += command.Text
		}
	}
	if paintedChildText != "visible child" {
		t.Errorf("visible descendant text = %q, want visible child", paintedChildText)
	}
}

func TestWhiteSpaceModesDriveCollapsingAndForcedBreaks(t *testing.T) {
	t.Parallel()

	for _, test := range []struct {
		name  string
		tag   string
		style string
		text  string
		want  []string
	}{
		{name: "normal", tag: "div", style: "white-space:normal;width:300px", text: "a  b\nc", want: []string{"a b c"}},
		{name: "UA pre", tag: "pre", style: "width:300px", text: "a  b\nc", want: []string{"a  b", "c"}},
		{name: "pre line", tag: "div", style: "white-space:pre-line;width:300px", text: "a  b\nc", want: []string{"a b", "c"}},
		{name: "pre wrap", tag: "div", style: "white-space:pre-wrap;width:300px", text: "a  b\nc", want: []string{"a  b", "c"}},
		{name: "nonbreaking spaces", tag: "div", style: "white-space:normal;width:300px", text: "a\u00a0\u00a0b", want: []string{"a\u00a0\u00a0b"}},
	} {
		t.Run(test.name, func(t *testing.T) {
			document, body := boxModelDocument()
			text := dom.NewText(test.text)
			target := dom.NewElement(test.tag, dom.Attribute{Name: "style", Value: test.style})
			target.AppendChild(text)
			body.AppendChild(target)
			frame, err := render.Render(document, render.Viewport{Width: 400, Height: 200})
			if err != nil {
				t.Fatal(err)
			}
			if got := textLinesForNode(frame.Root, text); !slices.Equal(got, test.want) {
				t.Fatalf("text lines = %q, want %q", got, test.want)
			}
		})
	}
}

func TestNoWrapAndPreWrapChooseDifferentSoftWrapPolicies(t *testing.T) {
	t.Parallel()

	renderLines := func(t *testing.T, whiteSpace string) []string {
		t.Helper()
		document, body := boxModelDocument()
		text := dom.NewText("one two three")
		target := dom.NewElement("div", dom.Attribute{Name: "style", Value: "width:40px;white-space:" + whiteSpace})
		target.AppendChild(text)
		body.AppendChild(target)
		frame, err := render.Render(document, render.Viewport{Width: 200, Height: 160})
		if err != nil {
			t.Fatal(err)
		}
		return textLinesForNode(frame.Root, text)
	}
	if got := renderLines(t, "nowrap"); !slices.Equal(got, []string{"one two three"}) {
		t.Fatalf("nowrap lines = %q", got)
	}
	if got := renderLines(t, "normal"); len(got) < 2 {
		t.Fatalf("normal did not soft wrap: %q", got)
	}
	if got := renderLines(t, "pre-wrap"); len(got) < 2 || joinTextLines(got) != "one two three" {
		t.Fatalf("pre-wrap lines = %q", got)
	}
	if got := renderLines(t, "pre"); !slices.Equal(got, []string{"one two three"}) {
		t.Fatalf("pre lines = %q", got)
	}

	document, body := boxModelDocument()
	text := dom.NewText("a   b")
	target := dom.NewElement("div", dom.Attribute{Name: "style", Value: "width:12px;white-space:break-spaces"})
	target.AppendChild(text)
	body.AppendChild(target)
	frame, err := render.Render(document, render.Viewport{Width: 200, Height: 160})
	if err != nil {
		t.Fatal(err)
	}
	if got := textLinesForNode(frame.Root, text); len(got) < 2 || joinTextLines(got) != "a   b" {
		t.Fatalf("break-spaces lines = %q", got)
	}
}

func TestPreservedTrailingNewlineCreatesAnEmptyLineBox(t *testing.T) {
	t.Parallel()

	document, body := boxModelDocument()
	target := dom.NewElement("pre")
	target.AppendChild(dom.NewText("a\n"))
	body.AppendChild(target)
	frame, err := render.Render(document, render.Viewport{Width: 200, Height: 160})
	if err != nil {
		t.Fatal(err)
	}
	box := findBox(frame.Root, target)
	if box == nil || box.ContentBounds.Height < 38 {
		t.Fatalf("trailing preserved newline did not create two line boxes: %#v", box)
	}
}

func textLinesForNode(root *render.Box, node *dom.Node) []string {
	fragments := collectTextFragments(root)
	lines := make([]string, 0)
	baselines := make([]float64, 0)
	for _, fragment := range fragments {
		if fragment.Node != node {
			continue
		}
		index := slices.Index(baselines, fragment.BaselineY)
		if index < 0 {
			baselines = append(baselines, fragment.BaselineY)
			lines = append(lines, fragment.Text)
		} else {
			lines[index] += fragment.Text
		}
	}
	return lines
}

func joinTextLines(lines []string) string {
	result := ""
	for _, line := range lines {
		result += line
	}
	return result
}
