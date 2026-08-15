package render_test

import (
	"image/color"
	"reflect"
	"testing"

	"github.com/JediWattson/gossamer/internal/dom"
	"github.com/JediWattson/gossamer/internal/render"
)

func TestRenderUnorderedListMarkersUseDiscAndSquare(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		listStyle string
		want      string
	}{
		{name: "default disc", want: "•"},
		{name: "author square", listStyle: "list-style-type: square", want: "▪"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			document, body := markerTestDocument()
			list := dom.NewElement("ul", dom.Attribute{Name: "style", Value: "margin: 0; " + test.listStyle})
			item := dom.NewElement("li")
			item.AppendChild(dom.NewText("Item"))
			list.AppendChild(item)
			body.AppendChild(list)

			frame, err := render.Render(document, render.Viewport{Width: 240, Height: 100})
			if err != nil {
				t.Fatalf("Render() error = %v", err)
			}
			markers := markerTestCommands(frame.DisplayList.Commands, test.want)
			if len(markers) != 1 {
				t.Fatalf("%q marker command count = %d, want 1", test.want, len(markers))
			}
		})
	}
}

func TestRenderOrderedListMarkersNumberItems(t *testing.T) {
	t.Parallel()

	document, body := markerTestDocument()
	list := dom.NewElement("ol", dom.Attribute{Name: "style", Value: "margin: 0"})
	for _, text := range []string{"Alpha", "Beta", "Gamma"} {
		item := dom.NewElement("li")
		item.AppendChild(dom.NewText(text))
		list.AppendChild(item)
	}
	body.AppendChild(list)

	frame, err := render.Render(document, render.Viewport{Width: 240, Height: 140})
	if err != nil {
		t.Fatalf("Render() error = %v", err)
	}

	var markers []string
	for _, command := range frame.DisplayList.Commands {
		if command.Kind != render.DrawTextCommand {
			continue
		}
		switch command.Text {
		case "1.", "2.", "3.":
			markers = append(markers, command.Text)
		}
	}
	if want := []string{"1.", "2.", "3."}; !reflect.DeepEqual(markers, want) {
		t.Errorf("ordered markers = %q, want %q", markers, want)
	}
}

func TestRenderListStyleNoneSuppressesMarker(t *testing.T) {
	t.Parallel()

	document, body := markerTestDocument()
	list := dom.NewElement("ul", dom.Attribute{Name: "style", Value: "margin: 0; padding: 0; list-style-type: none"})
	item := dom.NewElement("li")
	item.AppendChild(dom.NewText("Only"))
	list.AppendChild(item)
	body.AppendChild(list)

	frame, err := render.Render(document, render.Viewport{Width: 240, Height: 100})
	if err != nil {
		t.Fatalf("Render() error = %v", err)
	}

	var paintedText []string
	for _, command := range frame.DisplayList.Commands {
		if command.Kind == render.DrawTextCommand {
			paintedText = append(paintedText, command.Text)
		}
	}
	if want := []string{"Only"}; !reflect.DeepEqual(paintedText, want) {
		t.Errorf("painted text = %q, want item text without a marker %q", paintedText, want)
	}
}

func TestRenderListStyleShorthandCircleAndNone(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		listStyle  string
		wantMarker string
	}{
		{name: "circle", listStyle: "circle outside", wantMarker: "◦"},
		{name: "none", listStyle: "none"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			document, body := markerTestDocument()
			list := dom.NewElement("ul", dom.Attribute{Name: "style", Value: "margin: 0; list-style: " + test.listStyle})
			item := dom.NewElement("li")
			item.AppendChild(dom.NewText("Shorthand"))
			list.AppendChild(item)
			body.AppendChild(list)

			frame, err := render.Render(document, render.Viewport{Width: 240, Height: 100})
			if err != nil {
				t.Fatalf("Render() error = %v", err)
			}
			if test.wantMarker == "" {
				if got := markerTestMarkerCommands(frame.DisplayList.Commands); len(got) != 0 {
					t.Errorf("marker commands = %#v, want none", got)
				}
				return
			}
			if got := markerTestCommands(frame.DisplayList.Commands, test.wantMarker); len(got) != 1 {
				t.Errorf("%q marker command count = %d, want 1", test.wantMarker, len(got))
			}
		})
	}
}

