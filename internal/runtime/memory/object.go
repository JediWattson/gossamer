package memory

type PropertyKind uint8

const (
	PropertyData PropertyKind = iota + 1
	PropertyAccessor
)

// Property is one insertion-ordered own property. Name references either a
// String or a Symbol. Accessor getter/setter values are Undefined or Function
// Refs. Descriptor flags are physical graph state so copy, promotion,
// barriers, and teardown cannot disagree with the language layer.
type Property struct {
	Name         Ref
	Kind         PropertyKind
	Value        Value
	Getter       Value
	Setter       Value
	Writable     bool
	Enumerable   bool
	Configurable bool
}

func DataProperty(value Value, writable, enumerable, configurable bool) Property {
	return Property{Kind: PropertyData, Value: value, Writable: writable, Enumerable: enumerable, Configurable: configurable}
}

func AccessorProperty(getter, setter Value, enumerable, configurable bool) Property {
	return Property{Kind: PropertyAccessor, Getter: getter, Setter: setter, Enumerable: enumerable, Configurable: configurable}
}

// ObjectHeader is the shared JavaScript object identity carried by every
// property-bearing heap payload. Prototype is null or another object-like Ref;
// prototype traversal itself remains a language operation.
type ObjectHeader struct {
	Prototype          Value
	Properties         []Property
	NonExtensible      bool
	ImmutablePrototype bool
}

func cloneObjectHeader(header ObjectHeader) ObjectHeader {
	return ObjectHeader{
		Prototype:          header.Prototype,
		Properties:         append([]Property(nil), header.Properties...),
		NonExtensible:      header.NonExtensible,
		ImmutablePrototype: header.ImmutablePrototype,
	}
}

// Object is the ordinary named-field heap container.
type Object struct {
	ObjectHeader
}

func cloneObject(object Object) Object {
	return Object{ObjectHeader: cloneObjectHeader(object.ObjectHeader)}
}

func objectHeaderForSlot(slot *Slot) (*ObjectHeader, bool) {
	if slot == nil {
		return nil, false
	}
	switch slot.Kind {
	case HeapObject:
		return &slot.Object.ObjectHeader, true
	case HeapArray:
		return &slot.Array.ObjectHeader, true
	case HeapFunction:
		return &slot.Function.ObjectHeader, true
	case HeapPromise:
		return &slot.Promise.ObjectHeader, true
	case HeapMap:
		return &slot.Map.ObjectHeader, true
	case HeapSet:
		return &slot.Set.ObjectHeader, true
	case HeapError:
		return &slot.Error.ObjectHeader, true
	case HeapIterator:
		return &slot.Iterator.ObjectHeader, true
	default:
		return nil, false
	}
}

func objectHeaderTypeError(ref Ref, kind HeapKind) error {
	return typeError(ref, kind, HeapObject)
}
