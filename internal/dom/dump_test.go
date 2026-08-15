package dom

import (
	"bytes"
	"errors"
	"io"
	"strings"
	"testing"
)

func TestDump(t *testing.T) {
	t.Parallel()

	document := NewDocument()
	document.AppendChild(NewDoctype("html"))
	html := NewElement("html", Attribute{Name: "lang", Value: "en"})
	document.AppendChild(html)
	body := NewElement("body", Attribute{Name: "class", Value: "page"}, Attribute{Name: "data-value", Value: "a\"b"})
	html.AppendChild(body)
	body.AppendChild(NewText("Hello\nGossamer"))
	body.AppendChild(NewComment("one line\nthen another"))
	body.AppendChild(NewProcessingInstruction("build", "debug"))

	var output strings.Builder
	if err := Dump(&output, document); err != nil {
		t.Fatalf("Dump() error = %v", err)
	}

	want := "" +
		"#document\n" +
		"  #doctype \"html\"\n" +
		"  <html lang=\"en\">\n" +
		"    <body class=\"page\" data-value=\"a\\\"b\">\n" +
		"      #text \"Hello\\nGossamer\"\n" +
		"      #comment \"one line\\nthen another\"\n" +
		"      #processing-instruction \"build\" \"debug\"\n"
	if output.String() != want {
		t.Errorf("Dump() output:\n%s\nwant:\n%s", output.String(), want)
	}
}

func TestDumpIsDeterministic(t *testing.T) {
	t.Parallel()

	node := NewElement("input",
		Attribute{Name: "z", Value: "last"},
		Attribute{Name: "a", Value: "first"},
	)

	var first bytes.Buffer
	var second bytes.Buffer
	if err := Dump(&first, node); err != nil {
		t.Fatalf("first Dump() error = %v", err)
	}
	if err := Dump(&second, node); err != nil {
		t.Fatalf("second Dump() error = %v", err)
	}
	if first.String() != second.String() {
		t.Errorf("Dump() outputs differ: %q and %q", first.String(), second.String())
	}
	if got, want := first.String(), "<input z=\"last\" a=\"first\">\n"; got != want {
		t.Errorf("Dump() output = %q, want %q", got, want)
	}
}

func TestDumpRejectsNilInputs(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		writer io.Writer
		node   *Node
		want   string
	}{
		{name: "writer", node: NewDocument(), want: "nil dump writer"},
		{name: "node", writer: &strings.Builder{}, want: "nil dump node"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			err := Dump(test.writer, test.node)
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Errorf("Dump() error = %v, want it to contain %q", err, test.want)
			}
		})
	}
}

func TestDumpRejectsInvalidTreeNodes(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		node *Node
		want string
	}{
		{name: "unknown type", node: &Node{Type: NodeType(255)}, want: "unsupported node type 255"},
		{name: "nil child", node: &Node{Type: DocumentNode, Children: []*Node{nil}}, want: "nil child"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			err := Dump(&strings.Builder{}, test.node)
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Errorf("Dump() error = %v, want it to contain %q", err, test.want)
			}
		})
	}
}

func TestDumpReturnsWriterError(t *testing.T) {
	t.Parallel()

	want := errors.New("write failed")
	err := Dump(errorWriter{err: want}, NewDocument())
	if !errors.Is(err, want) {
		t.Errorf("Dump() error = %v, want %v", err, want)
	}
}

type errorWriter struct {
	err error
}

func (writer errorWriter) Write([]byte) (int, error) {
	return 0, writer.err
}
