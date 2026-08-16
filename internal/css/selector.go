package css

import (
	"strings"

	"github.com/JediWattson/gossamer/internal/dom"
)

// Matches reports whether selector matches node. Matching proceeds from the
// rightmost compound and backtracks across ancestor and sibling candidates.
func (selector Selector) Matches(node *dom.Node) bool {
	return selector.MatchesWithContext(node, MatchContext{})
}

// MatchContext supplies browser state that cannot be inferred from the DOM.
// Stateful nodes are callback-scoped DOM pointers owned by the caller's
// coherent read; selectors never retain them.
type MatchContext struct {
	Hovered      *dom.Node
	Active       *dom.Node
	Focused      *dom.Node
	FocusVisible bool
	Target       *dom.Node
	Visited      func(*dom.Node) bool
	// DefaultLanguage supplies the document's protocol-level fallback language
	// when no element or ancestor establishes one. The empty string represents
	// an untagged document.
	DefaultLanguage string
	// OperationLimit bounds tree, combinator, linguistic, and relational work
	// within one top-level selector evaluation. Zero uses a conservative engine
	// default; exhausted evaluations fail closed.
	OperationLimit int
}

const defaultSelectorOperationLimit = 100_000

type selectorMatchState struct {
	remaining int
	exhausted bool
}

func newSelectorMatchState(context MatchContext) *selectorMatchState {
	limit := context.OperationLimit
	if limit <= 0 {
		limit = defaultSelectorOperationLimit
	}
	return &selectorMatchState{remaining: limit}
}

func (state *selectorMatchState) take() bool {
	if state == nil || state.remaining <= 0 {
		if state != nil {
			state.exhausted = true
		}
		return false
	}
	state.remaining--
	return true
}

// MatchesWithContext reports whether selector matches node under context.
func (selector Selector) MatchesWithContext(node *dom.Node, context MatchContext) bool {
	return selector.matchesWithState(node, context, newSelectorMatchState(context))
}

type selectorMatchFrame struct {
	compoundIndex int
	node          *dom.Node
}

func (selector Selector) matchesWithState(node *dom.Node, context MatchContext, state *selectorMatchState) bool {
	if node == nil || node.Type != dom.ElementNode || len(selector.compounds) == 0 {
		return false
	}
	frames := []selectorMatchFrame{{compoundIndex: len(selector.compounds) - 1, node: node}}
	for len(frames) != 0 {
		last := len(frames) - 1
		frame := frames[last]
		frames = frames[:last]
		if !state.take() {
			return false
		}
		if !matchesCompound(selector.compounds[frame.compoundIndex], frame.node, context, state) {
			continue
		}
		if state.exhausted {
			return false
		}
		if frame.compoundIndex == 0 {
			return true
		}
		nextIndex := frame.compoundIndex - 1
		var ok bool
		frames, ok = appendReverseRelationFrames(frames, nextIndex, frame.node, selector.combinators[nextIndex], state)
		if !ok {
			return false
		}
	}
	return false
}

func appendReverseRelationFrames(frames []selectorMatchFrame, compoundIndex int, node *dom.Node, combinator selectorCombinator, state *selectorMatchState) ([]selectorMatchFrame, bool) {
	appendElement := func(candidate *dom.Node) bool {
		if !state.take() {
			return false
		}
		if candidate != nil && candidate.Type == dom.ElementNode {
			frames = append(frames, selectorMatchFrame{compoundIndex: compoundIndex, node: candidate})
		}
		return true
	}
	switch combinator {
	case childCombinator:
		return frames, appendElement(node.Parent)
	case descendantCombinator:
		ancestors := make([]*dom.Node, 0, 8)
		for ancestor := node.Parent; ancestor != nil; ancestor = ancestor.Parent {
			if !state.take() {
				return frames, false
			}
			if ancestor.Type == dom.ElementNode {
				ancestors = append(ancestors, ancestor)
			}
		}
		for index := len(ancestors) - 1; index >= 0; index-- {
			frames = append(frames, selectorMatchFrame{compoundIndex: compoundIndex, node: ancestors[index]})
		}
		return frames, true
	case adjacentSiblingCombinator:
		if node.Parent == nil {
			return frames, true
		}
		for index := childIndex(node.Parent, node) - 1; index >= 0; index-- {
			candidate := node.Parent.Children[index]
			if !state.take() {
				return frames, false
			}
			if candidate != nil && candidate.Type == dom.ElementNode {
				frames = append(frames, selectorMatchFrame{compoundIndex: compoundIndex, node: candidate})
				break
			}
		}
		return frames, true
	case generalSiblingCombinator:
		if node.Parent == nil {
			return frames, true
		}
		end := childIndex(node.Parent, node)
		for index := 0; index < end; index++ {
			if !appendElement(node.Parent.Children[index]) {
				return frames, false
			}
		}
		return frames, true
	default:
		return frames, false
	}
}

