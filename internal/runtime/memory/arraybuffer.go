package memory

// ArrayBuffer owns a mutable byte sequence until explicitly detached.
type ArrayBuffer struct {
	Bytes    []byte
	Detached bool
}

func cloneArrayBuffer(buffer ArrayBuffer) ArrayBuffer {
	return ArrayBuffer{Bytes: append([]byte(nil), buffer.Bytes...), Detached: buffer.Detached}
}
