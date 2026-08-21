package nativeengine

import (
	"fmt"
	"unicode/utf16"

	"github.com/JediWattson/gossamer/internal/browser"
	"github.com/JediWattson/gossamer/internal/dom"
	browserruntime "github.com/JediWattson/gossamer/internal/runtime"
	"github.com/JediWattson/gossamer/internal/runtime/memory"
)

const (
	rangeBrandProperty     = "\x00gossamer.range.brand"
	rangeStartNodeProperty = "\x00gossamer.range.start-node"
	rangeStartOffProperty  = "\x00gossamer.range.start-offset"
	rangeEndNodeProperty   = "\x00gossamer.range.end-node"
	rangeEndOffProperty    = "\x00gossamer.range.end-offset"
	selectionRangeProperty = "\x00gossamer.selection.range"
)

type nativeRangeState struct {
	object      memory.Ref
	startValue  memory.Value
	start       browser.NodeHandle
	startOffset int
	endValue    memory.Value
	end         browser.NodeHandle
	endOffset   int
}

func constructorPrototype(context *browserruntime.TaskContext, constructor memory.Ref, name string) (memory.Ref, error) {
	prototypeName, err := context.NewString("prototype")
	if err != nil {
		return memory.Ref{}, err
	}
	prototype, found, err := context.GetOwnProperty(constructor, prototypeName)
	if err != nil || !found || !prototype.IsRef() {
		return memory.Ref{}, fmt.Errorf("nativeengine: %s constructor lost its prototype", name)
	}
	return prototype.Ref(), nil
}

func (realm *Realm) documentCreateRange(context *browserruntime.TaskContext, _ memory.Value, _ []memory.Value) (memory.Value, error) {
	return realm.newRangeLocked(context, memory.RefValue(realm.bindings.document), 0, memory.RefValue(realm.bindings.document), 0)
}

func (realm *Realm) documentGetSelection(_ *browserruntime.TaskContext, _ memory.Value, _ []memory.Value) (memory.Value, error) {
	return memory.RefValue(realm.bindings.selection), nil
}

func (realm *Realm) newRangeLocked(
	context *browserruntime.TaskContext,
	start memory.Value,
	startOffset int,
	end memory.Value,
	endOffset int,
) (memory.Value, error) {
	object, err := context.NewHeapObject()
	if err != nil {
		return memory.Value{}, err
	}
	if err := context.SetPrototype(object, memory.RefValue(realm.bindings.rangePrototype)); err != nil {
		return memory.Value{}, err
	}
	for _, property := range []struct {
		name  string
		value memory.Value
	}{
		{rangeBrandProperty, memory.BoolValue(true)},
		{rangeStartNodeProperty, start},
		{rangeStartOffProperty, memory.NumberValue(float64(startOffset))},
		{rangeEndNodeProperty, end},
		{rangeEndOffProperty, memory.NumberValue(float64(endOffset))},
	} {
		if err := defineData(context, object, property.name, property.value, true, false, false); err != nil {
			return memory.Value{}, err
		}
	}
	return memory.RefValue(object), nil
}

func (realm *Realm) newSelectionLocked(context *browserruntime.TaskContext) (memory.Ref, error) {
	object, err := context.NewHeapObject()
	if err != nil {
		return memory.Ref{}, err
	}
	if err := context.SetPrototype(object, memory.RefValue(realm.bindings.selectionPrototype)); err != nil {
		return memory.Ref{}, err
	}
	if err := defineData(context, object, selectionRangeProperty, memory.NullValue(), true, false, false); err != nil {
		return memory.Ref{}, err
	}
	return object, nil
}

func hiddenProperty(context *browserruntime.TaskContext, object memory.Ref, name string) (memory.Value, bool, error) {
	key, err := context.NewString(name)
	if err != nil {
		return memory.Value{}, false, err
	}
	return context.GetOwnProperty(object, key)
}

func setHiddenProperty(context *browserruntime.TaskContext, object memory.Ref, name string, value memory.Value) error {
	key, err := context.NewString(name)
	if err != nil {
		return err
	}
	return context.SetProperty(object, key, value)
}

