package nativeengine

import (
	"fmt"
	"math"
	"strconv"
	"strings"

	"github.com/JediWattson/gossamer/internal/browser"
	browserruntime "github.com/JediWattson/gossamer/internal/runtime"
	"github.com/JediWattson/gossamer/internal/runtime/memory"
)

const (
	nativeImageConstructor uint64 = 21_000 + iota
	nativeImageCompleteGet
	nativeImageNaturalWidthGet
	nativeImageNaturalHeightGet
	nativeImageWidthGet
	nativeImageWidthSet
	nativeImageHeightGet
	nativeImageHeightSet
	nativeImageCurrentSrcGet
)

func (realm *Realm) newImageConstructor(context *browserruntime.TaskContext) (memory.Ref, error) {
	name, err := newString(context, "Image")
	if err != nil {
		return memory.Ref{}, err
	}
	constructor, err := context.NewNativeConstructor(name, memory.RefValue(realm.active.Global), 0, nativeImageConstructor)
	if err != nil {
		return memory.Ref{}, err
	}
	prototypeName, err := context.NewString("prototype")
	if err != nil {
		return memory.Ref{}, err
	}
	if err := context.SetProperty(constructor, prototypeName, memory.RefValue(realm.bindings.htmlImageElementPrototype)); err != nil {
		return memory.Ref{}, err
	}
	return constructor, nil
}

func (realm *Realm) imageConstructor(context *browserruntime.TaskContext, _ memory.Value, _ []memory.Value) (memory.Value, error) {
	handle, err := realm.host.CreateElement("img")
	if err != nil {
		return memory.Value{}, realm.throwDOMException(context, err)
	}
	return realm.wrappedNodeValue(context, handle)
}

func (realm *Realm) imageMetrics(context *browserruntime.TaskContext, this memory.Value) (browser.NodeHandle, browser.DOMImageMetrics, error) {
	handle, err := realm.unwrapNode(context, this)
	if err != nil {
		return browser.NodeHandle{}, browser.DOMImageMetrics{}, err
	}
	host, ok := realm.host.(browser.DOMImageHost)
	if !ok {
		return handle, browser.DOMImageMetrics{}, fmt.Errorf("nativeengine: browser host does not expose image metrics")
	}
	metrics, err := host.ImageMetrics(handle)
	if err != nil {
		return browser.NodeHandle{}, browser.DOMImageMetrics{}, realm.throwDOMException(context, err)
	}
	return handle, metrics, nil
}

func (realm *Realm) imageCompleteGet(context *browserruntime.TaskContext, this memory.Value, _ []memory.Value) (memory.Value, error) {
	_, metrics, err := realm.imageMetrics(context, this)
	if err != nil {
		return memory.Value{}, err
	}
	return memory.BoolValue(metrics.Complete), nil
}

func (realm *Realm) imageNaturalWidthGet(context *browserruntime.TaskContext, this memory.Value, _ []memory.Value) (memory.Value, error) {
	_, metrics, err := realm.imageMetrics(context, this)
	if err != nil {
		return memory.Value{}, err
	}
	return memory.NumberValue(float64(metrics.NaturalWidth)), nil
}

func (realm *Realm) imageNaturalHeightGet(context *browserruntime.TaskContext, this memory.Value, _ []memory.Value) (memory.Value, error) {
	_, metrics, err := realm.imageMetrics(context, this)
	if err != nil {
		return memory.Value{}, err
	}
	return memory.NumberValue(float64(metrics.NaturalHeight)), nil
}

func (realm *Realm) imageCurrentSrcGet(context *browserruntime.TaskContext, this memory.Value, _ []memory.Value) (memory.Value, error) {
	_, metrics, err := realm.imageMetrics(context, this)
	if err != nil {
		return memory.Value{}, err
	}
	return newString(context, metrics.CurrentSrc)
}

func (realm *Realm) imageDimensionGet(vertical bool) browserruntime.NativeFunction {
	attr := "width"
	if vertical {
		attr = "height"
	}
	return func(context *browserruntime.TaskContext, this memory.Value, _ []memory.Value) (memory.Value, error) {
		handle, metrics, err := realm.imageMetrics(context, this)
		if err != nil {
			return memory.Value{}, err
		}
		if geomHost, ok := realm.host.(browser.DOMGeometryHost); ok {
			if geom, err := geomHost.ElementGeometry(handle); err == nil {
				dimension := geom.ClientWidth
				if vertical {
					dimension = geom.ClientHeight
				}
				if dimension > 0 {
					return memory.NumberValue(math.Round(dimension)), nil
				}
			}
		}
		if raw, found, err := realm.host.GetAttribute(handle, attr); err == nil && found {
			if parsed, ok := parseHTMLNonNegativeInt(raw); ok {
				return memory.NumberValue(float64(parsed)), nil
			}
		}
		natural := metrics.NaturalWidth
		if vertical {
			natural = metrics.NaturalHeight
		}
		return memory.NumberValue(float64(natural)), nil
	}
}

func (realm *Realm) imageDimensionSet(attr string) browserruntime.NativeFunction {
	return func(context *browserruntime.TaskContext, this memory.Value, arguments []memory.Value) (memory.Value, error) {
		handle, _, err := realm.imageMetrics(context, this)
		if err != nil {
			return memory.Value{}, err
		}
		value := argument(arguments, 0)
		var number float64
		switch value.Kind() {
		case memory.ValueNumber:
			number = value.Number()
		case memory.ValueBool:
			if value.Bool() {
				number = 1
			}
		default:
			text, strErr := valueString(context, value)
			if strErr != nil {
				return memory.Value{}, strErr
			}
			parsed, parseErr := strconv.ParseFloat(strings.TrimSpace(text), 64)
			if parseErr == nil {
				number = parsed
			}
		}
		var uintVal uint32
		if !math.IsNaN(number) && !math.IsInf(number, 0) {
			uintVal = uint32(int64(math.Trunc(number)))
		}
		return memory.UndefinedValue(), realm.host.SetAttribute(handle, attr, strconv.FormatUint(uint64(uintVal), 10))
	}
}

func parseHTMLNonNegativeInt(raw string) (int, bool) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return 0, false
	}
	parsed, err := strconv.Atoi(trimmed)
	if err != nil || parsed < 0 {
		return 0, false
	}
	return parsed, true
}
