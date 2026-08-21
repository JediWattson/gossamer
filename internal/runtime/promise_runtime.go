package runtime

import (
	"errors"
	"fmt"

	"github.com/JediWattson/gossamer/internal/runtime/memory"
)

type microtaskJobKind uint8

const (
	microtaskCallback microtaskJobKind = iota + 1
	microtaskPromiseReaction
)

type microtaskJob struct {
	kind     microtaskJobKind
	callback memory.Ref
	state    memory.PromiseState
	result   memory.Value
	reaction memory.PromiseReaction
}

func (intrinsics *Intrinsics) installPromiseBuiltins(context *TaskContext) error {
	name, err := context.NewString("Promise")
	if err != nil {
		return err
	}
	constructor, err := context.Realm.store.AllocNativeConstructor(
		context.Owner, context.MemoryRegion, memory.RefValue(name), memory.NullValue(), 1, nativePromiseConstructor,
	)
	if err != nil {
		return err
	}
	if err := intrinsics.initializeFunctionWithPrototype(context, constructor, memory.RefValue(name), 1, intrinsics.PromisePrototype); err != nil {
		return err
	}
	intrinsics.PromiseConstructor = constructor
	if err := installMethods(intrinsics, context, constructor, []builtinMethod{
		{"resolve", 1, nativePromiseResolve}, {"reject", 1, nativePromiseReject}, {"all", 1, nativePromiseAll},
	}); err != nil {
		return err
	}
	if err := installMethods(intrinsics, context, intrinsics.PromisePrototype, []builtinMethod{
		{"then", 2, nativePromiseThen}, {"catch", 1, nativePromiseCatch},
	}); err != nil {
		return err
	}
	queueMicrotask, err := intrinsics.newBuiltinMethod(context, "queueMicrotask", 1, nativeQueueMicrotask)
	if err != nil {
		return err
	}
	intrinsics.QueueMicrotask = queueMicrotask
	return nil
}

func builtinPromiseAll(execution *execution, _ memory.Ref, _ memory.Function, _ memory.Value, arguments []memory.Value) (memory.Value, error) {
	output, err := execution.context.NewPromise()
	if err != nil {
		return memory.Value{}, err
	}
	values, err := execution.context.NewArray(0)
	if err == nil {
		err = execution.appendIterableToArray(values, argument(arguments, 0))
	}
	if err != nil {
		reason, language, conversionErr := execution.promiseRejectionValue(err)
		if conversionErr != nil {
			return memory.Value{}, conversionErr
		}
		if !language {
			return memory.Value{}, err
		}
		if rejectErr := execution.rejectPromise(output, reason); rejectErr != nil {
			return memory.Value{}, rejectErr
		}
		return memory.RefValue(output), nil
	}
	inputs, err := execution.context.DerefArray(values)
	if err != nil {
		return memory.Value{}, err
	}
	results, err := execution.context.NewArray(inputs.Length)
	if err != nil {
		return memory.Value{}, err
	}
	if inputs.Length == 0 {
		if err := execution.resolvePromise(output, memory.RefValue(results)); err != nil {
			return memory.Value{}, err
		}
		return memory.RefValue(output), nil
	}
	state, err := execution.context.NewCell()
	if err != nil {
		return memory.Value{}, err
	}
	if err := execution.context.Set(state, 0, memory.NumberValue(float64(inputs.Length))); err != nil {
		return memory.Value{}, err
	}
	reject, err := newPromiseSettler(execution.context, "reject", nativePromiseRejectFunction, output)
	if err != nil {
		return memory.Value{}, err
	}
	for index, element := range inputs.Elements {
		promiseValue, err := builtinPromiseResolve(execution, memory.Ref{}, memory.Function{}, memory.UndefinedValue(), []memory.Value{element.Value})
		if err != nil {
			return memory.Value{}, err
		}
		name, err := execution.context.NewString("Promise.all fulfill")
		if err != nil {
			return memory.Value{}, err
		}
		fulfill, err := execution.context.NewBoundNativeFunction(
			memory.RefValue(name), memory.NullValue(), 1, nativePromiseAllFulfill,
			memory.RefValue(output), memory.RefValue(results), memory.RefValue(state), memory.NumberValue(float64(index)),
		)
		if err != nil {
			return memory.Value{}, err
		}
		promise := promiseValue.Ref()
		if err := execution.context.AddPromiseReaction(promise, memory.PromiseReaction{
			OnFulfilled: memory.RefValue(fulfill), OnRejected: memory.RefValue(reject), Downstream: memory.NullValue(),
		}); err != nil {
			return memory.Value{}, err
		}
		if err := execution.context.MarkPromiseHandled(promise); err != nil {
			return memory.Value{}, err
		}
		snapshot, err := execution.context.DerefPromise(promise)
		if err != nil {
			return memory.Value{}, err
		}
		if snapshot.State != memory.PromisePending {
			if err := execution.enqueuePromiseReactions(promise); err != nil {
				return memory.Value{}, err
			}
		}
	}
	return memory.RefValue(output), nil
}

