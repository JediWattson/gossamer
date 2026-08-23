package memory

import (
	"strings"
	"unsafe"

	"github.com/JediWattson/gossamer/internal/runtime/ownership"
)

// RegionState describes whether storage is mutable, moving through a queue,
// globally readable, or gone.
type RegionState uint8

const (
	RegionPrivate RegionState = iota
	RegionInTransit
	RegionPublished
	RegionDestroyed
)

// Cell is the deliberately small object used to prove the heap semantics.
type Cell struct {
	Fields []Value
}

// HeapKind identifies the concrete payload stored in a generation-checked
// slot. Refs stay untyped so the Store can reject type confusion at deref.
type HeapKind uint8

const (
	HeapInvalid HeapKind = iota
	HeapCell
	HeapString
	HeapObject
	HeapArray
	HeapContext
	HeapFunction
	HeapPromise
	HeapBigInt
	HeapSymbol
	HeapArrayBuffer
	HeapTypedArray
	HeapMap
	HeapSet
	HeapDate
	HeapRegExp
	HeapError
	HeapWeakMap
	HeapWeakSet
	HeapIterator
	HeapHostObject
)

// StringObject is the first real typed native-heap payload. Text is immutable;
// cloning a graph clones its backing bytes into the destination region.
type StringObject struct {
	Text string
}

// slotPayload is the compact bridge from stable slot metadata into one typed
// arena entry. The direct pointer keeps hot payload access to one indirection;
// the handle proves arena ownership during release and invariant checks.
type slotPayload struct {
	handle  payloadHandle
	pointer unsafe.Pointer
}

// Slot holds one generation-checked typed heap payload. Vacant slots retain
// only this compact stable record, so stale Refs remain invalid without one
// separately allocated payload facade per live value.
type Slot struct {
	payload    slotPayload
	Generation uint32
	incoming   uint32
	Kind       HeapKind
	Occupied   bool
}

func payloadPointer[T any](slot *Slot) *T {
	if slot == nil || slot.payload.pointer == nil {
		return nil
	}
	return (*T)(slot.payload.pointer)
}

func (slot *Slot) cell() *Cell                 { return payloadPointer[Cell](slot) }
func (slot *Slot) stringObject() *StringObject { return payloadPointer[StringObject](slot) }
func (slot *Slot) object() *Object             { return payloadPointer[Object](slot) }
func (slot *Slot) array() *Array               { return payloadPointer[Array](slot) }
func (slot *Slot) context() *Context           { return payloadPointer[Context](slot) }
func (slot *Slot) function() *Function         { return payloadPointer[Function](slot) }
func (slot *Slot) promise() *Promise           { return payloadPointer[Promise](slot) }
func (slot *Slot) bigInt() *BigInt             { return payloadPointer[BigInt](slot) }
func (slot *Slot) symbol() *Symbol             { return payloadPointer[Symbol](slot) }
func (slot *Slot) arrayBuffer() *ArrayBuffer   { return payloadPointer[ArrayBuffer](slot) }
func (slot *Slot) typedArray() *TypedArray     { return payloadPointer[TypedArray](slot) }
func (slot *Slot) nativeMap() *Map             { return payloadPointer[Map](slot) }
func (slot *Slot) nativeSet() *Set             { return payloadPointer[Set](slot) }
func (slot *Slot) date() *Date                 { return payloadPointer[Date](slot) }
func (slot *Slot) regExp() *RegExp             { return payloadPointer[RegExp](slot) }
func (slot *Slot) errorObject() *ErrorObject   { return payloadPointer[ErrorObject](slot) }
func (slot *Slot) weakMap() *WeakMap           { return payloadPointer[WeakMap](slot) }
func (slot *Slot) weakSet() *WeakSet           { return payloadPointer[WeakSet](slot) }
func (slot *Slot) iterator() *Iterator         { return payloadPointer[Iterator](slot) }
func (slot *Slot) hostObject() *HostObject     { return payloadPointer[HostObject](slot) }

func clonedPayloadPointer[T any](value T) unsafe.Pointer {
	return unsafe.Pointer(&value)
}

