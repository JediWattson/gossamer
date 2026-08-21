package nativeengine

import (
	"fmt"

	browserruntime "github.com/JediWattson/gossamer/internal/runtime"
	"github.com/JediWattson/gossamer/internal/runtime/memory"
	"github.com/JediWattson/gossamer/internal/webapi"
)

const (
	nativeURLConstructor uint64 = 19_000 + iota
	nativeURLCanParse
	nativeURLToString
	nativeURLToJSON
	nativeURLHrefGet
	nativeURLHrefSet
	nativeURLOrigin
	nativeURLProtocolGet
	nativeURLProtocolSet
	nativeURLUsernameGet
	nativeURLUsernameSet
	nativeURLPasswordGet
	nativeURLPasswordSet
	nativeURLHostGet
	nativeURLHostSet
	nativeURLHostnameGet
	nativeURLHostnameSet
	nativeURLPortGet
	nativeURLPortSet
	nativeURLPathnameGet
	nativeURLPathnameSet
	nativeURLSearchGet
	nativeURLSearchSet
	nativeURLSearchParams
	nativeURLHashGet
	nativeURLHashSet
)

const (
	bindingURLPrototype   = "\x00gossamer.url.prototype"
	bindingURLConstructor = "\x00gossamer.url.constructor"
	urlDataProperty       = "\x00gossamer.url.data"
	urlSearchParamsCache  = "\x00gossamer.url.search-params"
)

func (realm *Realm) newURLConstructor(context *browserruntime.TaskContext) (memory.Ref, memory.Ref, error) {
	name, err := newString(context, "URL")
	if err != nil {
		return memory.Ref{}, memory.Ref{}, err
	}
	constructor, err := context.NewNativeConstructor(name, memory.RefValue(realm.active.Global), 1, nativeURLConstructor)
	if err != nil {
		return memory.Ref{}, memory.Ref{}, err
	}
	prototype, err := constructorPrototype(context, constructor, "URL")
	if err != nil {
		return memory.Ref{}, memory.Ref{}, err
	}
	if err := context.SetPrototype(prototype, memory.RefValue(realm.active.ObjectPrototype)); err != nil {
		return memory.Ref{}, memory.Ref{}, err
	}
	for _, method := range []struct {
		target memory.Ref
		name   string
		arity  uint32
		id     uint64
	}{
		{prototype, "toString", 0, nativeURLToString}, {prototype, "toJSON", 0, nativeURLToJSON},
		{constructor, "canParse", 1, nativeURLCanParse},
	} {
		function, err := realm.newNativeFunction(context, method.name, method.arity, method.id)
		if err != nil {
			return memory.Ref{}, memory.Ref{}, err
		}
		if err := defineData(context, method.target, method.name, memory.RefValue(function), true, false, true); err != nil {
			return memory.Ref{}, memory.Ref{}, err
		}
	}
	for _, accessor := range []struct {
		name   string
		getter uint64
		setter uint64
	}{
		{"href", nativeURLHrefGet, nativeURLHrefSet}, {"origin", nativeURLOrigin, 0},
		{"protocol", nativeURLProtocolGet, nativeURLProtocolSet}, {"username", nativeURLUsernameGet, nativeURLUsernameSet},
		{"password", nativeURLPasswordGet, nativeURLPasswordSet}, {"host", nativeURLHostGet, nativeURLHostSet},
		{"hostname", nativeURLHostnameGet, nativeURLHostnameSet}, {"port", nativeURLPortGet, nativeURLPortSet},
		{"pathname", nativeURLPathnameGet, nativeURLPathnameSet}, {"search", nativeURLSearchGet, nativeURLSearchSet},
		{"searchParams", nativeURLSearchParams, 0}, {"hash", nativeURLHashGet, nativeURLHashSet},
	} {
		getter, err := realm.newAccessorFunction(context, "get "+accessor.name, accessor.getter, 0)
		if err != nil {
			return memory.Ref{}, memory.Ref{}, err
		}
		setter := memory.UndefinedValue()
		if accessor.setter != 0 {
			function, err := realm.newAccessorFunction(context, "set "+accessor.name, accessor.setter, 1)
			if err != nil {
				return memory.Ref{}, memory.Ref{}, err
			}
			setter = memory.RefValue(function)
		}
		if err := defineAccessor(context, prototype, accessor.name, memory.RefValue(getter), setter); err != nil {
			return memory.Ref{}, memory.Ref{}, err
		}
	}
	tag, err := newString(context, "URL")
	if err != nil {
		return memory.Ref{}, memory.Ref{}, err
	}
	if err := context.DefineProperty(prototype, realm.active.SymbolToStringTag, memory.DataProperty(tag, false, false, true)); err != nil {
		return memory.Ref{}, memory.Ref{}, err
	}
	return constructor, prototype, nil
}

