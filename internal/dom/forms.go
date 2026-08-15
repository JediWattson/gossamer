package dom

import (
	"fmt"
	"strings"
	"unicode/utf16"
)

// FormCollectionKind identifies the live element lists exposed by select and
// form controls.
type FormCollectionKind uint8

const (
	SelectOptionCollection FormCollectionKind = iota + 1
	FormElementCollection
)

// FormValue returns the current value state for an HTML control. Dirty input
// and textarea values live in ControlState; option and button values reflect
// their content attributes; select values are coordinated with option state.
func (document *Document) FormValue(id NodeID) (string, error) {
	if document == nil || document.store == nil {
		return "", ErrInvalidDocument
	}
	document.store.mutex.RLock()
	defer document.store.mutex.RUnlock()
	node, ok := document.store.resolveLocked(id)
	if !ok {
		return "", fmt.Errorf("%w: %d", ErrUnknownNode, id)
	}
	return formValueLocked(node)
}

func formValueLocked(node *Node) (string, error) {
	if !isValueControl(node) {
		return "", fmt.Errorf("%w: node has no form value", ErrWrongNodeKind)
	}
	switch node.Data {
	case "input", "textarea":
		if node.Control != nil && node.Control.ValueDirty {
			return node.Control.Value, nil
		}
		if node.Data == "textarea" {
			return descendantText(node), nil
		}
		value, _ := attributeValue(node, "value")
		return value, nil
	case "option":
		if value, found := attributeValue(node, "value"); found {
			return value, nil
		}
		return strings.TrimSpace(descendantText(node)), nil
	case "select":
		options := optionNodes(node)
		index := selectedIndexForOptions(node, options)
		if index < 0 {
			return "", nil
		}
		return formValueLocked(options[index])
	case "button":
		value, _ := attributeValue(node, "value")
		return value, nil
	default:
		return "", fmt.Errorf("%w: node has no form value", ErrWrongNodeKind)
	}
}

func (document *Document) SetFormValue(id NodeID, value string) error {
	if document == nil || document.store == nil {
		return ErrInvalidDocument
	}
	document.store.mutex.Lock()
	defer document.store.mutex.Unlock()
	node, ok := document.store.resolveLocked(id)
	if !ok {
		return fmt.Errorf("%w: %d", ErrUnknownNode, id)
	}
	if !isValueControl(node) {
		return fmt.Errorf("%w: node %d has no form value", ErrWrongNodeKind, id)
	}
	changed := false
	switch node.Data {
	case "input", "textarea":
		state := ensureControlState(node)
		changed = !state.ValueDirty || state.Value != value
		state.Value = value
		state.ValueDirty = true
		state.SelectionStart = utf16Length(value)
		state.SelectionEnd = state.SelectionStart
		state.SelectionDirection = "none"
	case "select":
		options := optionNodes(node)
		matched := false
		for _, option := range options {
			optionValue, _ := formValueLocked(option)
			selected := !matched && optionValue == value
			state := ensureControlState(option)
			if !state.SelectedDirty || state.Selected != selected {
				changed = true
			}
			state.Selected = selected
			state.SelectedDirty = true
			if selected {
				matched = true
			}
		}
	case "option", "button":
		oldValue, oldValuePresent := attributeValue(node, "value")
		changed = setContentAttributeLocked(node, "value", value)
		if changed {
			document.recordAttributeMutationLocked(node, "value", oldValue, oldValuePresent)
		}
	}
	if changed {
		document.version.Add(1)
	}
	return nil
}

// FormSelection returns the UTF-16 selection range owned by a text control.
func (document *Document) FormSelection(id NodeID) (int, int, string, error) {
	if document == nil || document.store == nil {
		return 0, 0, "none", ErrInvalidDocument
	}
	document.store.mutex.RLock()
	defer document.store.mutex.RUnlock()
	node, ok := document.store.resolveLocked(id)
	if !ok {
		return 0, 0, "none", fmt.Errorf("%w: %d", ErrUnknownNode, id)
	}
	if !isTextControl(node) {
		return 0, 0, "none", fmt.Errorf("%w: node %d has no text selection", ErrWrongNodeKind, id)
	}
	state := node.Control
	if state == nil {
		return 0, 0, "none", nil
	}
	direction := normalizeSelectionDirection(state.SelectionDirection)
	return state.SelectionStart, state.SelectionEnd, direction, nil
}

