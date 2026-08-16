package runtime

import (
	"fmt"
	"math"

	"github.com/JediWattson/gossamer/internal/runtime/memory"
)

const nativeBuiltinBase uint64 = 1 << 63

const (
	nativeFunctionPrototype uint64 = nativeBuiltinBase + iota
	nativeObjectConstructor
	nativeFunctionConstructor
	nativeArrayConstructor
	nativeErrorConstructor
	nativeTypeErrorConstructor
	nativeRangeErrorConstructor
	nativeReferenceErrorConstructor
	nativeObjectCreate
	nativeObjectDefineProperty
	nativeObjectGetPrototypeOf
	nativeObjectSetPrototypeOf
	nativeObjectKeys
	nativeObjectGetOwnPropertyDescriptor
	nativeObjectPrototypeToString
	nativeObjectPrototypeValueOf
	nativeArrayPush
	nativeArrayPop
	nativeArrayJoin
	nativeArraySlice
	nativeStringConstructor
	nativeStringToString
	nativeStringValueOf
	nativeStringCharAt
	nativeStringIncludes
	nativeStringIndexOf
	nativeStringSlice
	nativeStringToUpperCase
	nativeStringToLowerCase
	nativeStringTrim
	nativeStringSplit
	nativeStringValues
	nativeIteratorNext
	nativeArrayMap
	nativeArrayFilter
	nativeArrayForEach
	nativeArrayIncludes
	nativeArrayIndexOf
	nativeArrayKeys
	nativeArrayValues
	nativeArrayEntries
	nativeMapConstructor
	nativeMapGet
	nativeMapSet
	nativeMapHas
	nativeMapDelete
	nativeMapClear
	nativeMapSize
	nativeMapKeys
	nativeMapValues
	nativeMapEntries
	nativeMapForEach
	nativeSetConstructor
	nativeSetAdd
	nativeSetHas
	nativeSetDelete
	nativeSetClear
	nativeSetSize
	nativeSetValues
	nativeSetEntries
	nativeSetForEach
	nativePromiseConstructor
	nativePromiseResolveFunction
	nativePromiseRejectFunction
	nativePromiseResolve
	nativePromiseReject
	nativePromiseThen
	nativePromiseCatch
	nativeQueueMicrotask
	nativeSymbolConstructor
	nativeSymbolFor
	nativeSymbolToString
	nativeSymbolValueOf
	nativeSymbolDescription
	nativeIteratorIdentity
	nativeObjectAssign
	nativeObjectGetOwnPropertyNames
	nativeObjectIs
	nativeObjectPrototypeHasOwnProperty
	nativeArrayIsArray
	nativeFunctionCall
	nativeFunctionApply
	nativeFunctionBind
	nativeBoundFunction
	nativeMathCLZ32
	nativeMathFloor
	nativeMathLog
	nativeMathMin
	nativeMathRandom
	nativeDateConstructor
	nativeDateNow
	nativeGlobalIsNaN
	nativeNumberConstructor
	nativeNumberToString
	nativeNumberValueOf
	nativeRegExpConstructor
	nativeRegExpTest
	nativeRegExpToString
	nativeWeakMapConstructor
	nativeWeakMapGet
	nativeWeakMapSet
	nativeWeakMapHas
	nativeWeakMapDelete
	nativeWeakSetConstructor
	nativeWeakSetAdd
	nativeWeakSetHas
	nativeWeakSetDelete
	nativeArrayConcat
	nativeArrayShift
	nativeArrayUnshift
	nativeArraySplice
)

// Intrinsics is one task-local instantiation of the native ECMAScript
// environment. The Refs are ordinary RegionStore graph nodes; N11 will decide
// how a browser Realm transfers this graph between event-loop tasks.
type Intrinsics struct {
	Global memory.Ref

	ObjectPrototype         memory.Ref
	FunctionPrototype       memory.Ref
	ArrayPrototype          memory.Ref
	StringPrototype         memory.Ref
	IteratorPrototype       memory.Ref
	ErrorPrototype          memory.Ref
	TypeErrorPrototype      memory.Ref
	RangeErrorPrototype     memory.Ref
	ReferenceErrorPrototype memory.Ref
	MapPrototype            memory.Ref
	SetPrototype            memory.Ref
	PromisePrototype        memory.Ref
	SymbolPrototype         memory.Ref
	DatePrototype           memory.Ref
	MathObject              memory.Ref
	NumberPrototype         memory.Ref
	RegExpPrototype         memory.Ref
	WeakMapPrototype        memory.Ref
	WeakSetPrototype        memory.Ref

	ObjectConstructor         memory.Ref
	FunctionConstructor       memory.Ref
	ArrayConstructor          memory.Ref
	StringConstructor         memory.Ref
	MapConstructor            memory.Ref
	SetConstructor            memory.Ref
	PromiseConstructor        memory.Ref
	QueueMicrotask            memory.Ref
	ErrorConstructor          memory.Ref
	TypeErrorConstructor      memory.Ref
	RangeErrorConstructor     memory.Ref
	ReferenceErrorConstructor memory.Ref
	SymbolConstructor         memory.Ref
	SymbolRegistry            memory.Ref
	SymbolIterator            memory.Ref
	DateConstructor           memory.Ref
	IsNaN                     memory.Ref
	NumberConstructor         memory.Ref
	RegExpConstructor         memory.Ref
	WeakMapConstructor        memory.Ref
	WeakSetConstructor        memory.Ref
}

const intrinsicRootCount = 41

// Roots returns every Ref needed to carry one intrinsic environment across an
// explicit ownership boundary. The ordering is private to this package and is
// consumed only by RestoreIntrinsics.
func (intrinsics *Intrinsics) Roots() []memory.Ref {
	if intrinsics == nil {
		return nil
	}
	return []memory.Ref{
		intrinsics.Global,
		intrinsics.ObjectPrototype,
		intrinsics.FunctionPrototype,
		intrinsics.ArrayPrototype,
		intrinsics.StringPrototype,
		intrinsics.IteratorPrototype,
		intrinsics.ErrorPrototype,
		intrinsics.TypeErrorPrototype,
		intrinsics.RangeErrorPrototype,
		intrinsics.ReferenceErrorPrototype,
		intrinsics.MapPrototype,
		intrinsics.SetPrototype,
		intrinsics.PromisePrototype,
		intrinsics.ObjectConstructor,
		intrinsics.FunctionConstructor,
		intrinsics.ArrayConstructor,
		intrinsics.StringConstructor,
		intrinsics.MapConstructor,
		intrinsics.SetConstructor,
		intrinsics.PromiseConstructor,
		intrinsics.QueueMicrotask,
		intrinsics.ErrorConstructor,
		intrinsics.TypeErrorConstructor,
		intrinsics.RangeErrorConstructor,
		intrinsics.ReferenceErrorConstructor,
		intrinsics.SymbolPrototype,
		intrinsics.SymbolConstructor,
		intrinsics.SymbolRegistry,
		intrinsics.SymbolIterator,
		intrinsics.DatePrototype,
		intrinsics.MathObject,
		intrinsics.DateConstructor,
		intrinsics.IsNaN,
		intrinsics.NumberPrototype,
		intrinsics.NumberConstructor,
		intrinsics.RegExpPrototype,
		intrinsics.RegExpConstructor,
		intrinsics.WeakMapPrototype,
		intrinsics.WeakSetPrototype,
		intrinsics.WeakMapConstructor,
		intrinsics.WeakSetConstructor,
	}
}

