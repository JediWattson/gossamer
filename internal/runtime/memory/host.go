package memory

// HostClass identifies one embedder-defined native facade family without
// coupling RegionStore to the browser package.
type HostClass uint32

// HostObject is an immutable native identity record. Scope is the embedder's
// lifetime generation and Identity is stable only within that scope.
type HostObject struct {
	Class    HostClass
	Scope    uint64
	Identity uint64
}
