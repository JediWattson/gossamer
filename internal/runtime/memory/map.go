package memory

// MapEntry is one insertion-ordered SameValueZero key/value pair.
type MapEntry struct {
	Key   Value
	Value Value
}

type Map struct {
	Entries []MapEntry
}

func cloneMap(value Map) Map {
	return Map{Entries: append([]MapEntry(nil), value.Entries...)}
}