// RestoreIntrinsics rebuilds the Go index over a graph copied from Roots.
// Copying remains a RegionStore operation; this helper only restores names.
func RestoreIntrinsics(roots []memory.Ref) (*Intrinsics, error) {
	if len(roots) != intrinsicRootCount {
		return nil, fmt.Errorf("runtime: intrinsic root count %d, want %d", len(roots), intrinsicRootCount)
	}
	for index, ref := range roots {
		if ref == (memory.Ref{}) {
			return nil, fmt.Errorf("runtime: intrinsic root %d is empty", index)
		}
	}
	return &Intrinsics{
		Global:                    roots[0],
		ObjectPrototype:           roots[1],
		FunctionPrototype:         roots[2],
		ArrayPrototype:            roots[3],
		StringPrototype:           roots[4],
		IteratorPrototype:         roots[5],
		ErrorPrototype:            roots[6],
		TypeErrorPrototype:        roots[7],
		RangeErrorPrototype:       roots[8],
		ReferenceErrorPrototype:   roots[9],
		MapPrototype:              roots[10],
		SetPrototype:              roots[11],
		PromisePrototype:          roots[12],
		ObjectConstructor:         roots[13],
		FunctionConstructor:       roots[14],
		ArrayConstructor:          roots[15],
		StringConstructor:         roots[16],
		MapConstructor:            roots[17],
		SetConstructor:            roots[18],
		PromiseConstructor:        roots[19],
		QueueMicrotask:            roots[20],
		ErrorConstructor:          roots[21],
		TypeErrorConstructor:      roots[22],
		RangeErrorConstructor:     roots[23],
		ReferenceErrorConstructor: roots[24],
		SymbolPrototype:           roots[25],
		SymbolConstructor:         roots[26],
		SymbolRegistry:            roots[27],
		SymbolIterator:            roots[28],
		DatePrototype:             roots[29],
		MathObject:                roots[30],
		DateConstructor:           roots[31],
		IsNaN:                     roots[32],
		NumberPrototype:           roots[33],
		NumberConstructor:         roots[34],
		RegExpPrototype:           roots[35],
		RegExpConstructor:         roots[36],
		WeakMapPrototype:          roots[37],
		WeakSetPrototype:          roots[38],
		WeakMapConstructor:        roots[39],
		WeakSetConstructor:        roots[40],
	}, nil
}