func TestRenderInlineListItemSuppressesMarker(t *testing.T) {
	t.Parallel()

	document, body := markerTestDocument()
	list := dom.NewElement("ul", dom.Attribute{Name: "style", Value: "margin: 0"})
	item := dom.NewElement("li", dom.Attribute{Name: "style", Value: "display: inline"})
	item.AppendChild(dom.NewText("Inline item"))
	list.AppendChild(item)
	body.AppendChild(list)

	frame, err := render.Render(document, render.Viewport{Width: 240, Height: 100})
	if err != nil {
		t.Fatalf("Render() error = %v", err)
	}
	if got := markerTestMarkerCommands(frame.DisplayList.Commands); len(got) != 0 {
		t.Errorf("marker commands = %#v, want none for display:inline list item", got)
	}
	if itemIndex := markerTestCommandIndex(frame.DisplayList.Commands, "Inline item"); itemIndex < 0 {
		t.Error("inline list-item content was not painted")
	}
}

func TestRenderOrderedListStartValueAndReversed(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name           string
		listAttributes []dom.Attribute
		itemValues     []string
		wantMarkers    []string
	}{
		{
			name:           "start and value",
			listAttributes: []dom.Attribute{{Name: "start", Value: "3"}},
			itemValues:     []string{"", "7", ""},
			wantMarkers:    []string{"3.", "7.", "8."},
		},
		{
			name:           "reversed default start",
			listAttributes: []dom.Attribute{{Name: "reversed"}},
			itemValues:     []string{"", "", ""},
			wantMarkers:    []string{"3.", "2.", "1."},
		},
		{
			name: "reversed explicit start and value",
			listAttributes: []dom.Attribute{
				{Name: "reversed"},
				{Name: "start", Value: "9"},
			},
			itemValues:  []string{"", "5", ""},
			wantMarkers: []string{"9.", "5.", "4."},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			document, body := markerTestDocument()
			attributes := append([]dom.Attribute{{Name: "style", Value: "margin: 0"}}, test.listAttributes...)
			list := dom.NewElement("ol", attributes...)
			for index, value := range test.itemValues {
				var attributes []dom.Attribute
				if value != "" {
					attributes = append(attributes, dom.Attribute{Name: "value", Value: value})
				}
				item := dom.NewElement("li", attributes...)
				item.AppendChild(dom.NewText([]string{"First", "Second", "Third"}[index]))
				list.AppendChild(item)
			}
			body.AppendChild(list)

			frame, err := render.Render(document, render.Viewport{Width: 240, Height: 140})
			if err != nil {
				t.Fatalf("Render() error = %v", err)
			}
			if got := markerTestMarkerTexts(frame.DisplayList.Commands); !reflect.DeepEqual(got, test.wantMarkers) {
				t.Errorf("ordered markers = %q, want %q", got, test.wantMarkers)
			}
		})
	}
}

func TestRenderNestedOrderedListResetsNumbering(t *testing.T) {
	t.Parallel()

	document, body := markerTestDocument()
	outer := dom.NewElement("ol", dom.Attribute{Name: "style", Value: "margin: 0"})
	firstOuter := dom.NewElement("li")
	firstOuter.AppendChild(dom.NewText("Outer one"))
	inner := dom.NewElement("ol", dom.Attribute{Name: "style", Value: "margin: 0"})
	for _, text := range []string{"Inner one", "Inner two"} {
		item := dom.NewElement("li")
		item.AppendChild(dom.NewText(text))
		inner.AppendChild(item)
	}
	firstOuter.AppendChild(inner)
	outer.AppendChild(firstOuter)
	secondOuter := dom.NewElement("li")
	secondOuter.AppendChild(dom.NewText("Outer two"))
	outer.AppendChild(secondOuter)
	body.AppendChild(outer)

	frame, err := render.Render(document, render.Viewport{Width: 320, Height: 220})
	if err != nil {
		t.Fatalf("Render() error = %v", err)
	}
	if got, want := markerTestMarkerTexts(frame.DisplayList.Commands), []string{"1.", "1.", "2.", "2."}; !reflect.DeepEqual(got, want) {
		t.Errorf("nested ordered markers = %q, want reset sequence %q", got, want)
	}
}

