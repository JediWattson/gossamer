package browser

import "time"

// ValueHandle is opaque engine-owned identity for a JavaScript value or
// callback. Browser code may transport it but never inspect engine storage.
type ValueHandle uint64

type InputEventType uint8

const (
	InputClick InputEventType = iota + 1
)

// InputEvent contains browser-normalized input and a generation-safe target.
type InputEvent struct {
	Type   InputEventType
	Target NodeHandle
	X      float64
	Y      float64
	Button int
}

// ScriptSource is an engine-neutral unit of source evaluation. URL is the
// resolved source identity used for diagnostics; Source contains UTF-8 code.
type ScriptSource struct {
	URL    string
	Source string
}

// Engine is the engine-neutral factory boundary. A future V8 adapter owns its
// isolates and creates one JSRealm per browser Page through this interface.
type Engine interface {
	NewRealm() (JSRealm, error)
	Close() error
}

// JSRealm is the only script surface called by Page tasks. It receives an
// execution-scoped Host and may not retain it after the call returns.
type JSRealm interface {
	Evaluate(Host, ScriptSource) error
	DispatchEvent(Host, InputEvent) error
	Invoke(Host, ValueHandle) error
	DrainMicrotasks(Host) error
	Close() error
}

// Host is the execution-scoped browser API visible to an engine. Methods that
// schedule work preserve the current task as the publication boundary.
type Host interface {
	Text(NodeHandle) (string, error)
	SetText(NodeHandle, string) error
	QueueCallback(ValueHandle) error
	QueueMicrotask(ValueHandle) error
	SetTimeout(ValueHandle, time.Duration) (TimerID, error)
	ClearTimeout(TimerID) error
}
