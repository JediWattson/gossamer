package nativeengine

import (
	"fmt"
	"math"
	"net/url"
	"strings"

	"github.com/JediWattson/gossamer/internal/browser"
	browserruntime "github.com/JediWattson/gossamer/internal/runtime"
	"github.com/JediWattson/gossamer/internal/runtime/memory"
)

const (
	nativeElementClick uint64 = 26_000 + iota
	nativeElementScrollTo
	nativeWindowScrollTo
	nativeWindowScrollBy
	nativeHTMLDialogShow
	nativeHTMLDialogShowModal
	nativeHTMLDialogClose
	nativeHTMLDialogReturnValueGet
	nativeHTMLDialogReturnValueSet
	nativeElementInertGet
	nativeElementInertSet
	nativeElementTitleGet
	nativeElementTitleSet
	nativeElementDisabledGet
	nativeElementDisabledSet
	nativeElementTypeGet
	nativeElementTypeSet
	nativeElementNameGet
	nativeElementNameSet
	nativeElementHrefGet
	nativeElementHrefSet
	nativeElementRelGet
	nativeElementRelSet
	nativeElementAsGet
	nativeElementAsSet
	nativeElementMediaGet
	nativeElementMediaSet
	nativeElementCrossOriginGet
	nativeElementCrossOriginSet
	nativeElementAltGet
	nativeElementAltSet
	nativeElementTargetGet
	nativeElementTargetSet
	nativeElementDownloadGet
	nativeElementDownloadSet
	nativeElementDeferGet
	nativeElementDeferSet
	nativeElementNoModuleGet
	nativeElementNoModuleSet
	nativeElementOpenGet
	nativeElementOpenSet
	nativeElementActionGet
	nativeElementActionSet
	nativeElementMethodGet
	nativeElementMethodSet
)

const dialogReturnValueProperty = "\x00gossamer.dialog.return-value"

func (realm *Realm) elementStringAttrGet(attr string) browserruntime.NativeFunction {
	return func(context *browserruntime.TaskContext, this memory.Value, _ []memory.Value) (memory.Value, error) {
		handle, err := realm.unwrapNode(context, this)
		if err != nil {
			return memory.Value{}, err
		}
		value, _, err := realm.host.GetAttribute(handle, attr)
		if err != nil {
			return memory.Value{}, err
		}
		return newString(context, value)
	}
}

func (realm *Realm) elementStringAttrSet(attr string) browserruntime.NativeFunction {
	return func(context *browserruntime.TaskContext, this memory.Value, arguments []memory.Value) (memory.Value, error) {
		handle, err := realm.unwrapNode(context, this)
		if err != nil {
			return memory.Value{}, err
		}
		value, err := stringArgument(context, arguments, 0)
		if err != nil {
			return memory.Value{}, err
		}
		return memory.UndefinedValue(), realm.host.SetAttribute(handle, attr, value)
	}
}

func (realm *Realm) elementNullableAttrGet(attr string) browserruntime.NativeFunction {
	return func(context *browserruntime.TaskContext, this memory.Value, _ []memory.Value) (memory.Value, error) {
		handle, err := realm.unwrapNode(context, this)
		if err != nil {
			return memory.Value{}, err
		}
		value, found, err := realm.host.GetAttribute(handle, attr)
		if err != nil {
			return memory.Value{}, err
		}
		if !found {
			return memory.NullValue(), nil
		}
		return newString(context, value)
	}
}

func (realm *Realm) elementNullableAttrSet(attr string) browserruntime.NativeFunction {
	return func(context *browserruntime.TaskContext, this memory.Value, arguments []memory.Value) (memory.Value, error) {
		handle, err := realm.unwrapNode(context, this)
		if err != nil {
			return memory.Value{}, err
		}
		arg := argument(arguments, 0)
		if arg.Kind() == memory.ValueNull || arg.Kind() == memory.ValueUndefined {
			return memory.UndefinedValue(), realm.host.RemoveAttribute(handle, attr)
		}
		value, err := valueString(context, arg)
		if err != nil {
			return memory.Value{}, err
		}
		return memory.UndefinedValue(), realm.host.SetAttribute(handle, attr, value)
	}
}

func (realm *Realm) elementBoolAttrGet(attr string) browserruntime.NativeFunction {
	return func(context *browserruntime.TaskContext, this memory.Value, _ []memory.Value) (memory.Value, error) {
		handle, err := realm.unwrapNode(context, this)
		if err != nil {
			return memory.Value{}, err
		}
		_, found, err := realm.host.GetAttribute(handle, attr)
		return memory.BoolValue(found), err
	}
}

