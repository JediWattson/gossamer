package html

import (
	"strings"

	"github.com/JediWattson/gossamer/internal/dom"
)

// SerializeChildren returns the HTML fragment represented by node's direct
// children. Attribute order follows the DOM's insertion order.
func SerializeChildren(node *dom.Node) string {
	if node == nil {
		return ""
	}
	var output strings.Builder
	for _, child := range node.Children {
		serializeNode(&output, child, false)
	}
	return output.String()
}

// SerializeNode returns one node and its descendants as HTML.
func SerializeNode(node *dom.Node) string {
	var output strings.Builder
	serializeNode(&output, node, false)
	return output.String()
}

func serializeNode(output *strings.Builder, node *dom.Node, rawText bool) {
	if node == nil {
		return
	}
	switch node.Type {
	case dom.DocumentNode, dom.DocumentFragmentNode:
		for _, child := range node.Children {
			serializeNode(output, child, false)
		}
	case dom.DoctypeNode:
		output.WriteString("<!DOCTYPE ")
		output.WriteString(node.Data)
		output.WriteByte('>')
	case dom.CommentNode:
		output.WriteString("<!--")
		output.WriteString(node.Data)
		output.WriteString("-->")
	case dom.ProcessingInstructionNode:
		output.WriteString("<?")
		output.WriteString(node.Target)
		if node.Data != "" {
			output.WriteByte(' ')
			output.WriteString(node.Data)
		}
		output.WriteByte('>')
	case dom.TextNode:
		if rawText {
			output.WriteString(node.Data)
		} else {
			writeEscapedText(output, node.Data)
		}
	case dom.ElementNode:
		name := node.QualifiedName()
		output.WriteByte('<')
		output.WriteString(name)
		for _, attribute := range node.Attributes {
			output.WriteByte(' ')
			output.WriteString(attribute.Name)
			output.WriteString(`="`)
			writeEscapedAttribute(output, attribute.Value)
			output.WriteByte('"')
		}
		output.WriteByte('>')
		if node.NamespaceURI == dom.HTMLNamespace && isVoidElement(strings.ToLower(node.Data)) {
			return
		}
		raw := node.NamespaceURI == dom.HTMLNamespace && isRawTextElement(node.Data)
		children := node.Children
		if node.NamespaceURI == dom.HTMLNamespace && node.Data == "template" && node.TemplateContent != nil {
			children = node.TemplateContent.Children
		}
		for _, child := range children {
			serializeNode(output, child, raw)
		}
		output.WriteString("</")
		output.WriteString(name)
		output.WriteByte('>')
	}
}

func isRawTextElement(name string) bool {
	switch strings.ToLower(name) {
	case "style", "script", "xmp", "iframe", "noembed", "noframes":
		return true
	default:
		return false
	}
}

func writeEscapedText(output *strings.Builder, value string) {
	for _, character := range value {
		switch character {
		case '&':
			output.WriteString("&amp;")
		case '<':
			output.WriteString("&lt;")
		case '>':
			output.WriteString("&gt;")
		default:
			output.WriteRune(character)
		}
	}
}

func writeEscapedAttribute(output *strings.Builder, value string) {
	for _, character := range value {
		switch character {
		case '&':
			output.WriteString("&amp;")
		case '"':
			output.WriteString("&quot;")
		default:
			output.WriteRune(character)
		}
	}
}
