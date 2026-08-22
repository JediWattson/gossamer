package memory

// WeakMapEntry is an ephemeron pair. Key never contributes a strong edge.
// Value becomes strongly reachable during collection only when both the
// containing WeakMap and Key are independently reachable.
type WeakMapEntry struct {
	Key   Ref
	Value Value

	keyUse   uint32
	valueUse uint32
}

type WeakMap struct {
	Entries []WeakMapEntry
}

type WeakSet struct {
	Keys []Ref
	uses []uint32
}

func cloneWeakMap(value WeakMap) WeakMap {
	clone := WeakMap{Entries: make([]WeakMapEntry, len(value.Entries))}
	for index, entry := range value.Entries {
		clone.Entries[index] = WeakMapEntry{Key: entry.Key, Value: entry.Value}
	}
	return clone
}

func cloneWeakSet(value WeakSet) WeakSet {
	return WeakSet{Keys: append([]Ref(nil), value.Keys...)}
}

type weakUseRole uint8

const (
	weakMapKeyUse weakUseRole = iota + 1
	weakMapValueUse
	weakSetKeyUse
)

// weakUse is the exact reverse location of one non-retaining reference. The
// referenced table entry stores its index in Store.weakTargets[target], so
// removing a target never has to scan unrelated weak collections.
type weakUse struct {
	table Ref
	entry uint32
	role  weakUseRole
}