func (realm *Realm) urlConstructor(context *browserruntime.TaskContext, this memory.Value, arguments []memory.Value) (memory.Value, error) {
	if !this.IsRef() {
		return memory.Value{}, fmt.Errorf("%w: URL constructor requires new", browserruntime.ErrOperandType)
	}
	input, err := valueString(context, argument(arguments, 0))
	if err != nil {
		return memory.Value{}, err
	}
	var base *string
	if argument(arguments, 1).Kind() != memory.ValueUndefined {
		value, err := valueString(context, argument(arguments, 1))
		if err != nil {
			return memory.Value{}, err
		}
		base = &value
	}
	parsed, err := webapi.ParseURL(input, base)
	if err != nil {
		return memory.Value{}, fmt.Errorf("%w: %v", browserruntime.ErrOperandType, err)
	}
	return memory.UndefinedValue(), setURLState(context, this.Ref(), parsed)
}

func (realm *Realm) urlCanParse(context *browserruntime.TaskContext, _ memory.Value, arguments []memory.Value) (memory.Value, error) {
	input, err := valueString(context, argument(arguments, 0))
	if err != nil {
		return memory.BoolValue(false), nil
	}
	var base *string
	if argument(arguments, 1).Kind() != memory.ValueUndefined {
		value, err := valueString(context, argument(arguments, 1))
		if err != nil {
			return memory.BoolValue(false), nil
		}
		base = &value
	}
	_, err = webapi.ParseURL(input, base)
	return memory.BoolValue(err == nil), nil
}

func urlState(context *browserruntime.TaskContext, this memory.Value) (webapi.URL, memory.Ref, error) {
	if !this.IsRef() {
		return webapi.URL{}, memory.Ref{}, fmt.Errorf("%w: incompatible URL receiver", browserruntime.ErrOperandType)
	}
	name, err := context.NewString(urlDataProperty)
	if err != nil {
		return webapi.URL{}, memory.Ref{}, err
	}
	value, found, err := context.GetOwnProperty(this.Ref(), name)
	if err != nil || !found || !value.IsRef() {
		return webapi.URL{}, memory.Ref{}, fmt.Errorf("%w: incompatible URL receiver", browserruntime.ErrOperandType)
	}
	href, err := context.DerefString(value.Ref())
	if err != nil {
		return webapi.URL{}, memory.Ref{}, err
	}
	parsed, err := webapi.ParseURL(href, nil)
	return parsed, this.Ref(), err
}

func setURLState(context *browserruntime.TaskContext, object memory.Ref, value webapi.URL) error {
	href, err := newString(context, value.String())
	if err != nil {
		return err
	}
	name, err := context.NewString(urlDataProperty)
	if err != nil {
		return err
	}
	if _, found, err := context.GetOwnProperty(object, name); err != nil {
		return err
	} else if found {
		return context.SetProperty(object, name, href)
	}
	return context.DefineProperty(object, name, memory.DataProperty(href, true, false, false))
}

func (realm *Realm) urlToString(context *browserruntime.TaskContext, this memory.Value, _ []memory.Value) (memory.Value, error) {
	value, _, err := urlState(context, this)
	if err != nil {
		return memory.Value{}, err
	}
	return newString(context, value.String())
}

func (realm *Realm) urlToJSON(context *browserruntime.TaskContext, this memory.Value, arguments []memory.Value) (memory.Value, error) {
	return realm.urlToString(context, this, arguments)
}

func urlStringComponent(context *browserruntime.TaskContext, this memory.Value, component func(webapi.URL) string) (memory.Value, error) {
	value, _, err := urlState(context, this)
	if err != nil {
		return memory.Value{}, err
	}
	return newString(context, component(value))
}