func (realm *Realm) readRange(context *browserruntime.TaskContext, value memory.Value) (nativeRangeState, error) {
	if !value.IsRef() {
		return nativeRangeState{}, fmt.Errorf("%w: invalid Range receiver", browserruntime.ErrOperandType)
	}
	brand, found, err := hiddenProperty(context, value.Ref(), rangeBrandProperty)
	if err != nil || !found || brand.Kind() != memory.ValueBool || !brand.Bool() {
		return nativeRangeState{}, fmt.Errorf("%w: invalid Range receiver", browserruntime.ErrOperandType)
	}
	state := nativeRangeState{object: value.Ref()}
	properties := []struct {
		name        string
		destination *memory.Value
	}{
		{rangeStartNodeProperty, &state.startValue},
		{rangeEndNodeProperty, &state.endValue},
	}
	for _, property := range properties {
		current, exists, lookupErr := hiddenProperty(context, value.Ref(), property.name)
		if lookupErr != nil || !exists {
			return nativeRangeState{}, fmt.Errorf("nativeengine: Range lost %s", property.name)
		}
		*property.destination = current
	}
	state.start, err = realm.unwrapNode(context, state.startValue)
	if err != nil {
		return nativeRangeState{}, err
	}
	state.end, err = realm.unwrapNode(context, state.endValue)
	if err != nil {
		return nativeRangeState{}, err
	}
	for _, property := range []struct {
		name        string
		destination *int
	}{
		{rangeStartOffProperty, &state.startOffset},
		{rangeEndOffProperty, &state.endOffset},
	} {
		current, exists, lookupErr := hiddenProperty(context, value.Ref(), property.name)
		if lookupErr != nil || !exists || current.Kind() != memory.ValueNumber {
			return nativeRangeState{}, fmt.Errorf("nativeengine: Range lost %s", property.name)
		}
		*property.destination = int(current.Number())
	}
	return state, nil
}

func (realm *Realm) writeRangeBoundary(context *browserruntime.TaskContext, state nativeRangeState, start bool, node memory.Value, offset int) error {
	if offset < 0 {
		return fmt.Errorf("%w: Range boundary offset is negative", browserruntime.ErrOperandType)
	}
	handle, err := realm.unwrapNode(context, node)
	if err != nil {
		return err
	}
	limit, err := realm.rangeBoundaryLimit(handle)
	if err != nil {
		return err
	}
	if offset > limit {
		return fmt.Errorf("%w: Range boundary offset exceeds the node length", browserruntime.ErrOperandType)
	}
	nodeProperty, offsetProperty := rangeEndNodeProperty, rangeEndOffProperty
	if start {
		nodeProperty, offsetProperty = rangeStartNodeProperty, rangeStartOffProperty
	}
	if err := setHiddenProperty(context, state.object, nodeProperty, node); err != nil {
		return err
	}
	return setHiddenProperty(context, state.object, offsetProperty, memory.NumberValue(float64(offset)))
}

func (realm *Realm) rangeBoundaryLimit(handle browser.NodeHandle) (int, error) {
	host, ok := realm.host.(browser.DOMElementHost)
	if !ok {
		return 0, fmt.Errorf("nativeengine: browser host does not expose Range traversal")
	}
	metadata, err := host.NodeMetadata(handle)
	if err != nil {
		return 0, err
	}
	if metadata.Type == browser.DOMTextNode || metadata.Type == browser.DOMCommentNode || metadata.Type == browser.DOMProcessingInstructionNode {
		text, present, err := host.NodeValue(handle)
		if err != nil {
			return 0, err
		}
		if !present {
			return 0, nil
		}
		return len(utf16.Encode([]rune(text))), nil
	}
	children, err := host.ChildNodes(handle, false)
	return len(children), err
}

func (realm *Realm) rangeStartContainer(context *browserruntime.TaskContext, this memory.Value, _ []memory.Value) (memory.Value, error) {
	state, err := realm.readRange(context, this)
	return state.startValue, err
}

func (realm *Realm) rangeStartOffset(context *browserruntime.TaskContext, this memory.Value, _ []memory.Value) (memory.Value, error) {
	state, err := realm.readRange(context, this)
	return memory.NumberValue(float64(state.startOffset)), err
}

func (realm *Realm) rangeEndContainer(context *browserruntime.TaskContext, this memory.Value, _ []memory.Value) (memory.Value, error) {
	state, err := realm.readRange(context, this)
	return state.endValue, err
}

func (realm *Realm) rangeEndOffset(context *browserruntime.TaskContext, this memory.Value, _ []memory.Value) (memory.Value, error) {
	state, err := realm.readRange(context, this)
	return memory.NumberValue(float64(state.endOffset)), err
}

func (realm *Realm) rangeCollapsed(context *browserruntime.TaskContext, this memory.Value, _ []memory.Value) (memory.Value, error) {
	state, err := realm.readRange(context, this)
	return memory.BoolValue(state.start == state.end && state.startOffset == state.endOffset), err
}

