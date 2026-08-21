package runtime

import (
	"fmt"

	"github.com/JediWattson/gossamer/internal/runtime/memory"
)

// getIteratorRecord performs GetIterator for a synchronous iterable. The
// iterator's next method is captured once, matching the ECMAScript iterator
// record rather than re-reading iterator.next on every loop iteration.
func (execution *execution) getIteratorRecord(iterable memory.Value) (memory.Value, memory.Value, error) {
	if execution == nil || execution.context == nil {
		return memory.Value{}, memory.Value{}, fmt.Errorf("%w: iterator context is not initialized", ErrOperandType)
	}
	// Hand-assembled bytecode can run without a bootstrapped global Realm. Keep
	// that lower-level interpreter mode useful for its native collection kinds;
	// source execution with intrinsics always takes the observable protocol path
	// below and therefore honors Symbol.iterator overrides.
	if execution.context.intrinsics == nil {
		if !iterable.IsRef() {
			return memory.Value{}, memory.Value{}, fmt.Errorf("%w: value is not iterable", ErrOperandType)
		}
		kind, err := execution.context.HeapKind(iterable.Ref())
		if err != nil {
			return memory.Value{}, memory.Value{}, err
		}
		var iteratorKind memory.IteratorKind
		switch kind {
		case memory.HeapArray:
			iteratorKind = memory.IteratorArrayValues
		case memory.HeapString:
			iteratorKind = memory.IteratorStringValues
		case memory.HeapMap:
			iteratorKind = memory.IteratorMapEntries
		case memory.HeapSet:
			iteratorKind = memory.IteratorSetValues
		default:
			return memory.Value{}, memory.Value{}, fmt.Errorf("%w: value is not iterable", ErrOperandType)
		}
		iterator, err := execution.context.NewIterator(iterable.Ref(), iteratorKind)
		return memory.RefValue(iterator), memory.UndefinedValue(), err
	}
	method, present, err := execution.getProperty(iterable, memory.RefValue(execution.context.intrinsics.SymbolIterator))
	if err != nil {
		return memory.Value{}, memory.Value{}, err
	}
	if !present || method.Kind() == memory.ValueUndefined || method.Kind() == memory.ValueNull {
		return memory.Value{}, memory.Value{}, fmt.Errorf("%w: value is not iterable", ErrOperandType)
	}
	methodRef, err := requireRef(method, "Symbol.iterator method")
	if err != nil {
		return memory.Value{}, memory.Value{}, err
	}
	iterator, err := execution.call(methodRef, iterable, nil, callAny)
	if err != nil {
		return memory.Value{}, memory.Value{}, err
	}
	object, err := isObjectValue(execution.context, iterator)
	if err != nil {
		return memory.Value{}, memory.Value{}, err
	}
	if !object {
		return memory.Value{}, memory.Value{}, fmt.Errorf("%w: iterator method returned a non-Object", ErrOperandType)
	}
	nextName, err := execution.context.NewString("next")
	if err != nil {
		return memory.Value{}, memory.Value{}, err
	}
	next, present, err := execution.getProperty(iterator, memory.RefValue(nextName))
	if err != nil {
		return memory.Value{}, memory.Value{}, err
	}
	if !present {
		return memory.Value{}, memory.Value{}, fmt.Errorf("%w: iterator has no next method", ErrNotCallable)
	}
	return iterator, next, nil
}

func (execution *execution) iteratorNext(iterator, next memory.Value) (memory.Value, error) {
	if next.Kind() == memory.ValueUndefined && execution.context.intrinsics == nil {
		return builtinIteratorNext(execution, memory.Ref{}, memory.Function{}, iterator, nil)
	}
	nextRef, err := requireRef(next, "iterator next method")
	if err != nil {
		return memory.Value{}, err
	}
	result, err := execution.call(nextRef, iterator, nil, callAny)
	if err != nil {
		return memory.Value{}, err
	}
	object, err := isObjectValue(execution.context, result)
	if err != nil {
		return memory.Value{}, err
	}
	if !object {
		return memory.Value{}, fmt.Errorf("%w: iterator next returned a non-Object", ErrOperandType)
	}
	return result, nil
}

func (execution *execution) closeIterator(iterator memory.Value) error {
	if execution.context.intrinsics == nil {
		return nil
	}
	returnName, err := execution.context.NewString("return")
	if err != nil {
		return err
	}
	method, present, err := execution.getProperty(iterator, memory.RefValue(returnName))
	if err != nil {
		return err
	}
	if !present || method.Kind() == memory.ValueUndefined || method.Kind() == memory.ValueNull {
		return nil
	}
	methodRef, err := requireRef(method, "iterator return method")
	if err != nil {
		return err
	}
	result, err := execution.call(methodRef, iterator, nil, callAny)
	if err != nil {
		return err
	}
	object, err := isObjectValue(execution.context, result)
	if err != nil {
		return err
	}
	if !object {
		return fmt.Errorf("%w: iterator return returned a non-Object", ErrOperandType)
	}
	return nil
}

func (execution *execution) appendIterableToArray(array memory.Ref, iterable memory.Value) error {
	if _, err := execution.context.DerefArray(array); err != nil {
		return err
	}
	iterator, next, err := execution.getIteratorRecord(iterable)
	if err != nil {
		return err
	}
	abrupt := func(cause error) error {
		// An existing abrupt completion wins over a failure from IteratorClose.
		_ = execution.closeIterator(iterator)
		return cause
	}
	doneName, err := execution.context.NewString("done")
	if err != nil {
		return abrupt(err)
	}
	valueName, err := execution.context.NewString("value")
	if err != nil {
		return abrupt(err)
	}
	for {
		result, err := execution.iteratorNext(iterator, next)
		if err != nil {
			return err
		}
		done, found, err := execution.getProperty(result, memory.RefValue(doneName))
		if err != nil {
			return err
		}
		if !found {
			done = memory.UndefinedValue()
		}
		finished, err := valueTruthy(execution.context, done)
		if err != nil {
			return err
		}
		if finished {
			return nil
		}
		value, found, err := execution.getProperty(result, memory.RefValue(valueName))
		if err != nil {
			return err
		}
		if !found {
			value = memory.UndefinedValue()
		}
		snapshot, err := execution.context.DerefArray(array)
		if err != nil {
			return abrupt(err)
		}
		if err := execution.context.SetArrayElement(array, snapshot.Length, value); err != nil {
			return abrupt(err)
		}
	}
}