type relativeMatchFrame struct {
	compoundIndex int
	node          *dom.Node
}

func matchesHas(selectors []Selector, anchor *dom.Node, context MatchContext, state *selectorMatchState) bool {
	for _, selector := range selectors {
		if selector.matchesRelative(anchor, context, state) {
			return true
		}
	}
	return false
}

func (selector Selector) matchesRelative(anchor *dom.Node, context MatchContext, state *selectorMatchState) bool {
	if anchor == nil || anchor.Type != dom.ElementNode || len(selector.compounds) == 0 || selector.leading == 0 {
		return false
	}
	frames, ok := appendForwardRelationFrames(nil, 0, anchor, selector.leading, state)
	if !ok {
		return false
	}
	for len(frames) != 0 {
		last := len(frames) - 1
		frame := frames[last]
		frames = frames[:last]
		if !state.take() {
			return false
		}
		if !matchesCompound(selector.compounds[frame.compoundIndex], frame.node, context, state) {
			continue
		}
		if state.exhausted {
			return false
		}
		if frame.compoundIndex == len(selector.compounds)-1 {
			return true
		}
		nextIndex := frame.compoundIndex + 1
		frames, ok = appendForwardRelationFrames(frames, nextIndex, frame.node, selector.combinators[frame.compoundIndex], state)
		if !ok {
			return false
		}
	}
	return false
}

func appendForwardRelationFrames(frames []relativeMatchFrame, compoundIndex int, node *dom.Node, combinator selectorCombinator, state *selectorMatchState) ([]relativeMatchFrame, bool) {
	appendElement := func(candidate *dom.Node) bool {
		if !state.take() {
			return false
		}
		if candidate != nil && candidate.Type == dom.ElementNode {
			frames = append(frames, relativeMatchFrame{compoundIndex: compoundIndex, node: candidate})
		}
		return true
	}
	switch combinator {
	case childCombinator:
		for index := len(node.Children) - 1; index >= 0; index-- {
			if !appendElement(node.Children[index]) {
				return frames, false
			}
		}
		return frames, true
	case descendantCombinator:
		walk := make([]*dom.Node, 0, len(node.Children))
		for index := len(node.Children) - 1; index >= 0; index-- {
			walk = append(walk, node.Children[index])
		}
		elements := make([]*dom.Node, 0, len(walk))
		for len(walk) != 0 {
			last := len(walk) - 1
			candidate := walk[last]
			walk = walk[:last]
			if !state.take() {
				return frames, false
			}
			if candidate != nil && candidate.Type == dom.ElementNode {
				elements = append(elements, candidate)
			}
			if candidate != nil {
				for index := len(candidate.Children) - 1; index >= 0; index-- {
					walk = append(walk, candidate.Children[index])
				}
			}
		}
		for index := len(elements) - 1; index >= 0; index-- {
			frames = append(frames, relativeMatchFrame{compoundIndex: compoundIndex, node: elements[index]})
		}
		return frames, true
	case adjacentSiblingCombinator:
		if node.Parent == nil {
			return frames, true
		}
		for index := childIndex(node.Parent, node) + 1; index < len(node.Parent.Children); index++ {
			candidate := node.Parent.Children[index]
			if !state.take() {
				return frames, false
			}
			if candidate != nil && candidate.Type == dom.ElementNode {
				frames = append(frames, relativeMatchFrame{compoundIndex: compoundIndex, node: candidate})
				break
			}
		}
		return frames, true
	case generalSiblingCombinator:
		if node.Parent == nil {
			return frames, true
		}
		start := childIndex(node.Parent, node) + 1
		for index := len(node.Parent.Children) - 1; index >= start; index-- {
			if !appendElement(node.Parent.Children[index]) {
				return frames, false
			}
		}
		return frames, true
	default:
		return frames, false
	}
}