func (realm *Realm) rangeCommonAncestor(context *browserruntime.TaskContext, this memory.Value, _ []memory.Value) (memory.Value, error) {
	state, err := realm.readRange(context, this)
	if err != nil {
		return memory.Value{}, err
	}
	common, err := realm.commonRangeAncestor(state)
	if err != nil {
		return memory.Value{}, err
	}
	return realm.wrappedNodeValue(context, common)
}

func (realm *Realm) rangeSetStart(context *browserruntime.TaskContext, this memory.Value, arguments []memory.Value) (memory.Value, error) {
	return realm.rangeSetBoundary(context, this, arguments, true)
}

func (realm *Realm) rangeSetEnd(context *browserruntime.TaskContext, this memory.Value, arguments []memory.Value) (memory.Value, error) {
	return realm.rangeSetBoundary(context, this, arguments, false)
}

func (realm *Realm) rangeSetBoundary(context *browserruntime.TaskContext, this memory.Value, arguments []memory.Value, start bool) (memory.Value, error) {
	state, err := realm.readRange(context, this)
	if err != nil {
		return memory.Value{}, err
	}
	offset, err := integerArgument(arguments, 1)
	if err != nil {
		return memory.Value{}, err
	}
	return memory.UndefinedValue(), realm.writeRangeBoundary(context, state, start, argument(arguments, 0), offset)
}

func (realm *Realm) rangeNodeIndex(handle browser.NodeHandle) (browser.NodeHandle, int, error) {
	host := realm.host.(browser.DOMElementHost)
	parent, found, err := host.RelatedNode(handle, browser.RelationParentNode)
	if err != nil || !found {
		return browser.NodeHandle{}, 0, fmt.Errorf("nativeengine: Range node has no parent")
	}
	children, err := host.ChildNodes(parent, false)
	if err != nil {
		return browser.NodeHandle{}, 0, err
	}
	for index, child := range children {
		if child == handle {
			return parent, index, nil
		}
	}
	return browser.NodeHandle{}, 0, fmt.Errorf("nativeengine: Range node is missing from its parent")
}

func (realm *Realm) rangeSelectNode(context *browserruntime.TaskContext, this memory.Value, arguments []memory.Value) (memory.Value, error) {
	state, err := realm.readRange(context, this)
	if err != nil {
		return memory.Value{}, err
	}
	node, err := realm.unwrapNode(context, argument(arguments, 0))
	if err != nil {
		return memory.Value{}, err
	}
	parent, index, err := realm.rangeNodeIndex(node)
	if err != nil {
		return memory.Value{}, err
	}
	parentValue, err := realm.wrappedNodeValue(context, parent)
	if err != nil {
		return memory.Value{}, err
	}
	if err := realm.writeRangeBoundary(context, state, true, parentValue, index); err != nil {
		return memory.Value{}, err
	}
	return memory.UndefinedValue(), realm.writeRangeBoundary(context, state, false, parentValue, index+1)
}

func (realm *Realm) rangeSelectNodeContents(context *browserruntime.TaskContext, this memory.Value, arguments []memory.Value) (memory.Value, error) {
	state, err := realm.readRange(context, this)
	if err != nil {
		return memory.Value{}, err
	}
	nodeValue := argument(arguments, 0)
	node, err := realm.unwrapNode(context, nodeValue)
	if err != nil {
		return memory.Value{}, err
	}
	limit, err := realm.rangeBoundaryLimit(node)
	if err != nil {
		return memory.Value{}, err
	}
	if err := realm.writeRangeBoundary(context, state, true, nodeValue, 0); err != nil {
		return memory.Value{}, err
	}
	return memory.UndefinedValue(), realm.writeRangeBoundary(context, state, false, nodeValue, limit)
}

func (realm *Realm) rangeCollapse(context *browserruntime.TaskContext, this memory.Value, arguments []memory.Value) (memory.Value, error) {
	state, err := realm.readRange(context, this)
	if err != nil {
		return memory.Value{}, err
	}
	if truthy(argument(arguments, 0)) {
		if err := realm.writeRangeBoundary(context, state, false, state.startValue, state.startOffset); err != nil {
			return memory.Value{}, err
		}
	} else if err := realm.writeRangeBoundary(context, state, true, state.endValue, state.endOffset); err != nil {
		return memory.Value{}, err
	}
	return memory.UndefinedValue(), nil
}

