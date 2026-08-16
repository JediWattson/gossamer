package browser

import (
	"github.com/JediWattson/gossamer/internal/css"
	"github.com/JediWattson/gossamer/internal/dom"
)

type inlineStyleCacheEntry struct {
	source       string
	declarations []css.SourcedDeclaration
}

// inlineStyleCache owns parsed style-attribute declarations by stable element
// identity. A document-version guard makes repeated computed-style and layout
// reads O(1); a changed document still performs a coherent connected-tree scan
// but reparses only style attributes whose exact source changed.
type inlineStyleCache struct {
	entries     map[dom.NodeID]inlineStyleCacheEntry
	version     uint64
	initialized bool
	parseCount  uint64
}

func newInlineStyleCache() inlineStyleCache {
	return inlineStyleCache{entries: make(map[dom.NodeID]inlineStyleCacheEntry)}
}

func (cache *inlineStyleCache) ensure() {
	if cache.entries == nil {
		*cache = newInlineStyleCache()
	}
}

func (cache *inlineStyleCache) declarationsForView(view dom.ReadView) (map[*dom.Node][]css.SourcedDeclaration, error) {
	cache.ensure()
	access, err := view.Acquire()
	if err != nil {
		return nil, err
	}
	defer access.Close()
	if _, err := cache.syncAccess(access); err != nil {
		return nil, err
	}
	resolved := make(map[*dom.Node][]css.SourcedDeclaration, len(cache.entries))
	for owner, entry := range cache.entries {
		if node, ok := access.Resolve(owner); ok {
			resolved[node] = entry.declarations
		}
	}
	return resolved, nil
}

func (cache *inlineStyleCache) syncAccess(access *dom.ReadAccess) (bool, error) {
	if access == nil {
		return false, dom.ErrExpiredReadView
	}
	version := access.Version()
	if cache.initialized && cache.version == version {
		return false, nil
	}
	type descriptor struct {
		owner  dom.NodeID
		source string
	}
	descriptors := make([]descriptor, 0, len(cache.entries))
	var visit func(*dom.Node)
	visit = func(node *dom.Node) {
		if node == nil {
			return
		}
		if node.Type == dom.ElementNode {
			if source, ok := nodeAttribute(node, "style"); ok {
				if owner, found := access.ID(node); found {
					descriptors = append(descriptors, descriptor{owner: owner, source: source})
				}
			}
		}
		for _, child := range node.Children {
			visit(child)
		}
	}
	visit(access.Root())

	seen := make(map[dom.NodeID]struct{}, len(descriptors))
	changed := false
	for _, descriptor := range descriptors {
		seen[descriptor.owner] = struct{}{}
		if current, ok := cache.entries[descriptor.owner]; ok && current.source == descriptor.source {
			continue
		}
		declarations, _ := css.ParseRawDeclarationListWithSources(descriptor.source)
		cache.entries[descriptor.owner] = inlineStyleCacheEntry{
			source: descriptor.source, declarations: declarations,
		}
		cache.parseCount++
		changed = true
	}
	for owner := range cache.entries {
		if _, ok := seen[owner]; ok {
			continue
		}
		delete(cache.entries, owner)
		changed = true
	}
	cache.version = version
	cache.initialized = true
	return changed, nil
}