func urlSetComponent(context *browserruntime.TaskContext, this memory.Value, arguments []memory.Value, setter func(*webapi.URL, string) error) (memory.Value, error) {
	value, object, err := urlState(context, this)
	if err != nil {
		return memory.Value{}, err
	}
	text, err := valueString(context, argument(arguments, 0))
	if err != nil {
		return memory.Value{}, err
	}
	if err := setter(&value, text); err != nil {
		return memory.Value{}, err
	}
	return memory.UndefinedValue(), setURLState(context, object, value)
}

func (realm *Realm) urlHrefGet(context *browserruntime.TaskContext, this memory.Value, _ []memory.Value) (memory.Value, error) {
	return urlStringComponent(context, this, webapi.URL.Href)
}
func (realm *Realm) urlHrefSet(context *browserruntime.TaskContext, this memory.Value, arguments []memory.Value) (memory.Value, error) {
	result, err := urlSetComponent(context, this, arguments, func(value *webapi.URL, text string) error { return value.SetHref(text) })
	if err != nil {
		return memory.Value{}, err
	}
	if err := syncURLSearchParamsCache(context, this.Ref()); err != nil {
		return memory.Value{}, err
	}
	return result, nil
}
func (realm *Realm) urlOrigin(context *browserruntime.TaskContext, this memory.Value, _ []memory.Value) (memory.Value, error) {
	return urlStringComponent(context, this, webapi.URL.Origin)
}
func (realm *Realm) urlProtocolGet(context *browserruntime.TaskContext, this memory.Value, _ []memory.Value) (memory.Value, error) {
	return urlStringComponent(context, this, webapi.URL.Protocol)
}
func (realm *Realm) urlProtocolSet(context *browserruntime.TaskContext, this memory.Value, arguments []memory.Value) (memory.Value, error) {
	return urlSetComponent(context, this, arguments, func(value *webapi.URL, text string) error { value.SetProtocol(text); return nil })
}
func (realm *Realm) urlUsernameGet(context *browserruntime.TaskContext, this memory.Value, _ []memory.Value) (memory.Value, error) {
	return urlStringComponent(context, this, webapi.URL.Username)
}
func (realm *Realm) urlUsernameSet(context *browserruntime.TaskContext, this memory.Value, arguments []memory.Value) (memory.Value, error) {
	return urlSetComponent(context, this, arguments, func(value *webapi.URL, text string) error { value.SetUsername(text); return nil })
}
func (realm *Realm) urlPasswordGet(context *browserruntime.TaskContext, this memory.Value, _ []memory.Value) (memory.Value, error) {
	return urlStringComponent(context, this, webapi.URL.Password)
}
func (realm *Realm) urlPasswordSet(context *browserruntime.TaskContext, this memory.Value, arguments []memory.Value) (memory.Value, error) {
	return urlSetComponent(context, this, arguments, func(value *webapi.URL, text string) error { value.SetPassword(text); return nil })
}
func (realm *Realm) urlHostGet(context *browserruntime.TaskContext, this memory.Value, _ []memory.Value) (memory.Value, error) {
	return urlStringComponent(context, this, webapi.URL.Host)
}
func (realm *Realm) urlHostSet(context *browserruntime.TaskContext, this memory.Value, arguments []memory.Value) (memory.Value, error) {
	return urlSetComponent(context, this, arguments, func(value *webapi.URL, text string) error { value.SetHost(text); return nil })
}
func (realm *Realm) urlHostnameGet(context *browserruntime.TaskContext, this memory.Value, _ []memory.Value) (memory.Value, error) {
	return urlStringComponent(context, this, webapi.URL.Hostname)
}
func (realm *Realm) urlHostnameSet(context *browserruntime.TaskContext, this memory.Value, arguments []memory.Value) (memory.Value, error) {
	return urlSetComponent(context, this, arguments, func(value *webapi.URL, text string) error { value.SetHostname(text); return nil })
}
func (realm *Realm) urlPortGet(context *browserruntime.TaskContext, this memory.Value, _ []memory.Value) (memory.Value, error) {
	return urlStringComponent(context, this, webapi.URL.Port)
}
func (realm *Realm) urlPortSet(context *browserruntime.TaskContext, this memory.Value, arguments []memory.Value) (memory.Value, error) {
	return urlSetComponent(context, this, arguments, func(value *webapi.URL, text string) error { value.SetPort(text); return nil })
}
func (realm *Realm) urlPathnameGet(context *browserruntime.TaskContext, this memory.Value, _ []memory.Value) (memory.Value, error) {
	return urlStringComponent(context, this, webapi.URL.Pathname)
}
func (realm *Realm) urlPathnameSet(context *browserruntime.TaskContext, this memory.Value, arguments []memory.Value) (memory.Value, error) {
	return urlSetComponent(context, this, arguments, func(value *webapi.URL, text string) error { value.SetPathname(text); return nil })
}
func (realm *Realm) urlSearchGet(context *browserruntime.TaskContext, this memory.Value, _ []memory.Value) (memory.Value, error) {
	return urlStringComponent(context, this, webapi.URL.Search)
}
func (realm *Realm) urlSearchSet(context *browserruntime.TaskContext, this memory.Value, arguments []memory.Value) (memory.Value, error) {
	result, err := urlSetComponent(context, this, arguments, func(value *webapi.URL, text string) error { value.SetSearch(text); return nil })
	if err != nil {
		return memory.Value{}, err
	}
	if err := syncURLSearchParamsCache(context, this.Ref()); err != nil {
		return memory.Value{}, err
	}
	return result, nil
}
func (realm *Realm) urlHashGet(context *browserruntime.TaskContext, this memory.Value, _ []memory.Value) (memory.Value, error) {
	return urlStringComponent(context, this, webapi.URL.Hash)
}
func (realm *Realm) urlHashSet(context *browserruntime.TaskContext, this memory.Value, arguments []memory.Value) (memory.Value, error) {
	return urlSetComponent(context, this, arguments, func(value *webapi.URL, text string) error { value.SetHash(text); return nil })
}

