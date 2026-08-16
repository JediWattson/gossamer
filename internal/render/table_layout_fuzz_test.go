package render_test

import (
	"fmt"
	"math"
	"testing"

	"github.com/JediWattson/gossamer/internal/dom"
	"github.com/JediWattson/gossamer/internal/render"
)

func FuzzTableLayoutSpansStayFinite(f *testing.F) {
	f.Add(byte(2), byte(3), byte(1), byte(1), byte(0))
	f.Add(byte(4), byte(4), byte(2), byte(3), byte(0xff))
	f.Add(byte(8), byte(8), byte(255), byte(0), byte(0x55))
	f.Add(byte(4), byte(5), byte(0x82), byte(0x81), byte(0))
	f.Add(byte(0x82), byte(0x83), byte(0x42), byte(0x40), byte(0))
	f.Add(byte(4), byte(0x44), byte(2), byte(1), byte(2))
	f.Fuzz(func(t *testing.T, rawRows, rawColumns, rawColumnSpan, rawRowSpan, rawModes byte) {
		rows := int(rawRows%8) + 1
		columns := int(rawColumns%8) + 1
		columnSpan := int(rawColumnSpan%8) + 1
		rowSpan := int(rawRowSpan % 9)

		document := dom.NewDocument()
		html := dom.NewElement("html")
		body := dom.NewElement("body", dom.Attribute{Name: "style", Value: "margin:0"})
		borderCollapse := "separate"
		if rawModes&1 != 0 {
			borderCollapse = "collapse"
		}
		direction := "ltr"
		if rawRows&0x40 != 0 {
			direction = "rtl"
		}
		tableLayout := "auto"
		width := "auto"
		if rawModes&2 != 0 {
			tableLayout = "fixed"
			width = fmt.Sprintf("%dpx", 40+int(rawModes))
		} else if rawRows&0x80 != 0 {
			width = fmt.Sprintf("%dpx", 80+int(rawRows))
		}
		tableHeight := "auto"
		switch {
		case rawColumns&0x20 != 0:
			tableHeight = fmt.Sprintf("%dpx", 20+int(rawColumns))
		case rawColumns&0x10 != 0:
			tableHeight = fmt.Sprintf("%g%%", float64(1+rawColumns%200))
		}
		table := dom.NewElement("table", dom.Attribute{Name: "style", Value: fmt.Sprintf(
			"direction:%s;border-collapse:%s;border-spacing:%dpx %dpx;table-layout:%s;width:%s;height:%s;border:%dpx solid #123456;empty-cells:hide",
			direction, borderCollapse, rawModes%7, (rawModes/7)%7, tableLayout, width, tableHeight, 1+rawModes%5,
		)})
		captionSide := "top"
		if rawModes&4 != 0 {
			captionSide = "bottom"
		}
		caption := dom.NewElement("caption", dom.Attribute{Name: "style", Value: fmt.Sprintf(
			"caption-side:%s;height:%dpx;margin:%dpx %dpx %dpx %dpx",
			captionSide, rawRows%40, rawColumns%7, rawColumnSpan%7, rawRowSpan%7, rawModes%7,
		)})
		caption.AppendChild(dom.NewText("caption"))
		table.AppendChild(caption)
		columnGroupStyle := ""
		if rawModes&32 != 0 {
			columnGroupStyle = "visibility:collapse"
		}
		columnGroup := dom.NewElement("colgroup", dom.Attribute{Name: "style", Value: columnGroupStyle})
		if rawColumnSpan&0x80 != 0 {
			columnGroup.AppendChild(dom.NewElement("col",
				dom.Attribute{Name: "span", Value: fmt.Sprint(columns)},
				dom.Attribute{Name: "style", Value: "width:0"},
			))
		} else {
			for columnIndex := range columns {
				visibility := "visible"
				if rawModes&64 != 0 && columnIndex%2 == 0 {
					visibility = "collapse"
				}
				columnWidth := fmt.Sprintf("%dpx", 4+columnIndex)
				switch {
				case rawColumns&0x80 != 0:
					columnWidth = fmt.Sprintf("%g%%", float64((columnIndex+1)*int(rawColumns%37+1))/3)
				case rawColumns&0x40 != 0:
					columnWidth = fmt.Sprintf("calc(%d%% + %dpx)", 1+columnIndex%70, columnIndex%11)
				}
				columnGroup.AppendChild(dom.NewElement("col", dom.Attribute{Name: "style", Value: fmt.Sprintf("width:%s;visibility:%s", columnWidth, visibility)}))
			}
		}
		table.AppendChild(columnGroup)
		groupStyle := ""
		if rawModes&32 != 0 {
			groupStyle = "visibility:collapse"
		}
		group := dom.NewElement("tbody", dom.Attribute{Name: "style", Value: groupStyle})
		for rowIndex := range rows {
			rowVisibility := "visible"
			if rawModes&128 != 0 && rowIndex%2 == 0 {
				rowVisibility = "collapse"
			}
			rowHeight := "auto"
			switch {
			case rawRowSpan&0x20 != 0:
				rowHeight = fmt.Sprintf("%dpx", 1+(rowIndex+int(rawRowSpan))%120)
			case rawRowSpan&0x10 != 0:
				rowHeight = fmt.Sprintf("%d%%", 1+(rowIndex+int(rawRowSpan))%180)
			}
			row := dom.NewElement("tr", dom.Attribute{Name: "style", Value: fmt.Sprintf("visibility:%s;height:%s", rowVisibility, rowHeight)})
			for columnIndex := range columns {
				if rawRowSpan&0x80 != 0 && rowIndex%2 != 0 && columnIndex != 0 {
					continue
				}
				alignment := []string{"baseline", "top", "middle", "bottom"}[(rowIndex+columnIndex)%4]
				borderStyles := [...]string{"solid", "double", "dashed", "dotted", "ridge", "outset", "groove", "inset"}
				borderStyle := borderStyles[(rowIndex+columnIndex+int(rawModes))%len(borderStyles)]
				if rawModes&8 != 0 && rowIndex == 0 && columnIndex == 0 {
					borderStyle = "hidden"
				}
				borderColor := "#abcdef"
				if rawColumnSpan&8 != 0 && (rowIndex+columnIndex)%3 == 0 {
					borderColor = "transparent"
				}
				cellVisibility := ""
				if rawModes&32 != 0 && columnIndex == 0 {
					cellVisibility = "visibility:visible;"
				}
				cellWidth := fmt.Sprintf("%dpx", 4+columnIndex)
				if rawColumnSpan&0x40 != 0 {
					cellWidth = fmt.Sprintf("%g%%", float64((rowIndex+1)*(columnIndex+1)*int(rawColumnSpan%31+1))/5)
				}
				maxWidth := ""
				if rawRowSpan&0x40 != 0 {
					maxWidth = fmt.Sprintf("max-width:%d%%;", 1+(rowIndex+columnIndex)%100)
				}
				cellHeight := "auto"
				switch {
				case rawColumnSpan&0x20 != 0:
					cellHeight = fmt.Sprintf("%dpx", 1+(rowIndex+columnIndex+int(rawColumnSpan))%160)
				case rawColumnSpan&0x10 != 0:
					cellHeight = fmt.Sprintf("%d%%", 1+(rowIndex+columnIndex+int(rawColumnSpan))%220)
				}
				attributes := []dom.Attribute{{Name: "style", Value: fmt.Sprintf(
					"%swidth:%s;%sheight:%s;padding:%dpx;vertical-align:%s;border:%dpx %s %s",
					cellVisibility, cellWidth, maxWidth, cellHeight, rowIndex%3, alignment, 1+(rowIndex+columnIndex)%4, borderStyle, borderColor,
				)}}
				if columnIndex == 0 {
					attributes = append(attributes,
						dom.Attribute{Name: "colspan", Value: fmt.Sprint(columnSpan)},
						dom.Attribute{Name: "rowspan", Value: fmt.Sprint(rowSpan)},
					)
				}
				cell := dom.NewElement("td", attributes...)
				if rawModes&16 != 0 && (rowIndex+columnIndex)%3 == 0 {
					display, overflow, tag := "block", "visible", "div"
					if rawRows&0x20 != 0 {
						display, tag = "inline-block", "span"
					}
					if rawModes&8 != 0 {
						overflow = "auto"
					}
					child := dom.NewElement(tag, dom.Attribute{Name: "style", Value: fmt.Sprintf(
						"display:%s;height:%d%%;min-height:%dpx;overflow:%s;vertical-align:top",
						display, 1+(rowIndex+columnIndex+int(rawModes))%250, (rowIndex+columnIndex)%17, overflow,
					)})
					child.AppendChild(dom.NewElement("span", dom.Attribute{Name: "style", Value: fmt.Sprintf(
						"display:block;height:%dpx", 1+(rowIndex+columnIndex+int(rawModes))%64,
					)}))
					cell.AppendChild(child)
				} else {
					cell.AppendChild(dom.NewText(fmt.Sprintf("%d:%d", rowIndex, columnIndex)))
				}
				row.AppendChild(cell)
			}
			group.AppendChild(row)
		}
		table.AppendChild(group)
		body.AppendChild(table)
		html.AppendChild(dom.NewElement("head"))
		html.AppendChild(body)
		document.AppendChild(html)

		frame, err := render.Render(document, render.Viewport{Width: 320, Height: 240})
		if err != nil {
			t.Fatal(err)
		}
		assertFiniteTableBoxes(t, frame.Root)
		tableBox := findBox(frame.Root, table)
		if tableBox == nil || tableBox.Bounds.Width < 0 || tableBox.Bounds.Height < 0 {
			t.Fatalf("table box = %#v", tableBox)
		}
		if len(frame.DisplayList.Commands) > 100_000 {
			t.Fatalf("table paint command count = %d, want bounded output", len(frame.DisplayList.Commands))
		}
		for _, command := range frame.DisplayList.Commands {
			values := []float64{command.Rect.X, command.Rect.Y, command.Rect.Width, command.Rect.Height, command.X, command.BaselineY}
			for _, value := range values {
				if math.IsNaN(value) || math.IsInf(value, 0) {
					t.Fatalf("non-finite table paint command %#v", command)
				}
			}
		}
	})
}

func assertFiniteTableBoxes(t *testing.T, box *render.Box) {
	t.Helper()
	if box == nil {
		return
	}
	values := []float64{
		box.Bounds.X, box.Bounds.Y, box.Bounds.Width, box.Bounds.Height,
		box.ContentBounds.X, box.ContentBounds.Y, box.ContentBounds.Width, box.ContentBounds.Height,
	}
	for _, value := range values {
		if math.IsNaN(value) || math.IsInf(value, 0) {
			t.Fatalf("non-finite table geometry in %#v", box)
		}
	}
	if box.Bounds.Width < 0 || box.Bounds.Height < 0 || box.ContentBounds.Width < 0 || box.ContentBounds.Height < 0 {
		t.Fatalf("negative table geometry in %#v", box)
	}
	for _, child := range box.Children {
		assertFiniteTableBoxes(t, child)
	}
}
