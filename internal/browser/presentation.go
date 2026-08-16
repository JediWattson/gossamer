package browser

import (
	"net/url"
	"strings"

	"github.com/JediWattson/gossamer/internal/dom"
	"github.com/JediWattson/gossamer/internal/resource"
)

// PageMetadata is the browser-owned, live presentation metadata consumed by
// tabs and native window chrome. It never retains DOM nodes across calls.
type PageMetadata struct {
	Title      string
	FaviconURL *url.URL
}

func (page *Page) Metadata() PageMetadata {
	if page == nil {
		return PageMetadata{}
	}
	page.mutex.RLock()
	document := page.document
	location := cloneURL(page.location)
	metadata := pageMetadata(document, location)
	page.mutex.RUnlock()
	return metadata
}

func pageMetadata(document *dom.Document, location *url.URL) PageMetadata {
	if document == nil || document.Root() == nil {
		return PageMetadata{}
	}
	metadata := PageMetadata{Title: documentTitle(document.Root())}
	if location == nil {
		return metadata
	}
	graph, err := resource.Discover(document.Root(), location)
	if err != nil {
		return metadata
	}
	for _, reference := range graph.References {
		if reference.Kind == resource.Image && reference.Node != nil &&
			strings.EqualFold(reference.Node.Data, "link") {
			metadata.FaviconURL = cloneURL(reference.URL)
		}
	}
	return metadata
}

func documentTitle(root *dom.Node) string {
	var title string
	found := false
	var walk func(*dom.Node)
	walk = func(node *dom.Node) {
		if node == nil || found {
			return
		}
		if node.Type == dom.ElementNode && strings.EqualFold(node.Data, "title") {
			found = true
			title = collapseDocumentTitle(nodeText(node))
			return
		}
		for _, child := range node.Children {
			walk(child)
		}
	}
	walk(root)
	return title
}

func nodeText(node *dom.Node) string {
	if node == nil {
		return ""
	}
	if node.Type == dom.TextNode {
		return node.Data
	}
	var text strings.Builder
	for _, child := range node.Children {
		text.WriteString(nodeText(child))
	}
	return text.String()
}

func collapseDocumentTitle(value string) string {
	return strings.Join(strings.Fields(value), " ")
}

func findDocumentElement(root *dom.Node, name string) *dom.Node {
	if root == nil {
		return nil
	}
	if root.Type == dom.ElementNode && strings.EqualFold(root.Data, name) {
		return root
	}
	for _, child := range root.Children {
		if found := findDocumentElement(child, name); found != nil {
			return found
		}
	}
	return nil
}