func builtinPromiseAllFulfill(execution *execution, _ memory.Ref, function memory.Function, _ memory.Value, arguments []memory.Value) (memory.Value, error) {
	if len(function.Captures) != 4 || !function.Captures[0].IsRef() || !function.Captures[1].IsRef() ||
		!function.Captures[2].IsRef() || function.Captures[3].Kind() != memory.ValueNumber {
		return memory.Value{}, fmt.Errorf("%w: Promise.all callback has invalid captures", ErrNativeFunction)
	}
	output := function.Captures[0].Ref()
	results := function.Captures[1].Ref()
	state := function.Captures[2].Ref()
	index, err := requireUint32(function.Captures[3], "Promise.all index", true)
	if err != nil {
		return memory.Value{}, err
	}
	if err := execution.context.SetArrayElement(results, index, argument(arguments, 0)); err != nil {
		return memory.Value{}, err
	}
	cell, err := execution.context.Deref(state)
	if err != nil {
		return memory.Value{}, err
	}
	if len(cell.Fields) == 0 || cell.Fields[0].Kind() != memory.ValueNumber || cell.Fields[0].Number() < 1 {
		return memory.Value{}, fmt.Errorf("%w: Promise.all remaining count is invalid", ErrNativeFunction)
	}
	remaining := cell.Fields[0].Number() - 1
	if err := execution.context.Set(state, 0, memory.NumberValue(remaining)); err != nil {
		return memory.Value{}, err
	}
	if remaining == 0 {
		if err := execution.resolvePromise(output, memory.RefValue(results)); err != nil && !errors.Is(err, memory.ErrPromiseSettled) {
			return memory.Value{}, err
		}
	}
	return memory.UndefinedValue(), nil
}

func builtinPromiseConstructor(execution *execution, _ memory.Ref, _ memory.Function, _ memory.Value, arguments []memory.Value) (memory.Value, error) {
	executor, err := requireCallable(execution.context, argument(arguments, 0))
	if err != nil {
		return memory.Value{}, err
	}
	promise, err := execution.context.NewPromise()
	if err != nil {
		return memory.Value{}, err
	}
	resolve, err := newPromiseSettler(execution.context, "resolve", nativePromiseResolveFunction, promise)
	if err != nil {
		return memory.Value{}, err
	}
	reject, err := newPromiseSettler(execution.context, "reject", nativePromiseRejectFunction, promise)
	if err != nil {
		return memory.Value{}, err
	}
	_, callErr := execution.call(executor, memory.UndefinedValue(), []memory.Value{memory.RefValue(resolve), memory.RefValue(reject)}, callAny)
	if callErr != nil {
		reason, language, conversionErr := execution.promiseRejectionValue(callErr)
		if conversionErr != nil {
			return memory.Value{}, conversionErr
		}
		if !language {
			return memory.Value{}, callErr
		}
		if err := execution.rejectPromise(promise, reason); err != nil && !errors.Is(err, memory.ErrPromiseSettled) {
			return memory.Value{}, err
		}
	}
	return memory.RefValue(promise), nil
}

func newPromiseSettler(context *TaskContext, name string, nativeID uint64, promise memory.Ref) (memory.Ref, error) {
	nameRef, err := context.NewString(name)
	if err != nil {
		return memory.Ref{}, err
	}
	return context.NewBoundNativeFunction(memory.RefValue(nameRef), memory.NullValue(), 1, nativeID, memory.RefValue(promise))
}

func builtinPromiseResolveFunction(execution *execution, _ memory.Ref, function memory.Function, _ memory.Value, arguments []memory.Value) (memory.Value, error) {
	promise, err := capturedPromise(function)
	if err != nil {
		return memory.Value{}, err
	}
	if err := execution.resolvePromise(promise, argument(arguments, 0)); err != nil && !errors.Is(err, memory.ErrPromiseSettled) {
		return memory.Value{}, err
	}
	return memory.UndefinedValue(), nil
}

func builtinPromiseRejectFunction(execution *execution, _ memory.Ref, function memory.Function, _ memory.Value, arguments []memory.Value) (memory.Value, error) {
	promise, err := capturedPromise(function)
	if err != nil {
		return memory.Value{}, err
	}
	if err := execution.rejectPromise(promise, argument(arguments, 0)); err != nil && !errors.Is(err, memory.ErrPromiseSettled) {
		return memory.Value{}, err
	}
	return memory.UndefinedValue(), nil
}