func TestRenderDecimalStyleNumbersGeneratedListItemSiblings(t *testing.T) {
	t.Parallel()

	document, body := markerTestDocument()
	container := dom.NewElement("div", dom.Attribute{Name: "style", Value: "list-style-type: decimal"})
	for _, element := range []string{"span", "div", "p"} {
		item := dom.NewElement(element, dom.Attribute{Name: "style", Value: "display: list-item"})
		item.AppendChild(dom.NewText("Generated item"))
		container.AppendChild(item)
	}
	body.AppendChild(container)

	frame, err := render.Render(document, render.Viewport{Width: 280, Height: 180})
	if err != nil {
		t.Fatalf("Render() error = %v", err)
	}
	if got, want := markerTestMarkerTexts(frame.DisplayList.Commands), []string{"1.", "2.", "3."}; !reflect.DeepEqual(got, want) {
		t.Errorf("generated list-item markers = %q, want %q", got, want)
	}
}

func TestRenderReplacedListItemReceivesMarker(t *testing.T) {
	t.Parallel()

	document, body := markerTestDocument()
	imageItem := dom.NewElement("img", dom.Attribute{
		Name:  "style",
		Value: "display: list-item; list-style-type: square; width: 12px; height: 8px",
	})
	body.AppendChild(imageItem)

	frame, err := render.Render(document, render.Viewport{Width: 200, Height: 100})
	if err != nil {
		t.Fatalf("Render() error = %v", err)
	}
	if got := markerTestCommands(frame.DisplayList.Commands, "▪"); len(got) != 1 {
		t.Errorf("replaced list-item square marker count = %d, want 1", len(got))
	}
}

func TestRenderListItemPaddingDoesNotMoveOutsideMarker(t *testing.T) {
	t.Parallel()

	document, body := markerTestDocument()
	for _, padding := range []string{"0", "30px"} {
		list := dom.NewElement("ul", dom.Attribute{Name: "style", Value: "margin: 0"})
		item := dom.NewElement("li", dom.Attribute{Name: "style", Value: "padding-left: " + padding})
		item.AppendChild(dom.NewText("Padded"))
		list.AppendChild(item)
		body.AppendChild(list)
	}

	frame, err := render.Render(document, render.Viewport{Width: 280, Height: 140})
	if err != nil {
		t.Fatalf("Render() error = %v", err)
	}
	markers := markerTestCommands(frame.DisplayList.Commands, "•")
	items := markerTestCommands(frame.DisplayList.Commands, "Padded")
	if len(markers) != 2 || len(items) != 2 {
		t.Fatalf("command counts = markers:%d items:%d, want 2 each", len(markers), len(items))
	}
	assertNear(t, "padded outside marker x", markers[1].X, markers[0].X)
	assertNear(t, "padded text shift", items[1].X-items[0].X, 30)
}

func TestRenderWrappedListItemPaintsOneMarker(t *testing.T) {
	t.Parallel()

	document, body := markerTestDocument()
	list := dom.NewElement("ul", dom.Attribute{Name: "style", Value: "margin: 0; width: 64px"})
	item := dom.NewElement("li")
	item.AppendChild(dom.NewText("Alpha Beta Gamma Delta"))
	list.AppendChild(item)
	body.AppendChild(list)

	frame, err := render.Render(document, render.Viewport{Width: 180, Height: 180})
	if err != nil {
		t.Fatalf("Render() error = %v", err)
	}
	markers := markerTestCommands(frame.DisplayList.Commands, "•")
	if len(markers) != 1 {
		t.Fatalf("marker command count = %d, want exactly 1 for wrapped item", len(markers))
	}
	var lineBaselines []float64
	for _, command := range frame.DisplayList.Commands {
		if command.Kind != render.DrawTextCommand || command.Text == "•" {
			continue
		}
		if len(lineBaselines) == 0 || !near(lineBaselines[len(lineBaselines)-1], command.BaselineY) {
			lineBaselines = append(lineBaselines, command.BaselineY)
		}
	}
	if len(lineBaselines) < 2 {
		t.Fatalf("item line baselines = %v, want wrapped content on at least 2 lines", lineBaselines)
	}
	assertNear(t, "wrapped item marker baseline", markers[0].BaselineY, lineBaselines[0])
}

