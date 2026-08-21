package nativeengine

import (
	"fmt"
	"math"

	"github.com/JediWattson/gossamer/internal/browser"
	browserruntime "github.com/JediWattson/gossamer/internal/runtime"
	"github.com/JediWattson/gossamer/internal/runtime/memory"
)

const (
	nativeStorageGetItem uint64 = 23_000 + iota
	nativeStorageSetItem
	nativeStorageRemoveItem
	nativeStorageClear
	nativeStorageKey
	nativeStorageLength
	nativeDocumentCookieGet
	nativeDocumentCookieSet
)

const storageAreaProperty = "\x00gossamer.storage.area"

func (realm *Realm) newStorageBindings(context *browserruntime.TaskContext) (memory.Ref, memory.Ref, memory.Ref, memory.Ref, error) {
	constructor, err := realm.newDOMInterfaceConstructor(context, "Storage", realm.active.ObjectPrototype)
	if err != nil {
		return memory.Ref{}, memory.Ref{}, memory.Ref{}, memory.Ref{}, err
	}
	prototype, err := constructorPrototype(context, constructor, "Storage")
	if err != nil {
		return memory.Ref{}, memory.Ref{}, memory.Ref{}, memory.Ref{}, err
	}
	for _, method := range []struct {
		name  string
		arity uint32
		id    uint64
	}{{"getItem", 1, nativeStorageGetItem}, {"setItem", 2, nativeStorageSetItem}, {"removeItem", 1, nativeStorageRemoveItem}, {"clear", 0, nativeStorageClear}, {"key", 1, nativeStorageKey}} {
		function, err := realm.newNativeFunction(context, method.name, method.arity, method.id)
		if err != nil {
			return memory.Ref{}, memory.Ref{}, memory.Ref{}, memory.Ref{}, err
		}
		if err := defineData(context, prototype, method.name, memory.RefValue(function), true, false, true); err != nil {
			return memory.Ref{}, memory.Ref{}, memory.Ref{}, memory.Ref{}, err
		}
	}
	length, err := realm.newAccessorFunction(context, "get length", nativeStorageLength, 0)
	if err != nil {
		return memory.Ref{}, memory.Ref{}, memory.Ref{}, memory.Ref{}, err
	}
	if err := defineAccessor(context, prototype, "length", memory.RefValue(length), memory.UndefinedValue()); err != nil {
		return memory.Ref{}, memory.Ref{}, memory.Ref{}, memory.Ref{}, err
	}
	create := func(area browser.StorageArea) (memory.Ref, error) {
		object, err := context.NewHeapObject()
		if err != nil {
			return memory.Ref{}, err
		}
		if err := context.SetPrototype(object, memory.RefValue(prototype)); err != nil {
			return memory.Ref{}, err
		}
		return object, defineData(context, object, storageAreaProperty, memory.NumberValue(float64(area)), false, false, false)
	}
	local, err := create(browser.LocalStorage)
	if err != nil {
		return memory.Ref{}, memory.Ref{}, memory.Ref{}, memory.Ref{}, err
	}
	session, err := create(browser.SessionStorage)
	if err != nil {
		return memory.Ref{}, memory.Ref{}, memory.Ref{}, memory.Ref{}, err
	}
	return constructor, prototype, local, session, nil
}

func (realm *Realm) installStorageDocumentCookie(context *browserruntime.TaskContext) error {
	getter, err := realm.newAccessorFunction(context, "get cookie", nativeDocumentCookieGet, 0)
	if err != nil {
		return err
	}
	setter, err := realm.newAccessorFunction(context, "set cookie", nativeDocumentCookieSet, 1)
	if err != nil {
		return err
	}
	return defineAccessor(context, realm.bindings.documentPrototype, "cookie", memory.RefValue(getter), memory.RefValue(setter))
}

func (realm *Realm) storageGetItem(context *browserruntime.TaskContext, this memory.Value, arguments []memory.Value) (memory.Value, error) {
	host, area, err := realm.storageHost(context, this)
	if err != nil {
		return memory.Value{}, err
	}
	key, err := stringArgument(context, arguments, 0)
	if err != nil {
		return memory.Value{}, err
	}
	value, found, err := host.StorageGet(area, key)
	if err != nil {
		return memory.Value{}, err
	}
	if !found {
		return memory.NullValue(), nil
	}
	return newString(context, value)
}

