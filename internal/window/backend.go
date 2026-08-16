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
	EventTextInput
	EventCompositionStart
	EventCompositionUpdate
	EventCompositionEnd
)

type Modifiers struct {
	Alt   bool
	Ctrl  bool
	Meta  bool
	Shift bool
}

// Event is a backend-neutral native input record. Coordinates and scroll
// deltas are CSS pixels in a top-left coordinate system. Insertable Unicode
// text arrives separately from physical key events so IME composition can be
// represented without pretending every edit is one key-down.
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
	Composing bool
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

// ClipboardBackend is an optional native capability. Clipboard strings cross
// the backend boundary by value; DOM selection and edits remain Page-owned.
type ClipboardBackend interface {
	ReadClipboardText() (string, error)
	WriteClipboardText(string) error
}

// TitleBackend is an optional native capability for keeping the operating
// system window title synchronized with the active browser tab.
type TitleBackend interface {
	SetTitle(string) error
}

type Cursor uint8

const (
	CursorDefault Cursor = iota
	CursorPointer
	CursorText
	CursorResizeHorizontal
	CursorResizeVertical
)

// CursorBackend is an optional native capability. Cursor selection remains a
// Go shell decision derived from copied geometry and stable NodeHandles.
type CursorBackend interface {
	SetCursor(Cursor) error
}

type ContextAction string

const (
	ContextActionNone     ContextAction = ""
	ContextActionBack     ContextAction = "back"
	ContextActionForward  ContextAction = "forward"
	ContextActionReload   ContextAction = "reload"
	ContextActionOpenLink ContextAction = "open-link-new-tab"
	ContextActionCopyLink ContextAction = "copy-link"
	ContextActionDownload ContextAction = "download"
)

type ContextMenuItem struct {
	Action  ContextAction
	Label   string
	Enabled bool
}

type ContextMenu struct {
	X, Y  float64
	Items []ContextMenuItem
}

// ContextMenuBackend optionally presents a native menu and returns only the
// selected action token. The target NodeHandle and action execution remain in
// the Go browser shell.
type ContextMenuBackend interface {
	ShowContextMenu(ContextMenu) (ContextAction, error)
}
