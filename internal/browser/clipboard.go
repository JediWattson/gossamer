package browser

import (
	"fmt"
	"unicode/utf16"

	"github.com/JediWattson/gossamer/internal/dom"
)

// SelectedText returns the UTF-16 selection of a text form control. It is a
// copied value for native clipboard integration; native backends never retain
// a DOM pointer or mutate the document directly.
func (page *Page) SelectedText(handle NodeHandle) (string, error) {
	if page == nil {
		return "", fmt.Errorf("browser: nil page")
	}
	page.mutex.RLock()
	defer page.mutex.RUnlock()
	if page.closed {
		return "", ErrPageClosed
	}
	if handle.Document != page.documentGeneration || handle.Node == dom.InvalidNodeID {
		return "", ErrStaleNodeHandle
	}
	value, err := page.document.FormValue(handle.Node)
	if err != nil {
		return "", err
	}
	start, end, _, err := page.document.FormSelection(handle.Node)
	if err != nil {
		return "", err
	}
	units := utf16.Encode([]rune(value))
	start = clampSelectionOffset(start, len(units))
	end = clampSelectionOffset(end, len(units))
	if end < start {
		start, end = end, start
	}
	return string(utf16.Decode(units[start:end])), nil
}

func clampSelectionOffset(offset, length int) int {
	if offset < 0 {
		return 0
	}
	if offset > length {
		return length
	}
	return offset
}
