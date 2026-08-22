package nativeengine

import (
	"fmt"

	browserruntime "github.com/JediWattson/gossamer/internal/runtime"
	"github.com/JediWattson/gossamer/internal/runtime/memory"
	"github.com/JediWattson/gossamer/internal/webapi"
)

const (
	nativeURLSearchParamsConstructor uint64 = 16_000 + iota
	nativeURLSearchParamsAppend
	nativeURLSearchParamsDelete
	nativeURLSearchParamsGet
	nativeURLSearchParamsGetAll
	nativeURLSearchParamsHas
	nativeURLSearchParamsSet
	nativeURLSearchParamsSort
	nativeURLSearchParamsToString
	nativeURLSearchParamsKeys
	nativeURLSearchParamsValues
	nativeURLSearchParamsEntries
	nativeURLSearchParamsForEach
	nativeURLSearchParamsSize
)

const (
	bindingURLSearchParamsPrototype   = "\x00gossamer.url-search-params.prototype"
	bindingURLSearchParamsConstructor = "\x00gossamer.url-search-params.constructor"
	urlSearchParamsDataProperty       = "\x00gossamer.url-search-params.data"
	urlSearchParamsOwnerURLProperty   = "\x00gossamer.url-search-params.owner-url"
)

func (realm *Realm) newURLSearchParamsConstructor(context *browserruntime.TaskContext) (memory.Ref, memory.Ref, error) {
	name, err := newString(context, "URLSearchParams")
	if err != nil {
		return memory.Ref{}, memory.Ref{}, err
	}
	constructor, err := context.NewNativeConstructor(name, memory.RefValue(realm.active.Global), 1, nativeURLSearchParamsConstructor)
	if err != nil {
		return memory.Ref{}, memory.Ref{}, err
	}
	prototype, err := constructorPrototype(context, constructor, "URLSearchParams")
	if err != nil {
		return memory.Ref{}, memory.Ref{}, err
	}
	if err := context.SetPrototype(prototype, memory.RefValue(realm.active.ObjectPrototype)); err != nil {
		return memory.Ref{}, memory.Ref{}, err
	}
	methods := []struct {
		name  string
		arity uint32
		id    uint64
	}{
		{"append", 2, nativeURLSearchParamsAppend}, {"delete", 1, nativeURLSearchParamsDelete},
		{"get", 1, nativeURLSearchParamsGet}, {"getAll", 1, nativeURLSearchParamsGetAll},
		{"has", 1, nativeURLSearchParamsHas}, {"set", 2, nativeURLSearchParamsSet},
		{"sort", 0, nativeURLSearchParamsSort}, {"toString", 0, nativeURLSearchParamsToString},
		{"keys", 0, nativeURLSearchParamsKeys}, {"values", 0, nativeURLSearchParamsValues},
		{"entries", 0, nativeURLSearchParamsEntries}, {"forEach", 1, nativeURLSearchParamsForEach},
	}
	var entries memory.Ref
	for _, method := range methods {
		function, err := realm.newNativeFunction(context, method.name, method.arity, method.id)
		if err != nil {
			return memory.Ref{}, memory.Ref{}, err
		}
		if err := defineData(context, prototype, method.name, memory.RefValue(function), true, false, true); err != nil {
			return memory.Ref{}, memory.Ref{}, err
		}
		if method.id == nativeURLSearchParamsEntries {
			entries = function
		}
	}
	size, err := realm.newAccessorFunction(context, "get size", nativeURLSearchParamsSize, 0)
	if err != nil {
		return memory.Ref{}, memory.Ref{}, err
	}
	if err := defineAccessor(context, prototype, "size", memory.RefValue(size), memory.UndefinedValue()); err != nil {
		return memory.Ref{}, memory.Ref{}, err
	}
	if err := context.DefineProperty(prototype, realm.active.SymbolIterator, memory.DataProperty(memory.RefValue(entries), true, false, true)); err != nil {
		return memory.Ref{}, memory.Ref{}, err
	}
	tag, err := newString(context, "URLSearchParams")
	if err != nil {
		return memory.Ref{}, memory.Ref{}, err
	}
	if err := context.DefineProperty(prototype, realm.active.SymbolToStringTag, memory.DataProperty(tag, false, false, true)); err != nil {
		return memory.Ref{}, memory.Ref{}, err
	}
	return constructor, prototype, nil
}

