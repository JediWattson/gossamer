package memory

type PropertyKind uint8

const (
	PropertyData PropertyKind = iota + 1
	PropertyAccessor
)

// Property is one insertion-ordered own property. Name references either a
// String or a Symbol. Data descriptors use values[0]; accessor descriptors use
// values[0] and values[1] for their getter and setter. The tagged union avoids
// reserving three Values when JavaScript descriptors can never use all three
// simultaneously. Descriptor flags remain physical graph state so copy,
// promotion, barriers, and teardown cannot disagree with the language layer.
type Property struct {
	Name         Ref
	values       [2]Value
	Kind         PropertyKind
	Writable     bool
	Enumerable   bool
	Configurable bool
}

func DataProperty(value Value, writable, enumerable, configurable bool) Property {
	return Property{
		values: [2]Value{value}, Kind: PropertyData,
		Writable: writable, Enumerable: enumerable, Configurable: configurable,
	}
}

func AccessorProperty(getter, setter Value, enumerable, configurable bool) Property {
	return Property{
		values: [2]Value{getter, setter}, Kind: PropertyAccessor,
		Enumerable: enumerable, Configurable: configurable,
	}
}

func (property Property) Value() Value {
	if property.Kind != PropertyData {
		return UndefinedValue()
	}
	return property.values[0]
}

func (property Property) Getter() Value {
	if property.Kind != PropertyAccessor {
		return UndefinedValue()
	}
	return property.values[0]
}

func (property Property) Setter() Value {
	if property.Kind != PropertyAccessor {
		return UndefinedValue()
	}
	return property.values[1]
}

// SetValue, SetGetter, and SetSetter mutate a descriptor being prepared by the
// language layer. DefineProperty validates its Kind before storing it.
func (property *Property) SetValue(value Value) {
	if property == nil || property.Kind != PropertyData {
		return
	}
	property.values[0] = value
	property.values[1] = Value{}
}

func (property *Property) SetGetter(getter Value) {
	if property != nil && property.Kind == PropertyAccessor {
		property.values[0] = getter
	}
}

func (property *Property) SetSetter(setter Value) {
	if property != nil && property.Kind == PropertyAccessor {
		property.values[1] = setter
	}
}

// ObjectHeader is the shared JavaScript object identity carried by every
// property-bearing heap payload. Prototype is null or another object-like Ref;
// prototype traversal itself remains a language operation.
type ObjectHeader struct {
	Prototype          Value
	Properties         []Property
	NonExtensible      bool
	ImmutablePrototype bool

	// propertyLookup indexes semantic String contents and Symbol identities.
	// Descriptors themselves remain in Properties so replacement, barriers,
	// generation validation, and insertion order still have one source of truth.
	propertyLookup map[propertyKey]uint32
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
	if slot == nil || slot.payload.pointer == nil {
		return nil, false
	}
	switch slot.Kind {
	case HeapObject:
		return &slot.object().ObjectHeader, true
	case HeapArray:
		return &slot.array().ObjectHeader, true
	case HeapFunction:
		return &slot.function().ObjectHeader, true
	case HeapPromise:
		return &slot.promise().ObjectHeader, true
	case HeapMap:
		return &slot.nativeMap().ObjectHeader, true
	case HeapSet:
		return &slot.nativeSet().ObjectHeader, true
	case HeapError:
		return &slot.errorObject().ObjectHeader, true
	case HeapIterator:
		return &slot.iterator().ObjectHeader, true
	default:
		return nil, false
	}
}

func objectHeaderTypeError(ref Ref, kind HeapKind) error {
	return typeError(ref, kind, HeapObject)
}