func (realm *Realm) rangeCloneRange(context *browserruntime.TaskContext, this memory.Value, _ []memory.Value) (memory.Value, error) {
	state, err := realm.readRange(context, this)
	if err != nil {
		return memory.Value{}, err
	}
	return realm.newRangeLocked(context, state.startValue, state.startOffset, state.endValue, state.endOffset)
}

func (realm *Realm) rangeCloneContents(context *browserruntime.TaskContext, this memory.Value, _ []memory.Value) (memory.Value, error) {
	return realm.rangeContents(context, this, dom.RangeCloneContents)
}

func (realm *Realm) rangeExtractContents(context *browserruntime.TaskContext, this memory.Value, _ []memory.Value) (memory.Value, error) {
	return realm.rangeContents(context, this, dom.RangeExtractContents)
}

func (realm *Realm) rangeDeleteContents(context *browserruntime.TaskContext, this memory.Value, _ []memory.Value) (memory.Value, error) {
	return realm.rangeContents(context, this, dom.RangeDeleteContents)
}

func (realm *Realm) rangeContents(context *browserruntime.TaskContext, this memory.Value, operation dom.RangeContentOperation) (memory.Value, error) {
	state, err := realm.readRange(context, this)
	if err != nil {
		return memory.Value{}, err
	}
	host, ok := realm.host.(browser.DOMElementHost)
	if !ok {
		return memory.Value{}, fmt.Errorf("nativeengine: browser host does not expose Range contents")
	}
	fragment, err := host.RangeContents(state.start, state.startOffset, state.end, state.endOffset, operation)
	if err != nil {
		return memory.Value{}, err
	}
	if operation != dom.RangeCloneContents {
		if err := realm.writeRangeBoundary(context, state, false, state.startValue, state.startOffset); err != nil {
			return memory.Value{}, err
		}
		if err := realm.refreshCollections(context, state.start, state.end, realm.parentHandle(state.start), realm.parentHandle(state.end)); err != nil {
			return memory.Value{}, err
		}
	}
	if operation == dom.RangeDeleteContents {
		return memory.UndefinedValue(), nil
	}
	return realm.wrappedNodeValue(context, fragment)
}

func (realm *Realm) rangeInsertNode(context *browserruntime.TaskContext, this memory.Value, arguments []memory.Value) (memory.Value, error) {
	state, err := realm.readRange(context, this)
	if err != nil {
		return memory.Value{}, err
	}
	node, err := realm.unwrapNode(context, argument(arguments, 0))
	if err != nil {
		return memory.Value{}, err
	}
	host := realm.host.(browser.DOMElementHost)
	metadata, err := host.NodeMetadata(state.start)
	if err != nil {
		return memory.Value{}, err
	}
	oldParent := realm.parentHandle(node)
	parent := state.start
	if metadata.Type == browser.DOMTextNode {
		tail, err := host.SplitText(state.start, state.startOffset)
		if err != nil {
			return memory.Value{}, err
		}
		parent, _, err = host.RelatedNode(tail, browser.RelationParentNode)
		if err != nil {
			return memory.Value{}, err
		}
		if err := realm.host.InsertBefore(parent, node, tail); err != nil {
			return memory.Value{}, err
		}
	} else {
		children, err := host.ChildNodes(state.start, false)
		if err != nil {
			return memory.Value{}, err
		}
		if state.startOffset < len(children) {
			err = realm.host.InsertBefore(state.start, node, children[state.startOffset])
		} else {
			err = realm.host.AppendChild(state.start, node)
		}
		if err != nil {
			return memory.Value{}, err
		}
	}
	return memory.UndefinedValue(), realm.refreshCollections(context, parent, oldParent)
}

func (realm *Realm) rangeDetach(_ *browserruntime.TaskContext, _ memory.Value, _ []memory.Value) (memory.Value, error) {
	return memory.UndefinedValue(), nil
}

func (realm *Realm) commonRangeAncestor(state nativeRangeState) (browser.NodeHandle, error) {
	host := realm.host.(browser.DOMElementHost)
	ancestors := make(map[browser.NodeHandle]struct{})
	for cursor := state.start; cursor != (browser.NodeHandle{}); {
		ancestors[cursor] = struct{}{}
		parent, found, err := host.RelatedNode(cursor, browser.RelationParentNode)
		if err != nil {
			return browser.NodeHandle{}, err
		}
		if !found {
			break
		}
		cursor = parent
	}
	for cursor := state.end; cursor != (browser.NodeHandle{}); {
		if _, found := ancestors[cursor]; found {
			return cursor, nil
		}
		parent, found, err := host.RelatedNode(cursor, browser.RelationParentNode)
		if err != nil {
			return browser.NodeHandle{}, err
		}
		if !found {
			break
		}
		cursor = parent
	}
	return browser.NodeHandle{}, fmt.Errorf("nativeengine: Range boundaries do not share a document tree")
}

