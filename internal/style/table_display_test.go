package style_test

import (
	"testing"

	"github.com/JediWattson/gossamer/internal/dom"
	"github.com/JediWattson/gossamer/internal/style"
)

func TestHTMLTableElementsReceiveUserAgentDisplayRoles(t *testing.T) {
	t.Parallel()

	document := dom.NewDocument()
	html := dom.NewElement("html")
	body := dom.NewElement("body")
	table := dom.NewElement("table")
	caption := dom.NewElement("caption")
	colgroup := dom.NewElement("colgroup")
	column := dom.NewElement("col")
	thead := dom.NewElement("thead")
	tbody := dom.NewElement("tbody")
	tfoot := dom.NewElement("tfoot")
	row := dom.NewElement("tr")
	cell := dom.NewElement("td")
	header := dom.NewElement("th")
	colgroup.AppendChild(column)
	row.AppendChild(cell)
	row.AppendChild(header)
	tbody.AppendChild(row)
	for _, child := range []*dom.Node{caption, colgroup, thead, tbody, tfoot} {
		table.AppendChild(child)
	}
	body.AppendChild(table)
	html.AppendChild(dom.NewElement("head"))
	html.AppendChild(body)
	document.AppendChild(html)

	snapshot := style.Compute(document, style.Input{})
	tests := []struct {
		node *dom.Node
		want string
	}{
		{table, "table"},
		{caption, "table-caption"},
		{colgroup, "table-column-group"},
		{column, "table-column"},
		{thead, "table-header-group"},
		{tbody, "table-row-group"},
		{tfoot, "table-footer-group"},
		{row, "table-row"},
		{cell, "table-cell"},
		{header, "table-cell"},
	}
	for _, test := range tests {
		computed, ok := snapshot.Lookup(test.node)
		if !ok {
			t.Fatalf("style missing for <%s>", test.node.Data)
		}
		if got, _ := style.ComputedPropertyValue(computed, "display"); got != test.want {
			t.Errorf("<%s> display = %q, want %q", test.node.Data, got, test.want)
		}
	}
}

func TestAuthorDisplayAcceptsEveryTableRole(t *testing.T) {
	t.Parallel()

	roles := []string{
		"table", "inline-table", "table-row-group", "table-header-group", "table-footer-group",
		"table-row", "table-cell", "table-column-group", "table-column", "table-caption",
	}
	document := dom.NewDocument()
	html := dom.NewElement("html")
	body := dom.NewElement("body")
	html.AppendChild(dom.NewElement("head"))
	html.AppendChild(body)
	document.AppendChild(html)
	nodes := make([]*dom.Node, len(roles))
	for index, role := range roles {
		nodes[index] = dom.NewElement("x-role", dom.Attribute{Name: "style", Value: "display:" + role})
		body.AppendChild(nodes[index])
	}
	snapshot := style.Compute(document, style.Input{})
	for index, node := range nodes {
		computed, ok := snapshot.Lookup(node)
		if !ok {
			t.Fatalf("style missing for %q", roles[index])
		}
		if got, _ := style.ComputedPropertyValue(computed, "display"); got != roles[index] {
			t.Errorf("display = %q, want %q", got, roles[index])
		}
	}
}