// Bootstrap installs canonical prototypes, constructors, and global bindings
// in the current task region. It must run before loading a source Program so
// loaded Functions receive Function.prototype and constructor metadata.
func (interpreter *Interpreter) Bootstrap(context *TaskContext) (*Intrinsics, error) {
	if interpreter == nil {
		return nil, fmt.Errorf("runtime: nil interpreter")
	}
	if context == nil || context.Realm == nil {
		return nil, fmt.Errorf("runtime: nil task context")
	}
	if context.intrinsics != nil {
		return context.intrinsics, nil
	}
	if err := interpreter.installBuiltinCallbacks(); err != nil {
		return nil, err
	}

	intrinsics := &Intrinsics{}
	var err error
	intrinsics.ObjectPrototype, err = context.NewHeapObject()
	if err != nil {
		return nil, err
	}
	if err := context.SetPrototype(intrinsics.ObjectPrototype, memory.NullValue()); err != nil {
		return nil, err
	}

	emptyName, err := context.NewString("")
	if err != nil {
		return nil, err
	}
	intrinsics.FunctionPrototype, err = context.Realm.store.AllocNativeFunction(
		context.Owner, context.MemoryRegion, memory.RefValue(emptyName), memory.NullValue(), 0, nativeFunctionPrototype,
	)
	if err != nil {
		return nil, err
	}
	if err := context.SetPrototype(intrinsics.FunctionPrototype, memory.RefValue(intrinsics.ObjectPrototype)); err != nil {
		return nil, err
	}

	intrinsics.ArrayPrototype, err = context.NewArray(0)
	if err != nil {
		return nil, err
	}
	if err := context.SetPrototype(intrinsics.ArrayPrototype, memory.RefValue(intrinsics.ObjectPrototype)); err != nil {
		return nil, err
	}
	for _, target := range []*memory.Ref{
		&intrinsics.StringPrototype,
		&intrinsics.IteratorPrototype,
		&intrinsics.ErrorPrototype,
		&intrinsics.TypeErrorPrototype,
		&intrinsics.RangeErrorPrototype,
		&intrinsics.ReferenceErrorPrototype,
		&intrinsics.MapPrototype,
		&intrinsics.SetPrototype,
		&intrinsics.PromisePrototype,
		&intrinsics.SymbolPrototype,
		&intrinsics.DatePrototype,
		&intrinsics.NumberPrototype,
		&intrinsics.RegExpPrototype,
		&intrinsics.WeakMapPrototype,
		&intrinsics.WeakSetPrototype,
	} {
		*target, err = context.NewHeapObject()
		if err != nil {
			return nil, err
		}
	}
	for _, target := range []memory.Ref{intrinsics.StringPrototype, intrinsics.IteratorPrototype, intrinsics.ErrorPrototype, intrinsics.MapPrototype, intrinsics.SetPrototype, intrinsics.PromisePrototype, intrinsics.SymbolPrototype, intrinsics.DatePrototype, intrinsics.NumberPrototype, intrinsics.RegExpPrototype, intrinsics.WeakMapPrototype, intrinsics.WeakSetPrototype} {
		if err := context.SetPrototype(target, memory.RefValue(intrinsics.ObjectPrototype)); err != nil {
			return nil, err
		}
	}
	for _, target := range []memory.Ref{intrinsics.TypeErrorPrototype, intrinsics.RangeErrorPrototype, intrinsics.ReferenceErrorPrototype} {
		if err := context.SetPrototype(target, memory.RefValue(intrinsics.ErrorPrototype)); err != nil {
			return nil, err
		}
	}

	intrinsics.Global, err = context.NewContext(memory.NullValue())
	if err != nil {
		return nil, err
	}
	context.intrinsics = intrinsics

	if err := intrinsics.installConstructors(context); err != nil {
		context.intrinsics = nil
		return nil, err
	}
	if err := intrinsics.installSymbolBuiltins(context); err != nil {
		context.intrinsics = nil
		return nil, err
	}
	if err := intrinsics.installNumericBuiltins(context); err != nil {
		context.intrinsics = nil
		return nil, err
	}
	if err := intrinsics.installRegExpBuiltins(context); err != nil {
		context.intrinsics = nil
		return nil, err
	}
	if err := intrinsics.installWeakCollectionBuiltins(context); err != nil {
		context.intrinsics = nil
		return nil, err
	}
	if err := intrinsics.installObjectBuiltins(context); err != nil {
		context.intrinsics = nil
		return nil, err
	}
	if err := intrinsics.installFunctionBuiltins(context); err != nil {
		context.intrinsics = nil
		return nil, err
	}
	if err := intrinsics.installArrayBuiltins(context); err != nil {
		context.intrinsics = nil
		return nil, err
	}
	if err := intrinsics.installCollectionBuiltins(context); err != nil {
		context.intrinsics = nil
		return nil, err
	}
	if err := intrinsics.installSymbolIteratorAliases(context); err != nil {
		context.intrinsics = nil
		return nil, err
	}
	if err := intrinsics.installPromiseBuiltins(context); err != nil {
		context.intrinsics = nil
		return nil, err
	}
	if err := intrinsics.installErrorPrototypes(context); err != nil {
		context.intrinsics = nil
		return nil, err
	}
	for _, global := range []struct {
		name  string
		value memory.Value
	}{
		{"undefined", memory.UndefinedValue()},
		{"NaN", memory.NumberValue(math.NaN())},
		{"Infinity", memory.NumberValue(math.Inf(1))},
		{"Object", memory.RefValue(intrinsics.ObjectConstructor)},
		{"Function", memory.RefValue(intrinsics.FunctionConstructor)},
		{"Array", memory.RefValue(intrinsics.ArrayConstructor)},
		{"String", memory.RefValue(intrinsics.StringConstructor)},
		{"Map", memory.RefValue(intrinsics.MapConstructor)},
		{"Set", memory.RefValue(intrinsics.SetConstructor)},
		{"Promise", memory.RefValue(intrinsics.PromiseConstructor)},
		{"queueMicrotask", memory.RefValue(intrinsics.QueueMicrotask)},
		{"Error", memory.RefValue(intrinsics.ErrorConstructor)},
		{"TypeError", memory.RefValue(intrinsics.TypeErrorConstructor)},
		{"RangeError", memory.RefValue(intrinsics.RangeErrorConstructor)},
		{"ReferenceError", memory.RefValue(intrinsics.ReferenceErrorConstructor)},
		{"Symbol", memory.RefValue(intrinsics.SymbolConstructor)},
		{"Math", memory.RefValue(intrinsics.MathObject)},
		{"Date", memory.RefValue(intrinsics.DateConstructor)},
		{"isNaN", memory.RefValue(intrinsics.IsNaN)},
		{"Number", memory.RefValue(intrinsics.NumberConstructor)},
		{"RegExp", memory.RefValue(intrinsics.RegExpConstructor)},
		{"WeakMap", memory.RefValue(intrinsics.WeakMapConstructor)},
		{"WeakSet", memory.RefValue(intrinsics.WeakSetConstructor)},
	} {
		if err := intrinsics.defineGlobal(context, global.name, global.value); err != nil {
			context.intrinsics = nil
			return nil, err
		}
	}
	return intrinsics, nil
}

func (interpreter *Interpreter) installBuiltinCallbacks() error {
	interpreter.builtinOnce.Do(func() {
		interpreter.builtinErr = interpreter.registerBuiltinCallbacks()
	})
	return interpreter.builtinErr
}

