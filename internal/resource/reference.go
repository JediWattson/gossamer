// Package resource discovers and retrieves the external resources referenced
// by a DOM document.
package resource

import (
	"fmt"
	"net/url"
	"strings"

	"github.com/JediWattson/gossamer/internal/dom"
	"github.com/JediWattson/gossamer/internal/loader"
)

// Kind identifies how a DOM reference will be consumed.
type Kind uint8

const (
	Unknown Kind = iota
	Stylesheet
	Image
)

func (kind Kind) String() string {
	switch kind {
	case Stylesheet:
		return "stylesheet"
	case Image:
		return "image"
	default:
		return "unknown"
	}
}

func (kind Kind) destination() loader.Destination {
	switch kind {
	case Stylesheet:
		return loader.StyleDestination
	case Image:
		return loader.ImageDestination
	default:
		return loader.DocumentDestination
	}
}

// Reference connects a fetchable absolute URL to the DOM element and
// attribute that initiated it.
type Reference struct {
	Kind      Kind
	URL       *url.URL
	Node      *dom.Node
	Attribute string
}

// Graph is the resource view of a parsed document. References preserve DOM
// tree order and are not deduplicated because multiple elements can consume
// the same fetched bytes independently.
type Graph struct {
	DocumentURL *url.URL
	BaseURL     *url.URL
	References  []Reference
}

// Discover resolves the document's base URL and finds its initial external
// stylesheet and image references. Invalid and non-HTTP(S) references are
// ignored; they do not invalidate the document.
func Discover(document *dom.Node, documentURL *url.URL) (Graph, error) {
	if document == nil {
		return Graph{}, fmt.Errorf("resource: nil document")
	}
	if document.Type != dom.DocumentNode {
		return Graph{}, fmt.Errorf("resource: root node must be a document")
	}
	if documentURL == nil || !documentURL.IsAbs() {
		return Graph{}, fmt.Errorf("resource: document URL must be absolute")
	}

	documentURLCopy := cloneURL(documentURL)
	baseURL := findBaseURL(document, documentURLCopy)
	graph := Graph{
		DocumentURL: documentURLCopy,
		BaseURL:     cloneURL(baseURL),
	}

	walk(document, func(node *dom.Node) {
		if node.Type != dom.ElementNode {
			return
		}
		for _, candidate := range elementReferences(node) {
			resolved, ok := resolveHTTPReference(baseURL, candidate.value)
			if !ok {
				continue
			}
			graph.References = append(graph.References, Reference{
				Kind:      candidate.kind,
				URL:       resolved,
				Node:      node,
				Attribute: candidate.attribute,
			})
		}
	})
	return graph, nil
}

type referenceCandidate struct {
	kind      Kind
	attribute string
	value     string
}

func elementReferences(node *dom.Node) []referenceCandidate {
	name := strings.ToLower(node.Data)
	switch name {
	case "link":
		href, hasHref := attribute(node, "href")
		if !hasHref || strings.TrimSpace(href) == "" {
			return nil
		}
		rel, _ := attribute(node, "rel")
		var references []referenceCandidate
		if containsHTMLToken(rel, "stylesheet") {
			references = append(references, referenceCandidate{kind: Stylesheet, attribute: "href", value: href})
		}
		if containsHTMLToken(rel, "icon") {
			references = append(references, referenceCandidate{kind: Image, attribute: "href", value: href})
		}
		return references

	case "img":
		if source, ok := nonEmptyAttribute(node, "src"); ok {
			return []referenceCandidate{{kind: Image, attribute: "src", value: source}}
		}

	case "input":
		typeValue, _ := attribute(node, "type")
		if strings.EqualFold(strings.TrimSpace(typeValue), "image") {
			if source, ok := nonEmptyAttribute(node, "src"); ok {
				return []referenceCandidate{{kind: Image, attribute: "src", value: source}}
			}
		}

	case "video":
		if poster, ok := nonEmptyAttribute(node, "poster"); ok {
			return []referenceCandidate{{kind: Image, attribute: "poster", value: poster}}
		}
	}
	return nil
}

func findBaseURL(document *dom.Node, fallback *url.URL) *url.URL {
	var result *url.URL
	found := false
	walk(document, func(node *dom.Node) {
		if found || node.Type != dom.ElementNode || !strings.EqualFold(node.Data, "base") {
			return
		}
		href, ok := attribute(node, "href")
		if !ok {
			return
		}
		found = true
		parsed, err := url.Parse(strings.TrimSpace(href))
		if err == nil {
			result = fallback.ResolveReference(parsed)
		}
	})
	if result == nil {
		return cloneURL(fallback)
	}
	return result
}

func resolveHTTPReference(baseURL *url.URL, source string) (*url.URL, bool) {
	parsed, err := url.Parse(strings.TrimSpace(source))
	if err != nil {
		return nil, false
	}
	resolved := baseURL.ResolveReference(parsed)
	if !strings.EqualFold(resolved.Scheme, "http") && !strings.EqualFold(resolved.Scheme, "https") {
		return nil, false
	}
	if resolved.Hostname() == "" {
		return nil, false
	}
	resolved.Scheme = strings.ToLower(resolved.Scheme)
	resolved.Fragment = ""
	return resolved, true
}

func walk(root *dom.Node, visit func(*dom.Node)) {
	if root == nil {
		return
	}
	visit(root)
	for _, child := range root.Children {
		walk(child, visit)
	}
}

func nonEmptyAttribute(node *dom.Node, name string) (string, bool) {
	value, ok := attribute(node, name)
	return value, ok && strings.TrimSpace(value) != ""
}

func attribute(node *dom.Node, name string) (string, bool) {
	for _, candidate := range node.Attributes {
		if strings.EqualFold(candidate.Name, name) {
			return candidate.Value, true
		}
	}
	return "", false
}

func containsHTMLToken(source, token string) bool {
	for _, candidate := range strings.Fields(source) {
		if strings.EqualFold(candidate, token) {
			return true
		}
	}
	return false
}

func cloneURL(source *url.URL) *url.URL {
	if source == nil {
		return nil
	}
	clone := *source
	return &clone
}