// Match reports whether any selector in the rule matches node. When more than
// one selector matches, it returns the greatest matching specificity.
func (rule Rule) Match(node *dom.Node) (Specificity, bool) {
	return rule.MatchWithContext(node, MatchContext{})
}

// MatchWithContext reports the greatest matching specificity under context.
func (rule Rule) MatchWithContext(node *dom.Node, context MatchContext) (Specificity, bool) {
	state := newSelectorMatchState(context)
	var greatest Specificity
	matched := false
	for _, selector := range rule.Selectors {
		selectorMatched := selector.matchesWithState(node, context, state)
		if state.exhausted {
			return Specificity{}, false
		}
		if !selectorMatched {
			continue
		}
		if !matched || selector.specificity.Compare(greatest) > 0 {
			greatest = selector.specificity
		}
		matched = true
	}
	return greatest, matched
}

func matchesCompound(compound compoundSelector, node *dom.Node, context MatchContext, state *selectorMatchState) bool {
	if node == nil || node.Type != dom.ElementNode {
		return false
	}
	if !state.take() {
		return false
	}
	if compound.typeName != "" && compound.typeName != "*" && !equalASCIIFold(compound.typeName, node.Data) {
		return false
	}
	id, hasID := attributeValue(node, "id")
	for _, requiredID := range compound.ids {
		if !state.take() {
			return false
		}
		if !hasID || id != requiredID {
			return false
		}
	}
	classValue, hasClass := attributeValue(node, "class")
	for _, className := range compound.classes {
		if !state.take() {
			return false
		}
		if !hasClass || !containsCSSSpaceSeparatedToken(classValue, className) {
			return false
		}
	}
	for _, attribute := range compound.attributes {
		if !state.take() {
			return false
		}
		if !matchesAttribute(attribute, node) {
			return false
		}
	}
	for _, pseudo := range compound.pseudos {
		if !state.take() || !matchesPseudoClass(pseudo, node, context, state) {
			return false
		}
	}
	return true
}

func matchesAttribute(selector attributeSelector, node *dom.Node) bool {
	actual, present := attributeValue(node, selector.name)
	if !present {
		return false
	}
	if selector.operator == attributeExists {
		return true
	}
	expected := selector.value
	insensitive := selector.valueCase == attributeCaseInsensitive ||
		selector.valueCase == attributeCaseDefault && htmlAttributeValueCaseInsensitive(selector.name)
	if insensitive {
		actual = lowerASCII(actual)
		expected = lowerASCII(expected)
	}
	switch selector.operator {
	case attributeEquals:
		return actual == expected
	case attributeIncludes:
		return expected != "" && !containsCSSWhitespace(expected) && containsCSSSpaceSeparatedToken(actual, expected)
	case attributeDashMatch:
		return actual == expected || strings.HasPrefix(actual, expected+"-")
	case attributePrefix:
		return expected != "" && strings.HasPrefix(actual, expected)
	case attributeSuffix:
		return expected != "" && strings.HasSuffix(actual, expected)
	case attributeSubstring:
		return expected != "" && strings.Contains(actual, expected)
	default:
		return false
	}
}