func (realm *Realm) elementBoolAttrSet(attr string) browserruntime.NativeFunction {
	return func(context *browserruntime.TaskContext, this memory.Value, arguments []memory.Value) (memory.Value, error) {
		handle, err := realm.unwrapNode(context, this)
		if err != nil {
			return memory.Value{}, err
		}
		if truthy(argument(arguments, 0)) {
			return memory.UndefinedValue(), realm.host.SetAttribute(handle, attr, "")
		}
		return memory.UndefinedValue(), realm.host.RemoveAttribute(handle, attr)
	}
}

func (realm *Realm) elementURLAttrGet(attr string) browserruntime.NativeFunction {
	return func(context *browserruntime.TaskContext, this memory.Value, _ []memory.Value) (memory.Value, error) {
		handle, err := realm.unwrapNode(context, this)
		if err != nil {
			return memory.Value{}, err
		}
		raw, found, err := realm.host.GetAttribute(handle, attr)
		if err != nil {
			return memory.Value{}, err
		}
		if !found {
			return newString(context, "")
		}
		resolved := realm.resolveElementURL(raw)
		return newString(context, resolved)
	}
}

func (realm *Realm) resolveElementURL(raw string) string {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return raw
	}
	locationHost, ok := realm.host.(browser.SessionHistoryHost)
	if !ok {
		return raw
	}
	href, err := locationHost.LocationComponent(browser.LocationHref)
	if err != nil || href == "" {
		return raw
	}
	base, err := url.Parse(href)
	if err != nil {
		return raw
	}
	ref, err := url.Parse(trimmed)
	if err != nil {
		return raw
	}
	return base.ResolveReference(ref).String()
}

func (realm *Realm) elementTypeGet(context *browserruntime.TaskContext, this memory.Value, _ []memory.Value) (memory.Value, error) {
	handle, err := realm.unwrapNode(context, this)
	if err != nil {
		return memory.Value{}, err
	}
	value, found, err := realm.host.GetAttribute(handle, "type")
	if err != nil {
		return memory.Value{}, err
	}
	if found {
		return newString(context, value)
	}
	host, err := realm.elementHost()
	if err != nil {
		return newString(context, "")
	}
	metadata, err := host.NodeMetadata(handle)
	if err != nil {
		return memory.Value{}, err
	}
	switch metadata.LocalName {
	case "input":
		return newString(context, "text")
	case "button":
		return newString(context, "submit")
	default:
		return newString(context, "")
	}
}

func (realm *Realm) elementClick(context *browserruntime.TaskContext, this memory.Value, _ []memory.Value) (memory.Value, error) {
	handle, err := realm.unwrapNode(context, this)
	if err != nil {
		return memory.Value{}, err
	}
	_, err = realm.dispatchEventLocked(context, browser.InputEvent{
		Type:   browser.InputClick,
		Target: handle,
	})
	return memory.UndefinedValue(), err
}

func (realm *Realm) elementScrollTo(context *browserruntime.TaskContext, this memory.Value, arguments []memory.Value) (memory.Value, error) {
	handle, host, err := realm.geometryOperands(context, this)
	if err != nil {
		return memory.Value{}, err
	}
	geometry, err := host.ElementGeometry(handle)
	if err != nil {
		return memory.Value{}, err
	}
	x, y, err := scrollCoordinates(context, geometry.ScrollLeft, geometry.ScrollTop, arguments)
	if err != nil {
		return memory.Value{}, err
	}
	_, err = host.ScrollElement(handle, x, y)
	return memory.UndefinedValue(), err
}

func (realm *Realm) windowScroll(relative bool) browserruntime.NativeFunction {
	return func(context *browserruntime.TaskContext, _ memory.Value, arguments []memory.Value) (memory.Value, error) {
		host, ok := realm.host.(browser.DOMGeometryHost)
		if !ok {
			return memory.Value{}, fmt.Errorf("nativeengine: browser host does not expose viewport geometry")
		}
		geometry, err := host.ViewportGeometry()
		if err != nil {
			return memory.Value{}, err
		}
		baseX, baseY := geometry.ScrollX, geometry.ScrollY
		if relative {
			baseX, baseY = 0, 0
		}
		x, y, err := scrollCoordinates(context, baseX, baseY, arguments)
		if err != nil {
			return memory.Value{}, err
		}
		if relative {
			x += geometry.ScrollX
			y += geometry.ScrollY
		}
		_, err = host.ScrollViewport(x, y)
		return memory.UndefinedValue(), err
	}
}

func scrollCoordinates(context *browserruntime.TaskContext, defaultX, defaultY float64, arguments []memory.Value) (float64, float64, error) {
	if len(arguments) == 0 {
		return defaultX, defaultY, nil
	}
	first := arguments[0]
	if first.IsRef() {
		kind, err := context.HeapKind(first.Ref())
		if err != nil {
			return 0, 0, err
		}
		if kind == memory.HeapObject {
			x := optionalScrollNumber(context, first.Ref(), "left", defaultX)
			y := optionalScrollNumber(context, first.Ref(), "top", defaultY)
			return x, y, nil
		}
	}
	if len(arguments) < 2 {
		return 0, 0, fmt.Errorf("%w: scrollTo requires 2 arguments when called with coordinates", browserruntime.ErrOperandType)
	}
	x := numberOrZero(arguments[0])
	y := numberOrZero(arguments[1])
	return x, y, nil
}

