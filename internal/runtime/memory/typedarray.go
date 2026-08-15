package memory

// ElementKind identifies numeric TypedArray element formats. BigInt views and
// DataView are separate later types because their coercion APIs differ.
type ElementKind uint8

const (
	ElementInt8 ElementKind = iota + 1
	ElementUint8
	ElementUint8Clamped
	ElementInt16
	ElementUint16
	ElementInt32
	ElementUint32
	ElementFloat32
	ElementFloat64
)

// TypedArray is an immutable view descriptor over a mutable ArrayBuffer.
type TypedArray struct {
	Buffer     Ref
	Element    ElementKind
	ByteOffset uint64
	Length     uint64
}

func cloneTypedArray(view TypedArray) TypedArray { return view }

func elementSize(kind ElementKind) (uint64, bool) {
	switch kind {
	case ElementInt8, ElementUint8, ElementUint8Clamped:
		return 1, true
	case ElementInt16, ElementUint16:
		return 2, true
	case ElementInt32, ElementUint32, ElementFloat32:
		return 4, true
	case ElementFloat64:
		return 8, true
	default:
		return 0, false
	}
}
