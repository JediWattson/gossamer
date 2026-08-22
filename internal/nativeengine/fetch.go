package nativeengine

import (
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"strings"

	"github.com/JediWattson/gossamer/internal/browser"
	browserruntime "github.com/JediWattson/gossamer/internal/runtime"
	"github.com/JediWattson/gossamer/internal/runtime/memory"
)

const (
	nativeGlobalFetch uint64 = 22_000 + iota
	nativeHeadersConstructor
	nativeHeadersAppend
	nativeHeadersDelete
	nativeHeadersGet
	nativeHeadersHas
	nativeHeadersSet
	nativeHeadersForEach
	nativeRequestConstructor
	nativeResponseConstructor
	nativeResponseText
	nativeResponseJSON
	nativeResponseArrayBuffer
	nativeResponseClone
)

const (
	headersStateProperty = "\x00gossamer.headers.state"
	requestBrandProperty = "\x00gossamer.request.brand"
	requestBodyProperty  = "\x00gossamer.request.body"
	responseBodyProperty = "\x00gossamer.response.body"
)

func (realm *Realm) newFetchConstructors(context *browserruntime.TaskContext) (
	memory.Ref, memory.Ref, memory.Ref, memory.Ref, memory.Ref, memory.Ref, error,
) {
	constructors := make([]memory.Ref, 3)
	prototypes := make([]memory.Ref, 3)
	for index, item := range []struct {
		name string
		id   uint64
	}{{"Headers", nativeHeadersConstructor}, {"Request", nativeRequestConstructor}, {"Response", nativeResponseConstructor}} {
		name, err := context.NewString(item.name)
		if err != nil {
			return memory.Ref{}, memory.Ref{}, memory.Ref{}, memory.Ref{}, memory.Ref{}, memory.Ref{}, err
		}
		constructor, err := context.NewNativeConstructor(memory.RefValue(name), memory.RefValue(realm.active.Global), 1, item.id)
		if err != nil {
			return memory.Ref{}, memory.Ref{}, memory.Ref{}, memory.Ref{}, memory.Ref{}, memory.Ref{}, err
		}
		prototype, err := constructorPrototype(context, constructor, item.name)
		if err != nil {
			return memory.Ref{}, memory.Ref{}, memory.Ref{}, memory.Ref{}, memory.Ref{}, memory.Ref{}, err
		}
		if err := context.SetPrototype(prototype, memory.RefValue(realm.active.ObjectPrototype)); err != nil {
			return memory.Ref{}, memory.Ref{}, memory.Ref{}, memory.Ref{}, memory.Ref{}, memory.Ref{}, err
		}
		constructors[index], prototypes[index] = constructor, prototype
	}
	for _, method := range []struct {
		target memory.Ref
		name   string
		arity  uint32
		id     uint64
	}{
		{prototypes[0], "append", 2, nativeHeadersAppend}, {prototypes[0], "delete", 1, nativeHeadersDelete},
		{prototypes[0], "get", 1, nativeHeadersGet}, {prototypes[0], "has", 1, nativeHeadersHas},
		{prototypes[0], "set", 2, nativeHeadersSet}, {prototypes[0], "forEach", 1, nativeHeadersForEach},
		{prototypes[2], "text", 0, nativeResponseText}, {prototypes[2], "json", 0, nativeResponseJSON},
		{prototypes[2], "arrayBuffer", 0, nativeResponseArrayBuffer}, {prototypes[2], "clone", 0, nativeResponseClone},
	} {
		function, err := realm.newNativeFunction(context, method.name, method.arity, method.id)
		if err != nil {
			return memory.Ref{}, memory.Ref{}, memory.Ref{}, memory.Ref{}, memory.Ref{}, memory.Ref{}, err
		}
		if err := defineData(context, method.target, method.name, memory.RefValue(function), true, false, true); err != nil {
			return memory.Ref{}, memory.Ref{}, memory.Ref{}, memory.Ref{}, memory.Ref{}, memory.Ref{}, err
		}
	}
	for index, tag := range []string{"Headers", "Request", "Response"} {
		value, err := newString(context, tag)
		if err != nil {
			return memory.Ref{}, memory.Ref{}, memory.Ref{}, memory.Ref{}, memory.Ref{}, memory.Ref{}, err
		}
		if err := context.DefineProperty(prototypes[index], realm.active.SymbolToStringTag, memory.DataProperty(value, false, false, true)); err != nil {
			return memory.Ref{}, memory.Ref{}, memory.Ref{}, memory.Ref{}, memory.Ref{}, memory.Ref{}, err
		}
	}
	return constructors[0], prototypes[0], constructors[1], prototypes[1], constructors[2], prototypes[2], nil
}

