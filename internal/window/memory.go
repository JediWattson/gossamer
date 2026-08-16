package window

import (
	"context"
	"fmt"
	"image"
	"sync"
)

// MemoryBackend is a deterministic backend used to prove the complete window
// loop without depending on a display server. Presented images are cloned so
// tests observe the same ownership boundary as a native front buffer.
type MemoryBackend struct {
	mutex          sync.Mutex
	events         []Event
	frames         []*image.RGBA
	config         Config
	titles         []string
	cursors        []Cursor
	menus          []ContextMenu
	contextActions []ContextAction
	accessibility  []AccessibilitySnapshot
	clipboard      string
	open           bool
	closed         bool
}

func (backend *MemoryBackend) UpdateAccessibility(snapshot AccessibilitySnapshot) error {
	if backend == nil {
		return fmt.Errorf("window: nil memory backend")
	}
	backend.mutex.Lock()
	defer backend.mutex.Unlock()
	if !backend.open || backend.closed {
		return fmt.Errorf("window: memory backend is not open")
	}
	copySnapshot := snapshot
	copySnapshot.Nodes = append([]AccessibilityNode(nil), snapshot.Nodes...)
	backend.accessibility = append(backend.accessibility, copySnapshot)
	return nil
}

func (backend *MemoryBackend) AccessibilitySnapshots() []AccessibilitySnapshot {
	if backend == nil {
		return nil
	}
	backend.mutex.Lock()
	defer backend.mutex.Unlock()
	result := make([]AccessibilitySnapshot, len(backend.accessibility))
	for index, snapshot := range backend.accessibility {
		result[index] = snapshot
		result[index].Nodes = append([]AccessibilityNode(nil), snapshot.Nodes...)
	}
	return result
}

func (backend *MemoryBackend) QueueContextAction(action ContextAction) {
	if backend == nil {
		return
	}
	backend.mutex.Lock()
	backend.contextActions = append(backend.contextActions, action)
	backend.mutex.Unlock()
}

func (backend *MemoryBackend) ShowContextMenu(menu ContextMenu) (ContextAction, error) {
	if backend == nil {
		return ContextActionNone, fmt.Errorf("window: nil memory backend")
	}
	backend.mutex.Lock()
	defer backend.mutex.Unlock()
	copyMenu := menu
	copyMenu.Items = append([]ContextMenuItem(nil), menu.Items...)
	backend.menus = append(backend.menus, copyMenu)
	if len(backend.contextActions) == 0 {
		return ContextActionNone, nil
	}
	action := backend.contextActions[0]
	backend.contextActions = backend.contextActions[1:]
	return action, nil
}

func (backend *MemoryBackend) ContextMenus() []ContextMenu {
	if backend == nil {
		return nil
	}
	backend.mutex.Lock()
	defer backend.mutex.Unlock()
	result := make([]ContextMenu, len(backend.menus))
	for index, menu := range backend.menus {
		result[index] = menu
		result[index].Items = append([]ContextMenuItem(nil), menu.Items...)
	}
	return result
}

func (backend *MemoryBackend) SetCursor(cursor Cursor) error {
	if backend == nil {
		return fmt.Errorf("window: nil memory backend")
	}
	backend.mutex.Lock()
	defer backend.mutex.Unlock()
	if !backend.open || backend.closed {
		return fmt.Errorf("window: memory backend is not open")
	}
	backend.cursors = append(backend.cursors, cursor)
	return nil
}

func (backend *MemoryBackend) Cursors() []Cursor {
	if backend == nil {
		return nil
	}
	backend.mutex.Lock()
	defer backend.mutex.Unlock()
	return append([]Cursor(nil), backend.cursors...)
}

func (backend *MemoryBackend) SetTitle(title string) error {
	if backend == nil {
		return fmt.Errorf("window: nil memory backend")
	}
	backend.mutex.Lock()
	defer backend.mutex.Unlock()
	if !backend.open || backend.closed {
		return fmt.Errorf("window: memory backend is not open")
	}
	backend.titles = append(backend.titles, title)
	return nil
}

func (backend *MemoryBackend) Titles() []string {
	if backend == nil {
		return nil
	}
	backend.mutex.Lock()
	defer backend.mutex.Unlock()
	return append([]string(nil), backend.titles...)
}

func (backend *MemoryBackend) ReadClipboardText() (string, error) {
	if backend == nil {
		return "", fmt.Errorf("window: nil memory backend")
	}
	backend.mutex.Lock()
	defer backend.mutex.Unlock()
	return backend.clipboard, nil
}

func (backend *MemoryBackend) WriteClipboardText(value string) error {
	if backend == nil {
		return fmt.Errorf("window: nil memory backend")
	}
	backend.mutex.Lock()
	backend.clipboard = value
	backend.mutex.Unlock()
	return nil
}

func (backend *MemoryBackend) ClipboardText() string {
	if backend == nil {
		return ""
	}
	backend.mutex.Lock()
	defer backend.mutex.Unlock()
	return backend.clipboard
}

func (backend *MemoryBackend) SetClipboardText(value string) {
	if backend == nil {
		return
	}
	backend.mutex.Lock()
	backend.clipboard = value
	backend.mutex.Unlock()
}

func NewMemoryBackend(events ...Event) *MemoryBackend {
	return &MemoryBackend{events: append([]Event(nil), events...)}
}

func (backend *MemoryBackend) Open(config Config) error {
	if backend == nil {
		return fmt.Errorf("window: nil memory backend")
	}
	backend.mutex.Lock()
	defer backend.mutex.Unlock()
	if backend.open || backend.closed {
		return fmt.Errorf("window: memory backend already opened or closed")
	}
	backend.config = config
	backend.open = true
	return nil
}

func (backend *MemoryBackend) NextEvent(ctx context.Context) (Event, error) {
	if err := ctx.Err(); err != nil {
		return Event{}, err
	}
	backend.mutex.Lock()
	defer backend.mutex.Unlock()
	if !backend.open || backend.closed {
		return Event{}, fmt.Errorf("window: memory backend is not open")
	}
	if len(backend.events) == 0 {
		return Event{Kind: EventClose}, nil
	}
	event := backend.events[0]
	backend.events = backend.events[1:]
	return event, nil
}

func (backend *MemoryBackend) Present(frame *image.RGBA) error {
	if frame == nil {
		return fmt.Errorf("window: nil presented image")
	}
	backend.mutex.Lock()
	defer backend.mutex.Unlock()
	if !backend.open || backend.closed {
		return fmt.Errorf("window: memory backend is not open")
	}
	clone := image.NewRGBA(frame.Bounds())
	copy(clone.Pix, frame.Pix)
	backend.frames = append(backend.frames, clone)
	return nil
}

func (backend *MemoryBackend) Close() error {
	if backend == nil {
		return nil
	}
	backend.mutex.Lock()
	backend.closed = true
	backend.mutex.Unlock()
	return nil
}

func (backend *MemoryBackend) Frames() []*image.RGBA {
	if backend == nil {
		return nil
	}
	backend.mutex.Lock()
	defer backend.mutex.Unlock()
	return append([]*image.RGBA(nil), backend.frames...)
}

func (backend *MemoryBackend) Config() Config {
	if backend == nil {
		return Config{}
	}
	backend.mutex.Lock()
	defer backend.mutex.Unlock()
	return backend.config
}