func matchesPseudoClass(pseudo pseudoClassSelector, node *dom.Node, context MatchContext, state *selectorMatchState) bool {
	switch pseudo.name {
	case "root":
		return isDocumentElement(node)
	case "empty":
		return isEmptyElement(node)
	case "first-child":
		return isFirstElementSibling(node, false)
	case "last-child":
		return isLastElementSibling(node, false)
	case "only-child":
		return isFirstElementSibling(node, false) && isLastElementSibling(node, false)
	case "first-of-type":
		return isFirstElementSibling(node, true)
	case "last-of-type":
		return isLastElementSibling(node, true)
	case "only-of-type":
		return isFirstElementSibling(node, true) && isLastElementSibling(node, true)
	case "link":
		return isUnvisitedLink(node) && !isVisitedLink(node, context)
	case "any-link":
		return isUnvisitedLink(node)
	case "visited":
		return isUnvisitedLink(node) && isVisitedLink(node, context)
	case "hover":
		return inclusiveAncestor(node, context.Hovered)
	case "active":
		return inclusiveAncestor(node, context.Active)
	case "focus":
		return node == context.Focused
	case "focus-visible":
		return context.FocusVisible && node == context.Focused
	case "focus-within":
		return inclusiveAncestor(node, context.Focused)
	case "target":
		return node == context.Target
	case "checked":
		return isHTMLChecked(node)
	case "disabled":
		eligible, disabled := htmlDisabledState(node)
		return eligible && disabled
	case "enabled":
		eligible, disabled := htmlDisabledState(node)
		return eligible && !disabled
	case "required":
		eligible, required := htmlRequiredState(node)
		return eligible && required
	case "optional":
		eligible, required := htmlRequiredState(node)
		return eligible && !required
	case "read-write":
		readWrite, ok := htmlReadWriteState(node, state)
		return ok && readWrite
	case "read-only":
		readWrite, ok := htmlReadWriteState(node, state)
		return ok && !readWrite
	case "placeholder-shown":
		return htmlPlaceholderShown(node, state)
	case "default":
		return htmlDefaultState(node, state)
	case "indeterminate":
		return htmlIndeterminateState(node, state)
	case "valid":
		participates, valid, ok := htmlValidityState(node, state)
		return ok && participates && valid
	case "invalid":
		participates, valid, ok := htmlValidityState(node, state)
		return ok && participates && !valid
	case "user-valid":
		participates, valid, ok := htmlUserValidityState(node, state)
		return ok && participates && valid
	case "user-invalid":
		participates, valid, ok := htmlUserValidityState(node, state)
		return ok && participates && !valid
	case "in-range":
		participates, inRange, ok := dom.EvaluateRangeValidity(node, state.take)
		return ok && participates && inRange
	case "out-of-range":
		participates, inRange, ok := dom.EvaluateRangeValidity(node, state.take)
		return ok && participates && !inRange
	case "lang":
		return matchesLanguageRanges(node, pseudo.arguments, context, state)
	case "dir":
		return len(pseudo.arguments) == 1 && matchesDirectionality(node, pseudo.arguments[0], state)
	case "is", "where":
		return selectorListMatches(pseudo.selectors, node, context, state)
	case "not":
		matched := selectorListMatches(pseudo.selectors, node, context, state)
		return !state.exhausted && !matched
	case "has":
		return matchesHas(pseudo.selectors, node, context, state)
	case "nth-child":
		return matchesNth(pseudo, node, false, false, context, state)
	case "nth-last-child":
		return matchesNth(pseudo, node, true, false, context, state)
	case "nth-of-type":
		return matchesNth(pseudo, node, false, true, context, state)
	case "nth-last-of-type":
		return matchesNth(pseudo, node, true, true, context, state)
	default:
		return false
	}
}

func supportedSimplePseudoClass(name string) bool {
	switch name {
	case "root", "empty",
		"first-child", "last-child", "only-child",
		"first-of-type", "last-of-type", "only-of-type",
		"link", "any-link", "visited",
		"hover", "active", "focus", "focus-visible", "focus-within", "target",
		"checked", "disabled", "enabled", "required", "optional",
		"read-only", "read-write", "placeholder-shown", "default", "indeterminate", "valid", "invalid",
		"user-valid", "user-invalid", "in-range", "out-of-range":
		return true
	default:
		return false
	}
}

func selectorListMatches(selectors []Selector, node *dom.Node, context MatchContext, state *selectorMatchState) bool {
	for _, selector := range selectors {
		if selector.matchesWithState(node, context, state) {
			return true
		}
		if state.exhausted {
			return false
		}
	}
	return false
}

func matchesNth(pseudo pseudoClassSelector, node *dom.Node, fromEnd, ofType bool, context MatchContext, state *selectorMatchState) bool {
	index, ok := elementSiblingIndex(node, fromEnd, ofType, pseudo.selectors, context, state)
	if !ok {
		return false
	}
	if pseudo.nth.a == 0 {
		return index == pseudo.nth.b
	}
	difference := index - pseudo.nth.b
	return difference%pseudo.nth.a == 0 && difference/pseudo.nth.a >= 0
}