// SetFormSelection updates a text control selection using DOM UTF-16 offsets.
func (document *Document) SetFormSelection(id NodeID, start, end int, direction string) error {
	if document == nil || document.store == nil {
		return ErrInvalidDocument
	}
	document.store.mutex.Lock()
	defer document.store.mutex.Unlock()
	node, ok := document.store.resolveLocked(id)
	if !ok {
		return fmt.Errorf("%w: %d", ErrUnknownNode, id)
	}
	if !isTextControl(node) {
		return fmt.Errorf("%w: node %d has no text selection", ErrWrongNodeKind, id)
	}
	value, err := formValueLocked(node)
	if err != nil {
		return err
	}
	length := utf16Length(value)
	start = clampSelectionOffset(start, length)
	end = clampSelectionOffset(end, length)
	if end < start {
		start = end
	}
	direction = normalizeSelectionDirection(direction)
	state := ensureControlState(node)
	if state.SelectionStart == start && state.SelectionEnd == end && state.SelectionDirection == direction {
		return nil
	}
	state.SelectionStart = start
	state.SelectionEnd = end
	state.SelectionDirection = direction
	document.version.Add(1)
	return nil
}

// ReplaceFormSelection performs the native edit associated with beforeinput.
func (document *Document) ReplaceFormSelection(id NodeID, data, inputType string) error {
	if document == nil || document.store == nil {
		return ErrInvalidDocument
	}
	document.store.mutex.Lock()
	defer document.store.mutex.Unlock()
	node, ok := document.store.resolveLocked(id)
	if !ok {
		return fmt.Errorf("%w: %d", ErrUnknownNode, id)
	}
	if !isTextControl(node) {
		return fmt.Errorf("%w: node %d has no editable selection", ErrWrongNodeKind, id)
	}
	value, err := formValueLocked(node)
	if err != nil {
		return err
	}
	state := ensureControlState(node)
	units := utf16.Encode([]rune(value))
	start := clampSelectionOffset(state.SelectionStart, len(units))
	end := clampSelectionOffset(state.SelectionEnd, len(units))
	if end < start {
		start = end
	}
	replacement := data
	switch inputType {
	case "", "insertText", "insertCompositionText", "insertReplacementText", "insertFromPaste", "insertFromDrop":
	case "insertLineBreak", "insertParagraph":
		if node.Data == "textarea" {
			replacement = "\n"
		} else {
			replacement = ""
		}
	case "deleteContentBackward":
		replacement = ""
		if start == end && start > 0 {
			start--
			if start > 0 && units[start] >= 0xdc00 && units[start] <= 0xdfff && units[start-1] >= 0xd800 && units[start-1] <= 0xdbff {
				start--
			}
		}
	case "deleteContentForward":
		replacement = ""
		if start == end && end < len(units) {
			end++
			if end < len(units) && units[end-1] >= 0xd800 && units[end-1] <= 0xdbff && units[end] >= 0xdc00 && units[end] <= 0xdfff {
				end++
			}
		}
	case "deleteByCut", "deleteByDrag", "deleteContent", "deleteEntireSoftLine":
		replacement = ""
	default:
		return nil
	}
	replacementUnits := utf16.Encode([]rune(replacement))
	updated := make([]uint16, 0, len(units)-(end-start)+len(replacementUnits))
	updated = append(updated, units[:start]...)
	updated = append(updated, replacementUnits...)
	updated = append(updated, units[end:]...)
	state.Value = string(utf16.Decode(updated))
	state.ValueDirty = true
	state.SelectionStart = start + len(replacementUnits)
	state.SelectionEnd = state.SelectionStart
	state.SelectionDirection = "none"
	document.version.Add(1)
	return nil
}

