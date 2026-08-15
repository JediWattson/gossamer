package dom

import (
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"
)

// Dump writes a deterministic, indented representation of node and its
// descendants. All document-provided text is quoted so every node occupies one
// output line.
func Dump(writer io.Writer, node *Node) error {
	if writer == nil {
		return errors.New("dom: nil dump writer")
	}
	if node == nil {
		return errors.New("dom: nil dump node")
	}

	return dumpNode(writer, node, 0)
}

func dumpNode(writer io.Writer, node *Node, depth int) error {
	line, err := dumpLine(node)
	if err != nil {
		return err
	}

	if _, err := fmt.Fprintf(writer, "%s%s\n", strings.Repeat("  ", depth), line); err != nil {
		return err
	}

	for _, child := range node.Children {
		if child == nil {
			return errors.New("dom: nil child in document tree")
		}
		if err := dumpNode(writer, child, depth+1); err != nil {
			return err
		}
	}

	return nil
}

func dumpLine(node *Node) (string, error) {
	switch node.Type {
	case DocumentNode:
		return "#document", nil
	case DoctypeNode:
		return "#doctype " + strconv.Quote(node.Data), nil
	case ElementNode:
		var line strings.Builder
		line.WriteByte('<')
		line.WriteString(node.Data)
		for _, attribute := range node.Attributes {
			line.WriteByte(' ')
			line.WriteString(attribute.Name)
			line.WriteByte('=')
			line.WriteString(strconv.Quote(attribute.Value))
		}
		line.WriteByte('>')
		return line.String(), nil
	case TextNode:
		return "#text " + strconv.Quote(node.Data), nil
	case CommentNode:
		return "#comment " + strconv.Quote(node.Data), nil
	case ProcessingInstructionNode:
		return "#processing-instruction " + strconv.Quote(node.Target) + " " + strconv.Quote(node.Data), nil
	default:
		return "", fmt.Errorf("dom: unsupported node type %d", node.Type)
	}
}
