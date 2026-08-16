package browser

import (
	"context"
	"fmt"
	"net/url"
	"slices"
	"strings"

	"github.com/JediWattson/gossamer/internal/css"
	"github.com/JediWattson/gossamer/internal/dom"
	"github.com/JediWattson/gossamer/internal/resource"
	browserruntime "github.com/JediWattson/gossamer/internal/runtime"
)

type stylesheetOwnerKind uint8

const (
	stylesheetOwnerEmbedded stylesheetOwnerKind = iota + 1
	stylesheetOwnerExternal
)

type stylesheetGraphEntry struct {
	owner      dom.NodeID
	kind       stylesheetOwnerKind
	generation uint64
	signature  string
	resolved   *url.URL
	stylesheet css.Stylesheet
	ready      bool
	requested  bool
	manual     bool
}

// stylesheetGraph is the browser-owned identity and generation layer for
// document stylesheets. Entries are keyed by stable owner identity and order
// is the current connected document order. Async results must match both the
// document generation and the entry generation before they can publish.
type stylesheetGraph struct {
	entries        map[dom.NodeID]stylesheetGraphEntry
	order          []dom.NodeID
	nextGeneration uint64
}

type stylesheetDescriptor struct {
	owner     dom.NodeID
	kind      stylesheetOwnerKind
	signature string
	resolved  *url.URL
	source    string
}

func newStylesheetGraph() stylesheetGraph {
	return stylesheetGraph{entries: make(map[dom.NodeID]stylesheetGraphEntry)}
}

func (graph *stylesheetGraph) ensure() {
	if graph.entries == nil {
		*graph = newStylesheetGraph()
	}
}

func (graph *stylesheetGraph) setManual(owner dom.NodeID, stylesheet css.Stylesheet) {
	graph.ensure()
	graph.nextGeneration++
	graph.entries[owner] = stylesheetGraphEntry{
		owner: owner, generation: graph.nextGeneration,
		stylesheet: stylesheet, ready: true, manual: true,
	}
	graph.order = append(graph.order, owner)
}

func (graph stylesheetGraph) resolvedStylesheets(document *dom.Document) map[*dom.Node]css.Stylesheet {
	result := make(map[*dom.Node]css.Stylesheet, len(graph.entries))
	for _, owner := range graph.order {
		entry, ok := graph.entries[owner]
		if !ok || !entry.ready {
			continue
		}
		if node, found := document.Resolve(owner); found {
			result[node] = entry.stylesheet
		}
	}
	return result
}

func (graph *stylesheetGraph) apply(result navigationResourceResult) bool {
	graph.ensure()
	entry, ok := graph.entries[result.target.Node]
	if !ok || entry.kind != stylesheetOwnerExternal || entry.generation != result.stylesheetGeneration {
		return false
	}
	entry.stylesheet = result.stylesheet
	entry.ready = true
	entry.requested = true
	entry.manual = false
	graph.entries[result.target.Node] = entry
	return true
}