func TestRenderEmptyListItemRetainsLineHeight(t *testing.T) {
	t.Parallel()

	document, body := markerTestDocument()
	list := dom.NewElement("ul", dom.Attribute{Name: "style", Value: "margin: 0; font-size: 20px; line-height: 2"})
	empty := dom.NewElement("li")
	list.AppendChild(empty)
	following := dom.NewElement("li")
	following.AppendChild(dom.NewText("Following"))
	list.AppendChild(following)
	body.AppendChild(list)

	frame, err := render.Render(document, render.Viewport{Width: 280, Height: 160})
	if err != nil {
		t.Fatalf("Render() error = %v", err)
	}
	emptyBox := findBox(frame.Root, empty)
	if emptyBox == nil {
		t.Fatal("empty list-item box not found")
	}
	followingBox := findBox(frame.Root, following)
	if followingBox == nil {
		t.Fatal("following list-item box not found")
	}
	assertNear(t, "empty item content height", emptyBox.ContentBounds.Height, 40)
	if followingBox.Bounds.Y < emptyBox.Bounds.Y+emptyBox.Bounds.Height {
		t.Errorf("following item y = %.2f, want at or below empty item bottom %.2f", followingBox.Bounds.Y, emptyBox.Bounds.Y+emptyBox.Bounds.Height)
	}
	markers := markerTestCommands(frame.DisplayList.Commands, "•")
	if len(markers) != 2 {
		t.Fatalf("marker command count = %d, want one for each item", len(markers))
	}
	if baseline := markers[0].BaselineY; baseline <= emptyBox.Bounds.Y || baseline >= emptyBox.Bounds.Y+emptyBox.Bounds.Height {
		t.Errorf("empty item marker baseline = %.2f, want inside bounds %#v", baseline, emptyBox.Bounds)
	}
}

func TestRenderTextAlignmentDoesNotMoveOutsideListMarker(t *testing.T) {
	t.Parallel()

	document, body := markerTestDocument()
	for _, alignment := range []string{"left", "center", "right"} {
		list := dom.NewElement("ul", dom.Attribute{
			Name:  "style",
			Value: "margin: 0; width: 160px; text-align: " + alignment,
		})
		item := dom.NewElement("li")
		item.AppendChild(dom.NewText("Aligned"))
		list.AppendChild(item)
		body.AppendChild(list)
	}

	frame, err := render.Render(document, render.Viewport{Width: 280, Height: 180})
	if err != nil {
		t.Fatalf("Render() error = %v", err)
	}
	markers := markerTestCommands(frame.DisplayList.Commands, "•")
	items := markerTestCommands(frame.DisplayList.Commands, "Aligned")
	if len(markers) != 3 || len(items) != 3 {
		t.Fatalf("command counts = markers:%d items:%d, want 3 each", len(markers), len(items))
	}
	for index := 1; index < len(markers); index++ {
		assertNear(t, "outside marker x", markers[index].X, markers[0].X)
	}
	if !(items[0].X < items[1].X && items[1].X < items[2].X) {
		t.Errorf("aligned item x positions = [%.2f %.2f %.2f], want left < center < right", items[0].X, items[1].X, items[2].X)
	}
	for index := range markers {
		if markers[index].X >= items[index].X {
			t.Errorf("alignment %d marker x = %.2f, want outside item x %.2f", index, markers[index].X, items[index].X)
		}
		assertNear(t, "aligned marker baseline", markers[index].BaselineY, items[index].BaselineY)
	}
}

