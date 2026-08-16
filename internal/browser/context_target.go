package browser

import (
	"fmt"
	"net/url"
	"strings"

	"github.com/JediWattson/gossamer/internal/dom"
)

// ContextTarget is copied context-menu metadata for one hit-tested element.
// It carries no DOM pointer and may safely cross into browser-owned chrome.
type ContextTarget struct {
	Handle   NodeHandle
	Tag      string
	Text     string
	LinkURL  *url.URL
	ImageURL *url.URL
	Editable bool
}

func (page *Page) ContextTarget(handle NodeHandle) (ContextTarget, error) {
	if page == nil {
		return ContextTarget{}, fmt.Errorf("browser: nil page")
	}
	page.mutex.RLock()
	if page.closed || handle.Document != page.documentGeneration || handle.Node == dom.InvalidNodeID {
		page.mutex.RUnlock()
		return ContextTarget{}, ErrStaleNodeHandle
	}
	document := page.document
	location := cloneURL(page.location)
	page.mutex.RUnlock()
	node, ok := document.Resolve(handle.Node)
	if !ok || node.Type != dom.ElementNode {
		return ContextTarget{}, dom.ErrWrongNodeKind
	}
	result := ContextTarget{Handle: handle, Tag: strings.ToLower(node.Data)}
	result.Text, _ = document.TextContent(handle.Node)
	result.Editable = result.Tag == "textarea" || result.Tag == "input"
	if source, found, _ := document.GetAttribute(handle.Node, "src"); found {
		result.ImageURL = resolveContextURL(location, source)
	}
	for candidate := handle.Node; candidate != dom.InvalidNodeID; {
		current, found := document.Resolve(candidate)
		if !found {
			break
		}
		if current.Type == dom.ElementNode && strings.EqualFold(current.Data, "a") {
			if href, exists, _ := document.GetAttribute(candidate, "href"); exists {
				result.LinkURL = resolveContextURL(location, href)
				break
			}
		}
		parent, found, err := document.RelatedNode(candidate, dom.ParentElement)
		if err != nil || !found {
			break
		}
		candidate = parent
	}
	return result, nil
}

func resolveContextURL(base *url.URL, raw string) *url.URL {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return nil
	}
	if base != nil {
		parsed = base.ResolveReference(parsed)
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return nil
	}
	return parsed
}