func (document *Document) FormChecked(id NodeID) (bool, error) {
	if document == nil || document.store == nil {
		return false, ErrInvalidDocument
	}
	document.store.mutex.RLock()
	defer document.store.mutex.RUnlock()
	node, ok := document.store.resolveLocked(id)
	if !ok {
		return false, fmt.Errorf("%w: %d", ErrUnknownNode, id)
	}
	if !isHTMLControl(node, "input") {
		return false, fmt.Errorf("%w: node %d has no checked state", ErrWrongNodeKind, id)
	}
	return checkedLocked(node), nil
}

func checkedLocked(node *Node) bool {
	if node.Control != nil && node.Control.CheckedDirty {
		return node.Control.Checked
	}
	return hasAttributeValue(node, "checked")
}

func (document *Document) SetFormChecked(id NodeID, checked bool) error {
	if document == nil || document.store == nil {
		return ErrInvalidDocument
	}
	document.store.mutex.Lock()
	defer document.store.mutex.Unlock()
	node, ok := document.store.resolveLocked(id)
	if !ok {
		return fmt.Errorf("%w: %d", ErrUnknownNode, id)
	}
	if !isHTMLControl(node, "input") {
		return fmt.Errorf("%w: node %d has no checked state", ErrWrongNodeKind, id)
	}
	changed := setCheckedState(node, checked)
	if checked && strings.EqualFold(contentAttribute(node, "type"), "radio") {
		for _, candidate := range document.store.nodes {
			if candidate == nil || candidate == node || !sameRadioGroupLocked(document, node, candidate) {
				continue
			}
			if setCheckedState(candidate, false) {
				changed = true
			}
		}
	}
	if changed {
		document.version.Add(1)
	}
	return nil
}

// RadioGroupNodes returns the same-document radio group containing id.
func (document *Document) RadioGroupNodes(id NodeID) ([]NodeID, error) {
	if document == nil || document.store == nil {
		return nil, ErrInvalidDocument
	}
	document.store.mutex.RLock()
	defer document.store.mutex.RUnlock()
	node, ok := document.store.resolveLocked(id)
	if !ok {
		return nil, fmt.Errorf("%w: %d", ErrUnknownNode, id)
	}
	if !isHTMLControl(node, "input") || !strings.EqualFold(contentAttribute(node, "type"), "radio") {
		return nil, fmt.Errorf("%w: node %d is not a radio input", ErrWrongNodeKind, id)
	}
	result := make([]NodeID, 0, 2)
	for candidateID, candidate := range document.store.nodes {
		if sameRadioGroupLocked(document, node, candidate) {
			result = append(result, NodeID(candidateID))
		}
	}
	return result, nil
}

func setCheckedState(node *Node, checked bool) bool {
	state := ensureControlState(node)
	changed := !state.CheckedDirty || state.Checked != checked
	state.Checked = checked
	state.CheckedDirty = true
	return changed
}

// FormSelected exposes HTMLOptionElement selectedness, including the implicit
// first-option selection of a single-select with no explicit selection.
func (document *Document) FormSelected(id NodeID) (bool, error) {
	if document == nil || document.store == nil {
		return false, ErrInvalidDocument
	}
	document.store.mutex.RLock()
	defer document.store.mutex.RUnlock()
	node, ok := document.store.resolveLocked(id)
	if !ok {
		return false, fmt.Errorf("%w: %d", ErrUnknownNode, id)
	}
	if !isHTMLControl(node, "option") {
		return false, fmt.Errorf("%w: node %d has no selected state", ErrWrongNodeKind, id)
	}
	return optionSelectedLocked(node), nil
}

