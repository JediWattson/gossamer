package dom

import (
	"fmt"
	"image"
	"strings"
	"sync"
)

const (
	// DefaultCanvasWidth matches the HTML specification default intrinsic width.
	DefaultCanvasWidth = 300
	// DefaultCanvasHeight matches the HTML specification default intrinsic height.
	DefaultCanvasHeight = 150
	// MaxCanvasDimension bounds individual canvas bitmap dimensions to prevent OOM.
	MaxCanvasDimension = 8192
	// maxCanvasPixels bounds total backing bitmap pixel count (~64 MiB RGBA).
	maxCanvasPixels = 16 << 20
)

// CanvasState stores the live 2D backing bitmap for an HTML <canvas> element.
//
// Mutable pixel writes happen exclusively on bitmap under Document.store.mutex.
// snapshotMu guards snapshot so renderers may read an immutable *image.RGBA
// without holding the document mutex or racing with subsequent JS draws.
type CanvasState struct {
	Width    int
	Height   int
	Revision uint64

	bitmap     *image.RGBA
	snapshotMu sync.Mutex
	snapshot   *image.RGBA
}

// IsHTMLCanvas reports whether node is an HTML <canvas> element.
func IsHTMLCanvas(node *Node) bool {
	return node != nil && node.Type == ElementNode && node.NamespaceURI == HTMLNamespace && node.Data == "canvas"
}

// ParseCanvasDimension parses a canvas width or height content attribute per
// the HTML rules for parsing non-negative integers, returning fallback when the
// value is absent, malformed, or negative.
func ParseCanvasDimension(value string, present bool, fallback int) int {
	if !present {
		return fallback
	}
	trimmed := strings.TrimLeft(value, " \t\n\f\r")
	if trimmed == "" {
		return fallback
	}
	if trimmed[0] == '+' {
		trimmed = trimmed[1:]
	} else if trimmed[0] == '-' {
		return fallback
	}
	if trimmed == "" || trimmed[0] < '0' || trimmed[0] > '9' {
		return fallback
	}
	parsed := 0
	for index := 0; index < len(trimmed); index++ {
		digit := trimmed[index]
		if digit < '0' || digit > '9' {
			break
		}
		if parsed > MaxCanvasDimension {
			return MaxCanvasDimension
		}
		parsed = parsed*10 + int(digit-'0')
	}
	if parsed > MaxCanvasDimension {
		return MaxCanvasDimension
	}
	return parsed
}

func canvasAttributeDimensions(node *Node) (int, int) {
	rawWidth, hasWidth := attributeValue(node, "width")
	rawHeight, hasHeight := attributeValue(node, "height")
	width := ParseCanvasDimension(rawWidth, hasWidth, DefaultCanvasWidth)
	height := ParseCanvasDimension(rawHeight, hasHeight, DefaultCanvasHeight)
	if width > 0 && height > 0 && int64(width)*int64(height) > maxCanvasPixels {
		if width > height {
			width = maxCanvasPixels / height
		} else {
			height = maxCanvasPixels / width
		}
	}
	return width, height
}

// CanvasDimensions returns the intrinsic bitmap width and height for node when
// node is an HTML <canvas> element.
func (node *Node) CanvasDimensions() (int, int, bool) {
	if !IsHTMLCanvas(node) {
		return 0, 0, false
	}
	if node.Canvas != nil {
		return node.Canvas.Width, node.Canvas.Height, true
	}
	width, height := canvasAttributeDimensions(node)
	return width, height, true
}

// CanvasImage returns an immutable RGBA snapshot of the canvas backing bitmap,
// or nil if the canvas is empty or has never been drawn into.
func (node *Node) CanvasImage() image.Image {
	if !IsHTMLCanvas(node) || node.Canvas == nil {
		return nil
	}
	state := node.Canvas
	if state.Width <= 0 || state.Height <= 0 || state.bitmap == nil {
		return nil
	}
	state.snapshotMu.Lock()
	defer state.snapshotMu.Unlock()
	if state.snapshot == nil {
		clone := image.NewRGBA(state.bitmap.Bounds())
		copy(clone.Pix, state.bitmap.Pix)
		state.snapshot = clone
	}
	return state.snapshot
}

func (state *CanvasState) invalidateSnapshotLocked() {
	if state == nil {
		return
	}
	state.snapshotMu.Lock()
	state.snapshot = nil
	state.snapshotMu.Unlock()
}

func ensureCanvasStateLocked(node *Node) *CanvasState {
	width, height := canvasAttributeDimensions(node)
	if node.Canvas == nil {
		node.Canvas = &CanvasState{
			Width:  width,
			Height: height,
		}
	}
	return node.Canvas
}

func resetCanvasLocked(node *Node) *CanvasState {
	width, height := canvasAttributeDimensions(node)
	if node.Canvas == nil {
		node.Canvas = &CanvasState{
			Width:    width,
			Height:   height,
			Revision: 1,
		}
		return node.Canvas
	}
	state := node.Canvas
	state.invalidateSnapshotLocked()
	state.Width = width
	state.Height = height
	if width > 0 && height > 0 && state.bitmap != nil && len(state.bitmap.Pix) == width*height*4 && state.bitmap.Stride == width*4 {
		state.bitmap.Rect = image.Rect(0, 0, width, height)
		clear(state.bitmap.Pix)
	} else {
		state.bitmap = nil
	}
	state.Revision++
	return state
}

