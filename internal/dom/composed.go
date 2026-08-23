package dom

// ComposedChildren returns the children that participate in rendering and
// inherited style for node. A shadow host substitutes its shadow-root children
// for its light-DOM children; the ShadowRoot itself is an ownership boundary,
// not a rendered box. Callers traversing an indexed Document must hold a
// coherent ReadView or ReadAccess while using the returned slice.
func ComposedChildren(node *Node) []*Node {
	if node == nil {
		return nil
	}
	if node.ShadowRoot != nil {
		return node.ShadowRoot.Children
	}
	return node.Children
}
