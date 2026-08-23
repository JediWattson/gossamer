package nativeengine

import (
	"fmt"
	"math"

	"github.com/JediWattson/gossamer/internal/browser"
	browserruntime "github.com/JediWattson/gossamer/internal/runtime"
	"github.com/JediWattson/gossamer/internal/runtime/memory"
)

const (
	nativeDocumentCreateTreeWalker uint64 = 25_000 + iota
	nativeTreeWalkerRoot
	nativeTreeWalkerWhatToShow
	nativeTreeWalkerFilter
	nativeTreeWalkerCurrentNodeGet
	nativeTreeWalkerCurrentNodeSet
	nativeTreeWalkerNextNode
	nativeDocumentCreateNodeIterator
	nativeNodeIteratorRoot
	nativeNodeIteratorWhatToShow
	nativeNodeIteratorFilter
	nativeNodeIteratorReferenceNode
	nativeNodeIteratorPointerBeforeReferenceNode
	nativeNodeIteratorNextNode
	nativeNodeIteratorDetach
)

const (
	treeWalkerBrandProperty        = "\x00gossamer.tree-walker.brand"
	treeWalkerRootProperty         = "\x00gossamer.tree-walker.root"
	treeWalkerCurrentProperty      = "\x00gossamer.tree-walker.current"
	treeWalkerWhatToShowProperty   = "\x00gossamer.tree-walker.what-to-show"
	treeWalkerFilterProperty       = "\x00gossamer.tree-walker.filter"
	nodeIteratorBrandProperty      = "\x00gossamer.node-iterator.brand"
	nodeIteratorRootProperty       = "\x00gossamer.node-iterator.root"
	nodeIteratorReferenceProperty  = "\x00gossamer.node-iterator.reference"
	nodeIteratorBeforeProperty     = "\x00gossamer.node-iterator.before"
	nodeIteratorWhatToShowProperty = "\x00gossamer.node-iterator.what-to-show"
	nodeIteratorFilterProperty     = "\x00gossamer.node-iterator.filter"
)

const nodeFilterShowAll = uint32(math.MaxUint32)

func newNodeFilter(context *browserruntime.TaskContext) (memory.Ref, error) {
	object, err := context.NewHeapObject()
	if err != nil {
		return memory.Ref{}, err
	}
	constants := []struct {
		name  string
		value uint32
	}{
		{"FILTER_ACCEPT", 1}, {"FILTER_REJECT", 2}, {"FILTER_SKIP", 3},
		{"SHOW_ALL", nodeFilterShowAll}, {"SHOW_ELEMENT", 1}, {"SHOW_ATTRIBUTE", 2},
		{"SHOW_TEXT", 4}, {"SHOW_CDATA_SECTION", 8}, {"SHOW_ENTITY_REFERENCE", 16},
		{"SHOW_ENTITY", 32}, {"SHOW_PROCESSING_INSTRUCTION", 64}, {"SHOW_COMMENT", 128},
		{"SHOW_DOCUMENT", 256}, {"SHOW_DOCUMENT_TYPE", 512}, {"SHOW_DOCUMENT_FRAGMENT", 1024},
		{"SHOW_NOTATION", 2048},
	}
	for _, constant := range constants {
		if err := defineData(context, object, constant.name, memory.NumberValue(float64(constant.value)), false, true, false); err != nil {
			return memory.Ref{}, err
		}
	}
	return object, nil
}

