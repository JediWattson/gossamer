package window

import (
	"image"
	"strings"

	"github.com/JediWattson/gossamer/internal/browser"
	"github.com/JediWattson/gossamer/internal/dom"
)

func (shell *graphiteShell) cursorAt(page *browser.Page, event Event) Cursor {
	if shell == nil {
		return pageCursorAt(page, event.X, event.Y)
	}
	layout := shell.layout()
	point := image.Pt(int(event.X), int(event.Y))
	if point.In(layout.address) {
		return CursorText
	}
	for _, target := range []image.Rectangle{
		layout.newTab, layout.tabOverflowBack, layout.tabOverflowNext,
		layout.back, layout.forward, layout.reload, layout.railDisclosure, layout.errorRetry,
	} {
		if !target.Empty() && point.In(target) {
			return CursorPointer
		}
	}
	for _, tab := range layout.tabs {
		if (!tab.body.Empty() && point.In(tab.body)) || (!tab.close.Empty() && point.In(tab.close)) {
			return CursorPointer
		}
	}
	if point.In(layout.content) && !point.In(layout.inspector) {
		return pageCursorAt(page, event.X-float64(layout.content.Min.X), event.Y-float64(layout.content.Min.Y))
	}
	return CursorDefault
}

func pageCursorAt(page *browser.Page, x, y float64) Cursor {
	if page == nil {
		return CursorDefault
	}
	handle, ok := page.HitTest(x, y)
	if !ok {
		return CursorDefault
	}
	node, ok := page.Resolve(handle)
	if !ok || node.Type != dom.ElementNode {
		return CursorDefault
	}
	switch strings.ToLower(node.Data) {
	case "a":
		for _, attribute := range node.Attributes {
			if strings.EqualFold(attribute.Name, "href") {
				return CursorPointer
			}
		}
	case "button", "select", "summary":
		return CursorPointer
	case "input":
		inputType := "text"
		for _, attribute := range node.Attributes {
			if strings.EqualFold(attribute.Name, "type") && attribute.Value != "" {
				inputType = strings.ToLower(attribute.Value)
			}
		}
		switch inputType {
		case "button", "checkbox", "color", "file", "image", "radio", "range", "reset", "submit":
			return CursorPointer
		default:
			return CursorText
		}
	case "textarea", "p", "span", "label", "li", "h1", "h2", "h3", "h4", "h5", "h6":
		return CursorText
	}
	return CursorDefault
}