func (graph *stylesheetGraph) sync(
	document *dom.Document,
	location *url.URL,
) ([]navigationResourceRequest, bool, error) {
	graph.ensure()
	if document == nil {
		return nil, false, fmt.Errorf("browser: nil stylesheet document")
	}

	var descriptors []stylesheetDescriptor
	err := document.WithReadView(func(view dom.ReadView) error {
		access, err := view.Acquire()
		if err != nil {
			return err
		}
		defer access.Close()
		root := access.Root()
		base := stylesheetBaseURL(root, location)
		var visit func(*dom.Node) error
		visit = func(node *dom.Node) error {
			if node == nil {
				return nil
			}
			if node.Type == dom.ElementNode {
				owner, ok := access.ID(node)
				if ok {
					switch node.Data {
					case "style":
						if embeddedStyleOwnerActive(node) {
							source := styleElementText(node)
							descriptors = append(descriptors, stylesheetDescriptor{
								owner: owner, kind: stylesheetOwnerEmbedded,
								signature: "style\x00" + source, source: source,
							})
						}
					case "link":
						if resolved, ok := externalStylesheetURL(node, base); ok {
							descriptors = append(descriptors, stylesheetDescriptor{
								owner: owner, kind: stylesheetOwnerExternal,
								signature: "link\x00" + resolved.String(), resolved: resolved,
							})
						}
					}
				}
			}
			for _, child := range node.Children {
				if err := visit(child); err != nil {
					return err
				}
			}
			return nil
		}
		return visit(root)
	})
	if err != nil {
		return nil, false, err
	}

	seen := make(map[dom.NodeID]struct{}, len(descriptors))
	order := make([]dom.NodeID, 0, len(descriptors))
	requests := make([]navigationResourceRequest, 0)
	changed := false
	for _, descriptor := range descriptors {
		seen[descriptor.owner] = struct{}{}
		order = append(order, descriptor.owner)
		entry, exists := graph.entries[descriptor.owner]
		if exists && entry.manual && entry.kind == 0 {
			entry.kind = descriptor.kind
			entry.signature = descriptor.signature
			entry.resolved = cloneURL(descriptor.resolved)
			entry.manual = false
			graph.entries[descriptor.owner] = entry
			continue
		}
		if exists && entry.kind == descriptor.kind && entry.signature == descriptor.signature {
			if entry.kind == stylesheetOwnerExternal && !entry.ready && !entry.requested {
				requests = append(requests, navigationResourceRequest{
					kind: resource.Stylesheet, url: cloneURL(entry.resolved), node: entry.owner,
					stylesheetGeneration: entry.generation,
				})
			}
			continue
		}

		graph.nextGeneration++
		entry = stylesheetGraphEntry{
			owner: descriptor.owner, kind: descriptor.kind,
			generation: graph.nextGeneration, signature: descriptor.signature,
			resolved: cloneURL(descriptor.resolved),
		}
		if descriptor.kind == stylesheetOwnerEmbedded {
			entry.stylesheet, _ = css.Parse(descriptor.source)
			entry.ready = true
		} else {
			requests = append(requests, navigationResourceRequest{
				kind: resource.Stylesheet, url: cloneURL(descriptor.resolved), node: descriptor.owner,
				stylesheetGeneration: entry.generation,
			})
		}
		graph.entries[descriptor.owner] = entry
		changed = true
	}
	for owner := range graph.entries {
		if _, connected := seen[owner]; !connected {
			delete(graph.entries, owner)
			changed = true
		}
	}
	if !slices.Equal(graph.order, order) {
		graph.order = order
		changed = true
	}
	return requests, changed, nil
}

func (graph *stylesheetGraph) markRequested(requests []navigationResourceRequest) {
	for _, request := range requests {
		if request.kind != resource.Stylesheet {
			continue
		}
		entry, ok := graph.entries[request.node]
		if !ok || entry.generation != request.stylesheetGeneration {
			continue
		}
		entry.requested = true
		graph.entries[request.node] = entry
	}
}

func embeddedStyleOwnerActive(node *dom.Node) bool {
	if sourceType, ok := nodeAttribute(node, "type"); ok && strings.TrimSpace(sourceType) != "" {
		mediaType := strings.TrimSpace(strings.SplitN(sourceType, ";", 2)[0])
		return strings.EqualFold(mediaType, "text/css")
	}
	return true
}

func styleElementText(node *dom.Node) string {
	var result strings.Builder
	for _, child := range node.Children {
		if child.Type == dom.TextNode {
			result.WriteString(child.Data)
		}
	}
	return result.String()
}