func (interpreter *Interpreter) registerBuiltinCallbacks() error {
	callbacks := map[uint64]nativeFunction{
		nativeFunctionPrototype:              builtinFunctionPrototype,
		nativeObjectConstructor:              builtinObjectConstructor,
		nativeFunctionConstructor:            builtinFunctionConstructor,
		nativeArrayConstructor:               builtinArrayConstructor,
		nativeErrorConstructor:               builtinErrorConstructor(memory.ErrorGeneric),
		nativeTypeErrorConstructor:           builtinErrorConstructor(memory.ErrorType),
		nativeRangeErrorConstructor:          builtinErrorConstructor(memory.ErrorRange),
		nativeReferenceErrorConstructor:      builtinErrorConstructor(memory.ErrorReference),
		nativeObjectCreate:                   builtinObjectCreate,
		nativeObjectDefineProperty:           builtinObjectDefineProperty,
		nativeObjectGetPrototypeOf:           builtinObjectGetPrototypeOf,
		nativeObjectSetPrototypeOf:           builtinObjectSetPrototypeOf,
		nativeObjectKeys:                     builtinObjectKeys,
		nativeObjectGetOwnPropertyDescriptor: builtinObjectGetOwnPropertyDescriptor,
		nativeObjectPrototypeToString:        builtinObjectPrototypeToString,
		nativeObjectPrototypeValueOf:         builtinObjectPrototypeValueOf,
		nativeArrayPush:                      builtinArrayPush,
		nativeArrayPop:                       builtinArrayPop,
		nativeArrayJoin:                      builtinArrayJoin,
		nativeArraySlice:                     builtinArraySlice,
		nativeStringConstructor:              builtinStringConstructor,
		nativeStringToString:                 builtinStringToString,
		nativeStringValueOf:                  builtinStringToString,
		nativeStringCharAt:                   builtinStringCharAt,
		nativeStringIncludes:                 builtinStringIncludes,
		nativeStringIndexOf:                  builtinStringIndexOf,
		nativeStringSlice:                    builtinStringSlice,
		nativeStringToUpperCase:              builtinStringToUpperCase,
		nativeStringToLowerCase:              builtinStringToLowerCase,
		nativeStringTrim:                     builtinStringTrim,
		nativeStringSplit:                    builtinStringSplit,
		nativeStringValues:                   builtinStringValues,
		nativeIteratorNext:                   builtinIteratorNext,
		nativeArrayMap:                       builtinArrayMap,
		nativeArrayFilter:                    builtinArrayFilter,
		nativeArrayForEach:                   builtinArrayForEach,
		nativeArrayIncludes:                  builtinArrayIncludes,
		nativeArrayIndexOf:                   builtinArrayIndexOf,
		nativeArrayKeys:                      builtinArrayKeys,
		nativeArrayValues:                    builtinArrayValues,
		nativeArrayEntries:                   builtinArrayEntries,
		nativeMapConstructor:                 builtinMapConstructor,
		nativeMapGet:                         builtinMapGet,
		nativeMapSet:                         builtinMapSet,
		nativeMapHas:                         builtinMapHas,
		nativeMapDelete:                      builtinMapDelete,
		nativeMapClear:                       builtinMapClear,
		nativeMapSize:                        builtinMapSize,
		nativeMapKeys:                        builtinMapKeys,
		nativeMapValues:                      builtinMapValues,
		nativeMapEntries:                     builtinMapEntries,
		nativeMapForEach:                     builtinMapForEach,
		nativeSetConstructor:                 builtinSetConstructor,
		nativeSetAdd:                         builtinSetAdd,
		nativeSetHas:                         builtinSetHas,
		nativeSetDelete:                      builtinSetDelete,
		nativeSetClear:                       builtinSetClear,
		nativeSetSize:                        builtinSetSize,
		nativeSetValues:                      builtinSetValues,
		nativeSetEntries:                     builtinSetEntries,
		nativeSetForEach:                     builtinSetForEach,
		nativePromiseConstructor:             builtinPromiseConstructor,
		nativePromiseResolveFunction:         builtinPromiseResolveFunction,
		nativePromiseRejectFunction:          builtinPromiseRejectFunction,
		nativePromiseResolve:                 builtinPromiseResolve,
		nativePromiseReject:                  builtinPromiseReject,
		nativePromiseThen:                    builtinPromiseThen,
		nativePromiseCatch:                   builtinPromiseCatch,
		nativeQueueMicrotask:                 builtinQueueMicrotask,
		nativeSymbolConstructor:              builtinSymbolConstructor,
		nativeSymbolFor:                      builtinSymbolFor,
		nativeSymbolToString:                 builtinSymbolToString,
		nativeSymbolValueOf:                  builtinSymbolValueOf,
		nativeSymbolDescription:              builtinSymbolDescription,
		nativeIteratorIdentity:               builtinIteratorIdentity,
		nativeObjectAssign:                   builtinObjectAssign,
		nativeObjectGetOwnPropertyNames:      builtinObjectGetOwnPropertyNames,
		nativeObjectIs:                       builtinObjectIs,
		nativeObjectPrototypeHasOwnProperty:  builtinObjectPrototypeHasOwnProperty,
		nativeArrayIsArray:                   builtinArrayIsArray,
		nativeFunctionCall:                   builtinFunctionCall,
		nativeFunctionApply:                  builtinFunctionApply,
		nativeFunctionBind:                   builtinFunctionBind,
		nativeBoundFunction:                  builtinBoundFunction,
		nativeMathCLZ32:                      builtinMathCLZ32,
		nativeMathFloor:                      builtinMathFloor,
		nativeMathLog:                        builtinMathLog,
		nativeMathMin:                        builtinMathMin,
		nativeMathRandom:                     builtinMathRandom,
		nativeDateConstructor:                builtinDateConstructor,
		nativeDateNow:                        builtinDateNow,
		nativeGlobalIsNaN:                    builtinGlobalIsNaN,
		nativeNumberConstructor:              builtinNumberConstructor,
		nativeNumberToString:                 builtinNumberToString,
		nativeNumberValueOf:                  builtinNumberValueOf,
		nativeRegExpConstructor:              builtinRegExpConstructor,
		nativeRegExpTest:                     builtinRegExpTest,
		nativeRegExpToString:                 builtinRegExpToString,
		nativeWeakMapConstructor:             builtinWeakMapConstructor,
		nativeWeakMapGet:                     builtinWeakMapGet,
		nativeWeakMapSet:                     builtinWeakMapSet,
		nativeWeakMapHas:                     builtinWeakMapHas,
		nativeWeakMapDelete:                  builtinWeakMapDelete,
		nativeWeakSetConstructor:             builtinWeakSetConstructor,
		nativeWeakSetAdd:                     builtinWeakSetAdd,
		nativeWeakSetHas:                     builtinWeakSetHas,
		nativeWeakSetDelete:                  builtinWeakSetDelete,
		nativeArrayConcat:                    builtinArrayConcat,
		nativeArrayShift:                     builtinArrayShift,
		nativeArrayUnshift:                   builtinArrayUnshift,
		nativeArraySplice:                    builtinArraySplice,
	}
	for id, callback := range callbacks {
		interpreter.nativeMutex.RLock()
		_, exists := interpreter.natives[id]
		interpreter.nativeMutex.RUnlock()
		if exists {
			continue
		}
		if err := interpreter.registerNative(id, callback); err != nil {
			return err
		}
	}
	return nil
}

func (intrinsics *Intrinsics) installConstructors(context *TaskContext) error {
	constructors := []struct {
		name        string
		arity       uint32
		id          uint64
		prototype   memory.Ref
		destination *memory.Ref
	}{
		{"Object", 1, nativeObjectConstructor, intrinsics.ObjectPrototype, &intrinsics.ObjectConstructor},
		{"Function", 1, nativeFunctionConstructor, intrinsics.FunctionPrototype, &intrinsics.FunctionConstructor},
		{"Array", 1, nativeArrayConstructor, intrinsics.ArrayPrototype, &intrinsics.ArrayConstructor},
		{"Error", 1, nativeErrorConstructor, intrinsics.ErrorPrototype, &intrinsics.ErrorConstructor},
		{"TypeError", 1, nativeTypeErrorConstructor, intrinsics.TypeErrorPrototype, &intrinsics.TypeErrorConstructor},
		{"RangeError", 1, nativeRangeErrorConstructor, intrinsics.RangeErrorPrototype, &intrinsics.RangeErrorConstructor},
		{"ReferenceError", 1, nativeReferenceErrorConstructor, intrinsics.ReferenceErrorPrototype, &intrinsics.ReferenceErrorConstructor},
		{"Date", 7, nativeDateConstructor, intrinsics.DatePrototype, &intrinsics.DateConstructor},
		{"RegExp", 2, nativeRegExpConstructor, intrinsics.RegExpPrototype, &intrinsics.RegExpConstructor},
		{"WeakMap", 0, nativeWeakMapConstructor, intrinsics.WeakMapPrototype, &intrinsics.WeakMapConstructor},
		{"WeakSet", 0, nativeWeakSetConstructor, intrinsics.WeakSetPrototype, &intrinsics.WeakSetConstructor},
	}
	for _, constructor := range constructors {
		name, err := context.NewString(constructor.name)
		if err != nil {
			return err
		}
		ref, err := context.Realm.store.AllocNativeConstructor(context.Owner, context.MemoryRegion, memory.RefValue(name), memory.NullValue(), constructor.arity, constructor.id)
		if err != nil {
			return err
		}
		if err := intrinsics.initializeFunctionWithPrototype(context, ref, memory.RefValue(name), constructor.arity, constructor.prototype); err != nil {
			return err
		}
		*constructor.destination = ref
	}
	return nil
}

