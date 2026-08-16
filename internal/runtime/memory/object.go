package memory

type PropertyKind uint8

const (
	PropertyData PropertyKind = iota + 1
	PropertyAccessor
)

// Property is one insertion-ordered own property. Accessor getter/setter
// values are Undefined or Function Refs. Descriptor flags are physical graph
// state so copy, promotion, barriers, and teardown cannot disagree with the
// language layer.
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

// Object is the named-field heap container. Prototype is either null or an
// Object Ref; prototype traversal itself remains a language operation.
type Object struct {
	Prototype  Value
	Properties []Property
}

func cloneObject(object Object) Object {
	return Object{
		Prototype:  object.Prototype,
		Properties: append([]Property(nil), object.Properties...),
	}
}
