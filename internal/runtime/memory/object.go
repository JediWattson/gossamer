package memory

// Property is one insertion-ordered own property. Name must reference a native
// String; Value may hold an inline scalar or any heap Ref.
type Property struct {
	Name  Ref
	Value Value
}

// Object is the first named-field heap container. Prototype is either null or
// an Object Ref. Attribute flags and prototype-chain lookup are later semantic
// layers; RegionStore owns only the physical graph.
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