func (document *Document) SetFormSelected(id NodeID, selected bool) error {
	if document == nil || document.store == nil {
		return ErrInvalidDocument
	}
	document.store.mutex.Lock()
	defer document.store.mutex.Unlock()
	option, ok := document.store.resolveLocked(id)
	if !ok {
		return fmt.Errorf("%w: %d", ErrUnknownNode, id)
	}
	if !isHTMLControl(option, "option") {
		return fmt.Errorf("%w: node %d has no selected state", ErrWrongNodeKind, id)
	}
	changed := false
	selectNode := nearestHTMLAncestor(option, "select")
	if selectNode != nil {
		options := optionNodes(selectNode)
		currentIndex := selectedIndexForOptions(selectNode, options)
		multiple := hasAttributeValue(selectNode, "multiple")
		for index, candidate := range options {
			state := ensureControlState(candidate)
			value := index == currentIndex
			if multiple {
				value = optionSelectedState(candidate)
			}
			if candidate == option {
				value = selected
			} else if selected && !multiple {
				value = false
			}
			if !state.SelectedDirty || state.Selected != value {
				changed = true
			}
			state.Selected = value
			state.SelectedDirty = true
		}
	} else {
		state := ensureControlState(option)
		changed = !state.SelectedDirty || state.Selected != selected
		state.Selected = selected
		state.SelectedDirty = true
	}
	if changed {
		document.version.Add(1)
	}
	return nil
}

func optionSelectedLocked(option *Node) bool {
	if option.Control != nil && option.Control.SelectedDirty {
		return option.Control.Selected
	}
	selectNode := nearestHTMLAncestor(option, "select")
	if selectNode == nil || hasAttributeValue(selectNode, "multiple") {
		return hasAttributeValue(option, "selected")
	}
	options := optionNodes(selectNode)
	index := selectedIndexForOptions(selectNode, options)
	return index >= 0 && options[index] == option
}

// FormSelectedIndex returns or updates HTMLSelectElement.selectedIndex.
func (document *Document) FormSelectedIndex(id NodeID) (int, error) {
	if document == nil || document.store == nil {
		return -1, ErrInvalidDocument
	}
	document.store.mutex.RLock()
	defer document.store.mutex.RUnlock()
	selectNode, ok := document.store.resolveLocked(id)
	if !ok {
		return -1, fmt.Errorf("%w: %d", ErrUnknownNode, id)
	}
	if !isHTMLControl(selectNode, "select") {
		return -1, fmt.Errorf("%w: node %d is not a select", ErrWrongNodeKind, id)
	}
	return selectedIndexForOptions(selectNode, optionNodes(selectNode)), nil
}

func (document *Document) SetFormSelectedIndex(id NodeID, selectedIndex int) error {
	if document == nil || document.store == nil {
		return ErrInvalidDocument
	}
	document.store.mutex.Lock()
	defer document.store.mutex.Unlock()
	selectNode, ok := document.store.resolveLocked(id)
	if !ok {
		return fmt.Errorf("%w: %d", ErrUnknownNode, id)
	}
	if !isHTMLControl(selectNode, "select") {
		return fmt.Errorf("%w: node %d is not a select", ErrWrongNodeKind, id)
	}
	options := optionNodes(selectNode)
	changed := false
	for index, option := range options {
		state := ensureControlState(option)
		selected := selectedIndex >= 0 && index == selectedIndex
		if !state.SelectedDirty || state.Selected != selected {
			changed = true
		}
		state.Selected = selected
		state.SelectedDirty = true
	}
	if changed {
		document.version.Add(1)
	}
	return nil
}

func selectedIndexForOptions(selectNode *Node, options []*Node) int {
	hasDirtyState := false
	for index, option := range options {
		if option.Control != nil && option.Control.SelectedDirty {
			hasDirtyState = true
			if option.Control.Selected {
				return index
			}
		}
	}
	if hasDirtyState {
		return -1
	}
	lastExplicit := -1
	for index, option := range options {
		if option.Control != nil && option.Control.SelectedDirty {
			continue
		}
		if hasAttributeValue(option, "selected") {
			lastExplicit = index
		}
	}
	if lastExplicit >= 0 {
		return lastExplicit
	}
	if !hasAttributeValue(selectNode, "multiple") && len(options) != 0 {
		for index, option := range options {
			if !hasAttributeValue(option, "disabled") {
				return index
			}
		}
	}
	return -1
}

func optionSelectedState(option *Node) bool {
	if option.Control != nil && option.Control.SelectedDirty {
		return option.Control.Selected
	}
	return hasAttributeValue(option, "selected")
}

