package render

import (
	"image/color"
	"strings"
	"testing"

	"github.com/JediWattson/gossamer/internal/dom"
	computed "github.com/JediWattson/gossamer/internal/style"
)

func TestTableModelPlacesOverlappingSpansAndRowspanZeroWithinGroup(t *testing.T) {
	t.Parallel()

	table := internalTableNode(displayTable, nil)
	group := internalTableNode(displayRowGroup, nil)
	for rowIndex := range 3 {
		row := internalTableNode(displayTableRow, nil)
		if rowIndex == 0 {
			row.children = append(row.children, internalTableNode(displayTableCell, dom.NewElement("td",
				dom.Attribute{Name: "colspan", Value: "2"},
				dom.Attribute{Name: "rowspan", Value: "0"},
			)))
		}
		row.children = append(row.children, internalTableNode(displayTableCell, dom.NewElement("td")))
		group.children = append(group.children, row)
	}
	table.children = append(table.children, group)
	model, err := buildTableModel(table)
	if err != nil {
		t.Fatal(err)
	}
	if len(model.cells) != 4 || model.cells[0].column != 0 || model.cells[0].columnSpan != 2 || model.cells[0].rowSpan != 3 {
		t.Fatalf("spanning placement = %#v", model.cells)
	}
	for index, placement := range model.cells[1:] {
		if placement.row != index || placement.column != 2 {
			t.Errorf("ordinary placement %d = %#v, want row %d column 2", index, placement, index)
		}
	}
}

func TestTableModelEnforcesRowAndColumnBudgets(t *testing.T) {
	t.Parallel()

	tooManyRows := internalTableNode(displayTable, nil)
	for range maxTableRows + 1 {
		tooManyRows.children = append(tooManyRows.children, internalTableNode(displayTableRow, nil))
	}
	if _, err := buildTableModel(tooManyRows); err == nil || !strings.Contains(err.Error(), "rows") {
		t.Fatalf("row budget error = %v", err)
	}

	tooManyColumns := internalTableNode(displayTable, nil)
	tooManyColumns.children = append(tooManyColumns.children,
		internalTableNode(displayTableColumn, dom.NewElement("col", dom.Attribute{Name: "span", Value: "1024"})),
		internalTableNode(displayTableColumn, dom.NewElement("col")),
	)
	if _, err := buildTableModel(tooManyColumns); err == nil || !strings.Contains(err.Error(), "columns") {
		t.Fatalf("column budget error = %v", err)
	}

	expensiveGrid := internalTableNode(displayTable, nil)
	for range maxTableRows {
		row := internalTableNode(displayTableRow, nil)
		row.children = append(row.children, internalTableNode(displayTableCell, dom.NewElement("td", dom.Attribute{Name: "colspan", Value: "1024"})))
		expensiveGrid.children = append(expensiveGrid.children, row)
	}
	if _, err := buildTableModel(expensiveGrid); err == nil || !strings.Contains(err.Error(), "grid operations") {
		t.Fatalf("grid operation budget error = %v", err)
	}
}

func TestCollapsedBorderSpecificityOrder(t *testing.T) {
	t.Parallel()

	solid := collapsedBorder{style: borderStyleSolid, width: 2, source: collapsedBorderTable}
	tests := []struct {
		name      string
		candidate collapsedBorder
		current   collapsedBorder
		wins      bool
	}{
		{name: "hidden suppresses solid", candidate: collapsedBorder{style: borderStyleHidden}, current: solid, wins: true},
		{name: "solid cannot replace hidden", candidate: solid, current: collapsedBorder{style: borderStyleHidden}, wins: false},
		{name: "solid beats none", candidate: solid, current: collapsedBorder{style: borderStyleNone}, wins: true},
		{name: "wider beats role", candidate: collapsedBorder{style: borderStyleSolid, width: 3, source: collapsedBorderTable}, current: collapsedBorder{style: borderStyleSolid, width: 2, source: collapsedBorderCell}, wins: true},
		{name: "cell beats table on tie", candidate: collapsedBorder{style: borderStyleSolid, width: 2, source: collapsedBorderCell}, current: solid, wins: true},
		{name: "first top-left cell survives exact tie", candidate: collapsedBorder{style: borderStyleSolid, width: 2, source: collapsedBorderCell, color: color.NRGBA{R: 1}}, current: collapsedBorder{style: borderStyleSolid, width: 2, source: collapsedBorderCell, color: color.NRGBA{B: 1}}, wins: false},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := collapsedBorderMoreSpecific(test.candidate, test.current); got != test.wins {
				t.Fatalf("collapsedBorderMoreSpecific() = %t, want %t", got, test.wins)
			}
		})
	}
}

func TestCollapsedBorderSegmentBudgetFailsClosed(t *testing.T) {
	t.Parallel()

	document := dom.NewDocument()
	html := dom.NewElement("html")
	body := dom.NewElement("body")
	tableNode := dom.NewElement("table", dom.Attribute{Name: "style", Value: "border-collapse:collapse"})
	body.AppendChild(tableNode)
	html.AppendChild(dom.NewElement("head"))
	html.AppendChild(body)
	document.AppendChild(html)
	computedStyle, ok := computed.Compute(document, computed.Input{}).Lookup(tableNode)
	if !ok {
		t.Fatal("computed table style missing")
	}
	table := &styledNode{node: tableNode, style: computedStyle}
	model := tableModel{rows: make([]tableRowRecord, 1000), columnCount: 1000}
	context := layoutContext{viewport: Viewport{Width: 800, Height: 600}}
	if _, err := context.resolveCollapsedTableBorders(table, model); err == nil || !strings.Contains(err.Error(), "border segments") {
		t.Fatalf("collapsed segment budget error = %v", err)
	}
}

func TestTableStructuralBackgroundBudgetFailsClosed(t *testing.T) {
	t.Parallel()

	document := dom.NewDocument()
	html := dom.NewElement("html")
	body := dom.NewElement("body")
	tableNode := dom.NewElement("table")
	rowNode := dom.NewElement("tr", dom.Attribute{Name: "style", Value: "background:#123456"})
	tableNode.AppendChild(rowNode)
	body.AppendChild(tableNode)
	html.AppendChild(dom.NewElement("head"))
	html.AppendChild(body)
	document.AppendChild(html)
	rowStyle, ok := computed.Compute(document, computed.Input{}).Lookup(rowNode)
	if !ok {
		t.Fatal("computed row style missing")
	}
	row := &styledNode{node: rowNode, style: rowStyle}
	model := tableModel{rows: make([]tableRowRecord, 1000), columnCount: 1000}
	for index := range model.rows {
		model.rows[index].node = row
	}
	if count := tableStructuralBackgroundRectCount(model); count <= maxTableBackgroundRects {
		t.Fatalf("structural background rect count = %d, want over budget %d", count, maxTableBackgroundRects)
	}
}

func internalTableNode(display displayMode, node *dom.Node) *styledNode {
	return &styledNode{node: node, style: computedStyle{}.WithAnonymousDisplay(display)}
}