func (realm *Realm) headersConstructor(context *browserruntime.TaskContext, this memory.Value, arguments []memory.Value) (memory.Value, error) {
	if !this.IsRef() {
		return memory.Value{}, fmt.Errorf("%w: Headers constructor requires new", browserruntime.ErrOperandType)
	}
	header, err := readHeadersInit(context, argument(arguments, 0))
	if err != nil {
		return memory.Value{}, err
	}
	return memory.UndefinedValue(), writeHeaders(context, this.Ref(), header)
}

func (realm *Realm) headersAppend(context *browserruntime.TaskContext, this memory.Value, arguments []memory.Value) (memory.Value, error) {
	header, object, err := requireHeaders(context, this)
	if err != nil {
		return memory.Value{}, err
	}
	name, err := headerNameArgument(context, arguments, 0)
	if err != nil {
		return memory.Value{}, err
	}
	value, err := stringArgument(context, arguments, 1)
	if err != nil {
		return memory.Value{}, err
	}
	header.Add(name, strings.TrimSpace(value))
	return memory.UndefinedValue(), writeHeaders(context, object, header)
}

func (realm *Realm) headersDelete(context *browserruntime.TaskContext, this memory.Value, arguments []memory.Value) (memory.Value, error) {
	header, object, err := requireHeaders(context, this)
	if err != nil {
		return memory.Value{}, err
	}
	name, err := headerNameArgument(context, arguments, 0)
	if err != nil {
		return memory.Value{}, err
	}
	header.Del(name)
	return memory.UndefinedValue(), writeHeaders(context, object, header)
}

func (realm *Realm) headersGet(context *browserruntime.TaskContext, this memory.Value, arguments []memory.Value) (memory.Value, error) {
	header, _, err := requireHeaders(context, this)
	if err != nil {
		return memory.Value{}, err
	}
	name, err := headerNameArgument(context, arguments, 0)
	if err != nil {
		return memory.Value{}, err
	}
	values, ok := header[http.CanonicalHeaderKey(name)]
	if !ok {
		return memory.NullValue(), nil
	}
	return newString(context, strings.Join(values, ", "))
}

func (realm *Realm) headersHas(context *browserruntime.TaskContext, this memory.Value, arguments []memory.Value) (memory.Value, error) {
	header, _, err := requireHeaders(context, this)
	if err != nil {
		return memory.Value{}, err
	}
	name, err := headerNameArgument(context, arguments, 0)
	if err != nil {
		return memory.Value{}, err
	}
	_, ok := header[http.CanonicalHeaderKey(name)]
	return memory.BoolValue(ok), nil
}

func (realm *Realm) headersSet(context *browserruntime.TaskContext, this memory.Value, arguments []memory.Value) (memory.Value, error) {
	header, object, err := requireHeaders(context, this)
	if err != nil {
		return memory.Value{}, err
	}
	name, err := headerNameArgument(context, arguments, 0)
	if err != nil {
		return memory.Value{}, err
	}
	value, err := stringArgument(context, arguments, 1)
	if err != nil {
		return memory.Value{}, err
	}
	header.Set(name, strings.TrimSpace(value))
	return memory.UndefinedValue(), writeHeaders(context, object, header)
}