func (intrinsics *Intrinsics) initializeFunction(context *TaskContext, function memory.Ref, name memory.Value, arity uint32, constructible bool) error {
	prototype := memory.Ref{}
	if constructible {
		var err error
		prototype, err = context.NewHeapObject()
		if err != nil {
			return err
		}
	}
	return intrinsics.initializeFunctionWithPrototype(context, function, name, arity, prototype)
}

func (intrinsics *Intrinsics) initializeFunctionWithPrototype(context *TaskContext, function memory.Ref, name memory.Value, arity uint32, prototype memory.Ref) error {
	if err := context.SetPrototype(function, memory.RefValue(intrinsics.FunctionPrototype)); err != nil {
		return err
	}
	nameValue := name
	if !nameValue.IsRef() {
		var err error
		nameValue, err = newStringValue(context, "")
		if err != nil {
			return err
		}
	}
	if err := defineData(context, function, "name", nameValue, false, false, true); err != nil {
		return err
	}
	if err := defineData(context, function, "length", memory.NumberValue(float64(arity)), false, false, true); err != nil {
		return err
	}
	if prototype != (memory.Ref{}) {
		if err := defineData(context, function, "prototype", memory.RefValue(prototype), true, false, false); err != nil {
			return err
		}
		if err := defineData(context, prototype, "constructor", memory.RefValue(function), true, false, true); err != nil {
			return err
		}
	}
	return nil
}

func (intrinsics *Intrinsics) installObjectBuiltins(context *TaskContext) error {
	for _, method := range []struct {
		target memory.Ref
		name   string
		arity  uint32
		id     uint64
	}{
		{intrinsics.ObjectConstructor, "create", 2, nativeObjectCreate},
		{intrinsics.ObjectConstructor, "defineProperty", 3, nativeObjectDefineProperty},
		{intrinsics.ObjectConstructor, "getPrototypeOf", 1, nativeObjectGetPrototypeOf},
		{intrinsics.ObjectConstructor, "setPrototypeOf", 2, nativeObjectSetPrototypeOf},
		{intrinsics.ObjectConstructor, "keys", 1, nativeObjectKeys},
		{intrinsics.ObjectConstructor, "getOwnPropertyDescriptor", 2, nativeObjectGetOwnPropertyDescriptor},
		{intrinsics.ObjectConstructor, "assign", 2, nativeObjectAssign},
		{intrinsics.ObjectConstructor, "getOwnPropertyNames", 1, nativeObjectGetOwnPropertyNames},
		{intrinsics.ObjectConstructor, "is", 2, nativeObjectIs},
		{intrinsics.ObjectPrototype, "toString", 0, nativeObjectPrototypeToString},
		{intrinsics.ObjectPrototype, "valueOf", 0, nativeObjectPrototypeValueOf},
		{intrinsics.ObjectPrototype, "hasOwnProperty", 1, nativeObjectPrototypeHasOwnProperty},
	} {
		function, err := intrinsics.newBuiltinMethod(context, method.name, method.arity, method.id)
		if err != nil {
			return err
		}
		if err := defineData(context, method.target, method.name, memory.RefValue(function), true, false, true); err != nil {
			return err
		}
	}
	return nil
}

func (intrinsics *Intrinsics) installArrayBuiltins(context *TaskContext) error {
	isArray, err := intrinsics.newBuiltinMethod(context, "isArray", 1, nativeArrayIsArray)
	if err != nil {
		return err
	}
	if err := defineData(context, intrinsics.ArrayConstructor, "isArray", memory.RefValue(isArray), true, false, true); err != nil {
		return err
	}
	for _, method := range []struct {
		name  string
		arity uint32
		id    uint64
	}{
		{"push", 1, nativeArrayPush},
		{"pop", 0, nativeArrayPop},
		{"join", 1, nativeArrayJoin},
		{"slice", 2, nativeArraySlice},
		{"concat", 1, nativeArrayConcat},
		{"shift", 0, nativeArrayShift},
		{"unshift", 1, nativeArrayUnshift},
		{"splice", 2, nativeArraySplice},
	} {
		function, err := intrinsics.newBuiltinMethod(context, method.name, method.arity, method.id)
		if err != nil {
			return err
		}
		if err := defineData(context, intrinsics.ArrayPrototype, method.name, memory.RefValue(function), true, false, true); err != nil {
			return err
		}
	}
	return nil
}

func (intrinsics *Intrinsics) installErrorPrototypes(context *TaskContext) error {
	for _, item := range []struct {
		prototype   memory.Ref
		name        string
		constructor memory.Ref
	}{
		{intrinsics.ErrorPrototype, "Error", intrinsics.ErrorConstructor},
		{intrinsics.TypeErrorPrototype, "TypeError", intrinsics.TypeErrorConstructor},
		{intrinsics.RangeErrorPrototype, "RangeError", intrinsics.RangeErrorConstructor},
		{intrinsics.ReferenceErrorPrototype, "ReferenceError", intrinsics.ReferenceErrorConstructor},
	} {
		name, err := newStringValue(context, item.name)
		if err != nil {
			return err
		}
		empty, err := newStringValue(context, "")
		if err != nil {
			return err
		}
		if err := defineData(context, item.prototype, "name", name, true, false, true); err != nil {
			return err
		}
		if err := defineData(context, item.prototype, "message", empty, true, false, true); err != nil {
			return err
		}
		if err := defineData(context, item.prototype, "constructor", memory.RefValue(item.constructor), true, false, true); err != nil {
			return err
		}
	}
	return nil
}