func (realm *Realm) documentCreateTreeWalker(context *browserruntime.TaskContext, _ memory.Value, arguments []memory.Value) (memory.Value, error) {
	root := argument(arguments, 0)
	if _, err := realm.unwrapNode(context, root); err != nil {
		return memory.Value{}, err
	}
	whatToShow, err := traversalWhatToShow(argument(arguments, 1))
	if err != nil {
		return memory.Value{}, err
	}
	filter := argument(arguments, 2)
	if filter.Kind() == memory.ValueUndefined {
		filter = memory.NullValue()
	}
	walker, err := context.NewHeapObject()
	if err != nil {
		return memory.Value{}, err
	}
	if err := context.SetPrototype(walker, memory.RefValue(realm.bindings.treeWalkerPrototype)); err != nil {
		return memory.Value{}, err
	}
	for _, property := range []struct {
		name  string
		value memory.Value
	}{
		{treeWalkerBrandProperty, memory.BoolValue(true)},
		{treeWalkerRootProperty, root},
		{treeWalkerCurrentProperty, root},
		{treeWalkerWhatToShowProperty, memory.NumberValue(float64(whatToShow))},
		{treeWalkerFilterProperty, filter},
	} {
		if err := defineData(context, walker, property.name, property.value, property.name == treeWalkerCurrentProperty, false, false); err != nil {
			return memory.Value{}, err
		}
	}
	return memory.RefValue(walker), nil
}

func (realm *Realm) documentCreateNodeIterator(context *browserruntime.TaskContext, _ memory.Value, arguments []memory.Value) (memory.Value, error) {
	root := argument(arguments, 0)
	if _, err := realm.unwrapNode(context, root); err != nil {
		return memory.Value{}, err
	}
	whatToShow, err := traversalWhatToShow(argument(arguments, 1))
	if err != nil {
		return memory.Value{}, err
	}
	filter := argument(arguments, 2)
	if filter.Kind() == memory.ValueUndefined {
		filter = memory.NullValue()
	}
	iterator, err := context.NewHeapObject()
	if err != nil {
		return memory.Value{}, err
	}
	if err := context.SetPrototype(iterator, memory.RefValue(realm.bindings.nodeIteratorPrototype)); err != nil {
		return memory.Value{}, err
	}
	for _, property := range []struct {
		name  string
		value memory.Value
	}{
		{nodeIteratorBrandProperty, memory.BoolValue(true)},
		{nodeIteratorRootProperty, root},
		{nodeIteratorReferenceProperty, root},
		{nodeIteratorBeforeProperty, memory.BoolValue(true)},
		{nodeIteratorWhatToShowProperty, memory.NumberValue(float64(whatToShow))},
		{nodeIteratorFilterProperty, filter},
	} {
		writable := property.name == nodeIteratorReferenceProperty || property.name == nodeIteratorBeforeProperty
		if err := defineData(context, iterator, property.name, property.value, writable, false, false); err != nil {
			return memory.Value{}, err
		}
	}
	return memory.RefValue(iterator), nil
}

func traversalWhatToShow(value memory.Value) (uint32, error) {
	if value.Kind() == memory.ValueUndefined {
		return nodeFilterShowAll, nil
	}
	if value.Kind() != memory.ValueNumber || math.IsNaN(value.Number()) || math.IsInf(value.Number(), 0) {
		return 0, fmt.Errorf("%w: traversal whatToShow must be a finite number", browserruntime.ErrOperandType)
	}
	return uint32(int64(math.Trunc(value.Number()))), nil
}

func (realm *Realm) treeWalkerRoot(context *browserruntime.TaskContext, this memory.Value, _ []memory.Value) (memory.Value, error) {
	state, err := readTreeWalker(context, this)
	return state.root, err
}

func (realm *Realm) treeWalkerWhatToShow(context *browserruntime.TaskContext, this memory.Value, _ []memory.Value) (memory.Value, error) {
	state, err := readTreeWalker(context, this)
	return memory.NumberValue(float64(state.whatToShow)), err
}

func (realm *Realm) treeWalkerFilter(context *browserruntime.TaskContext, this memory.Value, _ []memory.Value) (memory.Value, error) {
	state, err := readTreeWalker(context, this)
	return state.filter, err
}

func (realm *Realm) treeWalkerCurrentNodeGet(context *browserruntime.TaskContext, this memory.Value, _ []memory.Value) (memory.Value, error) {
	state, err := readTreeWalker(context, this)
	return state.current, err
}

