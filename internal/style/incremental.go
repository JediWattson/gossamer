package style

import (
	"fmt"
	"maps"
	"sort"
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

func (dependencies snapshotMutationDependencies) siblings() bool {
	for _, stylesheet := range dependencies.stylesheets {
		if stylesheet.DependsOnSiblings() {
			return true
		}
	}
	return false
}

func (dependencies snapshotMutationDependencies) relational() bool {
	for _, stylesheet := range dependencies.stylesheets {
		if stylesheet.DependsOnDescendants() {
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
	if err := snapshot.validateStableReadAccess(access); err != nil {
		return nil, false, err
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
	rebound.damageBaseVersion = snapshot.version
	rebound.damage = SnapshotStyleDamage{}
	return &rebound, true, nil
}

// RestyleReadViewAfterMutations recomputes only selector-affected connected
// subtrees while preserving the prior immutable snapshot. Non-DOM inputs must
// be the same inputs used for snapshot; callers must run a full pass when the
// environment, stylesheet set, or browser selector state changes. The boolean
// is false for mutation classes whose influence cannot yet be bounded safely.
func (snapshot *Snapshot) RestyleReadViewAfterMutations(
	view dom.ReadView,
	input Input,
	records []dom.MutationRecord,
) (*Snapshot, bool, error) {
	access, err := view.Acquire()
	if err != nil {
		return nil, false, err
	}
	defer access.Close()
	if err := snapshot.validateStableReadAccess(access); err != nil {
		return nil, false, err
	}
	if snapshot.version == access.Version() {
		return snapshot, true, nil
	}
	if input.Environment != snapshot.environment || len(records) == 0 {
		return nil, false, nil
	}
	dirtyRoots, supported := snapshot.dirtyStyleRoots(access, records)
	if !supported {
		return nil, false, nil
	}
	if len(dirtyRoots) == 0 {
		rebound := *snapshot
		rebound.version = access.Version()
		rebound.damageBaseVersion = snapshot.version
		rebound.damage = SnapshotStyleDamage{}
		return &rebound, true, nil
	}

	document := access.Root()
	input = prepareReadViewInput(document, access, input)
	context := buildCascadeStyleContext(document, input)
	sort.Slice(dirtyRoots, func(left, right int) bool {
		leftID, _ := access.ID(dirtyRoots[left])
		rightID, _ := access.ID(dirtyRoots[right])
		return leftID < rightID
	})
	styledRoots := make([]*styledNode, 0, len(dirtyRoots))
	styledParents := make([]*styledNode, 0, len(dirtyRoots))
	for _, root := range dirtyRoots {
		parent, parentErr := snapshot.incrementalStyledParent(access, root.Parent)
		if parentErr != nil {
			return nil, false, parentErr
		}
		styledParents = append(styledParents, parent)
		styledRoots = append(styledRoots, styleNode(root, parent, &context, input.Environment))
	}

	updated := *snapshot
	updated.version = access.Version()
	updated.byID = maps.Clone(snapshot.byID)
	updated.byPseudoID = maps.Clone(snapshot.byPseudoID)
	for _, root := range dirtyRoots {
		deleteStablePseudoSubtree(root, updated.byPseudoID, access)
	}
	for _, root := range styledRoots {
		indexStableStyles(root, updated.byID, access)
		indexStablePseudoStyles(root, updated.byPseudoID, access)
	}
	updated.provenance = restyleStableProvenance(snapshot.provenance, styledRoots, styledParents, access)
	updated.mutationDependencies = compileSnapshotMutationDependencies(input)
	updated.damageBaseVersion = snapshot.version
	updated.damage = damageForStableIDs(&updated, snapshot, stableStyledNodeIDs(styledRoots, access))
	return &updated, true, nil
}

func stableStyledNodeIDs(roots []*styledNode, access *dom.ReadAccess) []dom.NodeID {
	set := make(map[dom.NodeID]struct{})
	var visit func(*styledNode)
	visit = func(node *styledNode) {
		if node == nil {
			return
		}
		if id, ok := access.ID(node.node); ok {
			set[id] = struct{}{}
		}
		for _, child := range node.children {
			visit(child)
		}
	}
	for _, root := range roots {
		visit(root)
	}
	ids := make([]dom.NodeID, 0, len(set))
	for id := range set {
		ids = append(ids, id)
	}
	sort.Slice(ids, func(left, right int) bool { return ids[left] < ids[right] })
	return ids
}

func (snapshot *Snapshot) validateStableReadAccess(access *dom.ReadAccess) error {
	if snapshot == nil || access == nil || snapshot.root != nil || snapshot.documentIdentity != access.Identity() ||
		snapshot.rootID == dom.InvalidNodeID || snapshot.rootID != stableRootID(access) {
		return fmt.Errorf("style: snapshot does not belong to read view")
	}
	return nil
}

func (snapshot *Snapshot) dirtyStyleRoots(access *dom.ReadAccess, records []dom.MutationRecord) ([]*dom.Node, bool) {
	var roots []*dom.Node
	for _, record := range records {
		if !record.Connected {
			continue
		}
		switch record.Type {
		case dom.MutationChildList:
			return nil, false
		case dom.MutationAttributes:
			node, found := access.Resolve(record.Target)
			if !found || node == nil || node.Type != dom.ElementNode {
				return nil, false
			}
			name := lowerASCIIName(record.AttributeName)
			if styleOwnerAttributeAffectsSheet(node, name) {
				return nil, false
			}
			selectorDependent := snapshot.mutationDependencies.attribute(name)
			presentational := node.NamespaceURI == dom.HTMLNamespace && node.Data == "img" && (name == "width" || name == "height")
			if name != "style" && !selectorDependent && !presentational {
				continue
			}
			if selectorDependent && snapshot.mutationDependencies.relational() {
				return nil, false
			}
			root := node
			if selectorDependent && snapshot.mutationDependencies.siblings() {
				root = node.Parent
			}
			if !appendDirtyStyleRoot(&roots, root) {
				return nil, false
			}
		case dom.MutationCharacterData:
			node, found := access.Resolve(record.Target)
			if !found || node == nil || node.Type != dom.TextNode {
				return nil, false
			}
			if hasStylesheetTextAncestor(node) {
				return nil, false
			}
			if snapshot.mutationDependencies.formText() && hasFormTextAncestor(node) {
				return nil, false
			}
			if snapshot.mutationDependencies.emptyText() && (record.OldValue == "") != (node.Data == "") &&
				textCanDetermineParentEmptiness(node) {
				if !appendDirtyStyleRoot(&roots, node.Parent) {
					return nil, false
				}
			}
			if snapshot.mutationDependencies.directionalityText() {
				if ancestor := outermostAutoDirectionalityAncestor(node); ancestor != nil && !appendDirtyStyleRoot(&roots, ancestor) {
					return nil, false
				}
			}
		case dom.MutationState:
			if !snapshot.mutationDependencies.state(record.StateName) {
				continue
			}
			if record.StateName == "reset" || snapshot.mutationDependencies.relational() {
				return nil, false
			}
			node, found := access.Resolve(record.Target)
			if !found || node == nil || node.Type != dom.ElementNode {
				return nil, false
			}
			root := node
			if snapshot.mutationDependencies.siblings() {
				root = node.Parent
			}
			if !appendDirtyStyleRoot(&roots, root) {
				return nil, false
			}
		default:
			return nil, false
		}
	}
	return roots, true
}

func appendDirtyStyleRoot(roots *[]*dom.Node, candidate *dom.Node) bool {
	if candidate == nil || candidate.Type == dom.DocumentNode {
		return false
	}
	for ancestor := candidate; ancestor != nil; ancestor = ancestor.Parent {
		for _, root := range *roots {
			if root == ancestor {
				return true
			}
		}
	}
	filtered := (*roots)[:0]
	for _, root := range *roots {
		insideCandidate := false
		for ancestor := root.Parent; ancestor != nil; ancestor = ancestor.Parent {
			if ancestor == candidate {
				insideCandidate = true
				break
			}
		}
		if !insideCandidate {
			filtered = append(filtered, root)
		}
	}
	*roots = append(filtered, candidate)
	return true
}

func styleOwnerAttributeAffectsSheet(node *dom.Node, name string) bool {
	if node == nil || node.NamespaceURI != dom.HTMLNamespace {
		return false
	}
	switch node.Data {
	case "style":
		return name == "media" || name == "type"
	case "link":
		return name == "disabled" || name == "href" || name == "media" || name == "rel" || name == "type"
	default:
		return false
	}
}

func hasStylesheetTextAncestor(node *dom.Node) bool {
	for ancestor := node.Parent; ancestor != nil; ancestor = ancestor.Parent {
		if ancestor.Type == dom.ElementNode && ancestor.NamespaceURI == dom.HTMLNamespace && ancestor.Data == "style" {
			return true
		}
	}
	return false
}

func outermostAutoDirectionalityAncestor(node *dom.Node) *dom.Node {
	var result *dom.Node
	for ancestor := node.Parent; ancestor != nil && ancestor.Type == dom.ElementNode; ancestor = ancestor.Parent {
		if ancestor.NamespaceURI != dom.HTMLNamespace {
			continue
		}
		direction, found := attribute(ancestor, "dir")
		direction = lowerASCIIName(strings.TrimSpace(direction))
		if found && direction == "auto" || ancestor.Data == "bdi" && direction != "ltr" && direction != "rtl" {
			result = ancestor
		}
	}
	return result
}

func (snapshot *Snapshot) incrementalStyledParent(access *dom.ReadAccess, node *dom.Node) (*styledNode, error) {
	if node == nil {
		return nil, nil
	}
	id, ok := access.ID(node)
	if !ok {
		return nil, fmt.Errorf("style: incremental parent has no stable identity")
	}
	computed, ok := snapshot.byID[id]
	if !ok {
		return nil, fmt.Errorf("style: incremental parent %d is absent from snapshot", id)
	}
	explanations := make(map[string]PropertyExplanation, len(propertyDefinitions)+len(computed.customProperties.Names()))
	for index := range propertyDefinitions {
		property := propertyDefinitions[index].name
		explanation, found := snapshot.ExplainID(id, property)
		if !found {
			return nil, fmt.Errorf("style: incremental parent %d lacks %s provenance", id, property)
		}
		explanations[property] = clonePropertyExplanation(explanation)
	}
	for _, property := range computed.customProperties.Names() {
		if explanation, found := snapshot.ExplainID(id, property); found {
			explanations[property] = clonePropertyExplanation(explanation)
		}
	}
	return &styledNode{node: node, style: computed, explanations: explanations}, nil
}

func deleteStablePseudoSubtree(node *dom.Node, destination map[stablePseudoKey]ComputedStyle, access *dom.ReadAccess) {
	if node == nil {
		return
	}
	if id, ok := access.ID(node); ok {
		delete(destination, stablePseudoKey{id: id, pseudo: css.PseudoElementBefore})
		delete(destination, stablePseudoKey{id: id, pseudo: css.PseudoElementAfter})
	}
	for _, child := range node.Children {
		deleteStablePseudoSubtree(child, destination, access)
	}
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
	if !found || node == nil || node.Type != dom.ElementNode {
		return false
	}
	return styleOwnerAttributeAffectsSheet(node, name) || node.NamespaceURI == dom.HTMLNamespace &&
		node.Data == "img" && (name == "width" || name == "height")
}

func (snapshot *Snapshot) characterMutationRequiresRestyle(access *dom.ReadAccess, record dom.MutationRecord) bool {
	node, found := access.Resolve(record.Target)
	if !found || node == nil || node.Type != dom.TextNode {
		return false
	}
	if hasStylesheetTextAncestor(node) {
		return true
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
