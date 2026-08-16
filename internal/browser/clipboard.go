package browser

import (
	"fmt"
	"strings"
	"unicode/utf16"

	"github.com/JediWattson/gossamer/internal/dom"
)

// TextControlPresentation is a copied browser-owned view of one editable
// control. It is intentionally value-only so Graphite can paint native caret
// and selection feedback without retaining a DOM node.
type TextControlPresentation struct {
	Value          string
	SelectionStart int
	SelectionEnd   int
	Direction      string
	Multiline      bool
	Password       bool
}

func (page *Page) TextControlPresentation(handle NodeHandle) (TextControlPresentation, error) {
	if page == nil {
		return TextControlPresentation{}, fmt.Errorf("browser: nil page")
	}
	page.mutex.RLock()
	if page.closed {
		page.mutex.RUnlock()
		return TextControlPresentation{}, ErrPageClosed
	}
	if handle.Document != page.documentGeneration || handle.Node == dom.InvalidNodeID {
		page.mutex.RUnlock()
		return TextControlPresentation{}, ErrStaleNodeHandle
	}
	document := page.document
	page.mutex.RUnlock()
	node, ok := document.Resolve(handle.Node)
	if !ok || node.Type != dom.ElementNode {
		return TextControlPresentation{}, dom.ErrWrongNodeKind
	}
	value, err := document.FormValue(handle.Node)
	if err != nil {
		return TextControlPresentation{}, err
	}
	start, end, direction, err := document.FormSelection(handle.Node)
	if err != nil {
		return TextControlPresentation{}, err
	}
	typeValue, _, _ := document.GetAttribute(handle.Node, "type")
	return TextControlPresentation{
		Value: value, SelectionStart: start, SelectionEnd: end, Direction: direction,
		Multiline: strings.EqualFold(node.Data, "textarea"),
		Password:  strings.EqualFold(typeValue, "password"),
	}, nil
}

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