func optionalScrollNumber(context *browserruntime.TaskContext, object memory.Ref, name string, fallback float64) float64 {
	value, found, err := ownValue(context, object, name)
	if err != nil || !found || value.Kind() == memory.ValueUndefined {
		return fallback
	}
	return numberOrZero(value)
}

func numberOrZero(value memory.Value) float64 {
	if value.Kind() == memory.ValueNumber {
		n := value.Number()
		if math.IsNaN(n) || math.IsInf(n, 0) {
			return 0
		}
		return n
	}
	return 0
}

func (realm *Realm) htmlDialogShow(context *browserruntime.TaskContext, this memory.Value, _ []memory.Value) (memory.Value, error) {
	handle, err := realm.unwrapNode(context, this)
	if err != nil {
		return memory.Value{}, err
	}
	return memory.UndefinedValue(), realm.host.SetAttribute(handle, "open", "")
}

func (realm *Realm) htmlDialogClose(context *browserruntime.TaskContext, this memory.Value, arguments []memory.Value) (memory.Value, error) {
	handle, err := realm.unwrapNode(context, this)
	if err != nil {
		return memory.Value{}, err
	}
	_, wasOpen, err := realm.host.GetAttribute(handle, "open")
	if err != nil {
		return memory.Value{}, err
	}
	if err := realm.host.RemoveAttribute(handle, "open"); err != nil {
		return memory.Value{}, err
	}
	if arg := argument(arguments, 0); arg.Kind() != memory.ValueUndefined && this.IsRef() {
		value, err := valueString(context, arg)
		if err != nil {
			return memory.Value{}, err
		}
		if err := setStringProperty(context, this.Ref(), dialogReturnValueProperty, value); err != nil {
			return memory.Value{}, err
		}
	}
	if wasOpen && this.IsRef() {
		event, err := realm.newSimpleEvent(context, "close")
		if err != nil {
			return memory.Value{}, err
		}
		if _, err := realm.dispatchEventObjectLocked(context, nodeEventTarget(handle), event, "close", false, false); err != nil {
			return memory.Value{}, err
		}
	}
	return memory.UndefinedValue(), nil
}

func (realm *Realm) htmlDialogReturnValueGet(context *browserruntime.TaskContext, this memory.Value, _ []memory.Value) (memory.Value, error) {
	if _, err := realm.unwrapNode(context, this); err != nil {
		return memory.Value{}, err
	}
	if !this.IsRef() {
		return newString(context, "")
	}
	value, found, err := ownValue(context, this.Ref(), dialogReturnValueProperty)
	if err != nil {
		return memory.Value{}, err
	}
	if !found {
		return newString(context, "")
	}
	return value, nil
}

func (realm *Realm) htmlDialogReturnValueSet(context *browserruntime.TaskContext, this memory.Value, arguments []memory.Value) (memory.Value, error) {
	if _, err := realm.unwrapNode(context, this); err != nil {
		return memory.Value{}, err
	}
	value, err := stringArgument(context, arguments, 0)
	if err != nil {
		return memory.Value{}, err
	}
	if this.IsRef() {
		if err := setStringProperty(context, this.Ref(), dialogReturnValueProperty, value); err != nil {
			return memory.Value{}, err
		}
	}
	return memory.UndefinedValue(), nil
}

func setStringProperty(context *browserruntime.TaskContext, object memory.Ref, name string, value string) error {
	text, err := context.NewString(value)
	if err != nil {
		return err
	}
	nameRef, err := context.NewString(name)
	if err != nil {
		return err
	}
	if _, found, err := context.GetOwnProperty(object, nameRef); err != nil {
		return err
	} else if found {
		return context.SetProperty(object, nameRef, memory.RefValue(text))
	}
	return defineData(context, object, name, memory.RefValue(text), true, false, true)
}

func (realm *Realm) newSimpleEvent(context *browserruntime.TaskContext, eventType string) (memory.Ref, error) {
	event, err := context.NewHeapObject()
	if err != nil {
		return memory.Ref{}, err
	}
	if err := context.SetPrototype(event, memory.RefValue(realm.bindings.eventPrototype)); err != nil {
		return memory.Ref{}, err
	}
	typeStr, err := context.NewString(eventType)
	if err != nil {
		return memory.Ref{}, err
	}
	if _, err := realm.initializeConstructedEvent(context, memory.RefValue(event), []memory.Value{memory.RefValue(typeStr)}, false); err != nil {
		return memory.Ref{}, err
	}
	return event, nil
}
