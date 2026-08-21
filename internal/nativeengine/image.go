package nativeengine

import (
	browserruntime "github.com/JediWattson/gossamer/internal/runtime"
	"github.com/JediWattson/gossamer/internal/runtime/memory"
)

const nativeImageConstructor uint64 = 21_000

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