func (realm *Realm) headersForEach(context *browserruntime.TaskContext, this memory.Value, arguments []memory.Value) (memory.Value, error) {
	header, _, err := requireHeaders(context, this)
	if err != nil {
		return memory.Value{}, err
	}
	callback := argument(arguments, 0)
	if !callback.IsRef() {
		return memory.Value{}, fmt.Errorf("%w: Headers.forEach callback", browserruntime.ErrOperandType)
	}
	names := make([]string, 0, len(header))
	for name := range header {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		value, err := newString(context, strings.Join(header[name], ", "))
		if err != nil {
			return memory.Value{}, err
		}
		key, err := newString(context, strings.ToLower(name))
		if err != nil {
			return memory.Value{}, err
		}
		if _, err := realm.interpreter.CallWithoutCheckpoint(context, callback.Ref(), memory.UndefinedValue(), value, key, this); err != nil {
			return memory.Value{}, err
		}
	}
	return memory.UndefinedValue(), nil
}

func (realm *Realm) requestConstructor(context *browserruntime.TaskContext, this memory.Value, arguments []memory.Value) (memory.Value, error) {
	if !this.IsRef() {
		return memory.Value{}, fmt.Errorf("%w: Request constructor requires new", browserruntime.ErrOperandType)
	}
	request, err := realm.fetchRequest(context, arguments)
	if err != nil {
		return memory.Value{}, err
	}
	if err := defineData(context, this.Ref(), requestBrandProperty, memory.BoolValue(true), false, false, false); err != nil {
		return memory.Value{}, err
	}
	if err := defineStringData(context, this.Ref(), "url", request.URL, false, true); err != nil {
		return memory.Value{}, err
	}
	if err := defineStringData(context, this.Ref(), "method", normalizedMethod(request.Method), false, true); err != nil {
		return memory.Value{}, err
	}
	headers, err := realm.newHeadersObject(context, request.Header)
	if err != nil {
		return memory.Value{}, err
	}
	if err := defineData(context, this.Ref(), "headers", memory.RefValue(headers), false, true, true); err != nil {
		return memory.Value{}, err
	}
	body, err := context.NewArrayBuffer(request.Body)
	if err != nil {
		return memory.Value{}, err
	}
	return memory.UndefinedValue(), defineData(context, this.Ref(), requestBodyProperty, memory.RefValue(body), false, false, false)
}

func (realm *Realm) responseConstructor(context *browserruntime.TaskContext, this memory.Value, arguments []memory.Value) (memory.Value, error) {
	if !this.IsRef() {
		return memory.Value{}, fmt.Errorf("%w: Response constructor requires new", browserruntime.ErrOperandType)
	}
	body, err := bodyBytes(context, argument(arguments, 0))
	if err != nil {
		return memory.Value{}, err
	}
	status := 200
	statusText := http.StatusText(status)
	header := make(http.Header)
	if init := argument(arguments, 1); init.IsRef() {
		if value, found, err := ownValue(context, init.Ref(), "status"); err != nil {
			return memory.Value{}, err
		} else if found && value.Kind() == memory.ValueNumber {
			status = int(value.Number())
			statusText = http.StatusText(status)
		}
		if value, found, err := ownValue(context, init.Ref(), "statusText"); err != nil {
			return memory.Value{}, err
		} else if found {
			statusText, err = valueString(context, value)
			if err != nil {
				return memory.Value{}, err
			}
		}
		if value, found, err := ownValue(context, init.Ref(), "headers"); err != nil {
			return memory.Value{}, err
		} else if found {
			header, err = readHeadersInit(context, value)
			if err != nil {
				return memory.Value{}, err
			}
		}
	}
	return memory.UndefinedValue(), realm.initializeResponse(context, this.Ref(), browser.FetchResponse{Status: status, StatusText: statusText, Header: header, Body: body})
}

