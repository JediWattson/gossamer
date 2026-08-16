package nativeengine

import (
	"fmt"

	"github.com/JediWattson/gossamer/internal/browser"
	"github.com/JediWattson/gossamer/internal/dom"
	browserruntime "github.com/JediWattson/gossamer/internal/runtime"
	"github.com/JediWattson/gossamer/internal/runtime/memory"
)

func (realm *Realm) globalGetComputedStyle(context *browserruntime.TaskContext, _ memory.Value, arguments []memory.Value) (memory.Value, error) {
	handle, err := realm.unwrapNode(context, argument(arguments, 0))
	if err != nil {
		return memory.Value{}, err
	}
	metadata, err := realm.nodeMetadata(context, argument(arguments, 0))
	if err != nil {
		return memory.Value{}, err
	}
	if metadata.Type != browser.DOMElementNode {
		return memory.Value{}, fmt.Errorf("%w: getComputedStyle requires an Element", browserruntime.ErrOperandType)
	}
	pseudo := ""
	if len(arguments) > 1 && argument(arguments, 1).Kind() != memory.ValueNull && argument(arguments, 1).Kind() != memory.ValueUndefined {
		pseudo, err = stringArgument(context, arguments, 1)
		if err != nil {
			return memory.Value{}, err
		}
	}
	if pseudo != "" {
		return memory.Value{}, fmt.Errorf("%w: computed pseudo-elements are not yet supported", browserruntime.ErrOperandType)
	}
	host, ok := realm.host.(browser.DOMComputedStyleHost)
	if !ok {
		return memory.Value{}, fmt.Errorf("nativeengine: browser host does not expose computed style")
	}
	if _, err := host.ComputedStylePropertyNames(handle, pseudo); err != nil {
		return memory.Value{}, err
	}
	ref, err := realm.newHostWrapperLocked(context, memory.HostObject{
		Class: hostClassComputedStyle, Scope: uint64(handle.Document), Identity: uint64(handle.Node),
	}, realm.bindings.computedStylePrototype)
	if err != nil {
		return memory.Value{}, err
	}
	return memory.RefValue(ref), nil
}

func (realm *Realm) computedStyleCSSText(context *browserruntime.TaskContext, this memory.Value, _ []memory.Value) (memory.Value, error) {
	if _, _, err := realm.computedStyleOperands(context, this); err != nil {
		return memory.Value{}, err
	}
	return newString(context, "")
}

func (realm *Realm) computedStyleLength(context *browserruntime.TaskContext, this memory.Value, _ []memory.Value) (memory.Value, error) {
	handle, host, err := realm.computedStyleOperands(context, this)
	if err != nil {
		return memory.Value{}, err
	}
	names, err := host.ComputedStylePropertyNames(handle, "")
	return memory.NumberValue(float64(len(names))), err
}

func (realm *Realm) computedStyleGetPropertyValue(context *browserruntime.TaskContext, this memory.Value, arguments []memory.Value) (memory.Value, error) {
	handle, host, err := realm.computedStyleOperands(context, this)
	if err != nil {
		return memory.Value{}, err
	}
	name, err := stringArgument(context, arguments, 0)
	if err != nil {
		return memory.Value{}, err
	}
	value, _, err := host.ComputedStyleProperty(handle, "", name)
	if err != nil {
		return memory.Value{}, err
	}
	return newString(context, value)
}

func (realm *Realm) computedStyleGetPropertyPriority(context *browserruntime.TaskContext, this memory.Value, _ []memory.Value) (memory.Value, error) {
	if _, _, err := realm.computedStyleOperands(context, this); err != nil {
		return memory.Value{}, err
	}
	return newString(context, "")
}

func (realm *Realm) computedStyleItem(context *browserruntime.TaskContext, this memory.Value, arguments []memory.Value) (memory.Value, error) {
	handle, host, err := realm.computedStyleOperands(context, this)
	if err != nil {
		return memory.Value{}, err
	}
	index, err := integerArgument(arguments, 0)
	if err != nil {
		return memory.Value{}, err
	}
	names, err := host.ComputedStylePropertyNames(handle, "")
	if err != nil {
		return memory.Value{}, err
	}
	if index < 0 || index >= len(names) {
		return newString(context, "")
	}
	return newString(context, names[index])
}

func (realm *Realm) computedStyleOperands(context *browserruntime.TaskContext, this memory.Value) (browser.NodeHandle, browser.DOMComputedStyleHost, error) {
	if !this.IsRef() {
		return browser.NodeHandle{}, nil, fmt.Errorf("%w: invalid computed style receiver", browserruntime.ErrOperandType)
	}
	record, facade, err := realm.facadeRecord(context, this.Ref())
	if err != nil || !facade || record.Class != hostClassComputedStyle {
		return browser.NodeHandle{}, nil, fmt.Errorf("%w: invalid computed style receiver", browserruntime.ErrOperandType)
	}
	host, ok := realm.host.(browser.DOMComputedStyleHost)
	if !ok {
		return browser.NodeHandle{}, nil, fmt.Errorf("nativeengine: browser host does not expose computed style")
	}
	return browser.NodeHandle{Document: browser.DocumentGeneration(record.Scope), Node: dom.NodeID(record.Identity)}, host, nil
}

func (realm *Realm) elementGetBoundingClientRect(context *browserruntime.TaskContext, this memory.Value, _ []memory.Value) (memory.Value, error) {
	handle, host, err := realm.geometryOperands(context, this)
	if err != nil {
		return memory.Value{}, err
	}
	geometry, err := host.ElementGeometry(handle)
	if err != nil {
		return memory.Value{}, err
	}
	return realm.newDOMRect(context, geometry.Rect)
}

