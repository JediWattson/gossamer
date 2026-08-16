package memory

// IteratorKind identifies one deterministic native iteration order.
type IteratorKind uint8

const (
	IteratorArrayKeys IteratorKind = iota + 1
	IteratorArrayValues
	IteratorArrayEntries
	IteratorStringValues
	IteratorMapKeys
	IteratorMapValues
	IteratorMapEntries
	IteratorSetValues
	IteratorSetEntries
)

// Iterator retains its target and current logical position in RegionStore.
// It contains no Go callback or pointer identity.
type Iterator struct {
	ObjectHeader
	Target Ref
	Kind   IteratorKind
	Next   uint32
}

func cloneIterator(iterator Iterator) Iterator {
	return Iterator{
		ObjectHeader: cloneObjectHeader(iterator.ObjectHeader),
		Target:       iterator.Target,
		Kind:         iterator.Kind,
		Next:         iterator.Next,
	}
}

// IteratorStep is the physical result of one advance. Entry iterators return
// Key and Value with Pair set. String iterators return Text with Textual set.
type IteratorStep struct {
	Done    bool
	Pair    bool
	Textual bool
	Key     Value
	Value   Value
	Text    string
}