func capturedPromise(function memory.Function) (memory.Ref, error) {
	if len(function.Captures) != 1 || !function.Captures[0].IsRef() {
		return memory.Ref{}, fmt.Errorf("%w: Promise settler has invalid captures", ErrNativeFunction)
	}
	return function.Captures[0].Ref(), nil
}

func builtinPromiseResolve(execution *execution, _ memory.Ref, _ memory.Function, _ memory.Value, arguments []memory.Value) (memory.Value, error) {
	value := argument(arguments, 0)
	if value.IsRef() {
		if kind, err := execution.context.HeapKind(value.Ref()); err != nil {
			return memory.Value{}, err
		} else if kind == memory.HeapPromise {
			return value, nil
		}
	}
	promise, err := execution.context.NewPromise()
	if err != nil {
		return memory.Value{}, err
	}
	if err := execution.resolvePromise(promise, value); err != nil {
		return memory.Value{}, err
	}
	return memory.RefValue(promise), nil
}

func builtinPromiseReject(execution *execution, _ memory.Ref, _ memory.Function, _ memory.Value, arguments []memory.Value) (memory.Value, error) {
	promise, err := execution.context.NewPromise()
	if err != nil {
		return memory.Value{}, err
	}
	if err := execution.rejectPromise(promise, argument(arguments, 0)); err != nil {
		return memory.Value{}, err
	}
	return memory.RefValue(promise), nil
}

func builtinPromiseThen(execution *execution, _ memory.Ref, _ memory.Function, this memory.Value, arguments []memory.Value) (memory.Value, error) {
	promise, err := requireKind(execution.context, this, memory.HeapPromise, "Promise receiver")
	if err != nil {
		return memory.Value{}, err
	}
	downstream, err := execution.context.NewPromise()
	if err != nil {
		return memory.Value{}, err
	}
	onFulfilled := optionalCallable(execution.context, argument(arguments, 0))
	onRejected := optionalCallable(execution.context, argument(arguments, 1))
	reaction := memory.PromiseReaction{
		OnFulfilled: onFulfilled,
		OnRejected:  onRejected,
		Downstream:  memory.RefValue(downstream),
	}
	if err := execution.context.AddPromiseReaction(promise, reaction); err != nil {
		return memory.Value{}, err
	}
	if onRejected.IsRef() {
		if err := execution.context.MarkPromiseHandled(promise); err != nil {
			return memory.Value{}, err
		}
	}
	snapshot, err := execution.context.DerefPromise(promise)
	if err != nil {
		return memory.Value{}, err
	}
	if snapshot.State != memory.PromisePending {
		if err := execution.enqueuePromiseReactions(promise); err != nil {
			return memory.Value{}, err
		}
	}
	return memory.RefValue(downstream), nil
}

func builtinPromiseCatch(execution *execution, function memory.Ref, descriptor memory.Function, this memory.Value, arguments []memory.Value) (memory.Value, error) {
	return builtinPromiseThen(execution, function, descriptor, this, []memory.Value{memory.UndefinedValue(), argument(arguments, 0)})
}

func optionalCallable(context *TaskContext, value memory.Value) memory.Value {
	if !value.IsRef() {
		return memory.NullValue()
	}
	kind, err := context.HeapKind(value.Ref())
	if err != nil || kind != memory.HeapFunction {
		return memory.NullValue()
	}
	return value
}

func builtinQueueMicrotask(execution *execution, _ memory.Ref, _ memory.Function, _ memory.Value, arguments []memory.Value) (memory.Value, error) {
	callback, err := requireCallable(execution.context, argument(arguments, 0))
	if err != nil {
		return memory.Value{}, err
	}
	execution.enqueueJob(microtaskJob{kind: microtaskCallback, callback: callback})
	return memory.UndefinedValue(), nil
}

func (execution *execution) resolvePromise(promise memory.Ref, result memory.Value) error {
	if result.IsRef() {
		kind, err := execution.context.HeapKind(result.Ref())
		if err != nil {
			return err
		}
		if kind == memory.HeapPromise {
			if result.Ref() == promise {
				message, err := execution.context.NewString(memory.ErrPromiseSelfResolution.Error())
				if err != nil {
					return err
				}
				reason, err := execution.context.NewError(memory.ErrorType, memory.RefValue(message))
				if err != nil {
					return err
				}
				return execution.rejectPromise(promise, memory.RefValue(reason))
			}
			source := result.Ref()
			reaction := memory.PromiseReaction{OnFulfilled: memory.NullValue(), OnRejected: memory.NullValue(), Downstream: memory.RefValue(promise)}
			if err := execution.context.AddPromiseReaction(source, reaction); err != nil {
				return err
			}
			snapshot, err := execution.context.DerefPromise(source)
			if err != nil {
				return err
			}
			if snapshot.State != memory.PromisePending {
				return execution.enqueuePromiseReactions(source)
			}
			return nil
		}
	}
	if err := execution.context.ResolvePromise(promise, result); err != nil {
		return err
	}
	return execution.enqueuePromiseReactions(promise)
}