func (realm *Realm) urlSearchParamsConstructor(context *browserruntime.TaskContext, this memory.Value, arguments []memory.Value) (memory.Value, error) {
	if !this.IsRef() {
		return memory.Value{}, fmt.Errorf("%w: URLSearchParams constructor requires new", browserruntime.ErrOperandType)
	}
	params, err := urlSearchParamsInitializer(context, firstArgument(arguments))
	if err != nil {
		return memory.Value{}, err
	}
	return memory.UndefinedValue(), setURLSearchParamsState(context, this.Ref(), params)
}

func urlSearchParamsInitializer(context *browserruntime.TaskContext, initial memory.Value) (webapi.URLSearchParams, error) {
	if initial.Kind() == memory.ValueUndefined {
		return webapi.URLSearchParams{}, nil
	}
	if initial.IsRef() {
		kind, err := context.HeapKind(initial.Ref())
		if err != nil {
			return webapi.URLSearchParams{}, err
		}
		if kind == memory.HeapObject {
			if params, ok, err := maybeURLSearchParamsState(context, initial.Ref()); err != nil {
				return webapi.URLSearchParams{}, err
			} else if ok {
				return params, nil
			}
		}
		if kind == memory.HeapArray {
			return urlSearchParamsFromSequence(context, initial.Ref())
		}
		if kind == memory.HeapObject {
			return urlSearchParamsFromRecord(context, initial.Ref())
		}
	}
	text, err := valueString(context, initial)
	if err != nil {
		return webapi.URLSearchParams{}, err
	}
	return webapi.ParseURLSearchParams(text), nil
}

func urlSearchParamsFromSequence(context *browserruntime.TaskContext, sequence memory.Ref) (webapi.URLSearchParams, error) {
	array, err := context.DerefArray(sequence)
	if err != nil {
		return webapi.URLSearchParams{}, err
	}
	pairs := make([]webapi.SearchParam, 0, len(array.Elements))
	for index := uint32(0); index < array.Length; index++ {
		entry, present, err := context.ArrayElement(sequence, index)
		if err != nil {
			return webapi.URLSearchParams{}, err
		}
		if !present || !entry.IsRef() {
			return webapi.URLSearchParams{}, fmt.Errorf("%w: URLSearchParams entry %d is not a pair", browserruntime.ErrOperandType, index)
		}
		kind, err := context.HeapKind(entry.Ref())
		if err != nil || kind != memory.HeapArray {
			return webapi.URLSearchParams{}, fmt.Errorf("%w: URLSearchParams entry %d is not a pair", browserruntime.ErrOperandType, index)
		}
		pair, err := context.DerefArray(entry.Ref())
		if err != nil || pair.Length != 2 {
			return webapi.URLSearchParams{}, fmt.Errorf("%w: URLSearchParams entry %d must contain two values", browserruntime.ErrOperandType, index)
		}
		nameValue, _, err := context.ArrayElement(entry.Ref(), 0)
		if err != nil {
			return webapi.URLSearchParams{}, err
		}
		valueValue, _, err := context.ArrayElement(entry.Ref(), 1)
		if err != nil {
			return webapi.URLSearchParams{}, err
		}
		name, err := valueString(context, nameValue)
		if err != nil {
			return webapi.URLSearchParams{}, err
		}
		value, err := valueString(context, valueValue)
		if err != nil {
			return webapi.URLSearchParams{}, err
		}
		pairs = append(pairs, webapi.SearchParam{Name: name, Value: value})
	}
	return webapi.NewURLSearchParams(pairs), nil
}