func (realm *Realm) treeWalkerCurrentNodeSet(context *browserruntime.TaskContext, this memory.Value, arguments []memory.Value) (memory.Value, error) {
	state, err := readTreeWalker(context, this)
	if err != nil {
		return memory.Value{}, err
	}
	next := argument(arguments, 0)
	rootHandle, err := realm.unwrapNode(context, state.root)
	if err != nil {
		return memory.Value{}, err
	}
	nextHandle, err := realm.unwrapNode(context, next)
	if err != nil {
		return memory.Value{}, err
	}
	if rootHandle.Document != nextHandle.Document {
		return memory.Value{}, fmt.Errorf("%w: TreeWalker currentNode belongs to another document", browserruntime.ErrOperandType)
	}
	if err := setHiddenProperty(context, this.Ref(), treeWalkerCurrentProperty, next); err != nil {
		return memory.Value{}, err
	}
	return memory.UndefinedValue(), nil
}

func (realm *Realm) treeWalkerNextNode(context *browserruntime.TaskContext, this memory.Value, _ []memory.Value) (memory.Value, error) {
	state, err := readTreeWalker(context, this)
	if err != nil {
		return memory.Value{}, err
	}
	root, err := realm.unwrapNode(context, state.root)
	if err != nil {
		return memory.Value{}, err
	}
	current, err := realm.unwrapNode(context, state.current)
	if err != nil {
		return memory.Value{}, err
	}
	host, ok := realm.host.(browser.DOMElementHost)
	if !ok {
		return memory.Value{}, fmt.Errorf("nativeengine: browser host does not expose tree traversal")
	}
	for steps := 0; steps < 1_000_000; steps++ {
		candidate, found, err := nextTreeWalkerCandidate(host, root, current)
		if err != nil {
			return memory.Value{}, err
		}
		if !found {
			return memory.NullValue(), nil
		}
		current = candidate
		metadata, err := host.NodeMetadata(candidate)
		if err != nil {
			return memory.Value{}, err
		}
		if state.whatToShow&nodeFilterMask(metadata.Type) == 0 {
			continue
		}
		value, err := realm.wrappedNodeValue(context, candidate)
		if err != nil {
			return memory.Value{}, err
		}
		accepted, err := realm.treeWalkerAcceptNode(context, state.filter, value)
		if err != nil {
			return memory.Value{}, err
		}
		if !accepted {
			continue
		}
		if err := setHiddenProperty(context, this.Ref(), treeWalkerCurrentProperty, value); err != nil {
			return memory.Value{}, err
		}
		return value, nil
	}
	return memory.Value{}, fmt.Errorf("nativeengine: TreeWalker traversal limit exceeded")
}

func (realm *Realm) nodeIteratorRoot(context *browserruntime.TaskContext, this memory.Value, _ []memory.Value) (memory.Value, error) {
	state, err := readNodeIterator(context, this)
	return state.root, err
}

func (realm *Realm) nodeIteratorWhatToShow(context *browserruntime.TaskContext, this memory.Value, _ []memory.Value) (memory.Value, error) {
	state, err := readNodeIterator(context, this)
	return memory.NumberValue(float64(state.whatToShow)), err
}

func (realm *Realm) nodeIteratorFilter(context *browserruntime.TaskContext, this memory.Value, _ []memory.Value) (memory.Value, error) {
	state, err := readNodeIterator(context, this)
	return state.filter, err
}

func (realm *Realm) nodeIteratorReferenceNode(context *browserruntime.TaskContext, this memory.Value, _ []memory.Value) (memory.Value, error) {
	state, err := readNodeIterator(context, this)
	return state.reference, err
}

func (realm *Realm) nodeIteratorPointerBeforeReferenceNode(context *browserruntime.TaskContext, this memory.Value, _ []memory.Value) (memory.Value, error) {
	state, err := readNodeIterator(context, this)
	return memory.BoolValue(state.before), err
}