func (execution *execution) rejectPromise(promise memory.Ref, reason memory.Value) error {
	if err := execution.context.RejectPromise(promise, reason); err != nil {
		return err
	}
	return execution.enqueuePromiseReactions(promise)
}

func (execution *execution) enqueuePromiseReactions(promise memory.Ref) error {
	settlement, err := execution.context.DrainPromiseReactions(promise)
	if err != nil {
		return err
	}
	for _, reaction := range settlement.Reactions {
		execution.enqueueJob(microtaskJob{
			kind: microtaskPromiseReaction, state: settlement.State,
			result: settlement.Result, reaction: reaction,
		})
	}
	return nil
}

func (execution *execution) enqueueJob(job microtaskJob) {
	interpreter := execution.interpreter
	interpreter.jobMutex.Lock()
	interpreter.jobs[execution.context.TaskID] = append(interpreter.jobs[execution.context.TaskID], job)
	interpreter.jobMutex.Unlock()
}

func (execution *execution) popJob() (microtaskJob, bool) {
	interpreter := execution.interpreter
	interpreter.jobMutex.Lock()
	defer interpreter.jobMutex.Unlock()
	jobs := interpreter.jobs[execution.context.TaskID]
	if len(jobs) == 0 {
		delete(interpreter.jobs, execution.context.TaskID)
		return microtaskJob{}, false
	}
	job := jobs[0]
	jobs[0] = microtaskJob{}
	if len(jobs) == 1 {
		delete(interpreter.jobs, execution.context.TaskID)
	} else {
		interpreter.jobs[execution.context.TaskID] = jobs[1:]
	}
	return job, true
}

func (execution *execution) drainJobs() error {
	var result error
	for {
		job, ok := execution.popJob()
		if !ok {
			return result
		}
		var err error
		switch job.kind {
		case microtaskCallback:
			_, err = execution.call(job.callback, memory.UndefinedValue(), nil, callAny)
		case microtaskPromiseReaction:
			err = execution.runPromiseReaction(job)
		default:
			err = fmt.Errorf("runtime: unknown microtask job %d", job.kind)
		}
		result = errors.Join(result, err)
	}
}

func (execution *execution) runPromiseReaction(job microtaskJob) error {
	handler := job.reaction.OnFulfilled
	if job.state == memory.PromiseRejected {
		handler = job.reaction.OnRejected
	}
	downstream, hasDownstream := promiseReactionDownstream(job.reaction)
	if !handler.IsRef() {
		if !hasDownstream {
			return nil
		}
		if job.state == memory.PromiseRejected {
			return execution.rejectPromise(downstream, job.result)
		}
		return execution.resolvePromise(downstream, job.result)
	}
	value, err := execution.call(handler.Ref(), memory.UndefinedValue(), []memory.Value{job.result}, callAny)
	if err != nil {
		if !hasDownstream {
			return err
		}
		reason, language, conversionErr := execution.promiseRejectionValue(err)
		if conversionErr != nil {
			return conversionErr
		}
		if !language {
			return err
		}
		return execution.rejectPromise(downstream, reason)
	}
	if hasDownstream {
		return execution.resolvePromise(downstream, value)
	}
	return nil
}

func promiseReactionDownstream(reaction memory.PromiseReaction) (memory.Ref, bool) {
	if !reaction.Downstream.IsRef() {
		return memory.Ref{}, false
	}
	return reaction.Downstream.Ref(), true
}

func (execution *execution) promiseRejectionValue(err error) (memory.Value, bool, error) {
	if value, thrown := ThrownValue(err); thrown {
		return value, true, nil
	}
	kind, language := classifyLanguageError(err)
	if !language {
		return memory.Value{}, false, nil
	}
	message, allocErr := execution.context.NewString(err.Error())
	if allocErr != nil {
		return memory.Value{}, false, allocErr
	}
	ref, allocErr := execution.context.NewError(kind, memory.RefValue(message))
	if allocErr != nil {
		return memory.Value{}, false, allocErr
	}
	return memory.RefValue(ref), true, nil
}
