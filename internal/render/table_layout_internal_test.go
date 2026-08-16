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
	if len(model.cells) != 4 || model.cells[0].column != 0 || model.cells[0].columnSpan != 1 || model.cells[0].rowSpan != 3 {
		t.Fatalf("spanning placement = %#v", model.cells)
	}
	for index, placement := range model.cells[1:] {
		if placement.row != index || placement.column != 1 {
			t.Errorf("ordinary placement %d = %#v, want row %d column 1", index, placement, index)
		}
	}
}

func TestTableModelMergesAnonymousColumnsBeforeFillingMissingCells(t *testing.T) {
	t.Parallel()

	spanned := internalTableNode(displayTable, nil)
	for range 2 {
		row := internalTableNode(displayTableRow, nil)
		row.children = append(row.children,
			internalTableNode(displayTableCell, dom.NewElement("td", dom.Attribute{Name: "colspan", Value: "10"})),
			internalTableNode(displayTableCell, dom.NewElement("td")),
		)
		spanned.children = append(spanned.children, row)
	}
	model, err := buildTableModel(spanned)
	if err != nil {
		t.Fatal(err)
	}
	if model.columnCount != 2 || len(model.cells) != 4 {
		t.Fatalf("merged colspan grid = %d columns, %d cells; want 2 and 4", model.columnCount, len(model.cells))
	}
	for index, placement := range model.cells {
		if placement.columnSpan != 1 || placement.column != index%2 {
			t.Errorf("merged placement %d = %#v", index, placement)
		}
	}

	columns := internalTableNode(displayTable, nil)
	column := internalTableNode(displayTableColumn, dom.NewElement("col", dom.Attribute{Name: "span", Value: "10"}))
	columns.children = append(columns.children, column)
	for range 2 {
		row := internalTableNode(displayTableRow, nil)
		row.children = append(row.children,
			internalTableNode(displayTableCell, dom.NewElement("td")),
			internalTableNode(displayTableCell, dom.NewElement("td")),
		)
		columns.children = append(columns.children, row)
	}
	model, err = buildTableModel(columns)
	if err != nil {
		t.Fatal(err)
	}
	if model.columnCount != 2 || len(model.columnBoxes) != 1 || model.columnBoxes[0].span != 2 {
		t.Fatalf("merged col span model = columns:%d boxes:%#v", model.columnCount, model.columnBoxes)
	}
}

func TestTableModelPreservesExplicitTracksAndSynthesizesMissingCells(t *testing.T) {
	t.Parallel()

	table := internalTableNode(displayTable, nil)
	for range 3 {
		table.children = append(table.children, internalTableNode(displayTableColumn, dom.NewElement("col")))
	}
	first := internalTableNode(displayTableRow, nil)
	for range 3 {
		first.children = append(first.children, internalTableNode(displayTableCell, dom.NewElement("td")))
	}
	second := internalTableNode(displayTableRow, nil)
	second.children = append(second.children, internalTableNode(displayTableCell, dom.NewElement("td")))
	table.children = append(table.children, first, second)

	model, err := buildTableModel(table)
	if err != nil {
		t.Fatal(err)
	}
	if model.columnCount != 3 || len(model.cells) != 6 {
		t.Fatalf("missing-cell grid = %d columns, %d cells; want 3 and 6", model.columnCount, len(model.cells))
	}
	for index, placement := range model.cells[4:] {
		if placement.node == nil || placement.node.node != nil || placement.row != 1 || placement.column != index+1 || placement.rowSpan != 1 || placement.columnSpan != 1 {
			t.Errorf("anonymous missing cell %d = %#v", index, placement)
		}
	}
}