// Region is a snapshot-compatible physical allocation region. Store methods
// return copies so callers cannot mutate slots around the write barrier.
type Region struct {
	ID       RegionID
	Owner    ownership.OwnerID
	Lifetime ownership.OwnerKind
	State    RegionState
	Slots    []Slot

	claim ownership.RegionID
	free  []uint32
}

// RegionMetadata is the allocation-free ownership/state view used by hot
// execution paths. Region remains the deep snapshot API for diagnostics and
// tests that explicitly need slot payloads.
type RegionMetadata struct {
	ID           RegionID
	Owner        ownership.OwnerID
	Lifetime     ownership.OwnerKind
	State        RegionState
	Slots        int
	SlotCapacity int
}

func cloneCell(cell Cell) Cell {
	return Cell{Fields: append([]Value(nil), cell.Fields...)}
}

func cloneString(value StringObject) StringObject {
	return StringObject{Text: strings.Clone(value.Text)}
}

func cloneSlot(slot Slot) Slot {
	result := Slot{
		Generation: slot.Generation,
		Kind:       slot.Kind,
		Occupied:   slot.Occupied,
	}
	if slot.payload.pointer == nil {
		return result
	}
	switch slot.Kind {
	case HeapCell:
		result.payload.pointer = clonedPayloadPointer(cloneCell(*slot.cell()))
	case HeapString:
		result.payload.pointer = clonedPayloadPointer(cloneString(*slot.stringObject()))
	case HeapObject:
		result.payload.pointer = clonedPayloadPointer(cloneObject(*slot.object()))
	case HeapArray:
		result.payload.pointer = clonedPayloadPointer(cloneArray(*slot.array()))
	case HeapContext:
		result.payload.pointer = clonedPayloadPointer(cloneContext(*slot.context()))
	case HeapFunction:
		result.payload.pointer = clonedPayloadPointer(cloneFunction(*slot.function()))
	case HeapPromise:
		result.payload.pointer = clonedPayloadPointer(clonePromise(*slot.promise()))
	case HeapBigInt:
		result.payload.pointer = clonedPayloadPointer(cloneBigInt(*slot.bigInt()))
	case HeapSymbol:
		result.payload.pointer = clonedPayloadPointer(cloneSymbol(*slot.symbol()))
	case HeapArrayBuffer:
		result.payload.pointer = clonedPayloadPointer(cloneArrayBuffer(*slot.arrayBuffer()))
	case HeapTypedArray:
		result.payload.pointer = clonedPayloadPointer(cloneTypedArray(*slot.typedArray()))
	case HeapMap:
		result.payload.pointer = clonedPayloadPointer(cloneMap(*slot.nativeMap()))
	case HeapSet:
		result.payload.pointer = clonedPayloadPointer(cloneSet(*slot.nativeSet()))
	case HeapDate:
		result.payload.pointer = clonedPayloadPointer(*slot.date())
	case HeapRegExp:
		result.payload.pointer = clonedPayloadPointer(cloneRegExp(*slot.regExp()))
	case HeapError:
		result.payload.pointer = clonedPayloadPointer(cloneError(*slot.errorObject()))
	case HeapWeakMap:
		result.payload.pointer = clonedPayloadPointer(cloneWeakMap(*slot.weakMap()))
	case HeapWeakSet:
		result.payload.pointer = clonedPayloadPointer(cloneWeakSet(*slot.weakSet()))
	case HeapIterator:
		result.payload.pointer = clonedPayloadPointer(cloneIterator(*slot.iterator()))
	case HeapHostObject:
		result.payload.pointer = clonedPayloadPointer(*slot.hostObject())
	}
	return result
}

