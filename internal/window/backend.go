// Package window connects a browser Page to an interactive presentation and
// input backend without giving the backend ownership of DOM or JavaScript
// state. Backends exchange copied pixels and normalized value-only events;
// every browser mutation still crosses the Page's ordered Realm queue.
package window

import (
	"context"
	"image"
)

type EventKind uint8

const (
	EventNone EventKind = iota
	EventClose
	EventResize
	EventPointerMove
	EventPointerDown
	EventPointerUp
	EventScroll
	EventKeyDown
	EventKeyUp
	EventFocus
	EventBlur
)

type Modifiers struct {
	Alt   bool
	Ctrl  bool
	Meta  bool
	Shift bool
}

// Event is a backend-neutral native input record. Coordinates and scroll
// deltas are CSS pixels in a top-left coordinate system. Text accompanies a
// key-down when the platform produced insertable Unicode text.
type Event struct {
	Kind      EventKind
	Width     int
	Height    int
	X         float64
	Y         float64
	DeltaX    float64
	DeltaY    float64
	Button    int
	Buttons   uint
	Key       string
	Code      string
	Text      string
	Repeat    bool
	Modifiers Modifiers
}

type Config struct {
	Title  string
	Width  int
	Height int
}

// Backend owns only the platform window and its copied front buffer. Open,
// NextEvent, Present, and Close are called serially on the Run caller's OS
// thread so native backends can satisfy UI-thread requirements.
type Backend interface {
	Open(Config) error
	NextEvent(context.Context) (Event, error)
	Present(*image.RGBA) error
	Close() error
}