func (realm *Realm) storageSetItem(context *browserruntime.TaskContext, this memory.Value, arguments []memory.Value) (memory.Value, error) {
	host, area, err := realm.storageHost(context, this)
	if err != nil {
		return memory.Value{}, err
	}
	key, err := stringArgument(context, arguments, 0)
	if err != nil {
		return memory.Value{}, err
	}
	value, err := stringArgument(context, arguments, 1)
	if err != nil {
		return memory.Value{}, err
	}
	return memory.UndefinedValue(), host.StorageSet(area, key, value)
}

func (realm *Realm) storageRemoveItem(context *browserruntime.TaskContext, this memory.Value, arguments []memory.Value) (memory.Value, error) {
	host, area, err := realm.storageHost(context, this)
	if err != nil {
		return memory.Value{}, err
	}
	key, err := stringArgument(context, arguments, 0)
	if err != nil {
		return memory.Value{}, err
	}
	return memory.UndefinedValue(), host.StorageRemove(area, key)
}

func (realm *Realm) storageClear(context *browserruntime.TaskContext, this memory.Value, _ []memory.Value) (memory.Value, error) {
	host, area, err := realm.storageHost(context, this)
	if err != nil {
		return memory.Value{}, err
	}
	return memory.UndefinedValue(), host.StorageClear(area)
}

func (realm *Realm) storageKey(context *browserruntime.TaskContext, this memory.Value, arguments []memory.Value) (memory.Value, error) {
	host, area, err := realm.storageHost(context, this)
	if err != nil {
		return memory.Value{}, err
	}
	indexValue := argument(arguments, 0)
	if indexValue.Kind() != memory.ValueNumber || math.IsNaN(indexValue.Number()) || indexValue.Number() < 0 {
		return memory.NullValue(), nil
	}
	key, found, err := host.StorageKey(area, int(indexValue.Number()))
	if err != nil {
		return memory.Value{}, err
	}
	if !found {
		return memory.NullValue(), nil
	}
	return newString(context, key)
}

func (realm *Realm) storageLength(context *browserruntime.TaskContext, this memory.Value, _ []memory.Value) (memory.Value, error) {
	host, area, err := realm.storageHost(context, this)
	if err != nil {
		return memory.Value{}, err
	}
	length, err := host.StorageLength(area)
	return memory.NumberValue(float64(length)), err
}

func (realm *Realm) documentCookieGet(context *browserruntime.TaskContext, _ memory.Value, _ []memory.Value) (memory.Value, error) {
	host, ok := realm.host.(browser.StorageHost)
	if !ok {
		return memory.Value{}, fmt.Errorf("nativeengine: browser host does not expose document cookies")
	}
	value, err := host.DocumentCookie()
	if err != nil {
		return memory.Value{}, err
	}
	return newString(context, value)
}

func (realm *Realm) documentCookieSet(context *browserruntime.TaskContext, _ memory.Value, arguments []memory.Value) (memory.Value, error) {
	host, ok := realm.host.(browser.StorageHost)
	if !ok {
		return memory.Value{}, fmt.Errorf("nativeengine: browser host does not expose document cookies")
	}
	value, err := stringArgument(context, arguments, 0)
	if err != nil {
		return memory.Value{}, err
	}
	return memory.UndefinedValue(), host.SetDocumentCookie(value)
}

func (realm *Realm) storageHost(context *browserruntime.TaskContext, this memory.Value) (browser.StorageHost, browser.StorageArea, error) {
	if !this.IsRef() {
		return nil, 0, fmt.Errorf("%w: incompatible Storage receiver", browserruntime.ErrOperandType)
	}
	value, found, err := ownValue(context, this.Ref(), storageAreaProperty)
	if err != nil || !found || value.Kind() != memory.ValueNumber {
		return nil, 0, fmt.Errorf("%w: incompatible Storage receiver", browserruntime.ErrOperandType)
	}
	host, ok := realm.host.(browser.StorageHost)
	if !ok {
		return nil, 0, fmt.Errorf("nativeengine: browser host does not expose storage")
	}
	area := browser.StorageArea(value.Number())
	if area != browser.LocalStorage && area != browser.SessionStorage {
		return nil, 0, fmt.Errorf("%w: incompatible Storage receiver", browserruntime.ErrOperandType)
	}
	return host, area, nil
}
