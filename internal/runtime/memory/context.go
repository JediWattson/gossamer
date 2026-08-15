package memory

// Binding is one insertion-ordered lexical slot. An uninitialized binding is
// distinct from a binding initialized to UndefinedValue.
type Binding struct {
	Name        Ref
	Value       Value
	Mutable     bool
	Initialized bool
}

// Context is a native lexical environment. Parent is null or another Context
// Ref; resolution walks this explicit chain.
type Context struct {
	Parent   Value
	Bindings []Binding
}

func cloneContext(context Context) Context {
	return Context{Parent: context.Parent, Bindings: append([]Binding(nil), context.Bindings...)}
}
