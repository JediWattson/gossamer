package nativeengine

import (
	"fmt"
	"math"
	"time"

	"github.com/JediWattson/gossamer/internal/browser"
	browserruntime "github.com/JediWattson/gossamer/internal/runtime"
	"github.com/JediWattson/gossamer/internal/runtime/memory"
)

const (
	nativeGlobalSetTimeout uint64 = 11_000 + iota
	nativeGlobalClearTimeout
	nativeGlobalSetInterval
	nativeGlobalClearInterval
	nativeGlobalRequestAnimationFrame
	nativeGlobalCancelAnimationFrame
	nativePerformanceNow
)

func (realm *Realm) globalSetTimeout(context *browserruntime.TaskContext, _ memory.Value, arguments []memory.Value) (memory.Value, error) {
	callback := argument(arguments, 0)
	if err := requireFunction(context, callback); err != nil {
		return memory.Value{}, err
	}
	delay, err := timeoutDuration(argument(arguments, 1))
	if err != nil {
		return memory.Value{}, err
	}
	handle, err := realm.retainCallbackLocked(context, callback)
	if err != nil {
		return memory.Value{}, err
	}
	timer, err := realm.host.SetTimeout(handle, delay)
	if err != nil {
		_, _ = context.MapDelete(realm.bindings.callbackCache, memory.NumberValue(float64(handle)))
		return memory.Value{}, err
	}
	realm.timerCallbacks[timer] = handle
	return memory.NumberValue(float64(timer)), nil
}

func (realm *Realm) globalClearTimeout(context *browserruntime.TaskContext, _ memory.Value, arguments []memory.Value) (memory.Value, error) {
	timer := timerID(argument(arguments, 0))
	return realm.clearTimer(context, timer)
}

func (realm *Realm) globalSetInterval(context *browserruntime.TaskContext, _ memory.Value, arguments []memory.Value) (memory.Value, error) {
	callback := argument(arguments, 0)
	if err := requireFunction(context, callback); err != nil {
		return memory.Value{}, err
	}
	delay, err := timeoutDuration(argument(arguments, 1))
	if err != nil {
		return memory.Value{}, err
	}
	handle, err := realm.retainCallbackLocked(context, callback)
	if err != nil {
		return memory.Value{}, err
	}
	timer, err := realm.host.SetTimeout(handle, delay)
	if err != nil {
		_, _ = context.MapDelete(realm.bindings.callbackCache, memory.NumberValue(float64(handle)))
		return memory.Value{}, err
	}
	realm.intervalCallbacks[timer] = handle
	realm.intervalTimers[timer] = timer
	realm.callbackIntervals[handle] = timer
	realm.intervalDelays[timer] = delay
	return memory.NumberValue(float64(timer)), nil
}

func (realm *Realm) globalClearInterval(context *browserruntime.TaskContext, _ memory.Value, arguments []memory.Value) (memory.Value, error) {
	timer := timerID(argument(arguments, 0))
	return realm.clearTimer(context, timer)
}

func (realm *Realm) clearTimer(context *browserruntime.TaskContext, timer browser.TimerID) (memory.Value, error) {
	if timer == 0 {
		return memory.UndefinedValue(), nil
	}
	hostTimer := timer
	if current, found := realm.intervalTimers[timer]; found {
		hostTimer = current
	}
	if err := realm.host.ClearTimeout(hostTimer); err != nil {
		return memory.Value{}, err
	}
	if handle, found := realm.intervalCallbacks[timer]; found {
		delete(realm.intervalCallbacks, timer)
		delete(realm.intervalTimers, timer)
		delete(realm.callbackIntervals, handle)
		delete(realm.intervalDelays, timer)
		if _, err := context.MapDelete(realm.bindings.callbackCache, memory.NumberValue(float64(handle))); err != nil {
			return memory.Value{}, err
		}
		return memory.UndefinedValue(), nil
	}
	handle, found := realm.timerCallbacks[timer]
	if found {
		delete(realm.timerCallbacks, timer)
		if _, err := context.MapDelete(realm.bindings.callbackCache, memory.NumberValue(float64(handle))); err != nil {
			return memory.Value{}, err
		}
	}
	return memory.UndefinedValue(), nil
}

func (realm *Realm) globalRequestAnimationFrame(context *browserruntime.TaskContext, _ memory.Value, arguments []memory.Value) (memory.Value, error) {
	callback := argument(arguments, 0)
	if err := requireFunction(context, callback); err != nil {
		return memory.Value{}, err
	}
	host, ok := realm.host.(browser.AnimationFrameHost)
	if !ok {
		return memory.Value{}, fmt.Errorf("nativeengine: browser host does not expose animation frames")
	}
	handle, err := realm.retainCallbackLocked(context, callback)
	if err != nil {
		return memory.Value{}, err
	}
	frame, err := host.RequestAnimationFrame(handle)
	if err != nil {
		_, _ = context.MapDelete(realm.bindings.callbackCache, memory.NumberValue(float64(handle)))
		return memory.Value{}, err
	}
	realm.animationCallbacks[frame] = handle
	return memory.NumberValue(float64(frame)), nil
}

func (realm *Realm) globalCancelAnimationFrame(context *browserruntime.TaskContext, _ memory.Value, arguments []memory.Value) (memory.Value, error) {
	frame := browser.AnimationFrameID(timerID(argument(arguments, 0)))
	if frame == 0 {
		return memory.UndefinedValue(), nil
	}
	host, ok := realm.host.(browser.AnimationFrameHost)
	if !ok {
		return memory.Value{}, fmt.Errorf("nativeengine: browser host does not expose animation frames")
	}
	if err := host.CancelAnimationFrame(frame); err != nil {
		return memory.Value{}, err
	}
	if handle, found := realm.animationCallbacks[frame]; found {
		delete(realm.animationCallbacks, frame)
		if _, err := context.MapDelete(realm.bindings.callbackCache, memory.NumberValue(float64(handle))); err != nil {
			return memory.Value{}, err
		}
	}
	return memory.UndefinedValue(), nil
}