func (realm *Realm) nodeIteratorNextNode(context *browserruntime.TaskContext, this memory.Value, _ []memory.Value) (memory.Value, error) {
	state, err := readNodeIterator(context, this)
	if err != nil {
		return memory.Value{}, err
	}
	root, err := realm.unwrapNode(context, state.root)
	if err != nil {
		return memory.Value{}, err
	}
	reference, err := realm.unwrapNode(context, state.reference)
	if err != nil {
		return memory.Value{}, err
	}
	host, ok := realm.host.(browser.DOMElementHost)
	if !ok {
		return memory.Value{}, fmt.Errorf("nativeengine: browser host does not expose node iteration")
	}
	for steps := 0; steps < 1_000_000; steps++ {
		candidate := reference
		found := state.before
		if !found {
			candidate, found, err = nextTreeWalkerCandidate(host, root, reference)
			if err != nil {
				return memory.Value{}, err
			}
		}
		if !found {
			return memory.NullValue(), nil
		}
		state.before = false
		reference = candidate
		value, err := realm.wrappedNodeValue(context, candidate)
		if err != nil {
			return memory.Value{}, err
		}
		if err := setHiddenProperty(context, this.Ref(), nodeIteratorReferenceProperty, value); err != nil {
			return memory.Value{}, err
		}
		if err := setHiddenProperty(context, this.Ref(), nodeIteratorBeforeProperty, memory.BoolValue(false)); err != nil {
			return memory.Value{}, err
		}
		metadata, err := host.NodeMetadata(candidate)
		if err != nil {
			return memory.Value{}, err
		}
		if state.whatToShow&nodeFilterMask(metadata.Type) == 0 {
			continue
		}
		accepted, err := realm.treeWalkerAcceptNode(context, state.filter, value)
		if err != nil {
			return memory.Value{}, err
		}
		if accepted {
			return value, nil
		}
	}
	return memory.Value{}, fmt.Errorf("nativeengine: NodeIterator traversal limit exceeded")
}

func (realm *Realm) nodeIteratorDetach(context *browserruntime.TaskContext, this memory.Value, _ []memory.Value) (memory.Value, error) {
	if _, err := readNodeIterator(context, this); err != nil {
		return memory.Value{}, err
	}
	return memory.UndefinedValue(), nil
}

type treeWalkerState struct {
	root       memory.Value
	current    memory.Value
	filter     memory.Value
	whatToShow uint32
}

type nodeIteratorState struct {
	root       memory.Value
	reference  memory.Value
	filter     memory.Value
	whatToShow uint32
	before     bool
}

func readTreeWalker(context *browserruntime.TaskContext, this memory.Value) (treeWalkerState, error) {
	if !this.IsRef() {
		return treeWalkerState{}, fmt.Errorf("%w: invalid TreeWalker receiver", browserruntime.ErrOperandType)
	}
	brand, found, err := hiddenProperty(context, this.Ref(), treeWalkerBrandProperty)
	if err != nil || !found || brand.Kind() != memory.ValueBool || !brand.Bool() {
		return treeWalkerState{}, fmt.Errorf("%w: invalid TreeWalker receiver", browserruntime.ErrOperandType)
	}
	state := treeWalkerState{}
	for _, property := range []struct {
		name        string
		destination *memory.Value
	}{
		{treeWalkerRootProperty, &state.root},
		{treeWalkerCurrentProperty, &state.current},
		{treeWalkerFilterProperty, &state.filter},
	} {
		value, found, err := hiddenProperty(context, this.Ref(), property.name)
		if err != nil || !found {
			return treeWalkerState{}, fmt.Errorf("%w: invalid TreeWalker state", browserruntime.ErrOperandType)
		}
		*property.destination = value
	}
	value, found, err := hiddenProperty(context, this.Ref(), treeWalkerWhatToShowProperty)
	if err != nil || !found || value.Kind() != memory.ValueNumber {
		return treeWalkerState{}, fmt.Errorf("%w: invalid TreeWalker state", browserruntime.ErrOperandType)
	}
	state.whatToShow = uint32(value.Number())
	return state, nil
}

