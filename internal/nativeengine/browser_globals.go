package nativeengine

import (
	"fmt"

	"github.com/JediWattson/gossamer/internal/browser"
	"github.com/JediWattson/gossamer/internal/css"
	browserruntime "github.com/JediWattson/gossamer/internal/runtime"
	"github.com/JediWattson/gossamer/internal/runtime/memory"
)

const (
	nativeMatchMedia uint64 = 20_000 + iota
	nativeMediaQueryNoop
	nativeMediaQueryDispatch
)

func (realm *Realm) newNavigator(context *browserruntime.TaskContext) (memory.Ref, error) {
	navigator, err := context.NewHeapObject()
	if err != nil {
		return memory.Ref{}, err
	}
	languages, err := context.NewArray(1)
	if err != nil {
		return memory.Ref{}, err
	}
	language, err := newString(context, "en-US")
	if err != nil {
		return memory.Ref{}, err
	}
	if err := context.SetArrayElement(languages, 0, language); err != nil {
		return memory.Ref{}, err
	}
	userAgent, err := newString(context, "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Gossamer/0.1")
	if err != nil {
		return memory.Ref{}, err
	}
	appVersion, err := newString(context, "5.0 (Macintosh; Intel Mac OS X 10_15_7) Gossamer/0.1")
	if err != nil {
		return memory.Ref{}, err
	}
	platform, err := newString(context, "MacIntel")
	if err != nil {
		return memory.Ref{}, err
	}
	vendor, err := newString(context, "")
	if err != nil {
		return memory.Ref{}, err
	}
	properties := []struct {
		name  string
		value memory.Value
	}{
		{"userAgent", userAgent},
		{"appVersion", appVersion},
		{"platform", platform},
		{"vendor", vendor},
		{"language", language},
		{"languages", memory.RefValue(languages)},
		{"hardwareConcurrency", memory.NumberValue(4)},
		{"deviceMemory", memory.NumberValue(8)},
		{"maxTouchPoints", memory.NumberValue(0)},
		{"onLine", memory.BoolValue(true)},
		{"cookieEnabled", memory.BoolValue(true)},
		{"standalone", memory.BoolValue(false)},
	}
	for _, property := range properties {
		if err := defineData(context, navigator, property.name, property.value, false, true, true); err != nil {
			return memory.Ref{}, err
		}
	}
	return navigator, nil
}

func (realm *Realm) matchMedia(context *browserruntime.TaskContext, _ memory.Value, arguments []memory.Value) (memory.Value, error) {
	query, err := valueString(context, argument(arguments, 0))
	if err != nil {
		return memory.Value{}, err
	}
	host, ok := realm.host.(browser.DOMGeometryHost)
	if !ok {
		return memory.Value{}, fmt.Errorf("nativeengine: browser host does not expose viewport geometry")
	}
	geometry, err := host.ViewportGeometry()
	if err != nil {
		return memory.Value{}, err
	}
	matches := css.MediaQueryListMatches(query, css.MediaEnvironment{
		Type:            "screen",
		Width:           geometry.InnerWidth,
		Height:          geometry.InnerHeight,
		InitialFontSize: 16,
	})
	object, err := context.NewHeapObject()
	if err != nil {
		return memory.Value{}, err
	}
	media, err := newString(context, query)
	if err != nil {
		return memory.Value{}, err
	}
	noop, err := realm.newNativeFunction(context, "addEventListener", 2, nativeMediaQueryNoop)
	if err != nil {
		return memory.Value{}, err
	}
	dispatch, err := realm.newNativeFunction(context, "dispatchEvent", 1, nativeMediaQueryDispatch)
	if err != nil {
		return memory.Value{}, err
	}
	for _, property := range []struct {
		name  string
		value memory.Value
	}{
		{"matches", memory.BoolValue(matches)}, {"media", media}, {"onchange", memory.NullValue()},
		{"addListener", memory.RefValue(noop)}, {"removeListener", memory.RefValue(noop)},
		{"addEventListener", memory.RefValue(noop)}, {"removeEventListener", memory.RefValue(noop)},
		{"dispatchEvent", memory.RefValue(dispatch)},
	} {
		if err := defineData(context, object, property.name, property.value, property.name == "onchange", true, true); err != nil {
			return memory.Value{}, err
		}
	}
	return memory.RefValue(object), nil
}

func (realm *Realm) mediaQueryNoop(_ *browserruntime.TaskContext, _ memory.Value, _ []memory.Value) (memory.Value, error) {
	return memory.UndefinedValue(), nil
}

func (realm *Realm) mediaQueryDispatch(_ *browserruntime.TaskContext, _ memory.Value, _ []memory.Value) (memory.Value, error) {
	return memory.BoolValue(false), nil
}