func (intrinsics *Intrinsics) newBuiltinMethod(context *TaskContext, name string, arity uint32, id uint64) (memory.Ref, error) {
	nameRef, err := context.NewString(name)
	if err != nil {
		return memory.Ref{}, err
	}
	function, err := context.Realm.store.AllocNativeFunction(context.Owner, context.MemoryRegion, memory.RefValue(nameRef), memory.NullValue(), arity, id)
	if err != nil {
		return memory.Ref{}, err
	}
	if err := intrinsics.initializeFunctionWithPrototype(context, function, memory.RefValue(nameRef), arity, memory.Ref{}); err != nil {
		return memory.Ref{}, err
	}
	return function, nil
}

func (intrinsics *Intrinsics) defineGlobal(context *TaskContext, name string, value memory.Value) error {
	nameRef, err := context.NewString(name)
	if err != nil {
		return err
	}
	mutable := name != "undefined" && name != "NaN" && name != "Infinity"
	if err := context.DeclareBinding(intrinsics.Global, nameRef, mutable); err != nil {
		return err
	}
	return context.InitializeBinding(intrinsics.Global, nameRef, value)
}

func defineData(context *TaskContext, object memory.Ref, name string, value memory.Value, writable, enumerable, configurable bool) error {
	nameRef, err := context.NewString(name)
	if err != nil {
		return err
	}
	return context.DefineProperty(object, nameRef, memory.DataProperty(value, writable, enumerable, configurable))
}

func newStringValue(context *TaskContext, text string) (memory.Value, error) {
	ref, err := context.NewString(text)
	if err != nil {
		return memory.Value{}, err
	}
	return memory.RefValue(ref), nil
}

func builtinFunctionPrototype(_ *execution, _ memory.Ref, _ memory.Function, _ memory.Value, _ []memory.Value) (memory.Value, error) {
	return memory.UndefinedValue(), nil
}

func builtinObjectConstructor(execution *execution, _ memory.Ref, _ memory.Function, this memory.Value, arguments []memory.Value) (memory.Value, error) {
	if len(arguments) != 0 && arguments[0].Kind() != memory.ValueUndefined && arguments[0].Kind() != memory.ValueNull {
		if object, err := isObjectValue(execution.context, arguments[0]); err != nil {
			return memory.Value{}, err
		} else if object {
			return arguments[0], nil
		}
	}
	if this.IsRef() {
		return this, nil
	}
	object, err := execution.context.NewHeapObject()
	return memory.RefValue(object), err
}

func builtinFunctionConstructor(_ *execution, _ memory.Ref, _ memory.Function, _ memory.Value, _ []memory.Value) (memory.Value, error) {
	return memory.Value{}, fmt.Errorf("%w: dynamic Function construction is not implemented", ErrOperandType)
}

func builtinArrayConstructor(execution *execution, _ memory.Ref, _ memory.Function, _ memory.Value, arguments []memory.Value) (memory.Value, error) {
	length := uint32(0)
	if len(arguments) == 1 && arguments[0].Kind() == memory.ValueNumber {
		var err error
		length, err = requireUint32(arguments[0], "Array length", true)
		if err != nil {
			return memory.Value{}, err
		}
	}
	array, err := execution.context.NewArray(length)
	if err != nil {
		return memory.Value{}, err
	}
	if !(len(arguments) == 1 && arguments[0].Kind() == memory.ValueNumber) {
		for index, value := range arguments {
			if err := execution.context.SetArrayElement(array, uint32(index), value); err != nil {
				return memory.Value{}, err
			}
		}
	}
	return memory.RefValue(array), nil
}

func builtinErrorConstructor(kind memory.ErrorKind) nativeFunction {
	return func(execution *execution, _ memory.Ref, _ memory.Function, _ memory.Value, arguments []memory.Value) (memory.Value, error) {
		message := memory.NullValue()
		if len(arguments) != 0 && arguments[0].Kind() != memory.ValueUndefined {
			text, err := execution.toString(arguments[0])
			if err != nil {
				return memory.Value{}, err
			}
			ref, err := execution.context.NewString(text)
			if err != nil {
				return memory.Value{}, err
			}
			message = memory.RefValue(ref)
		}
		ref, err := execution.context.NewError(kind, message)
		return memory.RefValue(ref), err
	}
}

func builtinObjectCreate(execution *execution, _ memory.Ref, _ memory.Function, _ memory.Value, arguments []memory.Value) (memory.Value, error) {
	prototype := argument(arguments, 0)
	if prototype.Kind() != memory.ValueNull {
		if !prototype.IsRef() {
			return memory.Value{}, fmt.Errorf("%w: Object.create prototype must be null or an Object", ErrOperandType)
		}
		if _, err := execution.context.DerefObjectHeader(prototype.Ref()); err != nil {
			return memory.Value{}, fmt.Errorf("%w: Object.create prototype: %v", ErrOperandType, err)
		}
	}
	object, err := execution.context.NewHeapObject()
	if err != nil {
		return memory.Value{}, err
	}
	if err := execution.context.SetPrototype(object, prototype); err != nil {
		return memory.Value{}, err
	}
	return memory.RefValue(object), nil
}

func builtinObjectDefineProperty(execution *execution, _ memory.Ref, _ memory.Function, _ memory.Value, arguments []memory.Value) (memory.Value, error) {
	object, err := requireObjectLike(execution.context, argument(arguments, 0), "Object.defineProperty target")
	if err != nil {
		return memory.Value{}, err
	}
	name, err := execution.propertyName(argument(arguments, 1))
	if err != nil {
		return memory.Value{}, err
	}
	descriptorRef, err := requireObjectLike(execution.context, argument(arguments, 2), "property descriptor")
	if err != nil {
		return memory.Value{}, err
	}
	value, hasValue, err := builtinRead(execution, descriptorRef, "value")
	if err != nil {
		return memory.Value{}, err
	}
	getter, hasGetter, err := builtinRead(execution, descriptorRef, "get")
	if err != nil {
		return memory.Value{}, err
	}
	setter, hasSetter, err := builtinRead(execution, descriptorRef, "set")
	if err != nil {
		return memory.Value{}, err
	}
	writable, err := builtinBool(execution, descriptorRef, "writable")
	if err != nil {
		return memory.Value{}, err
	}
	enumerable, err := builtinBool(execution, descriptorRef, "enumerable")
	if err != nil {
		return memory.Value{}, err
	}
	configurable, err := builtinBool(execution, descriptorRef, "configurable")
	if err != nil {
		return memory.Value{}, err
	}
	var descriptor memory.Property
	if hasGetter || hasSetter {
		if hasValue || writable {
			return memory.Value{}, fmt.Errorf("%w: mixed data and accessor descriptor", ErrOperandType)
		}
		if !hasGetter {
			getter = memory.UndefinedValue()
		}
		if !hasSetter {
			setter = memory.UndefinedValue()
		}
		descriptor = memory.AccessorProperty(getter, setter, enumerable, configurable)
	} else {
		if !hasValue {
			value = memory.UndefinedValue()
		}
		descriptor = memory.DataProperty(value, writable, enumerable, configurable)
	}
	if err := execution.context.DefineProperty(object, name, descriptor); err != nil {
		return memory.Value{}, err
	}
	return memory.RefValue(object), nil
}