func urlSearchParamsFromRecord(context *browserruntime.TaskContext, record memory.Ref) (webapi.URLSearchParams, error) {
	header, err := context.DerefObjectHeader(record)
	if err != nil {
		return webapi.URLSearchParams{}, err
	}
	pairs := make([]webapi.SearchParam, 0, len(header.Properties))
	for _, property := range header.Properties {
		if !property.Enumerable || property.Kind != memory.PropertyData {
			continue
		}
		name, err := context.DerefString(property.Name)
		if err != nil {
			continue
		}
		value, err := valueString(context, property.Value)
		if err != nil {
			return webapi.URLSearchParams{}, err
		}
		pairs = append(pairs, webapi.SearchParam{Name: name, Value: value})
	}
	return webapi.NewURLSearchParams(pairs), nil
}

func firstArgument(arguments []memory.Value) memory.Value {
	if len(arguments) == 0 {
		return memory.UndefinedValue()
	}
	return arguments[0]
}

func optionalStringArgument(context *browserruntime.TaskContext, arguments []memory.Value, index int) (*string, error) {
	if index >= len(arguments) {
		return nil, nil
	}
	value, err := valueString(context, arguments[index])
	return &value, err
}

func maybeURLSearchParamsState(context *browserruntime.TaskContext, object memory.Ref) (webapi.URLSearchParams, bool, error) {
	name, err := context.NewString(urlSearchParamsDataProperty)
	if err != nil {
		return webapi.URLSearchParams{}, false, err
	}
	value, found, err := context.GetOwnProperty(object, name)
	if err != nil || !found || !value.IsRef() {
		return webapi.URLSearchParams{}, found, err
	}
	text, err := context.DerefString(value.Ref())
	if err != nil {
		return webapi.URLSearchParams{}, false, err
	}
	return webapi.ParseURLSearchParams(text), true, nil
}

func urlSearchParamsState(context *browserruntime.TaskContext, this memory.Value) (webapi.URLSearchParams, memory.Ref, error) {
	if !this.IsRef() {
		return webapi.URLSearchParams{}, memory.Ref{}, fmt.Errorf("%w: incompatible URLSearchParams receiver", browserruntime.ErrOperandType)
	}
	params, found, err := maybeURLSearchParamsState(context, this.Ref())
	if err != nil {
		return webapi.URLSearchParams{}, memory.Ref{}, err
	}
	if !found {
		return webapi.URLSearchParams{}, memory.Ref{}, fmt.Errorf("%w: incompatible URLSearchParams receiver", browserruntime.ErrOperandType)
	}
	return params, this.Ref(), nil
}

func setURLSearchParamsState(context *browserruntime.TaskContext, object memory.Ref, params webapi.URLSearchParams) error {
	value, err := newString(context, params.String())
	if err != nil {
		return err
	}
	name, err := context.NewString(urlSearchParamsDataProperty)
	if err != nil {
		return err
	}
	if _, found, err := context.GetOwnProperty(object, name); err != nil {
		return err
	} else if found {
		if err := context.SetProperty(object, name, value); err != nil {
			return err
		}
	} else if err := context.DefineProperty(object, name, memory.DataProperty(value, true, false, false)); err != nil {
		return err
	}
	ownerName, err := context.NewString(urlSearchParamsOwnerURLProperty)
	if err != nil {
		return err
	}
	owner, found, err := context.GetOwnProperty(object, ownerName)
	if err != nil || !found || !owner.IsRef() {
		return err
	}
	urlValue, _, err := urlState(context, owner)
	if err != nil {
		return err
	}
	urlValue.SetSearch(params.String())
	return setURLState(context, owner.Ref(), urlValue)
}

func (realm *Realm) urlSearchParamsAppend(context *browserruntime.TaskContext, this memory.Value, arguments []memory.Value) (memory.Value, error) {
	params, object, err := urlSearchParamsState(context, this)
	if err != nil {
		return memory.Value{}, err
	}
	name, err := valueString(context, firstArgument(arguments))
	if err != nil {
		return memory.Value{}, err
	}
	value := memory.UndefinedValue()
	if len(arguments) > 1 {
		value = arguments[1]
	}
	text, err := valueString(context, value)
	if err != nil {
		return memory.Value{}, err
	}
	params.Append(name, text)
	return memory.UndefinedValue(), setURLSearchParamsState(context, object, params)
}