func (realm *Realm) globalFetch(context *browserruntime.TaskContext, _ memory.Value, arguments []memory.Value) (memory.Value, error) {
	promise, err := context.NewPromise()
	if err != nil {
		return memory.Value{}, err
	}

	request, err := realm.fetchRequest(context, arguments)
	if err != nil {
		if rejectErr := rejectPromise(context, promise, err); rejectErr != nil {
			return memory.Value{}, rejectErr
		}
		return memory.RefValue(promise), nil
	}
	if host, ok := realm.host.(browser.AsyncFetchHost); ok {
		handle, err := realm.retainValueLocked(context, memory.RefValue(promise))
		if err != nil {
			return memory.Value{}, err
		}
		if err := host.QueueFetch(handle, request); err != nil {
			_, _ = context.MapDelete(realm.bindings.callbackCache, memory.NumberValue(float64(handle)))
			if rejectErr := rejectPromise(context, promise, err); rejectErr != nil {
				return memory.Value{}, rejectErr
			}
		}
		return memory.RefValue(promise), nil
	}
	host, ok := realm.host.(browser.FetchHost)
	if !ok {
		if rejectErr := rejectPromise(context, promise, fmt.Errorf("fetch is unavailable in this browser host")); rejectErr != nil {
			return memory.Value{}, rejectErr
		}
		return memory.RefValue(promise), nil
	}
	response, err := host.Fetch(request)
	if err != nil {
		if rejectErr := rejectPromise(context, promise, err); rejectErr != nil {
			return memory.Value{}, rejectErr
		}
		return memory.RefValue(promise), nil
	}
	object, err := realm.newResponseObject(context, response)
	if err != nil {
		return memory.Value{}, err
	}
	if err := context.ResolvePromise(promise, memory.RefValue(object)); err != nil {
		return memory.Value{}, err
	}
	return memory.RefValue(promise), nil
}

func (realm *Realm) DispatchFetch(host browser.Host, handle browser.ValueHandle, response browser.FetchResponse, fetchErr error) error {
	if realm == nil {
		return ErrRealmClosed
	}
	realm.mutex.Lock()
	defer realm.mutex.Unlock()
	if realm.closed {
		return ErrRealmClosed
	}
	realm.host = host
	defer func() { realm.host = nil }()
	task, err := runtimeTask(host)
	if err != nil {
		return err
	}
	context, err := realm.beginTaskLocked(task)
	if err != nil {
		return fmt.Errorf("nativeengine: begin fetch %d completion task %d: %w", handle, task.TaskID, err)
	}
	key := memory.NumberValue(float64(handle))
	value, found, err := context.MapGet(realm.bindings.callbackCache, key)
	if err != nil {
		return err
	}
	if !found || !value.IsRef() {
		return fmt.Errorf("nativeengine: missing fetch promise %d", handle)
	}
	if _, err := context.MapDelete(realm.bindings.callbackCache, key); err != nil {
		return err
	}
	promise := value.Ref()
	if fetchErr != nil {
		return realm.rejectPromise(context, promise, fetchErr)
	}
	object, err := realm.newResponseObject(context, response)
	if err != nil {
		return err
	}
	return realm.interpreter.ResolvePromise(context, promise, memory.RefValue(object))
}

func (realm *Realm) newResponseObject(context *browserruntime.TaskContext, response browser.FetchResponse) (memory.Ref, error) {
	object, err := context.NewHeapObject()
	if err != nil {
		return memory.Ref{}, err
	}
	if err := context.SetPrototype(object, memory.RefValue(realm.bindings.responsePrototype)); err != nil {
		return memory.Ref{}, err
	}
	if err := realm.initializeResponse(context, object, response); err != nil {
		return memory.Ref{}, err
	}
	return object, nil
}

func (realm *Realm) rejectPromise(context *browserruntime.TaskContext, promise memory.Ref, cause error) error {
	message, err := context.NewString(cause.Error())
	if err != nil {
		return err
	}
	reason, err := context.NewError(memory.ErrorType, memory.RefValue(message))
	if err != nil {
		return err
	}
	return realm.interpreter.RejectPromise(context, promise, memory.RefValue(reason))
}

func rejectPromise(context *browserruntime.TaskContext, promise memory.Ref, cause error) error {
	message, err := context.NewString(cause.Error())
	if err != nil {
		return err
	}
	reason, err := context.NewError(memory.ErrorType, memory.RefValue(message))
	if err != nil {
		return err
	}
	return context.RejectPromise(promise, memory.RefValue(reason))
}

var _ browser.JSFetchRealm = (*Realm)(nil)