// FormControlNodes returns a live collection snapshot in current tree order.
func (document *Document) FormControlNodes(id NodeID, kind FormCollectionKind) ([]NodeID, error) {
	if document == nil || document.store == nil {
		return nil, ErrInvalidDocument
	}
	document.store.mutex.RLock()
	defer document.store.mutex.RUnlock()
	node, ok := document.store.resolveLocked(id)
	if !ok {
		return nil, fmt.Errorf("%w: %d", ErrUnknownNode, id)
	}
	var nodes []*Node
	switch kind {
	case SelectOptionCollection:
		if !isHTMLControl(node, "select") {
			return nil, fmt.Errorf("%w: node %d is not a select", ErrWrongNodeKind, id)
		}
		nodes = optionNodes(node)
	case FormElementCollection:
		if !isHTMLControl(node, "form") {
			return nil, fmt.Errorf("%w: node %d is not a form", ErrWrongNodeKind, id)
		}
		root, _ := document.store.resolveLocked(document.root)
		collectionRoot := root
		if treeRoot(node) != root {
			collectionRoot = node
		}
		walkElements(collectionRoot, func(candidate *Node) {
			if isListedControl(candidate) && formOwnerNodeLocked(document, candidate) == node {
				nodes = append(nodes, candidate)
			}
		})
	default:
		return nil, NewException(SyntaxError, ErrInvalidTree, "unknown form collection %d", kind)
	}
	result := make([]NodeID, 0, len(nodes))
	for _, candidate := range nodes {
		candidateID, found := document.store.ids[candidate]
		if !found {
			return nil, fmt.Errorf("%w: form control is not indexed", ErrInvalidTree)
		}
		result = append(result, candidateID)
	}
	return result, nil
}

// FormOwner returns the associated form for a listed control.
func (document *Document) FormOwner(id NodeID) (NodeID, bool, error) {
	if document == nil || document.store == nil {
		return InvalidNodeID, false, ErrInvalidDocument
	}
	document.store.mutex.RLock()
	defer document.store.mutex.RUnlock()
	node, ok := document.store.resolveLocked(id)
	if !ok {
		return InvalidNodeID, false, fmt.Errorf("%w: %d", ErrUnknownNode, id)
	}
	if !isListedControl(node) {
		return InvalidNodeID, false, nil
	}
	owner := formOwnerNodeLocked(document, node)
	if owner == nil {
		return InvalidNodeID, false, nil
	}
	ownerID, found := document.store.ids[owner]
	return ownerID, found, nil
}

// ResetForm clears dirty control state for every associated control.
func (document *Document) ResetForm(id NodeID) error {
	if document == nil || document.store == nil {
		return ErrInvalidDocument
	}
	document.store.mutex.Lock()
	defer document.store.mutex.Unlock()
	form, ok := document.store.resolveLocked(id)
	if !ok {
		return fmt.Errorf("%w: %d", ErrUnknownNode, id)
	}
	if !isHTMLControl(form, "form") {
		return fmt.Errorf("%w: node %d is not a form", ErrWrongNodeKind, id)
	}
	changed := false
	root, _ := document.store.resolveLocked(document.root)
	resetRoot := root
	if treeRoot(form) != root {
		resetRoot = form
	}
	walkElements(resetRoot, func(candidate *Node) {
		if candidate.Control == nil {
			return
		}
		owner := formOwnerNodeLocked(document, candidate)
		if owner != form {
			selectNode := nearestHTMLAncestor(candidate, "select")
			if selectNode == nil || formOwnerNodeLocked(document, selectNode) != form {
				return
			}
		}
		if candidate.Control.ValueDirty || candidate.Control.CheckedDirty || candidate.Control.SelectedDirty {
			changed = true
		}
		candidate.Control.Value = ""
		candidate.Control.ValueDirty = false
		candidate.Control.Checked = false
		candidate.Control.CheckedDirty = false
		candidate.Control.Selected = false
		candidate.Control.SelectedDirty = false
		candidate.Control.SelectionStart = 0
		candidate.Control.SelectionEnd = 0
		candidate.Control.SelectionDirection = "none"
	})
	if changed {
		document.version.Add(1)
	}
	return nil
}