// CanvasSize returns the intrinsic canvas dimensions and current revision.
func (document *Document) CanvasSize(id NodeID) (width, height int, revision uint64, err error) {
	if document == nil || document.store == nil {
		return 0, 0, 0, ErrInvalidDocument
	}
	document.store.mutex.RLock()
	defer document.store.mutex.RUnlock()
	node, ok := document.store.resolveLocked(id)
	if !ok {
		return 0, 0, 0, fmt.Errorf("%w: %d", ErrUnknownNode, id)
	}
	if !IsHTMLCanvas(node) {
		return 0, 0, 0, fmt.Errorf("%w: node %d is not a canvas", ErrWrongNodeKind, id)
	}
	if node.Canvas != nil {
		return node.Canvas.Width, node.Canvas.Height, node.Canvas.Revision, nil
	}
	width, height = canvasAttributeDimensions(node)
	return width, height, 0, nil
}

// SetCanvasDimension updates a "width" or "height" content attribute on a
// <canvas> element and unconditionally resets its backing bitmap per HTML spec.
func (document *Document) SetCanvasDimension(id NodeID, name, value string) (width, height int, revision uint64, err error) {
	if document == nil || document.store == nil {
		return 0, 0, 0, ErrInvalidDocument
	}
	if name != "width" && name != "height" {
		return 0, 0, 0, fmt.Errorf("%w: invalid canvas dimension attribute %q", ErrInvalidName, name)
	}
	document.store.mutex.Lock()
	defer document.store.mutex.Unlock()
	node, ok := document.store.resolveLocked(id)
	if !ok {
		return 0, 0, 0, fmt.Errorf("%w: %d", ErrUnknownNode, id)
	}
	if !IsHTMLCanvas(node) {
		return 0, 0, 0, fmt.Errorf("%w: node %d is not a canvas", ErrWrongNodeKind, id)
	}
	setContentAttributeLockedWithMutation(document, node, name, value)
	state := resetCanvasLocked(node)
	document.recordStateMutationLocked(node, "canvas")
	document.version.Add(1)
	return state.Width, state.Height, state.Revision, nil
}

// ReadCanvas provides read-locked access to a <canvas> element's current
// dimensions and backing bitmap (bitmap may be nil if nothing has been drawn).
func (document *Document) ReadCanvas(id NodeID, fn func(width, height int, revision uint64, bitmap *image.RGBA)) error {
	if document == nil || document.store == nil {
		return ErrInvalidDocument
	}
	document.store.mutex.RLock()
	defer document.store.mutex.RUnlock()
	node, ok := document.store.resolveLocked(id)
	if !ok {
		return fmt.Errorf("%w: %d", ErrUnknownNode, id)
	}
	if !IsHTMLCanvas(node) {
		return fmt.Errorf("%w: node %d is not a canvas", ErrWrongNodeKind, id)
	}
	if node.Canvas != nil {
		fn(node.Canvas.Width, node.Canvas.Height, node.Canvas.Revision, node.Canvas.bitmap)
		return nil
	}
	width, height := canvasAttributeDimensions(node)
	fn(width, height, 0, nil)
	return nil
}

// MutateCanvas acquires the document write lock, allocates the backing bitmap
// if needed, and invokes fn. When fn returns true, the snapshot cache is
// invalidated and a "canvas" state mutation is recorded.
func (document *Document) MutateCanvas(id NodeID, fn func(width, height int, bitmap *image.RGBA) bool) (revision uint64, mutated bool, err error) {
	if document == nil || document.store == nil {
		return 0, false, ErrInvalidDocument
	}
	document.store.mutex.Lock()
	defer document.store.mutex.Unlock()
	node, ok := document.store.resolveLocked(id)
	if !ok {
		return 0, false, fmt.Errorf("%w: %d", ErrUnknownNode, id)
	}
	if !IsHTMLCanvas(node) {
		return 0, false, fmt.Errorf("%w: node %d is not a canvas", ErrWrongNodeKind, id)
	}
	state := ensureCanvasStateLocked(node)
	if state.Width > 0 && state.Height > 0 && state.bitmap == nil {
		state.bitmap = image.NewRGBA(image.Rect(0, 0, state.Width, state.Height))
	}
	changed := fn(state.Width, state.Height, state.bitmap)
	if changed {
		state.invalidateSnapshotLocked()
		state.Revision++
		document.recordStateMutationLocked(node, "canvas")
		document.version.Add(1)
	}
	return state.Revision, changed, nil
}

func setContentAttributeLockedWithMutation(document *Document, node *Node, name, value string) {
	for index := range node.Attributes {
		if node.Attributes[index].Name != name {
			continue
		}
		if node.Attributes[index].Value == value {
			return
		}
		oldValue := node.Attributes[index].Value
		node.Attributes[index].Value = value
		document.recordAttributeMutationLocked(node, name, oldValue, true)
		return
	}
	node.Attributes = append(node.Attributes, Attribute{Name: name, Value: value})
	document.recordAttributeMutationLocked(node, name, "", false)
}
