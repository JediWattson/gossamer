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
	mutex  sync.Mutex
	events []Event
	frames []*image.RGBA
	config Config
	open   bool
	closed bool
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