func (realm *Realm) fetchRequest(context *browserruntime.TaskContext, arguments []memory.Value) (browser.FetchRequest, error) {
	input := argument(arguments, 0)
	request := browser.FetchRequest{Method: http.MethodGet, Header: make(http.Header)}
	if input.IsRef() {
		kind, err := context.HeapKind(input.Ref())
		if err != nil {
			return request, err
		}
		if kind == memory.HeapString {
			request.URL, err = context.DerefString(input.Ref())
			if err != nil {
				return request, err
			}
		} else if branded, found, err := ownValue(context, input.Ref(), requestBrandProperty); err != nil {
			return request, err
		} else if found && branded.Kind() == memory.ValueBool && branded.Bool() {
			urlValue, _, err := ownValue(context, input.Ref(), "url")
			if err != nil {
				return request, err
			}
			request.URL, err = valueString(context, urlValue)
			if err != nil {
				return request, err
			}
			if value, found, _ := ownValue(context, input.Ref(), "method"); found {
				request.Method, _ = valueString(context, value)
			}
			if value, found, _ := ownValue(context, input.Ref(), "headers"); found {
				request.Header, err = readHeadersInit(context, value)
				if err != nil {
					return request, err
				}
			}
			if value, found, _ := ownValue(context, input.Ref(), requestBodyProperty); found && value.IsRef() {
				buffer, readErr := context.DerefArrayBuffer(value.Ref())
				if readErr != nil {
					return request, readErr
				}
				request.Body = buffer.Bytes
			}
		} else {
			return request, fmt.Errorf("%w: fetch input must be a URL string or Request", browserruntime.ErrOperandType)
		}
	} else {
		var err error
		request.URL, err = valueString(context, input)
		if err != nil {
			return request, err
		}
	}
	if init := argument(arguments, 1); init.IsRef() {
		if value, found, err := ownValue(context, init.Ref(), "method"); err != nil {
			return request, err
		} else if found {
			request.Method, err = valueString(context, value)
			if err != nil {
				return request, err
			}
		}
		if value, found, err := ownValue(context, init.Ref(), "headers"); err != nil {
			return request, err
		} else if found {
			request.Header, err = readHeadersInit(context, value)
			if err != nil {
				return request, err
			}
		}
		if value, found, err := ownValue(context, init.Ref(), "body"); err != nil {
			return request, err
		} else if found {
			request.Body, err = bodyBytes(context, value)
			if err != nil {
				return request, err
			}
		}
	}
	request.Method = normalizedMethod(request.Method)
	if (request.Method == http.MethodGet || request.Method == http.MethodHead) && len(request.Body) != 0 {
		return request, fmt.Errorf("%w: %s request cannot have a body", browserruntime.ErrOperandType, request.Method)
	}
	return request, nil
}

func (realm *Realm) initializeResponse(context *browserruntime.TaskContext, object memory.Ref, response browser.FetchResponse) error {
	if response.Header == nil {
		response.Header = make(http.Header)
	}
	body, err := context.NewArrayBuffer(response.Body)
	if err != nil {
		return err
	}
	headers, err := realm.newHeadersObject(context, response.Header)
	if err != nil {
		return err
	}
	properties := []struct {
		name  string
		value memory.Value
	}{
		{"status", memory.NumberValue(float64(response.Status))},
		{"ok", memory.BoolValue(response.Status >= 200 && response.Status <= 299)},
		{"redirected", memory.BoolValue(false)},
		{"bodyUsed", memory.BoolValue(false)},
		{"headers", memory.RefValue(headers)},
	}
	for _, property := range properties {
		if err := defineData(context, object, property.name, property.value, property.name == "bodyUsed", true, true); err != nil {
			return err
		}
	}
	for _, property := range []struct{ name, value string }{{"url", response.URL}, {"statusText", response.StatusText}, {"type", "basic"}} {
		if err := defineStringData(context, object, property.name, property.value, false, true); err != nil {
			return err
		}
	}
	return defineData(context, object, responseBodyProperty, memory.RefValue(body), false, false, false)
}

func (realm *Realm) responseText(context *browserruntime.TaskContext, this memory.Value, _ []memory.Value) (memory.Value, error) {
	body, object, err := consumeResponseBody(context, this)
	if err != nil {
		return rejectedPromise(context, err)
	}
	_ = object
	value, err := newString(context, string(body))
	if err != nil {
		return memory.Value{}, err
	}
	return resolvedPromise(context, value)
}