func appendSlotReferences(values []Value, slot *Slot) []Value {
	if slot == nil || !slot.Occupied {
		return values
	}
	if slot.Kind == HeapCell {
		return append(values, slot.cell().Fields...)
	}
	values = appendObjectHeaderReferences(values, slot)
	if slot.Kind == HeapObject {
		return values
	}
	if slot.Kind == HeapArray {
		for _, element := range slot.array().Elements {
			values = append(values, element.Value)
		}
		return values
	}
	if slot.Kind == HeapContext {
		values = append(values, slot.context().Parent)
		for _, binding := range slot.context().Bindings {
			values = append(values, RefValue(binding.Name))
			if binding.Indirect {
				values = append(values, RefValue(binding.Target), RefValue(binding.TargetName))
			} else if binding.Initialized {
				values = append(values, binding.Value)
			}
		}
		return values
	}
	if slot.Kind == HeapFunction {
		values = append(values, slot.function().Name, slot.function().Environment)
		if slot.function().ThisMode == FunctionThisLexical {
			values = append(values, slot.function().LexicalThis)
		}
		values = append(values, slot.function().Constants...)
		values = append(values, slot.function().Captures...)
		return values
	}
	if slot.Kind == HeapPromise {
		if slot.promise().State != PromisePending {
			values = append(values, slot.promise().Result)
		}
		for _, reaction := range slot.promise().Reactions {
			values = append(values, reaction.OnFulfilled, reaction.OnRejected, reaction.Downstream)
		}
		return values
	}
	if slot.Kind == HeapSymbol {
		return append(values, slot.symbol().Description)
	}
	if slot.Kind == HeapTypedArray {
		if slot.typedArray().Buffer == (Ref{}) {
			return values
		}
		return append(values, RefValue(slot.typedArray().Buffer))
	}
	if slot.Kind == HeapMap {
		for _, entry := range slot.nativeMap().Entries {
			values = append(values, entry.Key, entry.Value)
		}
		return values
	}
	if slot.Kind == HeapSet {
		return append(values, slot.nativeSet().Values...)
	}
	if slot.Kind == HeapRegExp {
		if slot.regExp().Pattern == (Ref{}) {
			return values
		}
		return append(values, RefValue(slot.regExp().Pattern))
	}
	if slot.Kind == HeapError {
		values = append(values, slot.errorObject().Message, slot.errorObject().Stack)
		if slot.errorObject().HasCause {
			values = append(values, slot.errorObject().Cause)
		}
		values = append(values, slot.errorObject().Errors...)
		return values
	}
	if slot.Kind == HeapIterator {
		return append(values, RefValue(slot.iterator().Target))
	}
	return values
}

func appendObjectHeaderReferences(values []Value, slot *Slot) []Value {
	header, ok := objectHeaderForSlot(slot)
	if !ok {
		return values
	}
	values = append(values, header.Prototype)
	for _, property := range header.Properties {
		values = append(values, RefValue(property.Name))
		if property.Kind == PropertyData {
			values = append(values, property.Value())
		} else {
			values = append(values, property.Getter(), property.Setter())
		}
	}
	return values
}