func (realm *Realm) selectionRange(context *browserruntime.TaskContext, this memory.Value) (memory.Value, bool, error) {
	if !this.IsRef() || this.Ref() != realm.bindings.selection {
		return memory.Value{}, false, fmt.Errorf("%w: invalid Selection receiver", browserruntime.ErrOperandType)
	}
	value, found, err := hiddenProperty(context, this.Ref(), selectionRangeProperty)
	if err != nil || !found {
		return memory.Value{}, false, err
	}
	return value, value.IsRef(), nil
}

func (realm *Realm) setSelectionRange(context *browserruntime.TaskContext, this memory.Value, value memory.Value) error {
	if _, _, err := realm.selectionRange(context, this); err != nil {
		return err
	}
	return setHiddenProperty(context, this.Ref(), selectionRangeProperty, value)
}

func (realm *Realm) selectionState(context *browserruntime.TaskContext, this memory.Value) (nativeRangeState, bool, error) {
	value, present, err := realm.selectionRange(context, this)
	if err != nil || !present {
		return nativeRangeState{}, false, err
	}
	state, err := realm.readRange(context, value)
	return state, err == nil, err
}

func (realm *Realm) selectionAnchorNode(context *browserruntime.TaskContext, this memory.Value, _ []memory.Value) (memory.Value, error) {
	state, present, err := realm.selectionState(context, this)
	if err != nil || !present {
		return memory.NullValue(), err
	}
	return state.startValue, nil
}

func (realm *Realm) selectionAnchorOffset(context *browserruntime.TaskContext, this memory.Value, _ []memory.Value) (memory.Value, error) {
	state, present, err := realm.selectionState(context, this)
	if err != nil || !present {
		return memory.NumberValue(0), err
	}
	return memory.NumberValue(float64(state.startOffset)), nil
}

func (realm *Realm) selectionFocusNode(context *browserruntime.TaskContext, this memory.Value, _ []memory.Value) (memory.Value, error) {
	state, present, err := realm.selectionState(context, this)
	if err != nil || !present {
		return memory.NullValue(), err
	}
	return state.endValue, nil
}

func (realm *Realm) selectionFocusOffset(context *browserruntime.TaskContext, this memory.Value, _ []memory.Value) (memory.Value, error) {
	state, present, err := realm.selectionState(context, this)
	if err != nil || !present {
		return memory.NumberValue(0), err
	}
	return memory.NumberValue(float64(state.endOffset)), nil
}

func (realm *Realm) selectionIsCollapsed(context *browserruntime.TaskContext, this memory.Value, _ []memory.Value) (memory.Value, error) {
	state, present, err := realm.selectionState(context, this)
	return memory.BoolValue(!present || (state.start == state.end && state.startOffset == state.endOffset)), err
}

func (realm *Realm) selectionRangeCount(context *browserruntime.TaskContext, this memory.Value, _ []memory.Value) (memory.Value, error) {
	_, present, err := realm.selectionRange(context, this)
	if present {
		return memory.NumberValue(1), err
	}
	return memory.NumberValue(0), err
}

func (realm *Realm) selectionType(context *browserruntime.TaskContext, this memory.Value, _ []memory.Value) (memory.Value, error) {
	state, present, err := realm.selectionState(context, this)
	if err != nil {
		return memory.Value{}, err
	}
	kind := "None"
	if present {
		kind = "Range"
		if state.start == state.end && state.startOffset == state.endOffset {
			kind = "Caret"
		}
	}
	return newString(context, kind)
}

func (realm *Realm) selectionGetRangeAt(context *browserruntime.TaskContext, this memory.Value, arguments []memory.Value) (memory.Value, error) {
	index, err := integerArgument(arguments, 0)
	if err != nil {
		return memory.Value{}, err
	}
	value, present, err := realm.selectionRange(context, this)
	if err != nil {
		return memory.Value{}, err
	}
	if index != 0 || !present {
		return memory.Value{}, fmt.Errorf("%w: Selection range index is out of bounds", browserruntime.ErrOperandType)
	}
	return value, nil
}