func elementSiblingIndex(node *dom.Node, fromEnd, ofType bool, filter []Selector, context MatchContext, state *selectorMatchState) (int, bool) {
	if node.Parent == nil {
		return 0, false
	}
	index := 0
	if fromEnd {
		for siblingIndex := len(node.Parent.Children) - 1; siblingIndex >= 0; siblingIndex-- {
			sibling := node.Parent.Children[siblingIndex]
			if !state.take() {
				return 0, false
			}
			if !includedSibling(sibling, node, ofType, filter, context, state) {
				continue
			}
			index++
			if sibling == node {
				return index, true
			}
		}
		return 0, false
	}
	for _, sibling := range node.Parent.Children {
		if !state.take() {
			return 0, false
		}
		if !includedSibling(sibling, node, ofType, filter, context, state) {
			continue
		}
		index++
		if sibling == node {
			return index, true
		}
	}
	return 0, false
}

func includedSibling(sibling, subject *dom.Node, ofType bool, filter []Selector, context MatchContext, state *selectorMatchState) bool {
	if sibling == nil || sibling.Type != dom.ElementNode {
		return false
	}
	if ofType && !equalASCIIFold(sibling.Data, subject.Data) {
		return false
	}
	return len(filter) == 0 || selectorListMatches(filter, sibling, context, state)
}

func inclusiveAncestor(candidate, descendant *dom.Node) bool {
	for current := descendant; current != nil; current = current.Parent {
		if current == candidate {
			return true
		}
	}
	return false
}

func isVisitedLink(node *dom.Node, context MatchContext) bool {
	return context.Visited != nil && context.Visited(node)
}

// isHTMLChecked implements HTML's live checkedness/selectedness rules. Dirty
// control state wins over the content attribute, just as the corresponding
// DOM properties do.
func isHTMLChecked(node *dom.Node) bool {
	switch {
	case isHTMLElementNamed(node, "input"):
		typeName := lowerASCII(attributeOrDefault(node, "type", "text"))
		if typeName != "checkbox" && typeName != "radio" {
			return false
		}
		if node.Control != nil && node.Control.CheckedDirty {
			return node.Control.Checked
		}
		return hasAttribute(node, "checked")
	case isHTMLElementNamed(node, "option"):
		return htmlOptionSelected(node)
	default:
		return false
	}
}

func htmlOptionSelected(option *dom.Node) bool {
	if option.Control != nil && option.Control.SelectedDirty {
		return option.Control.Selected
	}
	selectNode := nearestHTMLElementAncestor(option, "select")
	if selectNode == nil || hasAttribute(selectNode, "multiple") {
		return hasAttribute(option, "selected")
	}
	options := descendantHTMLOptions(selectNode)
	hasDirtyState := false
	for _, candidate := range options {
		if candidate.Control == nil || !candidate.Control.SelectedDirty {
			continue
		}
		hasDirtyState = true
		if candidate.Control.Selected {
			return candidate == option
		}
	}
	if hasDirtyState {
		return false
	}
	lastExplicit := -1
	for index, candidate := range options {
		if hasAttribute(candidate, "selected") {
			lastExplicit = index
		}
	}
	if lastExplicit >= 0 {
		return options[lastExplicit] == option
	}
	for _, candidate := range options {
		if !hasAttribute(candidate, "disabled") {
			return candidate == option
		}
	}
	return false
}

// htmlDisabledState returns whether node participates in HTML's enabled and
// disabled states and, when it does, whether it is actually disabled.
func htmlDisabledState(node *dom.Node) (bool, bool) {
	if node == nil || node.Type != dom.ElementNode || node.NamespaceURI != dom.HTMLNamespace {
		return false, false
	}
	switch lowerASCII(node.Data) {
	case "button", "input", "select", "textarea":
		return true, hasAttribute(node, "disabled") || disabledByFieldset(node)
	case "fieldset":
		return true, hasAttribute(node, "disabled") || disabledByFieldset(node)
	case "optgroup":
		selectNode := nearestHTMLElementAncestor(node, "select")
		return true, hasAttribute(node, "disabled") || htmlSelectDisabled(selectNode)
	case "option":
		if hasAttribute(node, "disabled") {
			return true, true
		}
		if group := nearestHTMLElementAncestorBefore(node, "optgroup", "select"); group != nil {
			if _, disabled := htmlDisabledState(group); disabled {
				return true, true
			}
		}
		return true, htmlSelectDisabled(nearestHTMLElementAncestor(node, "select"))
	default:
		return false, false
	}
}