func rewriteSlotReferences(slot *Slot, replacements map[Ref]Ref) {
	if slot == nil || !slot.Occupied || len(replacements) == 0 {
		return
	}
	rewriteValue := func(value Value) Value {
		if value.IsRef() {
			if replacement, exists := replacements[value.Ref()]; exists {
				return RefValue(replacement)
			}
		}
		return value
	}
	rewriteRef := func(ref Ref) Ref {
		if replacement, exists := replacements[ref]; exists {
			return replacement
		}
		return ref
	}
	if header, ok := objectHeaderForSlot(slot); ok {
		header.Prototype = rewriteValue(header.Prototype)
		for index := range header.Properties {
			property := &header.Properties[index]
			property.Name = rewriteRef(property.Name)
			if property.Kind == PropertyData {
				property.SetValue(rewriteValue(property.Value()))
			} else {
				property.SetGetter(rewriteValue(property.Getter()))
				property.SetSetter(rewriteValue(property.Setter()))
			}
		}
	}
	switch slot.Kind {
	case HeapCell:
		for index := range slot.cell().Fields {
			slot.cell().Fields[index] = rewriteValue(slot.cell().Fields[index])
		}
	case HeapArray:
		for index := range slot.array().Elements {
			slot.array().Elements[index].Value = rewriteValue(slot.array().Elements[index].Value)
		}
	case HeapContext:
		slot.context().Parent = rewriteValue(slot.context().Parent)
		for index := range slot.context().Bindings {
			binding := &slot.context().Bindings[index]
			binding.Name = rewriteRef(binding.Name)
			if binding.Indirect {
				binding.Target = rewriteRef(binding.Target)
				binding.TargetName = rewriteRef(binding.TargetName)
			} else if binding.Initialized {
				binding.Value = rewriteValue(binding.Value)
			}
		}
	case HeapFunction:
		slot.function().Name = rewriteValue(slot.function().Name)
		slot.function().Environment = rewriteValue(slot.function().Environment)
		if slot.function().ThisMode == FunctionThisLexical {
			slot.function().LexicalThis = rewriteValue(slot.function().LexicalThis)
		}
		for index := range slot.function().Constants {
			slot.function().Constants[index] = rewriteValue(slot.function().Constants[index])
		}
		for index := range slot.function().Captures {
			slot.function().Captures[index] = rewriteValue(slot.function().Captures[index])
		}
	case HeapPromise:
		if slot.promise().State != PromisePending {
			slot.promise().Result = rewriteValue(slot.promise().Result)
		}
		for index := range slot.promise().Reactions {
			reaction := &slot.promise().Reactions[index]
			reaction.OnFulfilled = rewriteValue(reaction.OnFulfilled)
			reaction.OnRejected = rewriteValue(reaction.OnRejected)
			reaction.Downstream = rewriteValue(reaction.Downstream)
		}
	case HeapSymbol:
		slot.symbol().Description = rewriteValue(slot.symbol().Description)
	case HeapTypedArray:
		slot.typedArray().Buffer = rewriteRef(slot.typedArray().Buffer)
	case HeapMap:
		for index := range slot.nativeMap().Entries {
			slot.nativeMap().Entries[index].Key = rewriteValue(slot.nativeMap().Entries[index].Key)
			slot.nativeMap().Entries[index].Value = rewriteValue(slot.nativeMap().Entries[index].Value)
		}
	case HeapSet:
		for index := range slot.nativeSet().Values {
			slot.nativeSet().Values[index] = rewriteValue(slot.nativeSet().Values[index])
		}
	case HeapRegExp:
		slot.regExp().Pattern = rewriteRef(slot.regExp().Pattern)
	case HeapError:
		slot.errorObject().Message = rewriteValue(slot.errorObject().Message)
		slot.errorObject().Stack = rewriteValue(slot.errorObject().Stack)
		if slot.errorObject().HasCause {
			slot.errorObject().Cause = rewriteValue(slot.errorObject().Cause)
		}
		for index := range slot.errorObject().Errors {
			slot.errorObject().Errors[index] = rewriteValue(slot.errorObject().Errors[index])
		}
	case HeapIterator:
		slot.iterator().Target = rewriteRef(slot.iterator().Target)
	}
}

func slotStorageEmpty(slot *Slot) bool {
	return slot != nil && slot.Kind == HeapInvalid && slot.payload == (slotPayload{})
}

func objectHeaderStorageEmpty(header ObjectHeader) bool {
	return header.Prototype == (Value{}) && len(header.Properties) == 0 && !header.NonExtensible && !header.ImmutablePrototype
}

func (store *Store) initializeSlotPayloadLocked(slot *Slot, kind HeapKind) {
	slot.payload = store.payloads.allocate(kind)
	slot.Kind = kind
	if header, ok := objectHeaderForSlot(slot); ok {
		header.Prototype = NullValue()
	}
	switch kind {
	case HeapContext:
		slot.context().Parent = NullValue()
	}
}

func (store *Store) clearSlotPayloadLocked(slot *Slot) error {
	if err := store.payloads.release(slot.Kind, &slot.payload); err != nil {
		return err
	}
	slot.Kind = HeapInvalid
	slot.payload = slotPayload{}
	return nil
}

func cloneRegion(region *Region) Region {
	result := Region{
		ID:       region.ID,
		Owner:    region.Owner,
		Lifetime: region.Lifetime,
		State:    region.State,
		Slots:    make([]Slot, len(region.Slots)),
	}
	for index, slot := range region.Slots {
		result.Slots[index] = cloneSlot(slot)
	}
	return result
}