func builtinObjectGetPrototypeOf(execution *execution, _ memory.Ref, _ memory.Function, _ memory.Value, arguments []memory.Value) (memory.Value, error) {
	object, err := requireObjectLike(execution.context, argument(arguments, 0), "Object.getPrototypeOf target")
	if err != nil {
		return memory.Value{}, err
	}
	header, err := execution.context.DerefObjectHeader(object)
	return header.Prototype, err
}

func builtinObjectSetPrototypeOf(execution *execution, _ memory.Ref, _ memory.Function, _ memory.Value, arguments []memory.Value) (memory.Value, error) {
	object, err := requireObjectLike(execution.context, argument(arguments, 0), "Object.setPrototypeOf target")
	if err != nil {
		return memory.Value{}, err
	}
	prototype := argument(arguments, 1)
	if prototype.Kind() != memory.ValueNull {
		if _, err := requireObjectLike(execution.context, prototype, "Object.setPrototypeOf prototype"); err != nil {
			return memory.Value{}, err
		}
	}
	if err := execution.context.SetPrototype(object, prototype); err != nil {
		return memory.Value{}, err
	}
	return memory.RefValue(object), nil
}

func builtinObjectKeys(execution *execution, _ memory.Ref, _ memory.Function, _ memory.Value, arguments []memory.Value) (memory.Value, error) {
	object, err := requireObjectLike(execution.context, argument(arguments, 0), "Object.keys target")
	if err != nil {
		return memory.Value{}, err
	}
	keys := make([]memory.Value, 0)
	if kind, err := execution.context.HeapKind(object); err != nil {
		return memory.Value{}, err
	} else if kind == memory.HeapArray {
		array, err := execution.context.DerefArray(object)
		if err != nil {
			return memory.Value{}, err
		}
		for _, element := range array.Elements {
			ref, err := execution.context.NewString(fmt.Sprintf("%d", element.Index))
			if err != nil {
				return memory.Value{}, err
			}
			keys = append(keys, memory.RefValue(ref))
		}
	}
	header, err := execution.context.DerefObjectHeader(object)
	if err != nil {
		return memory.Value{}, err
	}
	for _, property := range header.Properties {
		kind, err := execution.context.HeapKind(property.Name)
		if err != nil {
			return memory.Value{}, err
		}
		if property.Enumerable && kind == memory.HeapString {
			keys = append(keys, memory.RefValue(property.Name))
		}
	}
	array, err := execution.context.NewArray(uint32(len(keys)))
	if err != nil {
		return memory.Value{}, err
	}
	for index, key := range keys {
		if err := execution.context.SetArrayElement(array, uint32(index), key); err != nil {
			return memory.Value{}, err
		}
	}
	return memory.RefValue(array), nil
}

func builtinObjectGetOwnPropertyDescriptor(execution *execution, _ memory.Ref, _ memory.Function, _ memory.Value, arguments []memory.Value) (memory.Value, error) {
	object, err := requireObjectLike(execution.context, argument(arguments, 0), "Object.getOwnPropertyDescriptor target")
	if err != nil {
		return memory.Value{}, err
	}
	name, err := execution.propertyName(argument(arguments, 1))
	if err != nil {
		return memory.Value{}, err
	}
	descriptor, found, err := execution.context.GetOwnPropertyDescriptor(object, name)
	if err != nil || !found {
		return memory.UndefinedValue(), err
	}
	result, err := execution.context.NewHeapObject()
	if err != nil {
		return memory.Value{}, err
	}
	if descriptor.Kind == memory.PropertyData {
		if err := defineData(execution.context, result, "value", descriptor.Value, true, true, true); err != nil {
			return memory.Value{}, err
		}
		if err := defineData(execution.context, result, "writable", memory.BoolValue(descriptor.Writable), true, true, true); err != nil {
			return memory.Value{}, err
		}
	} else {
		if err := defineData(execution.context, result, "get", descriptor.Getter, true, true, true); err != nil {
			return memory.Value{}, err
		}
		if err := defineData(execution.context, result, "set", descriptor.Setter, true, true, true); err != nil {
			return memory.Value{}, err
		}
	}
	if err := defineData(execution.context, result, "enumerable", memory.BoolValue(descriptor.Enumerable), true, true, true); err != nil {
		return memory.Value{}, err
	}
	if err := defineData(execution.context, result, "configurable", memory.BoolValue(descriptor.Configurable), true, true, true); err != nil {
		return memory.Value{}, err
	}
	return memory.RefValue(result), nil
}

func builtinObjectPrototypeToString(execution *execution, _ memory.Ref, _ memory.Function, this memory.Value, _ []memory.Value) (memory.Value, error) {
	tag := "Object"
	if this.Kind() == memory.ValueUndefined {
		tag = "Undefined"
	} else if this.Kind() == memory.ValueNull {
		tag = "Null"
	} else if this.IsRef() {
		kind, err := execution.context.HeapKind(this.Ref())
		if err != nil {
			return memory.Value{}, err
		}
		switch kind {
		case memory.HeapArray:
			tag = "Array"
		case memory.HeapFunction:
			tag = "Function"
		case memory.HeapError:
			tag = "Error"
		case memory.HeapMap:
			tag = "Map"
		case memory.HeapSet:
			tag = "Set"
		case memory.HeapPromise:
			tag = "Promise"
		}
	}
	ref, err := execution.context.NewString("[object " + tag + "]")
	return memory.RefValue(ref), err
}