func htmlSelectDisabled(node *dom.Node) bool {
	return isHTMLElementNamed(node, "select") && (hasAttribute(node, "disabled") || disabledByFieldset(node))
}

func disabledByFieldset(node *dom.Node) bool {
	for fieldset := node.Parent; fieldset != nil; fieldset = fieldset.Parent {
		if !isHTMLElementNamed(fieldset, "fieldset") || !hasAttribute(fieldset, "disabled") {
			continue
		}
		legend := firstLegendElementChild(fieldset)
		if legend != nil && inclusiveAncestor(legend, node) {
			continue
		}
		return true
	}
	return false
}

func firstLegendElementChild(fieldset *dom.Node) *dom.Node {
	for _, child := range fieldset.Children {
		if child != nil && child.Type == dom.ElementNode && isHTMLElementNamed(child, "legend") {
			return child
		}
	}
	return nil
}

func htmlRequiredState(node *dom.Node) (bool, bool) {
	if node == nil || node.Type != dom.ElementNode || node.NamespaceURI != dom.HTMLNamespace {
		return false, false
	}
	switch lowerASCII(node.Data) {
	case "select", "textarea":
		return true, hasAttribute(node, "required")
	case "input":
		if !inputSupportsRequired(attributeOrDefault(node, "type", "text")) {
			return false, false
		}
		return true, hasAttribute(node, "required")
	default:
		return false, false
	}
}

func inputSupportsRequired(typeName string) bool {
	switch lowerASCII(typeName) {
	case "hidden", "range", "color", "submit", "image", "reset", "button":
		return false
	default:
		return true
	}
}

func isHTMLElementNamed(node *dom.Node, name string) bool {
	return node != nil && node.Type == dom.ElementNode && node.NamespaceURI == dom.HTMLNamespace && equalASCIIFold(node.Data, name)
}

func nearestHTMLElementAncestor(node *dom.Node, name string) *dom.Node {
	for ancestor := node.Parent; ancestor != nil; ancestor = ancestor.Parent {
		if isHTMLElementNamed(ancestor, name) {
			return ancestor
		}
	}
	return nil
}

func nearestHTMLElementAncestorBefore(node *dom.Node, name, boundary string) *dom.Node {
	for ancestor := node.Parent; ancestor != nil; ancestor = ancestor.Parent {
		if isHTMLElementNamed(ancestor, boundary) {
			return nil
		}
		if isHTMLElementNamed(ancestor, name) {
			return ancestor
		}
	}
	return nil
}

func descendantHTMLOptions(root *dom.Node) []*dom.Node {
	options := make([]*dom.Node, 0)
	stack := make([]*dom.Node, 0, len(root.Children))
	for index := len(root.Children) - 1; index >= 0; index-- {
		stack = append(stack, root.Children[index])
	}
	for len(stack) != 0 {
		last := len(stack) - 1
		node := stack[last]
		stack = stack[:last]
		if isHTMLElementNamed(node, "option") {
			options = append(options, node)
		}
		for index := len(node.Children) - 1; index >= 0; index-- {
			stack = append(stack, node.Children[index])
		}
	}
	return options
}

func hasAttribute(node *dom.Node, name string) bool {
	_, found := attributeValue(node, name)
	return found
}

func attributeOrDefault(node *dom.Node, name, fallback string) string {
	value, found := attributeValue(node, name)
	if !found || value == "" {
		return fallback
	}
	return value
}

func isDocumentElement(node *dom.Node) bool {
	if node.Parent == nil || node.Parent.Type != dom.DocumentNode {
		return false
	}
	for _, child := range node.Parent.Children {
		if child != nil && child.Type == dom.ElementNode {
			return child == node
		}
	}
	return false
}