func (realm *Realm) responseJSON(context *browserruntime.TaskContext, this memory.Value, _ []memory.Value) (memory.Value, error) {
	body, _, err := consumeResponseBody(context, this)
	if err != nil {
		return rejectedPromise(context, err)
	}
	var decoded any
	if err := json.Unmarshal(body, &decoded); err != nil {
		return rejectedPromise(context, fmt.Errorf("invalid JSON response: %w", err))
	}
	value, err := jsonMemoryValue(context, decoded)
	if err != nil {
		return memory.Value{}, err
	}
	return resolvedPromise(context, value)
}

func (realm *Realm) responseArrayBuffer(context *browserruntime.TaskContext, this memory.Value, _ []memory.Value) (memory.Value, error) {
	body, _, err := consumeResponseBody(context, this)
	if err != nil {
		return rejectedPromise(context, err)
	}
	buffer, err := context.NewArrayBuffer(body)
	if err != nil {
		return memory.Value{}, err
	}
	return resolvedPromise(context, memory.RefValue(buffer))
}

func (realm *Realm) responseClone(context *browserruntime.TaskContext, this memory.Value, _ []memory.Value) (memory.Value, error) {
	if !this.IsRef() {
		return memory.Value{}, fmt.Errorf("%w: incompatible Response receiver", browserruntime.ErrOperandType)
	}
	if used, found, err := ownValue(context, this.Ref(), "bodyUsed"); err != nil || !found || used.Kind() != memory.ValueBool {
		return memory.Value{}, fmt.Errorf("%w: incompatible Response receiver", browserruntime.ErrOperandType)
	} else if used.Bool() {
		return memory.Value{}, fmt.Errorf("%w: Response body is already used", browserruntime.ErrOperandType)
	}
	bodyValue, found, err := ownValue(context, this.Ref(), responseBodyProperty)
	if err != nil || !found || !bodyValue.IsRef() {
		return memory.Value{}, fmt.Errorf("%w: incompatible Response receiver", browserruntime.ErrOperandType)
	}
	body, err := context.DerefArrayBuffer(bodyValue.Ref())
	if err != nil {
		return memory.Value{}, err
	}
	response := browser.FetchResponse{Body: body.Bytes}
	if value, _, _ := ownValue(context, this.Ref(), "status"); value.Kind() == memory.ValueNumber {
		response.Status = int(value.Number())
	}
	for _, property := range []struct {
		name string
		dst  *string
	}{{"url", &response.URL}, {"statusText", &response.StatusText}} {
		if value, found, _ := ownValue(context, this.Ref(), property.name); found {
			*property.dst, _ = valueString(context, value)
		}
	}
	if value, found, _ := ownValue(context, this.Ref(), "headers"); found {
		response.Header, err = readHeadersInit(context, value)
		if err != nil {
			return memory.Value{}, err
		}
	}
	clone, err := context.NewHeapObject()
	if err != nil {
		return memory.Value{}, err
	}
	if err := context.SetPrototype(clone, memory.RefValue(realm.bindings.responsePrototype)); err != nil {
		return memory.Value{}, err
	}
	return memory.RefValue(clone), realm.initializeResponse(context, clone, response)
}

func (realm *Realm) newHeadersObject(context *browserruntime.TaskContext, header http.Header) (memory.Ref, error) {
	object, err := context.NewHeapObject()
	if err != nil {
		return memory.Ref{}, err
	}
	if err := context.SetPrototype(object, memory.RefValue(realm.bindings.headersPrototype)); err != nil {
		return memory.Ref{}, err
	}
	return object, writeHeaders(context, object, header.Clone())
}

func requireHeaders(context *browserruntime.TaskContext, this memory.Value) (http.Header, memory.Ref, error) {
	if !this.IsRef() {
		return nil, memory.Ref{}, fmt.Errorf("%w: incompatible Headers receiver", browserruntime.ErrOperandType)
	}
	header, found, err := readHeaders(context, this.Ref())
	if err != nil || !found {
		return nil, memory.Ref{}, fmt.Errorf("%w: incompatible Headers receiver", browserruntime.ErrOperandType)
	}
	return header, this.Ref(), nil
}

