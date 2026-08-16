package memory

// ArrayElement is one present entry in a sparse native Array. Missing indices
// are holes, distinct from a present UndefinedValue.
type ArrayElement struct {
	Index uint32
	Value Value
}

// Array owns a JavaScript-shaped uint32 length and sorted sparse elements.
// Index math and named properties remain higher-level language semantics.
type Array struct {
	ObjectHeader
	Length   uint32
	Elements []ArrayElement
}

func cloneArray(array Array) Array {
	return Array{
		ObjectHeader: cloneObjectHeader(array.ObjectHeader),
		Length:       array.Length,
		Elements:     append([]ArrayElement(nil), array.Elements...),
	}
}