func isEmptyElement(node *dom.Node) bool {
	for _, child := range node.Children {
		if child == nil {
			continue
		}
		if child.Type == dom.ElementNode || child.Type == dom.TextNode && child.Data != "" {
			return false
		}
	}
	return true
}

func isFirstElementSibling(node *dom.Node, sameType bool) bool {
	if node.Parent == nil {
		return false
	}
	for _, sibling := range node.Parent.Children {
		if sibling == node {
			return true
		}
		if sibling == nil || sibling.Type != dom.ElementNode {
			continue
		}
		if !sameType || equalASCIIFold(sibling.Data, node.Data) {
			return false
		}
	}
	return false
}

func isLastElementSibling(node *dom.Node, sameType bool) bool {
	if node.Parent == nil {
		return false
	}
	found := false
	for _, sibling := range node.Parent.Children {
		if sibling == node {
			found = true
			continue
		}
		if !found || sibling == nil || sibling.Type != dom.ElementNode {
			continue
		}
		if !sameType || equalASCIIFold(sibling.Data, node.Data) {
			return false
		}
	}
	return found
}

func isUnvisitedLink(node *dom.Node) bool {
	if !equalASCIIFold(node.Data, "a") && !equalASCIIFold(node.Data, "area") && !equalASCIIFold(node.Data, "link") {
		return false
	}
	_, hasHref := attributeValue(node, "href")
	return hasHref
}

func previousElementSibling(node *dom.Node) *dom.Node {
	if node.Parent == nil {
		return nil
	}
	index := childIndex(node.Parent, node)
	for siblingIndex := index - 1; siblingIndex >= 0; siblingIndex-- {
		sibling := node.Parent.Children[siblingIndex]
		if sibling != nil && sibling.Type == dom.ElementNode {
			return sibling
		}
	}
	return nil
}

func childIndex(parent, child *dom.Node) int {
	for index, candidate := range parent.Children {
		if candidate == child {
			return index
		}
	}
	return -1
}

func attributeValue(node *dom.Node, name string) (string, bool) {
	for _, attribute := range node.Attributes {
		if equalASCIIFold(attribute.Name, name) {
			return attribute.Value, true
		}
	}
	return "", false
}

func containsCSSSpaceSeparatedToken(value, token string) bool {
	for start := 0; start < len(value); {
		for start < len(value) && isCSSWhitespace(value[start]) {
			start++
		}
		end := start
		for end < len(value) && !isCSSWhitespace(value[end]) {
			end++
		}
		if start < end && value[start:end] == token {
			return true
		}
		start = end
	}
	return false
}

func containsCSSWhitespace(value string) bool {
	for position := 0; position < len(value); position++ {
		if isCSSWhitespace(value[position]) {
			return true
		}
	}
	return false
}

func htmlAttributeValueCaseInsensitive(name string) bool {
	switch lowerASCII(name) {
	case "accept", "accept-charset", "align", "alink", "axis", "bgcolor", "charset",
		"checked", "clear", "codetype", "color", "compact", "declare", "defer", "dir",
		"direction", "disabled", "enctype", "face", "frame", "hreflang", "http-equiv",
		"lang", "language", "link", "media", "method", "multiple", "nohref", "noresize",
		"noshade", "nowrap", "readonly", "rel", "rev", "rules", "scope", "scrolling",
		"selected", "shape", "target", "text", "type", "valign", "valuetype", "vlink":
		return true
	default:
		return false
	}
}

func equalASCIIFold(left, right string) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range len(left) {
		leftByte := left[index]
		rightByte := right[index]
		if leftByte >= 'A' && leftByte <= 'Z' {
			leftByte += 'a' - 'A'
		}
		if rightByte >= 'A' && rightByte <= 'Z' {
			rightByte += 'a' - 'A'
		}
		if leftByte != rightByte {
			return false
		}
	}
	return true
}

func lowerASCII(value string) string {
	var lowered strings.Builder
	lowered.Grow(len(value))
	for index := range len(value) {
		character := value[index]
		if character >= 'A' && character <= 'Z' {
			character += 'a' - 'A'
		}
		lowered.WriteByte(character)
	}
	return lowered.String()
}
