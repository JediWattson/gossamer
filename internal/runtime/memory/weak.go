package memory

// WeakMapEntry is an ephemeron pair. Key never contributes a strong edge.
// Value becomes strongly reachable during collection only when both the
// containing WeakMap and Key are independently reachable.
type WeakMapEntry struct {
	Key   Ref
	Value Value
}

type WeakMap struct {
	Entries []WeakMapEntry
}

type WeakSet struct {
	Keys []Ref
}

func cloneWeakMap(value WeakMap) WeakMap {
	return WeakMap{Entries: append([]WeakMapEntry(nil), value.Entries...)}
}

func cloneWeakSet(value WeakSet) WeakSet {
	return WeakSet{Keys: append([]Ref(nil), value.Keys...)}
}