func externalStylesheetURL(node *dom.Node, base *url.URL) (*url.URL, bool) {
	if node == nil || node.Type != dom.ElementNode || node.Data != "link" || !activeStylesheetLink(node) {
		return nil, false
	}
	rel, _ := nodeAttribute(node, "rel")
	if !containsHTMLToken(rel, "stylesheet") {
		return nil, false
	}
	if sourceType, ok := nodeAttribute(node, "type"); ok && strings.TrimSpace(sourceType) != "" {
		mediaType := strings.TrimSpace(strings.SplitN(sourceType, ";", 2)[0])
		if !strings.EqualFold(mediaType, "text/css") {
			return nil, false
		}
	}
	href, ok := nodeAttribute(node, "href")
	if !ok || strings.TrimSpace(href) == "" || base == nil {
		return nil, false
	}
	parsed, err := url.Parse(strings.TrimSpace(href))
	if err != nil {
		return nil, false
	}
	resolved := base.ResolveReference(parsed)
	if (!strings.EqualFold(resolved.Scheme, "http") && !strings.EqualFold(resolved.Scheme, "https")) || resolved.Hostname() == "" {
		return nil, false
	}
	resolved.Scheme = strings.ToLower(resolved.Scheme)
	resolved.Fragment = ""
	return resolved, true
}

func stylesheetBaseURL(root *dom.Node, fallback *url.URL) *url.URL {
	base := cloneURL(fallback)
	var visit func(*dom.Node)
	found := false
	visit = func(node *dom.Node) {
		if node == nil || found {
			return
		}
		if node.Type == dom.ElementNode && node.Data == "base" {
			if href, ok := nodeAttribute(node, "href"); ok {
				if parsed, err := url.Parse(strings.TrimSpace(href)); err == nil && base != nil {
					base = base.ResolveReference(parsed)
					found = true
					return
				}
			}
		}
		for _, child := range node.Children {
			visit(child)
		}
	}
	visit(root)
	return base
}

func (page *Page) syncStylesheetsLocked() ([]navigationResourceRequest, error) {
	requests, changed, err := page.resources.stylesheets.sync(page.document, page.location)
	if err != nil {
		return nil, err
	}
	if changed {
		page.invalidateStyleLocked()
	}
	return requests, nil
}

func (page *Page) syncAndLoadStylesheets() error {
	page.mutex.Lock()
	if page.closed {
		page.mutex.Unlock()
		return ErrPageClosed
	}
	requests, err := page.syncStylesheetsLocked()
	if err != nil {
		page.mutex.Unlock()
		return err
	}
	if len(requests) == 0 || page.resourceFetcher == nil || page.documentContext == nil {
		page.mutex.Unlock()
		return nil
	}
	page.resources.stylesheets.markRequested(requests)
	fetcher := page.resourceFetcher
	ctx := page.documentContext
	generation := page.documentGeneration
	page.mutex.Unlock()

	go page.loadDynamicStylesheets(ctx, generation, fetcher, requests)
	return nil
}

func (page *Page) loadDynamicStylesheets(
	ctx context.Context,
	generation DocumentGeneration,
	fetcher resource.Fetcher,
	requests []navigationResourceRequest,
) {
	pipeline := resource.NewPipeline(fetcher, resource.PipelineOptions{})
	_ = loadNavigationResourceSequence(ctx, pipeline, generation, requests, maxNavigationImagePixels, func(result navigationResourceResult) error {
		_, _, err := page.browser.scheduler.EnqueueExternalTask(page.Realm, func(task *browserruntime.TaskContext) error {
			return page.applyDynamicStylesheet(task, generation, result)
		})
		return err
	})
}

func (page *Page) applyDynamicStylesheet(
	task *browserruntime.TaskContext,
	generation DocumentGeneration,
	result navigationResourceResult,
) error {
	page.mutex.Lock()
	if page.closed || page.documentGeneration != generation {
		page.mutex.Unlock()
		return nil
	}
	applied := result.err == nil && page.resources.apply(result)
	if applied {
		page.invalidateStyleLocked()
	}
	page.mutex.Unlock()
	if !applied {
		return nil
	}
	return page.queueRenderFromTask(task)
}