func (realm *Realm) urlSearchParamsDelete(context *browserruntime.TaskContext, this memory.Value, arguments []memory.Value) (memory.Value, error) {
	params, object, err := urlSearchParamsState(context, this)
	if err != nil {
		return memory.Value{}, err
	}
	name, err := valueString(context, firstArgument(arguments))
	if err != nil {
		return memory.Value{}, err
	}
	value, err := optionalStringArgument(context, arguments, 1)
	if err != nil {
		return memory.Value{}, err
	}
	params.Delete(name, value)
	return memory.UndefinedValue(), setURLSearchParamsState(context, object, params)
}

func (realm *Realm) urlSearchParamsGet(context *browserruntime.TaskContext, this memory.Value, arguments []memory.Value) (memory.Value, error) {
	params, _, err := urlSearchParamsState(context, this)
	if err != nil {
		return memory.Value{}, err
	}
	name, err := valueString(context, firstArgument(arguments))
	if err != nil {
		return memory.Value{}, err
	}
	value, found := params.Get(name)
	if !found {
		return memory.NullValue(), nil
	}
	return newString(context, value)
}

func (realm *Realm) urlSearchParamsGetAll(context *browserruntime.TaskContext, this memory.Value, arguments []memory.Value) (memory.Value, error) {
	params, _, err := urlSearchParamsState(context, this)
	if err != nil {
		return memory.Value{}, err
	}
	name, err := valueString(context, firstArgument(arguments))
	if err != nil {
		return memory.Value{}, err
	}
	values := params.GetAll(name)
	array, err := context.NewArray(uint32(len(values)))
	if err != nil {
		return memory.Value{}, err
	}
	for index, value := range values {
		text, err := newString(context, value)
		if err != nil {
			return memory.Value{}, err
		}
		if err := context.SetArrayElement(array, uint32(index), text); err != nil {
			return memory.Value{}, err
		}
	}
	return memory.RefValue(array), nil
}

func (realm *Realm) urlSearchParamsHas(context *browserruntime.TaskContext, this memory.Value, arguments []memory.Value) (memory.Value, error) {
	params, _, err := urlSearchParamsState(context, this)
	if err != nil {
		return memory.Value{}, err
	}
	name, err := valueString(context, firstArgument(arguments))
	if err != nil {
		return memory.Value{}, err
	}
	value, err := optionalStringArgument(context, arguments, 1)
	if err != nil {
		return memory.Value{}, err
	}
	return memory.BoolValue(params.Has(name, value)), nil
}

func (realm *Realm) urlSearchParamsSet(context *browserruntime.TaskContext, this memory.Value, arguments []memory.Value) (memory.Value, error) {
	params, object, err := urlSearchParamsState(context, this)
	if err != nil {
		return memory.Value{}, err
	}
	name, err := valueString(context, firstArgument(arguments))
	if err != nil {
		return memory.Value{}, err
	}
	value := memory.UndefinedValue()
	if len(arguments) > 1 {
		value = arguments[1]
	}
	text, err := valueString(context, value)
	if err != nil {
		return memory.Value{}, err
	}
	params.Set(name, text)
	return memory.UndefinedValue(), setURLSearchParamsState(context, object, params)
}

func (realm *Realm) urlSearchParamsSort(context *browserruntime.TaskContext, this memory.Value, _ []memory.Value) (memory.Value, error) {
	params, object, err := urlSearchParamsState(context, this)
	if err != nil {
		return memory.Value{}, err
	}
	params.Sort()
	return memory.UndefinedValue(), setURLSearchParamsState(context, object, params)
}