func (realm *Realm) elementGetClientRects(context *browserruntime.TaskContext, this memory.Value, arguments []memory.Value) (memory.Value, error) {
	value, err := realm.elementGetBoundingClientRect(context, this, arguments)
	if err != nil {
		return memory.Value{}, err
	}
	array, err := context.NewArray(1)
	if err != nil {
		return memory.Value{}, err
	}
	if err := context.SetArrayElement(array, 0, value); err != nil {
		return memory.Value{}, err
	}
	return memory.RefValue(array), nil
}

func (realm *Realm) elementGeometryValue(property string) browserruntime.NativeFunction {
	return func(context *browserruntime.TaskContext, this memory.Value, _ []memory.Value) (memory.Value, error) {
		handle, host, err := realm.geometryOperands(context, this)
		if err != nil {
			return memory.Value{}, err
		}
		geometry, err := host.ElementGeometry(handle)
		if err != nil {
			return memory.Value{}, err
		}
		values := map[string]float64{
			"clientWidth": geometry.ClientWidth, "clientHeight": geometry.ClientHeight,
			"offsetWidth": geometry.OffsetWidth, "offsetHeight": geometry.OffsetHeight,
			"scrollWidth": geometry.ScrollWidth, "scrollHeight": geometry.ScrollHeight,
			"scrollLeft": geometry.ScrollLeft, "scrollTop": geometry.ScrollTop,
		}
		return memory.NumberValue(values[property]), nil
	}
}

func (realm *Realm) elementScrollSet(vertical bool) browserruntime.NativeFunction {
	return func(context *browserruntime.TaskContext, this memory.Value, arguments []memory.Value) (memory.Value, error) {
		handle, host, err := realm.geometryOperands(context, this)
		if err != nil {
			return memory.Value{}, err
		}
		value := argument(arguments, 0)
		if value.Kind() != memory.ValueNumber {
			return memory.Value{}, fmt.Errorf("%w: scroll offset must be a Number", browserruntime.ErrOperandType)
		}
		geometry, err := host.ElementGeometry(handle)
		if err != nil {
			return memory.Value{}, err
		}
		x, y := geometry.ScrollLeft, geometry.ScrollTop
		if vertical {
			y = value.Number()
		} else {
			x = value.Number()
		}
		_, err = host.ScrollElement(handle, x, y)
		return memory.UndefinedValue(), err
	}
}

func (realm *Realm) windowGeometryValue(property string) browserruntime.NativeFunction {
	return func(_ *browserruntime.TaskContext, _ memory.Value, _ []memory.Value) (memory.Value, error) {
		host, ok := realm.host.(browser.DOMGeometryHost)
		if !ok {
			return memory.Value{}, fmt.Errorf("nativeengine: browser host does not expose viewport geometry")
		}
		geometry, err := host.ViewportGeometry()
		if err != nil {
			return memory.Value{}, err
		}
		values := map[string]float64{
			"innerWidth": geometry.InnerWidth, "innerHeight": geometry.InnerHeight,
			"scrollX": geometry.ScrollX, "scrollY": geometry.ScrollY,
		}
		return memory.NumberValue(values[property]), nil
	}
}

func (realm *Realm) geometryOperands(context *browserruntime.TaskContext, this memory.Value) (browser.NodeHandle, browser.DOMGeometryHost, error) {
	handle, err := realm.unwrapNode(context, this)
	if err != nil {
		return browser.NodeHandle{}, nil, err
	}
	host, ok := realm.host.(browser.DOMGeometryHost)
	if !ok {
		return browser.NodeHandle{}, nil, fmt.Errorf("nativeengine: browser host does not expose geometry")
	}
	return handle, host, nil
}

func (realm *Realm) newDOMRect(context *browserruntime.TaskContext, rect browser.DOMRect) (memory.Value, error) {
	object, err := context.NewHeapObject()
	if err != nil {
		return memory.Value{}, err
	}
	if err := context.SetPrototype(object, memory.RefValue(realm.bindings.domRectPrototype)); err != nil {
		return memory.Value{}, err
	}
	left, right := rect.X, rect.X+rect.Width
	if left > right {
		left, right = right, left
	}
	top, bottom := rect.Y, rect.Y+rect.Height
	if top > bottom {
		top, bottom = bottom, top
	}
	for name, value := range map[string]float64{
		"x": rect.X, "y": rect.Y, "width": rect.Width, "height": rect.Height,
		"top": top, "right": right, "bottom": bottom, "left": left,
	} {
		if err := defineData(context, object, name, memory.NumberValue(value), false, true, false); err != nil {
			return memory.Value{}, err
		}
	}
	return memory.RefValue(object), nil
}

func (realm *Realm) domRectToJSON(context *browserruntime.TaskContext, this memory.Value, _ []memory.Value) (memory.Value, error) {
	if !this.IsRef() {
		return memory.Value{}, fmt.Errorf("%w: invalid DOMRect receiver", browserruntime.ErrOperandType)
	}
	result, err := context.NewHeapObject()
	if err != nil {
		return memory.Value{}, err
	}
	for _, name := range []string{"x", "y", "width", "height", "top", "right", "bottom", "left"} {
		nameRef, err := context.NewString(name)
		if err != nil {
			return memory.Value{}, err
		}
		value, found, err := context.GetOwnProperty(this.Ref(), nameRef)
		if err != nil || !found {
			return memory.Value{}, fmt.Errorf("%w: invalid DOMRect receiver", browserruntime.ErrOperandType)
		}
		if err := defineData(context, result, name, value, true, true, true); err != nil {
			return memory.Value{}, err
		}
	}
	return memory.RefValue(result), nil
}
