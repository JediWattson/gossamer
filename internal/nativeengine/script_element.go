package nativeengine

import (
	"github.com/JediWattson/gossamer/internal/runtime"
	"github.com/JediWattson/gossamer/internal/runtime/memory"
)

func (realm *Realm) scriptSourceGet(context *runtime.TaskContext, this memory.Value, _ []memory.Value) (memory.Value, error) {
	handle, err := realm.unwrapNode(context, this)
	if err != nil {
		return memory.Value{}, err
	}
	value, _, err := realm.host.GetAttribute(handle, "src")
	if err != nil {
		return memory.Value{}, err
	}
	return newString(context, value)
}

func (realm *Realm) scriptSourceSet(context *runtime.TaskContext, this memory.Value, arguments []memory.Value) (memory.Value, error) {
	handle, err := realm.unwrapNode(context, this)
	if err != nil {
		return memory.Value{}, err
	}
	value, err := stringArgument(context, arguments, 0)
	if err != nil {
		return memory.Value{}, err
	}
	return memory.UndefinedValue(), realm.host.SetAttribute(handle, "src", value)
}

func (realm *Realm) scriptAsyncGet(context *runtime.TaskContext, this memory.Value, _ []memory.Value) (memory.Value, error) {
	handle, err := realm.unwrapNode(context, this)
	if err != nil {
		return memory.Value{}, err
	}
	_, found, err := realm.host.GetAttribute(handle, "async")
	return memory.BoolValue(found), err
}

func (realm *Realm) scriptAsyncSet(context *runtime.TaskContext, this memory.Value, arguments []memory.Value) (memory.Value, error) {
	handle, err := realm.unwrapNode(context, this)
	if err != nil {
		return memory.Value{}, err
	}
	if truthy(argument(arguments, 0)) {
		return memory.UndefinedValue(), realm.host.SetAttribute(handle, "async", "")
	}
	return memory.UndefinedValue(), realm.host.RemoveAttribute(handle, "async")
}
