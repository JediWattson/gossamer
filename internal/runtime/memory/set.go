package memory

type Set struct {
	ObjectHeader
	Values []Value
}

func cloneSet(set Set) Set {
	return Set{ObjectHeader: cloneObjectHeader(set.ObjectHeader), Values: append([]Value(nil), set.Values...)}
}