func readHeadersInit(context *browserruntime.TaskContext, value memory.Value) (http.Header, error) {
	header := make(http.Header)
	if value.Kind() == memory.ValueUndefined || value.Kind() == memory.ValueNull {
		return header, nil
	}
	if !value.IsRef() {
		return nil, fmt.Errorf("%w: invalid Headers initializer", browserruntime.ErrOperandType)
	}
	if existing, found, err := readHeaders(context, value.Ref()); err != nil {
		return nil, err
	} else if found {
		return existing, nil
	}
	headerSnapshot, err := context.DerefObjectHeader(value.Ref())
	if err != nil {
		return nil, fmt.Errorf("%w: invalid Headers initializer", browserruntime.ErrOperandType)
	}
	for _, property := range headerSnapshot.Properties {
		if property.Kind != memory.PropertyData || !property.Enumerable {
			continue
		}
		name, err := context.DerefString(property.Name)
		if err != nil {
			continue
		}
		name, err = normalizeHeaderName(name)
		if err != nil {
			return nil, err
		}
		text, err := valueString(context, property.Value)
		if err != nil {
			return nil, err
		}
		header.Set(name, strings.TrimSpace(text))
	}
	return header, nil
}

func readHeaders(context *browserruntime.TaskContext, object memory.Ref) (http.Header, bool, error) {
	value, found, err := ownValue(context, object, headersStateProperty)
	if err != nil || !found || !value.IsRef() {
		return nil, found, err
	}
	encoded, err := context.DerefString(value.Ref())
	if err != nil {
		return nil, false, err
	}
	header := make(http.Header)
	if err := json.Unmarshal([]byte(encoded), &header); err != nil {
		return nil, false, err
	}
	return header, true, nil
}

func writeHeaders(context *browserruntime.TaskContext, object memory.Ref, header http.Header) error {
	encoded, err := json.Marshal(header)
	if err != nil {
		return err
	}
	value, err := context.NewString(string(encoded))
	if err != nil {
		return err
	}
	name, err := context.NewString(headersStateProperty)
	if err != nil {
		return err
	}
	if _, found, err := context.GetOwnProperty(object, name); err != nil {
		return err
	} else if found {
		return context.SetProperty(object, name, memory.RefValue(value))
	}
	return context.DefineProperty(object, name, memory.DataProperty(memory.RefValue(value), true, false, false))
}

func headerNameArgument(context *browserruntime.TaskContext, arguments []memory.Value, index int) (string, error) {
	name, err := stringArgument(context, arguments, index)
	if err != nil {
		return "", err
	}
	return normalizeHeaderName(name)
}

func normalizeHeaderName(name string) (string, error) {
	name = strings.TrimSpace(name)
	if name == "" || strings.ContainsAny(name, "()<>@,;:\\\"/[]?={} \t\r\n") {
		return "", fmt.Errorf("%w: invalid HTTP header name %q", browserruntime.ErrOperandType, name)
	}
	return http.CanonicalHeaderKey(name), nil
}

func bodyBytes(context *browserruntime.TaskContext, value memory.Value) ([]byte, error) {
	if value.Kind() == memory.ValueUndefined || value.Kind() == memory.ValueNull {
		return nil, nil
	}
	if !value.IsRef() {
		text, err := valueString(context, value)
		return []byte(text), err
	}
	kind, err := context.HeapKind(value.Ref())
	if err != nil {
		return nil, err
	}
	switch kind {
	case memory.HeapString:
		text, err := context.DerefString(value.Ref())
		return []byte(text), err
	case memory.HeapArrayBuffer:
		buffer, err := context.DerefArrayBuffer(value.Ref())
		return buffer.Bytes, err
	case memory.HeapTypedArray:
		view, err := context.DerefTypedArray(value.Ref())
		if err != nil {
			return nil, err
		}
		return context.ReadArrayBuffer(view.Buffer, view.ByteOffset, view.Length)
	default:
		return nil, fmt.Errorf("%w: unsupported request body", browserruntime.ErrOperandType)
	}
}

