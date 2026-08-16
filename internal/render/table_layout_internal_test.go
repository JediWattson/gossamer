package render

import (
	"strings"
	"testing"

	"github.com/JediWattson/gossamer/internal/dom"
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

func internalTableNode(display displayMode, node *dom.Node) *styledNode {
	return &styledNode{node: node, style: computedStyle{}.WithAnonymousDisplay(display)}
}
