package browser

import (
	"fmt"
	"net/url"
	"strings"

	"github.com/JediWattson/gossamer/internal/dom"
)

// visitedLinkMatcherLocked returns a snapshot of the Page's privacy-visible
// session history. Gossamer deliberately exposes only same-origin destinations
// from this Page's own history: a document can observe visits to its own origin,
// but cannot probe arbitrary cross-origin browsing history through selectors or
// computed style.
func (page *Page) visitedLinkMatcherLocked(root *dom.Node) func(*dom.Node) bool {
	documentURL := cloneURL(page.location)
	baseURL := stylesheetBaseURL(root, documentURL)
	visited := make(map[string]struct{}, len(page.history))
	for _, entry := range page.history {
		if !sameOriginURL(documentURL, entry.URL) {
			continue
		}
		if key, ok := visitedURLKey(entry.URL); ok {
			visited[key] = struct{}{}
		}
	}
	return func(node *dom.Node) bool {
		destination, ok := resolvedHyperlinkURL(node, baseURL)
		if !ok || !sameOriginURL(documentURL, destination) {
			return false
		}
		key, ok := visitedURLKey(destination)
		if !ok {
			return false
		}
		_, ok = visited[key]
		return ok
	}
}

func (page *Page) visitedLinkIDsForViewLocked(view dom.ReadView) ([]dom.NodeID, error) {
	access, err := view.Acquire()
	if err != nil {
		return nil, err
	}
	defer access.Close()
	root := access.Root()
	if root == nil {
		return nil, fmt.Errorf("%w: visited-link read has no document root", dom.ErrInvalidDocument)
	}
	matcher := page.visitedLinkMatcherLocked(root)
	stack := []*dom.Node{root}
	var result []dom.NodeID
	for len(stack) != 0 {
		last := len(stack) - 1
		node := stack[last]
		stack = stack[:last]
		if node.Type == dom.ElementNode && matcher(node) {
			if id, ok := access.ID(node); ok {
				result = append(result, id)
			}
		}
		for index := len(node.Children) - 1; index >= 0; index-- {
			stack = append(stack, node.Children[index])
		}
	}
	return result, nil
}

func resolvedHyperlinkURL(node *dom.Node, base *url.URL) (*url.URL, bool) {
	if node == nil || node.Type != dom.ElementNode || base == nil {
		return nil, false
	}
	name := strings.ToLower(node.Data)
	if (node.NamespaceURI != dom.HTMLNamespace || (name != "a" && name != "area")) &&
		(node.NamespaceURI != dom.SVGNamespace || name != "a") {
		return nil, false
	}
	href, ok := nodeAttribute(node, "href")
	if !ok {
		return nil, false
	}
	reference, err := url.Parse(strings.TrimSpace(href))
	if err != nil {
		return nil, false
	}
	return base.ResolveReference(reference), true
}

func sameOriginURL(left, right *url.URL) bool {
	if left == nil || right == nil || left.Scheme == "" || right.Scheme == "" {
		return false
	}
	return strings.EqualFold(left.Scheme, right.Scheme) &&
		strings.EqualFold(left.Hostname(), right.Hostname()) &&
		effectiveURLPort(left) == effectiveURLPort(right)
}

func effectiveURLPort(value *url.URL) string {
	if value == nil {
		return ""
	}
	if port := value.Port(); port != "" {
		return port
	}
	switch strings.ToLower(value.Scheme) {
	case "http":
		return "80"
	case "https":
		return "443"
	default:
		return ""
	}
}

func visitedURLKey(source *url.URL) (string, bool) {
	if source == nil || source.Scheme == "" || source.Hostname() == "" {
		return "", false
	}
	canonical := *source
	canonical.Scheme = strings.ToLower(canonical.Scheme)
	hostname := strings.ToLower(canonical.Hostname())
	if strings.Contains(hostname, ":") {
		hostname = "[" + hostname + "]"
	}
	port := canonical.Port()
	if (canonical.Scheme == "http" && port == "80") || (canonical.Scheme == "https" && port == "443") {
		port = ""
	}
	canonical.Host = hostname
	if port != "" {
		canonical.Host += ":" + port
	}
	canonical.User = nil
	canonical.Fragment = ""
	canonical.RawFragment = ""
	if canonical.Path == "" {
		canonical.Path = "/"
	}
	return canonical.String(), true
}