func consumeResponseBody(context *browserruntime.TaskContext, this memory.Value) ([]byte, memory.Ref, error) {
	if !this.IsRef() {
		return nil, memory.Ref{}, fmt.Errorf("%w: incompatible Response receiver", browserruntime.ErrOperandType)
	}
	used, found, err := ownValue(context, this.Ref(), "bodyUsed")
	if err != nil || !found || used.Kind() != memory.ValueBool {
		return nil, memory.Ref{}, fmt.Errorf("%w: incompatible Response receiver", browserruntime.ErrOperandType)
	}
	if used.Bool() {
		return nil, memory.Ref{}, fmt.Errorf("%w: Response body is already used", browserruntime.ErrOperandType)
	}
	bodyValue, found, err := ownValue(context, this.Ref(), responseBodyProperty)
	if err != nil || !found || !bodyValue.IsRef() {
		return nil, memory.Ref{}, fmt.Errorf("%w: incompatible Response receiver", browserruntime.ErrOperandType)
	}
	buffer, err := context.DerefArrayBuffer(bodyValue.Ref())
	if err != nil {
		return nil, memory.Ref{}, err
	}
	name, err := context.NewString("bodyUsed")
	if err != nil {
		return nil, memory.Ref{}, err
	}
	if err := context.SetProperty(this.Ref(), name, memory.BoolValue(true)); err != nil {
		return nil, memory.Ref{}, err
	}
	return buffer.Bytes, this.Ref(), nil
}

func resolvedPromise(context *browserruntime.TaskContext, value memory.Value) (memory.Value, error) {
	promise, err := context.NewPromise()
	if err != nil {
		return memory.Value{}, err
	}
	if err := context.ResolvePromise(promise, value); err != nil {
		return memory.Value{}, err
	}
	return memory.RefValue(promise), nil
}

func rejectedPromise(context *browserruntime.TaskContext, cause error) (memory.Value, error) {
	promise, err := context.NewPromise()
	if err != nil {
		return memory.Value{}, err
	}
	message, err := context.NewString(cause.Error())
	if err != nil {
		return memory.Value{}, err
	}
	reason, err := context.NewError(memory.ErrorType, memory.RefValue(message))
	if err != nil {
		return memory.Value{}, err
	}
	if err := context.RejectPromise(promise, memory.RefValue(reason)); err != nil {
		return memory.Value{}, err
	}
	return memory.RefValue(promise), nil
}

func jsonMemoryValue(context *browserruntime.TaskContext, value any) (memory.Value, error) {
	switch typed := value.(type) {
	case nil:
		return memory.NullValue(), nil
	case bool:
		return memory.BoolValue(typed), nil
	case float64:
		return memory.NumberValue(typed), nil
	case string:
		return newString(context, typed)
	case []any:
		array, err := context.NewArray(uint32(len(typed)))
		if err != nil {
			return memory.Value{}, err
		}
		for index, item := range typed {
			converted, err := jsonMemoryValue(context, item)
			if err != nil {
				return memory.Value{}, err
			}
			if err := context.SetArrayElement(array, uint32(index), converted); err != nil {
				return memory.Value{}, err
			}
		}
		return memory.RefValue(array), nil
	case map[string]any:
		object, err := context.NewHeapObject()
		if err != nil {
			return memory.Value{}, err
		}
		for name, item := range typed {
			converted, err := jsonMemoryValue(context, item)
			if err != nil {
				return memory.Value{}, err
			}
			if err := defineData(context, object, name, converted, true, true, true); err != nil {
				return memory.Value{}, err
			}
		}
		return memory.RefValue(object), nil
	default:
		return memory.Value{}, fmt.Errorf("unsupported JSON value %T", value)
	}
}

func ownValue(context *browserruntime.TaskContext, object memory.Ref, name string) (memory.Value, bool, error) {
	nameRef, err := context.NewString(name)
	if err != nil {
		return memory.Value{}, false, err
	}
	return context.GetOwnProperty(object, nameRef)
}

func defineStringData(context *browserruntime.TaskContext, object memory.Ref, name, value string, writable, enumerable bool) error {
	text, err := newString(context, value)
	if err != nil {
		return err
	}
	return defineData(context, object, name, text, writable, enumerable, true)
}

func normalizedMethod(method string) string {
	method = strings.ToUpper(strings.TrimSpace(method))
	if method == "" {
		return http.MethodGet
	}
	return method
}
