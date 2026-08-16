//go:build darwin && cgo

package window

/*
#cgo LDFLAGS: -framework AppKit -framework CoreGraphics
#include "cocoa_darwin.h"
#include <stdlib.h>
*/
import "C"

import (
	"context"
	"fmt"
	"image"
	"strings"
	"unsafe"
)

type cocoaBackend struct {
	window *C.gossamer_cocoa_window
}

// NewNativeBackend returns the macOS AppKit backend. Run keeps every method on
// one locked OS thread, as required by Cocoa.
func NewNativeBackend() Backend { return &cocoaBackend{} }

func (backend *cocoaBackend) Open(config Config) error {
	if backend == nil || backend.window != nil {
		return fmt.Errorf("window: Cocoa backend is nil or already open")
	}
	title := C.CString(config.Title)
	defer C.free(unsafe.Pointer(title))
	var nativeError *C.char
	backend.window = C.gossamer_cocoa_open(title, C.int(config.Width), C.int(config.Height), &nativeError)
	if backend.window == nil {
		return cocoaError(nativeError, "open Cocoa window")
	}
	C.free(unsafe.Pointer(nativeError))
	return nil
}

func (backend *cocoaBackend) NextEvent(ctx context.Context) (Event, error) {
	if err := ctx.Err(); err != nil {
		return Event{}, err
	}
	if backend == nil || backend.window == nil {
		return Event{}, fmt.Errorf("window: Cocoa backend is not open")
	}
	var native C.gossamer_cocoa_event
	var nativeError *C.char
	if C.gossamer_cocoa_next_event(backend.window, &native, 0.016, &nativeError) == 0 {
		return Event{}, cocoaError(nativeError, "read Cocoa event")
	}
	C.free(unsafe.Pointer(nativeError))
	key := C.GoString(&native.key[0])
	code, normalizedKey := cocoaKey(uint16(native.key_code), key)
	return Event{
		Kind: EventKind(native.kind), Width: int(native.width), Height: int(native.height),
		X: float64(native.x), Y: float64(native.y),
		DeltaX: float64(native.delta_x), DeltaY: float64(native.delta_y),
		Button: int(native.button), Buttons: uint(native.buttons),
		Key: normalizedKey, Code: code, Text: C.GoString(&native.text[0]), Repeat: native.repeat != 0,
		Modifiers: Modifiers{Alt: native.alt != 0, Ctrl: native.control != 0, Meta: native.command != 0, Shift: native.shift != 0},
	}, nil
}

func (backend *cocoaBackend) Present(frame *image.RGBA) error {
	if backend == nil || backend.window == nil || frame == nil {
		return fmt.Errorf("window: Cocoa backend is not open or frame is nil")
	}
	bounds := frame.Bounds()
	if bounds.Dx() <= 0 || bounds.Dy() <= 0 || len(frame.Pix) == 0 {
		return fmt.Errorf("window: invalid Cocoa frame")
	}
	var nativeError *C.char
	if C.gossamer_cocoa_present(
		backend.window, (*C.uint8_t)(unsafe.Pointer(&frame.Pix[0])),
		C.int(bounds.Dx()), C.int(bounds.Dy()), C.int(frame.Stride), &nativeError,
	) == 0 {
		return cocoaError(nativeError, "present Cocoa frame")
	}
	C.free(unsafe.Pointer(nativeError))
	return nil
}

func (backend *cocoaBackend) Close() error {
	if backend != nil && backend.window != nil {
		C.gossamer_cocoa_close(backend.window)
		backend.window = nil
	}
	return nil
}

func cocoaError(native *C.char, fallback string) error {
	defer C.free(unsafe.Pointer(native))
	if native == nil {
		return fmt.Errorf("window: %s failed", fallback)
	}
	return fmt.Errorf("window: %s: %s", fallback, C.GoString(native))
}

func cocoaKey(keyCode uint16, characters string) (string, string) {
	special := map[uint16][2]string{
		36: {"Enter", "Enter"}, 48: {"Tab", "Tab"}, 51: {"Backspace", "Backspace"},
		53: {"Escape", "Escape"}, 117: {"Delete", "Delete"},
		123: {"ArrowLeft", "ArrowLeft"}, 124: {"ArrowRight", "ArrowRight"},
		125: {"ArrowDown", "ArrowDown"}, 126: {"ArrowUp", "ArrowUp"},
	}
	if value, ok := special[keyCode]; ok {
		return value[1], value[0]
	}
	code := fmt.Sprintf("KeyCode%d", keyCode)
	if len(characters) == 1 {
		upper := strings.ToUpper(characters)
		if upper[0] >= 'A' && upper[0] <= 'Z' {
			code = "Key" + upper
		} else if characters[0] >= '0' && characters[0] <= '9' {
			code = "Digit" + characters
		}
	}
	return code, characters
}