func (realm *Realm) performanceNow(_ *browserruntime.TaskContext, _ memory.Value, _ []memory.Value) (memory.Value, error) {
	host, ok := realm.host.(browser.AnimationFrameHost)
	if !ok {
		return memory.Value{}, fmt.Errorf("nativeengine: browser host does not expose performance clock")
	}
	return memory.NumberValue(host.PerformanceNow()), nil
}

func (realm *Realm) retainCallbackLocked(context *browserruntime.TaskContext, callback memory.Value) (browser.ValueHandle, error) {
	return realm.retainValueLocked(context, callback)
}

func (realm *Realm) retainValueLocked(context *browserruntime.TaskContext, value memory.Value) (browser.ValueHandle, error) {
	if realm.bindings == nil || realm.bindings.callbackCache == (memory.Ref{}) {
		return 0, fmt.Errorf("nativeengine: callback cache is unavailable")
	}
	realm.nextCallback++
	if realm.nextCallback == 0 || uint64(realm.nextCallback) > maxExactInteger {
		return 0, fmt.Errorf("nativeengine: callback handle space exhausted")
	}
	handle := realm.nextCallback
	if err := context.MapSet(realm.bindings.callbackCache, memory.NumberValue(float64(handle)), value); err != nil {
		return 0, err
	}
	return handle, nil
}

func (realm *Realm) invokeCallbackLocked(context *browserruntime.TaskContext, handle browser.ValueHandle) error {
	return realm.invokeCallbackArgumentsLocked(context, handle, true, memory.UndefinedValue())
}

func (realm *Realm) invokeCallbackArgumentsLocked(context *browserruntime.TaskContext, handle browser.ValueHandle, remove bool, this memory.Value, arguments ...memory.Value) error {
	if handle == 0 || realm.bindings == nil {
		return fmt.Errorf("%w: %d", ErrUnknownValueHandle, handle)
	}
	key := memory.NumberValue(float64(handle))
	callback, found, err := context.MapGet(realm.bindings.callbackCache, key)
	if err != nil {
		return err
	}
	if !found {
		return fmt.Errorf("%w: %d", ErrUnknownValueHandle, handle)
	}
	interval, recurring := realm.callbackIntervals[handle]
	if remove && !recurring {
		if _, err := context.MapDelete(realm.bindings.callbackCache, key); err != nil {
			return err
		}
	}
	if !recurring {
		for timer, callbackHandle := range realm.timerCallbacks {
			if callbackHandle == handle {
				delete(realm.timerCallbacks, timer)
				break
			}
		}
	}
	if err := requireFunction(context, callback); err != nil {
		return err
	}
	_, callErr := realm.interpreter.CallWithoutCheckpoint(context, callback.Ref(), this, arguments...)
	if !recurring {
		return callErr
	}
	if current, active := realm.intervalCallbacks[interval]; !active || current != handle {
		return callErr
	}
	next, scheduleErr := realm.host.SetTimeout(handle, realm.intervalDelays[interval])
	if scheduleErr != nil {
		delete(realm.intervalCallbacks, interval)
		delete(realm.intervalTimers, interval)
		delete(realm.callbackIntervals, handle)
		delete(realm.intervalDelays, interval)
		_, _ = context.MapDelete(realm.bindings.callbackCache, key)
		if callErr != nil {
			return fmt.Errorf("%v; interval reschedule: %w", callErr, scheduleErr)
		}
		return scheduleErr
	}
	realm.intervalTimers[interval] = next
	return callErr
}

func requireFunction(context *browserruntime.TaskContext, value memory.Value) error {
	if !value.IsRef() {
		return fmt.Errorf("%w: callback is not callable", browserruntime.ErrOperandType)
	}
	kind, err := context.HeapKind(value.Ref())
	if err != nil {
		return err
	}
	if kind != memory.HeapFunction {
		return fmt.Errorf("%w: callback is not callable", browserruntime.ErrOperandType)
	}
	return nil
}

func timeoutDuration(value memory.Value) (time.Duration, error) {
	milliseconds := 0.0
	switch value.Kind() {
	case memory.ValueUndefined:
	case memory.ValueNull:
	case memory.ValueBool:
		if value.Bool() {
			milliseconds = 1
		}
	case memory.ValueNumber:
		milliseconds = value.Number()
	default:
		return 0, fmt.Errorf("%w: timeout delay is not numeric", browserruntime.ErrOperandType)
	}
	if math.IsNaN(milliseconds) || milliseconds < 0 {
		milliseconds = 0
	}
	maximum := float64(math.MaxInt64) / float64(time.Millisecond)
	if math.IsInf(milliseconds, 1) || milliseconds > maximum {
		milliseconds = maximum
	}
	return time.Duration(milliseconds * float64(time.Millisecond)), nil
}

func timerID(value memory.Value) browser.TimerID {
	if value.Kind() != memory.ValueNumber {
		return 0
	}
	number := value.Number()
	if math.IsNaN(number) || math.IsInf(number, 0) || number <= 0 || number > maxExactInteger {
		return 0
	}
	return browser.TimerID(uint64(number))
}

const maxExactInteger = 1<<53 - 1
