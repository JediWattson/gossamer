package memory

// MapEntry is one insertion-ordered SameValueZero key/value pair.
type MapEntry struct {
	Key   Value
	Value Value
}

type Map struct {
	ObjectHeader
	Entries []MapEntry
}

func cloneMap(value Map) Map {
	return Map{ObjectHeader: cloneObjectHeader(value.ObjectHeader), Entries: append([]MapEntry(nil), value.Entries...)}
}