func (realm *Realm) selectionAddRange(context *browserruntime.TaskContext, this memory.Value, arguments []memory.Value) (memory.Value, error) {
	value := argument(arguments, 0)
	if _, err := realm.readRange(context, value); err != nil {
		return memory.Value{}, err
	}
	return memory.UndefinedValue(), realm.setSelectionRange(context, this, value)
}

func (realm *Realm) selectionRemoveAllRanges(context *browserruntime.TaskContext, this memory.Value, _ []memory.Value) (memory.Value, error) {
	return memory.UndefinedValue(), realm.setSelectionRange(context, this, memory.NullValue())
}

func (realm *Realm) selectionCollapse(context *browserruntime.TaskContext, this memory.Value, arguments []memory.Value) (memory.Value, error) {
	node := argument(arguments, 0)
	if node.Kind() == memory.ValueNull || node.Kind() == memory.ValueUndefined {
		return realm.selectionRemoveAllRanges(context, this, nil)
	}
	offset := 0
	var err error
	if len(arguments) > 1 {
		offset, err = integerArgument(arguments, 1)
		if err != nil {
			return memory.Value{}, err
		}
	}
	rangeValue, err := realm.newRangeLocked(context, node, offset, node, offset)
	if err != nil {
		return memory.Value{}, err
	}
	state, err := realm.readRange(context, rangeValue)
	if err != nil {
		return memory.Value{}, err
	}
	if err := realm.writeRangeBoundary(context, state, true, node, offset); err != nil {
		return memory.Value{}, err
	}
	if err := realm.writeRangeBoundary(context, state, false, node, offset); err != nil {
		return memory.Value{}, err
	}
	return memory.UndefinedValue(), realm.setSelectionRange(context, this, rangeValue)
}

func (realm *Realm) selectionCollapseToStart(context *browserruntime.TaskContext, this memory.Value, _ []memory.Value) (memory.Value, error) {
	return realm.selectionCollapseToEdge(context, this, true)
}

func (realm *Realm) selectionCollapseToEnd(context *browserruntime.TaskContext, this memory.Value, _ []memory.Value) (memory.Value, error) {
	return realm.selectionCollapseToEdge(context, this, false)
}

func (realm *Realm) selectionCollapseToEdge(context *browserruntime.TaskContext, this memory.Value, start bool) (memory.Value, error) {
	state, present, err := realm.selectionState(context, this)
	if err != nil || !present {
		if err == nil {
			err = fmt.Errorf("nativeengine: Selection is empty")
		}
		return memory.Value{}, err
	}
	if start {
		err = realm.writeRangeBoundary(context, state, false, state.startValue, state.startOffset)
	} else {
		err = realm.writeRangeBoundary(context, state, true, state.endValue, state.endOffset)
	}
	return memory.UndefinedValue(), err
}

func (realm *Realm) selectionSelectAllChildren(context *browserruntime.TaskContext, this memory.Value, arguments []memory.Value) (memory.Value, error) {
	node := argument(arguments, 0)
	handle, err := realm.unwrapNode(context, node)
	if err != nil {
		return memory.Value{}, err
	}
	limit, err := realm.rangeBoundaryLimit(handle)
	if err != nil {
		return memory.Value{}, err
	}
	rangeValue, err := realm.newRangeLocked(context, node, 0, node, limit)
	if err != nil {
		return memory.Value{}, err
	}
	return memory.UndefinedValue(), realm.setSelectionRange(context, this, rangeValue)
}

func (realm *Realm) selectionDeleteFromDocument(context *browserruntime.TaskContext, this memory.Value, _ []memory.Value) (memory.Value, error) {
	rangeValue, present, err := realm.selectionRange(context, this)
	if err != nil || !present {
		return memory.UndefinedValue(), err
	}
	return realm.rangeDeleteContents(context, rangeValue, nil)
}

func (realm *Realm) selectionToString(context *browserruntime.TaskContext, this memory.Value, _ []memory.Value) (memory.Value, error) {
	rangeValue, present, err := realm.selectionRange(context, this)
	if err != nil {
		return memory.Value{}, err
	}
	if !present {
		return newString(context, "")
	}
	state, err := realm.readRange(context, rangeValue)
	if err != nil {
		return memory.Value{}, err
	}
	host := realm.host.(browser.DOMElementHost)
	fragment, err := host.RangeContents(state.start, state.startOffset, state.end, state.endOffset, dom.RangeCloneContents)
	if err != nil {
		return memory.Value{}, err
	}
	text, err := realm.host.TextContent(fragment)
	if err != nil {
		return memory.Value{}, err
	}
	return newString(context, text)
}