func TestRenderListMarkerPaintsBeforeAndOutsideItemText(t *testing.T) {
	t.Parallel()

	document, body := markerTestDocument()
	list := dom.NewElement("ul", dom.Attribute{Name: "style", Value: "margin: 0"})
	item := dom.NewElement("li")
	item.AppendChild(dom.NewText("Positioned"))
	list.AppendChild(item)
	body.AppendChild(list)

	frame, err := render.Render(document, render.Viewport{Width: 240, Height: 100})
	if err != nil {
		t.Fatalf("Render() error = %v", err)
	}

	markerIndex := markerTestCommandIndex(frame.DisplayList.Commands, "•")
	itemIndex := markerTestCommandIndex(frame.DisplayList.Commands, "Positioned")
	if markerIndex < 0 || itemIndex < 0 {
		t.Fatalf("paint command indexes = marker:%d item:%d, want both present", markerIndex, itemIndex)
	}
	if markerIndex >= itemIndex {
		t.Errorf("paint command indexes = marker:%d item:%d, want marker painted first", markerIndex, itemIndex)
	}
	marker := frame.DisplayList.Commands[markerIndex]
	itemText := frame.DisplayList.Commands[itemIndex]
	if marker.X >= itemText.X {
		t.Errorf("marker x = %.2f, want left of item text x %.2f", marker.X, itemText.X)
	}
	assertNear(t, "marker baseline", marker.BaselineY, itemText.BaselineY)
}

func TestRenderListMarkerInheritsAuthorTextStyle(t *testing.T) {
	t.Parallel()

	document, body := markerTestDocument()
	list := dom.NewElement("ul", dom.Attribute{
		Name:  "style",
		Value: "margin: 0; color: #123456; font-size: 24px; font-weight: bold",
	})
	item := dom.NewElement("li")
	item.AppendChild(dom.NewText("Styled"))
	list.AppendChild(item)
	body.AppendChild(list)

	frame, err := render.Render(document, render.Viewport{Width: 280, Height: 120})
	if err != nil {
		t.Fatalf("Render() error = %v", err)
	}

	markers := markerTestCommands(frame.DisplayList.Commands, "•")
	if len(markers) != 1 {
		t.Fatalf("marker command count = %d, want 1", len(markers))
	}
	marker := markers[0]
	if got, want := marker.Color, (color.NRGBA{R: 0x12, G: 0x34, B: 0x56, A: 0xff}); got != want {
		t.Errorf("marker color = %#v, want inherited author color %#v", got, want)
	}
	if got, want := marker.FontSize, 24.0; got != want {
		t.Errorf("marker font size = %.2f, want inherited %.2f", got, want)
	}
	if got, want := marker.FontWeight, render.FontWeightBold; got != want {
		t.Errorf("marker font weight = %v, want inherited bold", got)
	}
}

func markerTestDocument() (*dom.Node, *dom.Node) {
	document := dom.NewDocument()
	html := dom.NewElement("html")
	document.AppendChild(html)
	head := dom.NewElement("head")
	html.AppendChild(head)
	body := dom.NewElement("body", dom.Attribute{Name: "style", Value: "margin: 0"})
	html.AppendChild(body)
	return document, body
}

func markerTestCommands(commands []render.Command, text string) []render.Command {
	var matches []render.Command
	for _, command := range commands {
		if command.Kind == render.DrawTextCommand && command.Text == text {
			matches = append(matches, command)
		}
	}
	return matches
}

func markerTestCommandIndex(commands []render.Command, text string) int {
	for index, command := range commands {
		if command.Kind == render.DrawTextCommand && command.Text == text {
			return index
		}
	}
	return -1
}

func markerTestMarkerCommands(commands []render.Command) []render.Command {
	var markers []render.Command
	for _, command := range commands {
		if command.Kind != render.DrawTextCommand {
			continue
		}
		switch command.Text {
		case "•", "◦", "▪", "1.", "2.", "3.", "4.", "5.", "6.", "7.", "8.", "9.":
			markers = append(markers, command)
		}
	}
	return markers
}

func markerTestMarkerTexts(commands []render.Command) []string {
	markers := markerTestMarkerCommands(commands)
	texts := make([]string, len(markers))
	for index := range markers {
		texts[index] = markers[index].Text
	}
	return texts
}