func (realm *Realm) urlSearchParamsToString(context *browserruntime.TaskContext, this memory.Value, _ []memory.Value) (memory.Value, error) {
	params, _, err := urlSearchParamsState(context, this)
	if err != nil {
		return memory.Value{}, err
	}
	return newString(context, params.String())
}

func (realm *Realm) urlSearchParamsKeys(context *browserruntime.TaskContext, this memory.Value, _ []memory.Value) (memory.Value, error) {
	return realm.urlSearchParamsIterator(context, this, false, false)
}

func (realm *Realm) urlSearchParamsValues(context *browserruntime.TaskContext, this memory.Value, _ []memory.Value) (memory.Value, error) {
	return realm.urlSearchParamsIterator(context, this, true, false)
}

func (realm *Realm) urlSearchParamsEntries(context *browserruntime.TaskContext, this memory.Value, _ []memory.Value) (memory.Value, error) {
	return realm.urlSearchParamsIterator(context, this, false, true)
}

func (realm *Realm) urlSearchParamsIterator(context *browserruntime.TaskContext, this memory.Value, values, entries bool) (memory.Value, error) {
	params, _, err := urlSearchParamsState(context, this)
	if err != nil {
		return memory.Value{}, err
	}
	array, err := context.NewArray(uint32(len(params.Pairs)))
	if err != nil {
		return memory.Value{}, err
	}
	for index, pair := range params.Pairs {
		var item memory.Value
		if entries {
			entry, err := context.NewArray(2)
			if err != nil {
				return memory.Value{}, err
			}
			name, err := newString(context, pair.Name)
			if err != nil {
				return memory.Value{}, err
			}
			value, err := newString(context, pair.Value)
			if err != nil {
				return memory.Value{}, err
			}
			if err := context.SetArrayElement(entry, 0, name); err != nil {
				return memory.Value{}, err
			}
			if err := context.SetArrayElement(entry, 1, value); err != nil {
				return memory.Value{}, err
			}
			item = memory.RefValue(entry)
		} else if values {
			item, err = newString(context, pair.Value)
		} else {
			item, err = newString(context, pair.Name)
		}
		if err != nil {
			return memory.Value{}, err
		}
		if err := context.SetArrayElement(array, uint32(index), item); err != nil {
			return memory.Value{}, err
		}
	}
	iterator, err := context.NewIterator(array, memory.IteratorArrayValues)
	return memory.RefValue(iterator), err
}

func (realm *Realm) urlSearchParamsForEach(context *browserruntime.TaskContext, this memory.Value, arguments []memory.Value) (memory.Value, error) {
	params, _, err := urlSearchParamsState(context, this)
	if err != nil {
		return memory.Value{}, err
	}
	callback := firstArgument(arguments)
	if !callback.IsRef() {
		return memory.Value{}, fmt.Errorf("%w: URLSearchParams.forEach callback", browserruntime.ErrOperandType)
	}
	kind, err := context.HeapKind(callback.Ref())
	if err != nil || kind != memory.HeapFunction {
		return memory.Value{}, fmt.Errorf("%w: URLSearchParams.forEach callback", browserruntime.ErrOperandType)
	}
	callbackThis := memory.UndefinedValue()
	if len(arguments) > 1 {
		callbackThis = arguments[1]
	}
	for _, pair := range params.Pairs {
		name, err := newString(context, pair.Name)
		if err != nil {
			return memory.Value{}, err
		}
		value, err := newString(context, pair.Value)
		if err != nil {
			return memory.Value{}, err
		}
		if _, err := realm.interpreter.CallWithoutCheckpoint(context, callback.Ref(), callbackThis, value, name, this); err != nil {
			return memory.Value{}, err
		}
	}
	return memory.UndefinedValue(), nil
}

func (realm *Realm) urlSearchParamsSize(context *browserruntime.TaskContext, this memory.Value, _ []memory.Value) (memory.Value, error) {
	params, _, err := urlSearchParamsState(context, this)
	if err != nil {
		return memory.Value{}, err
	}
	return memory.NumberValue(float64(len(params.Pairs))), nil
}
