package style

import (
	"fmt"
	"strings"

	"github.com/JediWattson/gossamer/internal/css"
	"github.com/JediWattson/gossamer/internal/dom"
)

type snapshotMutationDependencies struct {
	stylesheets []css.SelectorDependencies
}

func compileSnapshotMutationDependencies(input Input) snapshotMutationDependencies {
	count := 1 + len(input.Stylesheets) + len(input.UserStylesheets) + len(input.UserAgentStylesheets)
	dependencies := snapshotMutationDependencies{stylesheets: make([]css.SelectorDependencies, 0, count)}
	dependencies.stylesheets = append(dependencies.stylesheets, builtInUserAgentStylesheet.SelectorDependencies())
	for _, stylesheet := range input.UserAgentStylesheets {
		dependencies.stylesheets = append(dependencies.stylesheets, stylesheet.SelectorDependencies())
	}
	for _, stylesheet := range input.UserStylesheets {
		dependencies.stylesheets = append(dependencies.stylesheets, stylesheet.SelectorDependencies())
	}
	for _, stylesheet := range input.Stylesheets {
		dependencies.stylesheets = append(dependencies.stylesheets, stylesheet.SelectorDependencies())
	}
	return dependencies
}

func (dependencies snapshotMutationDependencies) attribute(name string) bool {
	for _, stylesheet := range dependencies.stylesheets {
		if stylesheet.DependsOnAttribute(name) {
			return true
		}
	}
	return false
}

func (dependencies snapshotMutationDependencies) state(name string) bool {
	for _, stylesheet := range dependencies.stylesheets {
		if stylesheet.DependsOnState(name) {
			return true
		}
	}
	return false
}

func (dependencies snapshotMutationDependencies) emptyText() bool {
	for _, stylesheet := range dependencies.stylesheets {
		if stylesheet.DependsOnEmptyText() {
			return true
		}
	}
	return false
}

func (dependencies snapshotMutationDependencies) directionalityText() bool {
	for _, stylesheet := range dependencies.stylesheets {
		if stylesheet.DependsOnDirectionalityText() {
			return true
		}
	}
	return false
}

func (dependencies snapshotMutationDependencies) formText() bool {
	for _, stylesheet := range dependencies.stylesheets {
		if stylesheet.DependsOnFormText() {
			return true
		}
	}
	return false
}

// RebindReadViewAfterMutations returns a new immutable snapshot header sharing
// the previous computed/provenance storage when every supplied mutation is
// proven style-neutral. The boolean is false when the caller must run the full
// style pass. It never patches a style value or tree membership in place.
func (snapshot *Snapshot) RebindReadViewAfterMutations(view dom.ReadView, records []dom.MutationRecord) (*Snapshot, bool, error) {
	access, err := view.Acquire()
	if err != nil {
		return nil, false, err
	}
	defer access.Close()
	if snapshot == nil || snapshot.root != nil || snapshot.documentIdentity != access.Identity() ||
		snapshot.rootID == dom.InvalidNodeID || snapshot.rootID != stableRootID(access) {
		return nil, false, fmt.Errorf("style: snapshot does not belong to read view")
	}
	if snapshot.version == access.Version() {
		return snapshot, true, nil
	}
	if len(records) == 0 {
		return nil, false, nil
	}
	for _, record := range records {
		if !record.Connected {
			continue
		}
		switch record.Type {
		case dom.MutationChildList:
			return nil, false, nil
		case dom.MutationAttributes:
			if snapshot.attributeMutationRequiresRestyle(access, record) {
				return nil, false, nil
			}
		case dom.MutationCharacterData:
			if snapshot.characterMutationRequiresRestyle(access, record) {
				return nil, false, nil
			}
		case dom.MutationState:
			if snapshot.mutationDependencies.state(record.StateName) {
				return nil, false, nil
			}
		default:
			return nil, false, nil
		}
	}
	rebound := *snapshot
	rebound.version = access.Version()
	return &rebound, true, nil
}

func stableRootID(access *dom.ReadAccess) dom.NodeID {
	root := access.Root()
	id, _ := access.ID(root)
	return id
}

func (snapshot *Snapshot) attributeMutationRequiresRestyle(access *dom.ReadAccess, record dom.MutationRecord) bool {
	name := lowerASCIIName(record.AttributeName)
	if name == "style" || snapshot.mutationDependencies.attribute(name) {
		return true
	}
	node, found := access.Resolve(record.Target)
	return found && node != nil && node.Type == dom.ElementNode && node.NamespaceURI == dom.HTMLNamespace &&
		node.Data == "img" && (name == "width" || name == "height")
}

func (snapshot *Snapshot) characterMutationRequiresRestyle(access *dom.ReadAccess, record dom.MutationRecord) bool {
	node, found := access.Resolve(record.Target)
	if !found || node == nil || node.Type != dom.TextNode {
		return false
	}
	if snapshot.mutationDependencies.emptyText() && (record.OldValue == "") != (node.Data == "") &&
		textCanDetermineParentEmptiness(node) {
		return true
	}
	if snapshot.mutationDependencies.directionalityText() && hasAutoDirectionalityAncestor(node) {
		return true
	}
	return snapshot.mutationDependencies.formText() && hasFormTextAncestor(node)
}

func textCanDetermineParentEmptiness(node *dom.Node) bool {
	if node.Parent == nil || node.Parent.Type != dom.ElementNode {
		return true
	}
	for _, sibling := range node.Parent.Children {
		if sibling == nil || sibling == node {
			continue
		}
		if sibling.Type == dom.ElementNode || sibling.Type == dom.TextNode && sibling.Data != "" {
			return false
		}
	}
	return true
}

func hasAutoDirectionalityAncestor(node *dom.Node) bool {
	for ancestor := node.Parent; ancestor != nil && ancestor.Type == dom.ElementNode; ancestor = ancestor.Parent {
		if ancestor.NamespaceURI != dom.HTMLNamespace {
			continue
		}
		direction, found := attribute(ancestor, "dir")
		direction = lowerASCIIName(strings.TrimSpace(direction))
		if found && direction == "auto" {
			return true
		}
		if ancestor.Data == "bdi" && direction != "ltr" && direction != "rtl" {
			return true
		}
	}
	return false
}

func hasFormTextAncestor(node *dom.Node) bool {
	for ancestor := node.Parent; ancestor != nil && ancestor.Type == dom.ElementNode; ancestor = ancestor.Parent {
		if ancestor.NamespaceURI == dom.HTMLNamespace && (ancestor.Data == "textarea" || ancestor.Data == "option") {
			return true
		}
	}
	return false
}

func lowerASCIIName(value string) string {
	for index := 0; index < len(value); index++ {
		if value[index] < 'A' || value[index] > 'Z' {
			continue
		}
		lowered := []byte(value)
		for cursor := index; cursor < len(lowered); cursor++ {
			if lowered[cursor] >= 'A' && lowered[cursor] <= 'Z' {
				lowered[cursor] += 'a' - 'A'
			}
		}
		return string(lowered)
	}
	return value
}
