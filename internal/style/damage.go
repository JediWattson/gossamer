package style

import (
	"sort"

	"github.com/JediWattson/gossamer/internal/dom"
)

// StyleDamageClass identifies the furthest rendering stage invalidated by a
// computed-value change. Layout damage always requires repaint after layout;
// paint-only damage can reuse immutable geometry.
type StyleDamageClass uint8

const (
	StyleDamagePaint StyleDamageClass = 1 << iota
	StyleDamageLayout
)

func (damage StyleDamageClass) HasPaint() bool  { return damage&StyleDamagePaint != 0 }
func (damage StyleDamageClass) HasLayout() bool { return damage&StyleDamageLayout != 0 }

// NodeStyleDamage is one stable element/text or pseudo-element whose computed
// ordinary longhands changed. Custom-property changes are represented only
// when they alter an observable computed longhand.
type NodeStyleDamage struct {
	Node       dom.NodeID
	Pseudo     PseudoElement
	Class      StyleDamageClass
	Properties []string
}

// SnapshotStyleDamage is the deterministic stable-ID difference between two
// immutable snapshots of the same document and environment.
type SnapshotStyleDamage struct {
	Class StyleDamageClass
	Nodes []NodeStyleDamage
}

// DamageComparedTo compares computed ordinary longhands using the central
// property registry. The boolean is false when snapshots cannot be compared
// safely (pointer snapshots, different documents, roots, or environments).
func (snapshot *Snapshot) DamageComparedTo(previous *Snapshot) (SnapshotStyleDamage, bool) {
	if snapshot == nil || previous == nil || snapshot.root != nil || previous.root != nil ||
		snapshot.documentIdentity != previous.documentIdentity || snapshot.rootID == dom.InvalidNodeID ||
		snapshot.rootID != previous.rootID || snapshot.environment != previous.environment {
		return SnapshotStyleDamage{}, false
	}
	if snapshot.damageBaseVersion == previous.version {
		return cloneSnapshotStyleDamage(snapshot.damage), true
	}
	return damageForStableIDs(snapshot, previous, unionStableStyleIDs(snapshot.byID, previous.byID)), true
}

func damageForStableIDs(snapshot, previous *Snapshot, ids []dom.NodeID) SnapshotStyleDamage {
	result := SnapshotStyleDamage{}
	idSet := make(map[dom.NodeID]struct{}, len(ids))
	for _, id := range ids {
		idSet[id] = struct{}{}
		current, currentOK := snapshot.byID[id]
		prior, priorOK := previous.byID[id]
		class := StyleDamageClass(0)
		var properties []string
		if !currentOK || !priorOK {
			class = StyleDamageLayout | StyleDamagePaint
		} else {
			class, properties = computedStyleDamage(current, prior)
		}
		if class != 0 {
			result.Class |= class
			result.Nodes = append(result.Nodes, NodeStyleDamage{Node: id, Class: class, Properties: properties})
		}
	}

	pseudoKeys := unionStablePseudoStyleKeysForIDs(snapshot.byPseudoID, previous.byPseudoID, idSet)
	for _, key := range pseudoKeys {
		current, currentOK := snapshot.byPseudoID[key]
		prior, priorOK := previous.byPseudoID[key]
		class := StyleDamageClass(0)
		var properties []string
		if !currentOK || !priorOK {
			class = StyleDamageLayout | StyleDamagePaint
		} else {
			class, properties = computedStyleDamage(current, prior)
		}
		if class != 0 {
			result.Class |= class
			result.Nodes = append(result.Nodes, NodeStyleDamage{Node: key.id, Pseudo: key.pseudo, Class: class, Properties: properties})
		}
	}
	return result
}

func cloneSnapshotStyleDamage(source SnapshotStyleDamage) SnapshotStyleDamage {
	clone := SnapshotStyleDamage{Class: source.Class, Nodes: make([]NodeStyleDamage, len(source.Nodes))}
	for index, node := range source.Nodes {
		clone.Nodes[index] = node
		clone.Nodes[index].Properties = append([]string(nil), node.Properties...)
	}
	return clone
}

func computedStyleDamage(current, previous ComputedStyle) (StyleDamageClass, []string) {
	class := StyleDamageClass(0)
	var properties []string
	for index := range propertyDefinitions {
		definition := propertyDefinitions[index]
		if definition.serialize(current) == definition.serialize(previous) {
			continue
		}
		properties = append(properties, definition.name)
		if definition.invalidation&propertyInvalidatesLayout != 0 {
			class |= StyleDamageLayout
		}
		if definition.invalidation&propertyInvalidatesPaint != 0 {
			class |= StyleDamagePaint
		}
	}
	return class, properties
}

func unionStableStyleIDs(left, right map[dom.NodeID]ComputedStyle) []dom.NodeID {
	set := make(map[dom.NodeID]struct{}, len(left)+len(right))
	for id := range left {
		set[id] = struct{}{}
	}
	for id := range right {
		set[id] = struct{}{}
	}
	ids := make([]dom.NodeID, 0, len(set))
	for id := range set {
		ids = append(ids, id)
	}
	sort.Slice(ids, func(left, right int) bool { return ids[left] < ids[right] })
	return ids
}

func unionStablePseudoStyleKeysForIDs(
	left, right map[stablePseudoKey]ComputedStyle,
	ids map[dom.NodeID]struct{},
) []stablePseudoKey {
	set := make(map[stablePseudoKey]struct{}, len(left)+len(right))
	for key := range left {
		if _, included := ids[key.id]; included {
			set[key] = struct{}{}
		}
	}
	for key := range right {
		if _, included := ids[key.id]; included {
			set[key] = struct{}{}
		}
	}
	keys := make([]stablePseudoKey, 0, len(set))
	for key := range set {
		keys = append(keys, key)
	}
	sort.Slice(keys, func(left, right int) bool {
		if keys[left].id != keys[right].id {
			return keys[left].id < keys[right].id
		}
		return keys[left].pseudo < keys[right].pseudo
	})
	return keys
}

// GeneratedContentDependsOnAttribute reports whether any retained computed
// content value can read the named DOM attribute during layout.
func (snapshot *Snapshot) GeneratedContentDependsOnAttribute(name string) bool {
	if snapshot == nil {
		return false
	}
	name = asciiLower(name)
	for _, computed := range snapshot.byID {
		if contentDependsOnAttribute(computed.content, name) {
			return true
		}
	}
	for _, computed := range snapshot.byPseudoID {
		if contentDependsOnAttribute(computed.content, name) {
			return true
		}
	}
	return false
}

func contentDependsOnAttribute(content ContentValue, name string) bool {
	for _, item := range content.items {
		if item.kind == contentAttribute && asciiLower(item.value) == name {
			return true
		}
	}
	return false
}
