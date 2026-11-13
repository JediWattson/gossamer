package memory

// Binding is one insertion-ordered lexical slot. An uninitialized binding is
// distinct from a binding initialized to UndefinedValue.
type Binding struct {
	Name        Ref
	Value       Value
	Mutable     bool
	Initialized bool
	Indirect    bool
	Target      Ref
	TargetName  Ref
}

// Context is a native lexical environment. Parent is null or another Context
// Ref; resolution walks this explicit chain.
type Context struct {
	Parent   Value
	Bindings []Binding

	// bindingLookup is a degree-adaptive acceleration structure. It records
	// semantic String contents to insertion-ordered Binding indexes, never
	// Values or Refs, so mutations and generation checks remain authoritative.
	bindingLookup map[string]uint32
}

// BindingLocation is a generation-safe resolved lexical slot. It borrows the
// Context Ref and does not retain or otherwise change ownership.
type BindingLocation struct {
	Context Ref
	Index   uint32
}

func cloneContext(context Context) Context {
	return Context{Parent: context.Parent, Bindings: append([]Binding(nil), context.Bindings...)}
}