func readNodeIterator(context *browserruntime.TaskContext, this memory.Value) (nodeIteratorState, error) {
	if !this.IsRef() {
		return nodeIteratorState{}, fmt.Errorf("%w: invalid NodeIterator receiver", browserruntime.ErrOperandType)
	}
	brand, found, err := hiddenProperty(context, this.Ref(), nodeIteratorBrandProperty)
	if err != nil || !found || brand.Kind() != memory.ValueBool || !brand.Bool() {
		return nodeIteratorState{}, fmt.Errorf("%w: invalid NodeIterator receiver", browserruntime.ErrOperandType)
	}
	state := nodeIteratorState{}
	for _, property := range []struct {
		name        string
		destination *memory.Value
	}{
		{nodeIteratorRootProperty, &state.root},
		{nodeIteratorReferenceProperty, &state.reference},
		{nodeIteratorFilterProperty, &state.filter},
	} {
		value, found, err := hiddenProperty(context, this.Ref(), property.name)
		if err != nil || !found {
			return nodeIteratorState{}, fmt.Errorf("%w: invalid NodeIterator state", browserruntime.ErrOperandType)
		}
		*property.destination = value
	}
	show, found, err := hiddenProperty(context, this.Ref(), nodeIteratorWhatToShowProperty)
	if err != nil || !found || show.Kind() != memory.ValueNumber {
		return nodeIteratorState{}, fmt.Errorf("%w: invalid NodeIterator state", browserruntime.ErrOperandType)
	}
	before, found, err := hiddenProperty(context, this.Ref(), nodeIteratorBeforeProperty)
	if err != nil || !found || before.Kind() != memory.ValueBool {
		return nodeIteratorState{}, fmt.Errorf("%w: invalid NodeIterator state", browserruntime.ErrOperandType)
	}
	state.whatToShow = uint32(show.Number())
	state.before = before.Bool()
	return state, nil
}

func nextTreeWalkerCandidate(host browser.DOMElementHost, root, current browser.NodeHandle) (browser.NodeHandle, bool, error) {
	children, err := host.ChildNodes(current, false)
	if err != nil {
		return browser.NodeHandle{}, false, err
	}
	if len(children) != 0 {
		return children[0], true, nil
	}
	for current != root {
		if sibling, found, err := host.RelatedNode(current, browser.RelationNextSibling); err != nil {
			return browser.NodeHandle{}, false, err
		} else if found {
			return sibling, true, nil
		}
		parent, found, err := host.RelatedNode(current, browser.RelationParentNode)
		if err != nil {
			return browser.NodeHandle{}, false, err
		}
		if !found {
			return browser.NodeHandle{}, false, nil
		}
		current = parent
	}
	return browser.NodeHandle{}, false, nil
}

func nodeFilterMask(nodeType browser.DOMNodeType) uint32 {
	switch nodeType {
	case browser.DOMElementNode:
		return 1
	case browser.DOMTextNode:
		return 4
	case browser.DOMProcessingInstructionNode:
		return 64
	case browser.DOMCommentNode:
		return 128
	case browser.DOMDocumentNode:
		return 256
	case browser.DOMDocumentTypeNode:
		return 512
	case browser.DOMDocumentFragmentNode:
		return 1024
	default:
		return 0
	}
}

func (realm *Realm) treeWalkerAcceptNode(context *browserruntime.TaskContext, filter, node memory.Value) (bool, error) {
	if filter.Kind() == memory.ValueNull || filter.Kind() == memory.ValueUndefined {
		return true, nil
	}
	if !filter.IsRef() {
		return false, fmt.Errorf("%w: TreeWalker filter is not callable", browserruntime.ErrOperandType)
	}
	kind, err := context.HeapKind(filter.Ref())
	if err != nil {
		return false, err
	}
	if kind != memory.HeapFunction {
		return false, fmt.Errorf("%w: TreeWalker filter is not callable", browserruntime.ErrOperandType)
	}
	result, err := realm.interpreter.CallWithoutCheckpoint(context, filter.Ref(), memory.UndefinedValue(), node)
	if err != nil {
		return false, err
	}
	return result.Kind() == memory.ValueNumber && result.Number() == 1, nil
}