func TestTableModelPreservesFixedAndWidthConstrainedColumnSpans(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		tableStyle string
		column     *dom.Node
		want       int
	}{
		{
			name: "fixed layout keeps cell colspan tracks", tableStyle: "table-layout:fixed;width:400px",
			want: 11,
		},
		{
			name:   "nonzero column width keeps span tracks",
			column: dom.NewElement("col", dom.Attribute{Name: "span", Value: "10"}, dom.Attribute{Name: "style", Value: "width:30px"}),
			want:   10,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			document := dom.NewDocument()
			html := dom.NewElement("html")
			body := dom.NewElement("body")
			tableNode := dom.NewElement("table", dom.Attribute{Name: "style", Value: test.tableStyle})
			if test.column != nil {
				tableNode.AppendChild(test.column)
			}
			for range 2 {
				row := dom.NewElement("tr")
				if test.column == nil {
					row.AppendChild(dom.NewElement("td", dom.Attribute{Name: "colspan", Value: "10"}))
				}
				row.AppendChild(dom.NewElement("td"))
				if test.column != nil {
					row.AppendChild(dom.NewElement("td"))
				}
				tableNode.AppendChild(row)
			}
			body.AppendChild(tableNode)
			html.AppendChild(dom.NewElement("head"))
			html.AppendChild(body)
			document.AppendChild(html)

			projected := projectStyleTree(document, computed.Compute(document, computed.Input{}))
			fixupTableFormattingTree(projected)
			table := internalFindStyledNode(projected, tableNode)
			if table == nil {
				t.Fatal("projected table missing")
			}
			model, err := buildTableModel(table)
			if err != nil {
				t.Fatal(err)
			}
			if model.columnCount != test.want {
				t.Fatalf("column count = %d, want %d", model.columnCount, test.want)
			}
		})
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
		{name: "double beats solid at equal width", candidate: collapsedBorder{style: borderStyleDouble, width: 2}, current: solid, wins: true},
		{name: "solid beats dashed at equal width", candidate: solid, current: collapsedBorder{style: borderStyleDashed, width: 2}, wins: true},
		{name: "dashed beats dotted at equal width", candidate: collapsedBorder{style: borderStyleDashed, width: 2}, current: collapsedBorder{style: borderStyleDotted, width: 2}, wins: true},
		{name: "dotted beats ridge at equal width", candidate: collapsedBorder{style: borderStyleDotted, width: 2}, current: collapsedBorder{style: borderStyleRidge, width: 2}, wins: true},
		{name: "ridge beats outset at equal width", candidate: collapsedBorder{style: borderStyleRidge, width: 2}, current: collapsedBorder{style: borderStyleOutset, width: 2}, wins: true},
		{name: "outset beats groove at equal width", candidate: collapsedBorder{style: borderStyleOutset, width: 2}, current: collapsedBorder{style: borderStyleGroove, width: 2}, wins: true},
		{name: "groove beats inset at equal width", candidate: collapsedBorder{style: borderStyleGroove, width: 2}, current: collapsedBorder{style: borderStyleInset, width: 2}, wins: true},
		{name: "wider beats role", candidate: collapsedBorder{style: borderStyleSolid, width: 3, source: collapsedBorderTable}, current: collapsedBorder{style: borderStyleSolid, width: 2, source: collapsedBorderCell}, wins: true},
		{name: "cell beats table on tie", candidate: collapsedBorder{style: borderStyleSolid, width: 2, source: collapsedBorderCell}, current: solid, wins: true},
		{name: "first top-left cell survives exact tie", candidate: collapsedBorder{style: borderStyleSolid, width: 2, source: collapsedBorderCell, color: color.NRGBA{R: 1}}, current: collapsedBorder{style: borderStyleSolid, width: 2, source: collapsedBorderCell, color: color.NRGBA{B: 1}}, wins: false},
		{name: "earlier logical row wins independent of traversal", candidate: collapsedBorder{style: borderStyleSolid, width: 2, source: collapsedBorderCell, logicalRow: 0, logicalColumn: 2}, current: collapsedBorder{style: borderStyleSolid, width: 2, source: collapsedBorderCell, logicalRow: 1}, wins: true},
		{name: "later logical row loses independent of traversal", candidate: collapsedBorder{style: borderStyleSolid, width: 2, source: collapsedBorderCell, logicalRow: 2}, current: collapsedBorder{style: borderStyleSolid, width: 2, source: collapsedBorderCell, logicalRow: 1}, wins: false},
		{name: "earlier logical column wins independent of traversal", candidate: collapsedBorder{style: borderStyleSolid, width: 2, source: collapsedBorderCell, logicalColumn: 1}, current: collapsedBorder{style: borderStyleSolid, width: 2, source: collapsedBorderCell, logicalColumn: 2}, wins: true},
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

func internalFindStyledNode(root *styledNode, node *dom.Node) *styledNode {
	if root == nil {
		return nil
	}
	if root.node == node {
		return root
	}
	for _, child := range root.children {
		if found := internalFindStyledNode(child, node); found != nil {
			return found
		}
	}
	return nil
}