func builtinObjectPrototypeValueOf(execution *execution, _ memory.Ref, _ memory.Function, this memory.Value, _ []memory.Value) (memory.Value, error) {
	if _, err := requireObjectLike(execution.context, this, "Object.prototype.valueOf receiver"); err != nil {
		return memory.Value{}, err
	}
	return this, nil
}

func builtinArrayPush(execution *execution, _ memory.Ref, _ memory.Function, this memory.Value, arguments []memory.Value) (memory.Value, error) {
	array, err := requireArray(execution.context, this)
	if err != nil {
		return memory.Value{}, err
	}
	snapshot, err := execution.context.DerefArray(array)
	if err != nil {
		return memory.Value{}, err
	}
	for index, value := range arguments {
		position := uint64(snapshot.Length) + uint64(index)
		if position >= uint64(math.MaxUint32) {
			return memory.Value{}, memory.ErrInvalidIndex
		}
		if err := execution.context.SetArrayElement(array, uint32(position), value); err != nil {
			return memory.Value{}, err
		}
	}
	return memory.NumberValue(float64(snapshot.Length) + float64(len(arguments))), nil
}

func builtinArrayPop(execution *execution, _ memory.Ref, _ memory.Function, this memory.Value, _ []memory.Value) (memory.Value, error) {
	array, err := requireArray(execution.context, this)
	if err != nil {
		return memory.Value{}, err
	}
	snapshot, err := execution.context.DerefArray(array)
	if err != nil || snapshot.Length == 0 {
		return memory.UndefinedValue(), err
	}
	index := snapshot.Length - 1
	value, found, err := execution.context.ArrayElement(array, index)
	if err != nil {
		return memory.Value{}, err
	}
	if _, err := execution.context.DeleteArrayElement(array, index); err != nil {
		return memory.Value{}, err
	}
	if err := execution.context.SetArrayLength(array, index); err != nil {
		return memory.Value{}, err
	}
	if !found {
		return memory.UndefinedValue(), nil
	}
	return value, nil
}

func builtinArrayJoin(execution *execution, _ memory.Ref, _ memory.Function, this memory.Value, arguments []memory.Value) (memory.Value, error) {
	array, err := requireArray(execution.context, this)
	if err != nil {
		return memory.Value{}, err
	}
	separator := ","
	if len(arguments) != 0 && arguments[0].Kind() != memory.ValueUndefined {
		separator, err = execution.toString(arguments[0])
		if err != nil {
			return memory.Value{}, err
		}
	}
	snapshot, err := execution.context.DerefArray(array)
	if err != nil {
		return memory.Value{}, err
	}
	text := ""
	for index := uint32(0); index < snapshot.Length; index++ {
		if index != 0 {
			text += separator
		}
		value, found, err := execution.context.ArrayElement(array, index)
		if err != nil {
			return memory.Value{}, err
		}
		if !found || value.Kind() == memory.ValueUndefined || value.Kind() == memory.ValueNull {
			continue
		}
		part, err := execution.toString(value)
		if err != nil {
			return memory.Value{}, err
		}
		text += part
	}
	ref, err := execution.context.NewString(text)
	return memory.RefValue(ref), err
}

func builtinArraySlice(execution *execution, _ memory.Ref, _ memory.Function, this memory.Value, arguments []memory.Value) (memory.Value, error) {
	array, err := requireArray(execution.context, this)
	if err != nil {
		return memory.Value{}, err
	}
	snapshot, err := execution.context.DerefArray(array)
	if err != nil {
		return memory.Value{}, err
	}
	start, err := relativeIndex(execution, argument(arguments, 0), snapshot.Length, 0)
	if err != nil {
		return memory.Value{}, err
	}
	endValue := memory.UndefinedValue()
	if len(arguments) > 1 {
		endValue = arguments[1]
	}
	end, err := relativeIndex(execution, endValue, snapshot.Length, snapshot.Length)
	if err != nil {
		return memory.Value{}, err
	}
	if end < start {
		end = start
	}
	result, err := execution.context.NewArray(end - start)
	if err != nil {
		return memory.Value{}, err
	}
	for source := start; source < end; source++ {
		value, found, err := execution.context.ArrayElement(array, source)
		if err != nil {
			return memory.Value{}, err
		}
		if found {
			if err := execution.context.SetArrayElement(result, source-start, value); err != nil {
				return memory.Value{}, err
			}
		}
	}
	return memory.RefValue(result), nil
}

func argument(arguments []memory.Value, index int) memory.Value {
	if index < 0 || index >= len(arguments) {
		return memory.UndefinedValue()
	}
	return arguments[index]
}

func requireObjectLike(context *TaskContext, value memory.Value, label string) (memory.Ref, error) {
	ref, err := requireRef(value, label)
	if err != nil {
		return memory.Ref{}, err
	}
	if _, err := context.DerefObjectHeader(ref); err != nil {
		return memory.Ref{}, fmt.Errorf("%w: %s", ErrOperandType, label)
	}
	return ref, nil
}

func requireArray(context *TaskContext, value memory.Value) (memory.Ref, error) {
	ref, err := requireRef(value, "Array receiver")
	if err != nil {
		return memory.Ref{}, err
	}
	if kind, err := context.HeapKind(ref); err != nil {
		return memory.Ref{}, err
	} else if kind != memory.HeapArray {
		return memory.Ref{}, fmt.Errorf("%w: receiver is not an Array", ErrOperandType)
	}
	return ref, nil
}

func builtinRead(execution *execution, object memory.Ref, name string) (memory.Value, bool, error) {
	nameRef, err := execution.context.NewString(name)
	if err != nil {
		return memory.Value{}, false, err
	}
	return execution.getProperty(memory.RefValue(object), memory.RefValue(nameRef))
}

func builtinBool(execution *execution, object memory.Ref, name string) (bool, error) {
	value, found, err := builtinRead(execution, object, name)
	if err != nil {
		return false, err
	}
	if !found {
		return false, nil
	}
	truthy, err := valueTruthy(execution.context, value)
	return truthy, err
}

func relativeIndex(execution *execution, value memory.Value, length, fallback uint32) (uint32, error) {
	if value.Kind() == memory.ValueUndefined {
		return fallback, nil
	}
	number, err := execution.toNumber(value)
	if err != nil {
		return 0, err
	}
	if math.IsNaN(number) {
		return 0, nil
	}
	integer := int64(math.Trunc(number))
	if integer < 0 {
		integer += int64(length)
		if integer < 0 {
			return 0, nil
		}
	}
	if integer > int64(length) {
		return length, nil
	}
	return uint32(integer), nil
}
