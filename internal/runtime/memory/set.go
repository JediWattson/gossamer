package memory

type Set struct {
	Values []Value
}

func cloneSet(set Set) Set {
	return Set{Values: append([]Value(nil), set.Values...)}
}