func (realm *Realm) urlSearchParams(context *browserruntime.TaskContext, this memory.Value, _ []memory.Value) (memory.Value, error) {
	value, urlObject, err := urlState(context, this)
	if err != nil {
		return memory.Value{}, err
	}
	cacheName, err := context.NewString(urlSearchParamsCache)
	if err != nil {
		return memory.Value{}, err
	}
	if cached, found, err := context.GetOwnProperty(urlObject, cacheName); err != nil {
		return memory.Value{}, err
	} else if found {
		return cached, nil
	}
	object, err := context.NewHeapObject()
	if err != nil {
		return memory.Value{}, err
	}
	if err := context.SetPrototype(object, memory.RefValue(realm.bindings.urlSearchParamsPrototype)); err != nil {
		return memory.Value{}, err
	}
	if err := setURLSearchParamsState(context, object, value.SearchParams()); err != nil {
		return memory.Value{}, err
	}
	ownerName, err := context.NewString(urlSearchParamsOwnerURLProperty)
	if err != nil {
		return memory.Value{}, err
	}
	if err := context.DefineProperty(object, ownerName, memory.DataProperty(memory.RefValue(urlObject), false, false, false)); err != nil {
		return memory.Value{}, err
	}
	if err := context.DefineProperty(urlObject, cacheName, memory.DataProperty(memory.RefValue(object), false, false, false)); err != nil {
		return memory.Value{}, err
	}
	return memory.RefValue(object), nil
}

func syncURLSearchParamsCache(context *browserruntime.TaskContext, urlObject memory.Ref) error {
	cacheName, err := context.NewString(urlSearchParamsCache)
	if err != nil {
		return err
	}
	cached, found, err := context.GetOwnProperty(urlObject, cacheName)
	if err != nil || !found || !cached.IsRef() {
		return err
	}
	value, _, err := urlState(context, memory.RefValue(urlObject))
	if err != nil {
		return err
	}
	data, err := newString(context, value.SearchParams().String())
	if err != nil {
		return err
	}
	dataName, err := context.NewString(urlSearchParamsDataProperty)
	if err != nil {
		return err
	}
	return context.SetProperty(cached.Ref(), dataName, data)
}