func sameRadioGroupLocked(document *Document, first, second *Node) bool {
	if !isHTMLControl(second, "input") || !strings.EqualFold(contentAttribute(second, "type"), "radio") {
		return false
	}
	if contentAttribute(first, "name") != contentAttribute(second, "name") {
		return false
	}
	firstOwner := formOwnerNodeLocked(document, first)
	secondOwner := formOwnerNodeLocked(document, second)
	if firstOwner != nil || secondOwner != nil {
		return firstOwner == secondOwner
	}
	return treeRoot(first) == treeRoot(second)
}

func formOwnerNodeLocked(document *Document, node *Node) *Node {
	if explicit, found := attributeValue(node, "form"); found && explicit != "" {
		root, _ := document.store.resolveLocked(document.root)
		var match *Node
		walkElements(root, func(candidate *Node) {
			if match == nil && isHTMLControl(candidate, "form") && contentAttribute(candidate, "id") == explicit {
				match = candidate
			}
		})
		return match
	}
	return nearestHTMLAncestor(node, "form")
}

func nearestHTMLAncestor(node *Node, name string) *Node {
	for ancestor := node.Parent; ancestor != nil; ancestor = ancestor.Parent {
		if isHTMLControl(ancestor, name) {
			return ancestor
		}
	}
	return nil
}

func optionNodes(selectNode *Node) []*Node {
	var result []*Node
	walkElements(selectNode, func(candidate *Node) {
		if candidate != selectNode && isHTMLControl(candidate, "option") {
			result = append(result, candidate)
		}
	})
	return result
}

func walkElements(root *Node, callback func(*Node)) {
	if root == nil {
		return
	}
	stack := []*Node{root}
	for len(stack) != 0 {
		last := len(stack) - 1
		node := stack[last]
		stack = stack[:last]
		if node.Type == ElementNode {
			callback(node)
		}
		for index := len(node.Children) - 1; index >= 0; index-- {
			stack = append(stack, node.Children[index])
		}
	}
}

func isListedControl(node *Node) bool {
	if node == nil || node.Type != ElementNode || node.NamespaceURI != HTMLNamespace {
		return false
	}
	switch node.Data {
	case "input", "textarea", "select", "button":
		return true
	default:
		return false
	}
}

func isValueControl(node *Node) bool {
	if node == nil || node.Type != ElementNode || node.NamespaceURI != HTMLNamespace {
		return false
	}
	switch node.Data {
	case "input", "textarea", "option", "select", "button":
		return true
	default:
		return false
	}
}

func isTextControl(node *Node) bool {
	return isHTMLControl(node, "input") || isHTMLControl(node, "textarea")
}

func utf16Length(value string) int {
	return len(utf16.Encode([]rune(value)))
}

func clampSelectionOffset(offset, length int) int {
	if offset < 0 {
		return 0
	}
	if offset > length {
		return length
	}
	return offset
}

func normalizeSelectionDirection(direction string) string {
	switch direction {
	case "forward", "backward":
		return direction
	default:
		return "none"
	}
}

func isHTMLControl(node *Node, name string) bool {
	return node != nil && node.Type == ElementNode && node.NamespaceURI == HTMLNamespace && node.Data == name
}

func ensureControlState(node *Node) *ControlState {
	if node.Control == nil {
		node.Control = &ControlState{}
	}
	return node.Control
}

func treeRoot(node *Node) *Node {
	for node != nil && node.Parent != nil {
		node = node.Parent
	}
	return node
}

func contentAttribute(node *Node, name string) string {
	value, _ := attributeValue(node, name)
	return value
}

func hasAttributeValue(node *Node, name string) bool {
	_, found := attributeValue(node, name)
	return found
}

func setContentAttributeLocked(node *Node, name, value string) bool {
	for index := range node.Attributes {
		if node.Attributes[index].Name != name {
			continue
		}
		if node.Attributes[index].Value == value {
			return false
		}
		node.Attributes[index].Value = value
		return true
	}
	node.Attributes = append(node.Attributes, Attribute{Name: name, Value: value})
	return true
}
