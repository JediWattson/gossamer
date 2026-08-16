package memory

import (
	"errors"
	"fmt"
	"math"
	"sort"

	"github.com/JediWattson/gossamer/internal/runtime/ownership"
)

var ErrInvariantViolation = errors.New("memory: invariant violation")

type slotLocation struct {
	region RegionID
	slot   uint32
}

// CheckInvariants independently derives the Store's live slot, object-edge,
// region-edge, owner, and ledger state from typed heap contents. Tests and debug
// builds can call it after arbitrary operation sequences without mutating the
// heap.
func (store *Store) CheckInvariants() error {
	if store == nil {
		return fmt.Errorf("%w: nil store", ErrInvariantViolation)
	}
	store.mutex.Lock()
	defer store.mutex.Unlock()

	objects := make(map[ownership.ObjectID]slotLocation)
	liveSlots := uint64(0)
	liveCells := uint64(0)
	liveStrings := uint64(0)
	liveObjects := uint64(0)
	liveArrays := uint64(0)
	liveContexts := uint64(0)
	liveFunctions := uint64(0)
	livePromises := uint64(0)
	liveBigInts := uint64(0)
	liveSymbols := uint64(0)
	liveArrayBuffers := uint64(0)
	liveTypedArrays := uint64(0)
	liveMaps := uint64(0)
	liveSets := uint64(0)
	liveDates := uint64(0)
	liveRegExps := uint64(0)
	liveErrors := uint64(0)
	liveWeakMaps := uint64(0)
	liveWeakSets := uint64(0)
	liveIterators := uint64(0)
	liveHostObjects := uint64(0)
	liveBytes := uint64(0)
	liveRegions := uint64(0)
	reservedSlotCapacity := uint64(0)
	for owner, claim := range store.ownerClaims {
		if claim == 0 {
			return invariantError("%s has claim 0", owner)
		}
		snapshot, err := store.ledger.Region(claim)
		if err != nil {
			return invariantError("%s claim %d: %v", owner, claim, err)
		}
		if snapshot.Owner != owner {
			return invariantError("%s claim %d belongs to %s", owner, claim, snapshot.Owner)
		}
		if snapshot.Closed != store.closedOwners[owner] {
			return invariantError("%s claim closed=%t, Store records closed=%t", owner, snapshot.Closed, store.closedOwners[owner])
		}
	}
	for id, region := range store.regions {
		if region == nil {
			return invariantError("R%d has a nil record", id)
		}
		if id == 0 || region.ID != id {
			return invariantError("region map key R%d contains ID R%d", id, region.ID)
		}
		if region.State > RegionDestroyed {
			return invariantError("R%d has unknown state %d", id, region.State)
		}
		if region.State == RegionDestroyed {
			if cap(region.Slots) != 0 {
				return invariantError("destroyed R%d retains slot capacity %d", id, cap(region.Slots))
			}
			for index, slot := range region.Slots {
				if slot.Occupied || slot.object != 0 || !slotStorageEmpty(&slot) {
					return invariantError("destroyed R%d slot %d still owns storage", id, index)
				}
				if slot.Generation == 0 {
					return invariantError("destroyed R%d slot %d has generation 0", id, index)
				}
			}
			continue
		}
		reservedSlotCapacity += uint64(cap(region.Slots))

		liveRegions++
		if region.Owner.Value == 0 {
			return invariantError("live R%d has no owner", id)
		}
		switch region.State {
		case RegionPrivate:
			if region.Owner.Kind == ownership.OwnerQueue || region.Owner.Kind == ownership.OwnerShared {
				return invariantError("private R%d has invalid owner %s", id, region.Owner)
			}
		case RegionInTransit:
			if region.Owner.Kind != ownership.OwnerQueue {
				return invariantError("in-transit R%d is owned by %s", id, region.Owner)
			}
		case RegionPublished:
			if region.Owner.Kind != ownership.OwnerShared || region.Owner != store.sharedOwner {
				return invariantError("published R%d is owned by %s, shared owner is %s", id, region.Owner, store.sharedOwner)
			}
		}
		if store.closedOwners[region.Owner] {
			return invariantError("live R%d is owned by closed %s", id, region.Owner)
		}
		claim := store.ownerClaims[region.Owner]
		if claim == 0 || region.claim != claim {
			return invariantError("R%d claim %d does not match %s claim %d", id, region.claim, region.Owner, claim)
		}
		claimSnapshot, err := store.ledger.Region(claim)
		if err != nil {
			return invariantError("R%d claim %d: %v", id, claim, err)
		}
		if claimSnapshot.Closed || claimSnapshot.Owner != region.Owner {
			return invariantError("R%d has invalid claim snapshot %#v", id, claimSnapshot)
		}

		free := make(map[uint32]struct{}, len(region.free))
		for _, index := range region.free {
			if uint64(index) >= uint64(len(region.Slots)) {
				return invariantError("R%d free slot %d is out of range", id, index)
			}
			if _, duplicate := free[index]; duplicate {
				return invariantError("R%d free slot %d is duplicated", id, index)
			}
			free[index] = struct{}{}
			slot := region.Slots[index]
			if slot.Occupied || slot.object != 0 || !slotStorageEmpty(&slot) || slot.Generation == math.MaxUint32 {
				return invariantError("R%d free slot %d has live or retired state", id, index)
			}
		}
		for index := range region.Slots {
			slot := &region.Slots[index]
			if slot.Generation == 0 {
				return invariantError("R%d slot %d has generation 0", id, index)
			}
			_, onFreeList := free[uint32(index)]
			if !slot.Occupied {
				if slot.object != 0 || !slotStorageEmpty(slot) {
					return invariantError("R%d vacant slot %d retains storage", id, index)
				}
				if slot.Generation != math.MaxUint32 && !onFreeList {
					return invariantError("R%d reusable slot %d is missing from free list", id, index)
				}
				continue
			}
			if onFreeList {
				return invariantError("R%d occupied slot %d is on free list", id, index)
			}
			if slot.object == 0 {
				return invariantError("R%d occupied slot %d has no ledger object", id, index)
			}
			if previous, duplicate := objects[slot.object]; duplicate {
				return invariantError("ledger object %d appears at R%d:%d and R%d:%d", slot.object, previous.region, previous.slot, id, index)
			}
			objects[slot.object] = slotLocation{region: id, slot: uint32(index)}
			liveSlots++
			switch slot.Kind {
			case HeapCell:
				liveCells++
				if slotHasOtherPayload(slot, HeapCell) {
					return invariantError("Cell %s retains another typed payload", Ref{Region: id, Slot: uint32(index), Gen: slot.Generation})
				}
			case HeapString:
				liveStrings++
				liveBytes += uint64(len(slot.String.Text))
				if slotHasOtherPayload(slot, HeapString) {
					return invariantError("String %s retains another typed payload", Ref{Region: id, Slot: uint32(index), Gen: slot.Generation})
				}
			case HeapObject:
				liveObjects++
				if slotHasOtherPayload(slot, HeapObject) {
					return invariantError("Object %s retains another typed payload", Ref{Region: id, Slot: uint32(index), Gen: slot.Generation})
				}
				if slot.Object.Prototype.Kind() != ValueNull && !slot.Object.Prototype.IsRef() {
					return invariantError("Object %s has invalid prototype kind %d", Ref{Region: id, Slot: uint32(index), Gen: slot.Generation}, slot.Object.Prototype.Kind())
				}
				if slot.Object.Prototype.IsRef() {
					prototype := slot.Object.Prototype.Ref()
					prototypeRegion := store.regions[prototype.Region]
					if prototypeRegion == nil || prototypeRegion.State == RegionDestroyed || uint64(prototype.Slot) >= uint64(len(prototypeRegion.Slots)) {
						return invariantError("Object %s has stale prototype %s", Ref{Region: id, Slot: uint32(index), Gen: slot.Generation}, prototype)
					}
					prototypeSlot := &prototypeRegion.Slots[prototype.Slot]
					_, objectLike := objectHeaderForSlot(prototypeSlot)
					if !prototypeSlot.Occupied || prototypeSlot.Generation != prototype.Gen || !objectLike {
						return invariantError("Object %s has non-Object prototype %s", Ref{Region: id, Slot: uint32(index), Gen: slot.Generation}, prototype)
					}
				}
				names := make(map[string]struct{}, len(slot.Object.Properties))
				for propertyIndex, property := range slot.Object.Properties {
					if property.Kind != PropertyData && property.Kind != PropertyAccessor {
						return invariantError("Object %s property %d has invalid descriptor kind %d", Ref{Region: id, Slot: uint32(index), Gen: slot.Generation}, propertyIndex, property.Kind)
					}
					if property.Kind == PropertyData && (property.Getter.Kind() != ValueUndefined || property.Setter.Kind() != ValueUndefined) {
						return invariantError("Object %s data property %d retains accessors", Ref{Region: id, Slot: uint32(index), Gen: slot.Generation}, propertyIndex)
					}
					if property.Kind == PropertyAccessor && (property.Value.Kind() != ValueUndefined || property.Writable) {
						return invariantError("Object %s accessor property %d retains data state", Ref{Region: id, Slot: uint32(index), Gen: slot.Generation}, propertyIndex)
					}
					if property.Kind == PropertyAccessor {
						for _, callable := range []Value{property.Getter, property.Setter} {
							if callable.Kind() == ValueUndefined {
								continue
							}
							callableRegion := store.regions[callable.Ref().Region]
							if callableRegion == nil || uint64(callable.Ref().Slot) >= uint64(len(callableRegion.Slots)) {
								return invariantError("Object %s accessor property %d has stale Function %s", Ref{Region: id, Slot: uint32(index), Gen: slot.Generation}, propertyIndex, callable.Ref())
							}
							callableSlot := &callableRegion.Slots[callable.Ref().Slot]
							if !callableSlot.Occupied || callableSlot.Generation != callable.Ref().Gen || callableSlot.Kind != HeapFunction {
								return invariantError("Object %s accessor property %d has non-Function %s", Ref{Region: id, Slot: uint32(index), Gen: slot.Generation}, propertyIndex, callable.Ref())
							}
						}
					}
					targetRegion := store.regions[property.Name.Region]
					if targetRegion == nil || targetRegion.State == RegionDestroyed || uint64(property.Name.Slot) >= uint64(len(targetRegion.Slots)) {
						return invariantError("Object %s property %d has stale name %s", Ref{Region: id, Slot: uint32(index), Gen: slot.Generation}, propertyIndex, property.Name)
					}
					nameSlot := &targetRegion.Slots[property.Name.Slot]
					if !nameSlot.Occupied || nameSlot.Generation != property.Name.Gen || nameSlot.Kind != HeapString {
						return invariantError("Object %s property %d has non-String name %s", Ref{Region: id, Slot: uint32(index), Gen: slot.Generation}, propertyIndex, property.Name)
					}
					if _, duplicate := names[nameSlot.String.Text]; duplicate {
						return invariantError("Object %s has duplicate property %q", Ref{Region: id, Slot: uint32(index), Gen: slot.Generation}, nameSlot.String.Text)
					}
					names[nameSlot.String.Text] = struct{}{}
				}
			case HeapArray:
				liveArrays++
				if slotHasOtherPayload(slot, HeapArray) {
					return invariantError("Array %s retains another typed payload", Ref{Region: id, Slot: uint32(index), Gen: slot.Generation})
				}
				var previous uint32
				for elementIndex, element := range slot.Array.Elements {
					if element.Index >= slot.Array.Length {
						return invariantError("Array %s element %d index %d is outside length %d", Ref{Region: id, Slot: uint32(index), Gen: slot.Generation}, elementIndex, element.Index, slot.Array.Length)
					}
					if elementIndex != 0 && element.Index <= previous {
						return invariantError("Array %s elements are not strictly ordered at %d", Ref{Region: id, Slot: uint32(index), Gen: slot.Generation}, elementIndex)
					}
					previous = element.Index
				}
			case HeapContext:
				liveContexts++
				if slotHasOtherPayload(slot, HeapContext) {
					return invariantError("Context %s retains another typed payload", Ref{Region: id, Slot: uint32(index), Gen: slot.Generation})
				}
				if slot.Context.Parent.Kind() != ValueNull && !slot.Context.Parent.IsRef() {
					return invariantError("Context %s has invalid parent kind %d", Ref{Region: id, Slot: uint32(index), Gen: slot.Generation}, slot.Context.Parent.Kind())
				}
				if err := store.checkContextChainLocked(Ref{Region: id, Slot: uint32(index), Gen: slot.Generation}); err != nil {
					return err
				}
				seenNames := make(map[string]struct{}, len(slot.Context.Bindings))
				for bindingIndex, binding := range slot.Context.Bindings {
					nameRegion := store.regions[binding.Name.Region]
					if nameRegion == nil || nameRegion.State == RegionDestroyed || uint64(binding.Name.Slot) >= uint64(len(nameRegion.Slots)) {
						return invariantError("Context %s binding %d has stale name %s", Ref{Region: id, Slot: uint32(index), Gen: slot.Generation}, bindingIndex, binding.Name)
					}
					nameSlot := &nameRegion.Slots[binding.Name.Slot]
					if !nameSlot.Occupied || nameSlot.Generation != binding.Name.Gen || nameSlot.Kind != HeapString {
						return invariantError("Context %s binding %d has non-String name %s", Ref{Region: id, Slot: uint32(index), Gen: slot.Generation}, bindingIndex, binding.Name)
					}
					if _, duplicate := seenNames[nameSlot.String.Text]; duplicate {
						return invariantError("Context %s has duplicate binding %q", Ref{Region: id, Slot: uint32(index), Gen: slot.Generation}, nameSlot.String.Text)
					}
					seenNames[nameSlot.String.Text] = struct{}{}
					if !binding.Initialized && binding.Value != (Value{}) {
						return invariantError("Context %s binding %q retains an uninitialized value", Ref{Region: id, Slot: uint32(index), Gen: slot.Generation}, nameSlot.String.Text)
					}
				}
			case HeapFunction:
				liveFunctions++
				liveBytes += uint64(len(slot.Function.Code))
				if slotHasOtherPayload(slot, HeapFunction) {
					return invariantError("Function %s retains another typed payload", Ref{Region: id, Slot: uint32(index), Gen: slot.Generation})
				}
				if slot.Function.Kind != FunctionBytecode && slot.Function.Kind != FunctionNative {
					return invariantError("Function %s has invalid kind %d", Ref{Region: id, Slot: uint32(index), Gen: slot.Generation}, slot.Function.Kind)
				}
				if slot.Function.Kind == FunctionBytecode && slot.Function.NativeID != 0 {
					return invariantError("bytecode Function %s has native ID %d", Ref{Region: id, Slot: uint32(index), Gen: slot.Generation}, slot.Function.NativeID)
				}
				if slot.Function.Kind == FunctionNative && (slot.Function.NativeID == 0 || len(slot.Function.Code) != 0 || len(slot.Function.Constants) != 0) {
					return invariantError("native Function %s has an invalid descriptor", Ref{Region: id, Slot: uint32(index), Gen: slot.Generation})
				}
				if err := store.checkOptionalTypedRefLocked(slot.Function.Name, HeapString, "Function name"); err != nil {
					return err
				}
				if err := store.checkOptionalTypedRefLocked(slot.Function.Environment, HeapContext, "Function environment"); err != nil {
					return err
				}
			case HeapPromise:
				livePromises++
				if slotHasOtherPayload(slot, HeapPromise) {
					return invariantError("Promise %s retains another typed payload", Ref{Region: id, Slot: uint32(index), Gen: slot.Generation})
				}
				if slot.Promise.State > PromiseRejected {
					return invariantError("Promise %s has invalid state %d", Ref{Region: id, Slot: uint32(index), Gen: slot.Generation}, slot.Promise.State)
				}
				if slot.Promise.State == PromisePending && slot.Promise.Result != (Value{}) {
					return invariantError("pending Promise %s retains a result", Ref{Region: id, Slot: uint32(index), Gen: slot.Generation})
				}
				for reactionIndex, reaction := range slot.Promise.Reactions {
					if err := store.checkOptionalTypedRefLocked(reaction.OnFulfilled, HeapFunction, fmt.Sprintf("Promise reaction %d fulfillment handler", reactionIndex)); err != nil {
						return err
					}
					if err := store.checkOptionalTypedRefLocked(reaction.OnRejected, HeapFunction, fmt.Sprintf("Promise reaction %d rejection handler", reactionIndex)); err != nil {
						return err
					}
					if err := store.checkOptionalTypedRefLocked(reaction.Downstream, HeapPromise, fmt.Sprintf("Promise reaction %d downstream", reactionIndex)); err != nil {
						return err
					}
				}
			case HeapBigInt:
				liveBigInts++
				liveBytes += uint64(len(slot.BigInt.Magnitude))
				if slotHasOtherPayload(slot, HeapBigInt) {
					return invariantError("BigInt %s retains another typed payload", Ref{Region: id, Slot: uint32(index), Gen: slot.Generation})
				}
				if len(slot.BigInt.Magnitude) == 0 && slot.BigInt.Negative {
					return invariantError("BigInt %s stores negative zero", Ref{Region: id, Slot: uint32(index), Gen: slot.Generation})
				}
				if len(slot.BigInt.Magnitude) != 0 && slot.BigInt.Magnitude[0] == 0 {
					return invariantError("BigInt %s has a leading zero byte", Ref{Region: id, Slot: uint32(index), Gen: slot.Generation})
				}
			case HeapSymbol:
				liveSymbols++
				if slotHasOtherPayload(slot, HeapSymbol) {
					return invariantError("Symbol %s retains another typed payload", Ref{Region: id, Slot: uint32(index), Gen: slot.Generation})
				}
				if slot.Symbol.ID == 0 {
					return invariantError("Symbol %s has zero identity", Ref{Region: id, Slot: uint32(index), Gen: slot.Generation})
				}
				if err := store.checkOptionalTypedRefLocked(slot.Symbol.Description, HeapString, "Symbol description"); err != nil {
					return err
				}
			case HeapArrayBuffer:
				liveArrayBuffers++
				liveBytes += uint64(len(slot.ArrayBuffer.Bytes))
				if slotHasOtherPayload(slot, HeapArrayBuffer) {
					return invariantError("ArrayBuffer %s retains another typed payload", Ref{Region: id, Slot: uint32(index), Gen: slot.Generation})
				}
				if slot.ArrayBuffer.Detached && len(slot.ArrayBuffer.Bytes) != 0 {
					return invariantError("detached ArrayBuffer %s retains %d bytes", Ref{Region: id, Slot: uint32(index), Gen: slot.Generation}, len(slot.ArrayBuffer.Bytes))
				}
			case HeapTypedArray:
				liveTypedArrays++
				if slotHasOtherPayload(slot, HeapTypedArray) {
					return invariantError("TypedArray %s retains another typed payload", Ref{Region: id, Slot: uint32(index), Gen: slot.Generation})
				}
				size, valid := elementSize(slot.TypedArray.Element)
				if !valid || slot.TypedArray.ByteOffset%size != 0 {
					return invariantError("TypedArray %s has invalid element %d or offset %d", Ref{Region: id, Slot: uint32(index), Gen: slot.Generation}, slot.TypedArray.Element, slot.TypedArray.ByteOffset)
				}
				bufferRegion := store.regions[slot.TypedArray.Buffer.Region]
				if bufferRegion == nil || bufferRegion.State == RegionDestroyed || uint64(slot.TypedArray.Buffer.Slot) >= uint64(len(bufferRegion.Slots)) {
					return invariantError("TypedArray %s has stale buffer %s", Ref{Region: id, Slot: uint32(index), Gen: slot.Generation}, slot.TypedArray.Buffer)
				}
				bufferSlot := &bufferRegion.Slots[slot.TypedArray.Buffer.Slot]
				if !bufferSlot.Occupied || bufferSlot.Generation != slot.TypedArray.Buffer.Gen || bufferSlot.Kind != HeapArrayBuffer {
					return invariantError("TypedArray %s has non-ArrayBuffer target %s", Ref{Region: id, Slot: uint32(index), Gen: slot.Generation}, slot.TypedArray.Buffer)
				}
				byteLength := slot.TypedArray.Length * size
				if slot.TypedArray.Length != 0 && byteLength/slot.TypedArray.Length != size {
					return invariantError("TypedArray %s byte length overflows", Ref{Region: id, Slot: uint32(index), Gen: slot.Generation})
				}
				if !bufferSlot.ArrayBuffer.Detached {
					if _, _, err := bufferRange(len(bufferSlot.ArrayBuffer.Bytes), slot.TypedArray.ByteOffset, byteLength); err != nil {
						return invariantError("TypedArray %s range: %v", Ref{Region: id, Slot: uint32(index), Gen: slot.Generation}, err)
					}
				}
			case HeapMap:
				liveMaps++
				if slotHasOtherPayload(slot, HeapMap) {
					return invariantError("Map %s retains another typed payload", Ref{Region: id, Slot: uint32(index), Gen: slot.Generation})
				}
				for entryIndex, entry := range slot.Map.Entries {
					for previous := 0; previous < entryIndex; previous++ {
						equal, err := store.sameValueZeroLocked(slot.Map.Entries[previous].Key, entry.Key)
						if err != nil {
							return invariantError("Map %s key %d: %v", Ref{Region: id, Slot: uint32(index), Gen: slot.Generation}, entryIndex, err)
						}
						if equal {
							return invariantError("Map %s has duplicate key at entries %d and %d", Ref{Region: id, Slot: uint32(index), Gen: slot.Generation}, previous, entryIndex)
						}
					}
				}
			case HeapSet:
				liveSets++
				if slotHasOtherPayload(slot, HeapSet) {
					return invariantError("Set %s retains another typed payload", Ref{Region: id, Slot: uint32(index), Gen: slot.Generation})
				}
				for valueIndex, value := range slot.Set.Values {
					for previous := 0; previous < valueIndex; previous++ {
						equal, err := store.sameValueZeroLocked(slot.Set.Values[previous], value)
						if err != nil {
							return invariantError("Set %s value %d: %v", Ref{Region: id, Slot: uint32(index), Gen: slot.Generation}, valueIndex, err)
						}
						if equal {
							return invariantError("Set %s has duplicate values at %d and %d", Ref{Region: id, Slot: uint32(index), Gen: slot.Generation}, previous, valueIndex)
						}
					}
				}
			case HeapDate:
				liveDates++
				if slotHasOtherPayload(slot, HeapDate) {
					return invariantError("Date %s retains another typed payload", Ref{Region: id, Slot: uint32(index), Gen: slot.Generation})
				}
				milliseconds := slot.Date.Milliseconds
				if !math.IsNaN(milliseconds) && (math.IsInf(milliseconds, 0) || math.Abs(milliseconds) > maxDateMilliseconds || milliseconds != math.Trunc(milliseconds)) {
					return invariantError("Date %s has unclipped milliseconds %v", Ref{Region: id, Slot: uint32(index), Gen: slot.Generation}, milliseconds)
				}
			case HeapRegExp:
				liveRegExps++
				if slotHasOtherPayload(slot, HeapRegExp) {
					return invariantError("RegExp %s retains another typed payload", Ref{Region: id, Slot: uint32(index), Gen: slot.Generation})
				}
				if slot.RegExp.Flags&^allRegExpFlags != 0 || slot.RegExp.Flags&RegExpUnicode != 0 && slot.RegExp.Flags&RegExpUnicodeSets != 0 {
					return invariantError("RegExp %s has invalid flags %x", Ref{Region: id, Slot: uint32(index), Gen: slot.Generation}, slot.RegExp.Flags)
				}
				patternRegion := store.regions[slot.RegExp.Pattern.Region]
				if patternRegion == nil || patternRegion.State == RegionDestroyed || uint64(slot.RegExp.Pattern.Slot) >= uint64(len(patternRegion.Slots)) {
					return invariantError("RegExp %s has stale pattern %s", Ref{Region: id, Slot: uint32(index), Gen: slot.Generation}, slot.RegExp.Pattern)
				}
				patternSlot := &patternRegion.Slots[slot.RegExp.Pattern.Slot]
				if !patternSlot.Occupied || patternSlot.Generation != slot.RegExp.Pattern.Gen || patternSlot.Kind != HeapString {
					return invariantError("RegExp %s has non-String pattern %s", Ref{Region: id, Slot: uint32(index), Gen: slot.Generation}, slot.RegExp.Pattern)
				}
			case HeapError:
				liveErrors++
				if slotHasOtherPayload(slot, HeapError) {
					return invariantError("Error %s retains another typed payload", Ref{Region: id, Slot: uint32(index), Gen: slot.Generation})
				}
				if slot.Error.Kind.Name() == "" {
					return invariantError("Error %s has invalid kind %d", Ref{Region: id, Slot: uint32(index), Gen: slot.Generation}, slot.Error.Kind)
				}
				if err := store.checkOptionalTypedRefLocked(slot.Error.Message, HeapString, "Error message"); err != nil {
					return err
				}
				if err := store.checkOptionalTypedRefLocked(slot.Error.Stack, HeapString, "Error stack"); err != nil {
					return err
				}
				if !slot.Error.HasCause && slot.Error.Cause != (Value{}) {
					return invariantError("Error %s retains an absent cause", Ref{Region: id, Slot: uint32(index), Gen: slot.Generation})
				}
				if slot.Error.Kind != ErrorAggregate && len(slot.Error.Errors) != 0 {
					return invariantError("%s %s retains %d aggregate members", slot.Error.Kind.Name(), Ref{Region: id, Slot: uint32(index), Gen: slot.Generation}, len(slot.Error.Errors))
				}
			case HeapWeakMap:
				liveWeakMaps++
				if slotHasOtherPayload(slot, HeapWeakMap) {
					return invariantError("WeakMap %s retains another typed payload", Ref{Region: id, Slot: uint32(index), Gen: slot.Generation})
				}
				keys := make(map[Ref]int, len(slot.WeakMap.Entries))
				for entryIndex, entry := range slot.WeakMap.Entries {
					if entry.Key == (Ref{}) {
						return invariantError("WeakMap %s entry %d has a zero key", Ref{Region: id, Slot: uint32(index), Gen: slot.Generation}, entryIndex)
					}
					if previous, duplicate := keys[entry.Key]; duplicate {
						return invariantError("WeakMap %s has duplicate key at entries %d and %d", Ref{Region: id, Slot: uint32(index), Gen: slot.Generation}, previous, entryIndex)
					}
					keys[entry.Key] = entryIndex
				}
			case HeapWeakSet:
				liveWeakSets++
				if slotHasOtherPayload(slot, HeapWeakSet) {
					return invariantError("WeakSet %s retains another typed payload", Ref{Region: id, Slot: uint32(index), Gen: slot.Generation})
				}
				keys := make(map[Ref]int, len(slot.WeakSet.Keys))
				for keyIndex, key := range slot.WeakSet.Keys {
					if key == (Ref{}) {
						return invariantError("WeakSet %s key %d is zero", Ref{Region: id, Slot: uint32(index), Gen: slot.Generation}, keyIndex)
					}
					if previous, duplicate := keys[key]; duplicate {
						return invariantError("WeakSet %s has duplicate key at entries %d and %d", Ref{Region: id, Slot: uint32(index), Gen: slot.Generation}, previous, keyIndex)
					}
					keys[key] = keyIndex
				}
			case HeapIterator:
				liveIterators++
				if slotHasOtherPayload(slot, HeapIterator) {
					return invariantError("Iterator %s retains another typed payload", Ref{Region: id, Slot: uint32(index), Gen: slot.Generation})
				}
				targetRegion := store.regions[slot.Iterator.Target.Region]
				if targetRegion == nil || targetRegion.State == RegionDestroyed || uint64(slot.Iterator.Target.Slot) >= uint64(len(targetRegion.Slots)) {
					return invariantError("Iterator %s has stale target %s", Ref{Region: id, Slot: uint32(index), Gen: slot.Generation}, slot.Iterator.Target)
				}
				targetSlot := &targetRegion.Slots[slot.Iterator.Target.Slot]
				if !targetSlot.Occupied || targetSlot.Generation != slot.Iterator.Target.Gen || !iteratorTargetMatches(slot.Iterator.Kind, targetSlot.Kind) {
					return invariantError("Iterator %s has invalid target %s for kind %d", Ref{Region: id, Slot: uint32(index), Gen: slot.Generation}, slot.Iterator.Target, slot.Iterator.Kind)
				}
			case HeapHostObject:
				liveHostObjects++
				if slotHasOtherPayload(slot, HeapHostObject) {
					return invariantError("HostObject %s retains another typed payload", Ref{Region: id, Slot: uint32(index), Gen: slot.Generation})
				}
				if slot.HostObject.Class == 0 || slot.HostObject.Scope == 0 || slot.HostObject.Identity == 0 {
					return invariantError("HostObject %s has invalid identity %#v", Ref{Region: id, Slot: uint32(index), Gen: slot.Generation}, slot.HostObject)
				}
			default:
				return invariantError("R%d occupied slot %d has unknown heap kind %d", id, index, slot.Kind)
			}
			if slot.Kind != HeapObject {
				if _, objectLike := objectHeaderForSlot(slot); objectLike {
					if err := store.checkObjectHeaderLocked(Ref{Region: id, Slot: uint32(index), Gen: slot.Generation}, slot); err != nil {
						return err
					}
				}
			}
		}
	}
	pooledSlotCapacity := uint64(0)
	for bufferIndex, buffer := range store.slotBuffers {
		if len(buffer) != 0 || cap(buffer) == 0 || cap(buffer) > maxPooledSlotCapacity {
			return invariantError("pooled slot buffer %d has len/cap %d/%d", bufferIndex, len(buffer), cap(buffer))
		}
		pooledSlotCapacity += uint64(cap(buffer))
		for slotIndex, slot := range buffer[:cap(buffer)] {
			if slot.Occupied || slot.object != 0 || !slotStorageEmpty(&slot) || slot.Generation != 0 {
				return invariantError("pooled slot buffer %d slot %d retains state", bufferIndex, slotIndex)
			}
		}
	}
	reservedSlotCapacity += pooledSlotCapacity

	expectedObjectEdges := make(map[objectEdge]uint32)
	expectedRegionEdges := make(map[edgeKey]uint32)
	expectedLedgerEdges := make(map[ownership.ObjectID]map[ownership.ObjectID]struct{})
	for id, region := range store.regions {
		if region == nil || region.State == RegionDestroyed {
			continue
		}
		for index := range region.Slots {
			slot := &region.Slots[index]
			if !slot.Occupied {
				continue
			}
			for field, value := range slotReferences(slot) {
				if !value.IsRef() {
					continue
				}
				targetRegion := store.regions[value.Ref().Region]
				if targetRegion == nil || targetRegion.State == RegionDestroyed || uint64(value.Ref().Slot) >= uint64(len(targetRegion.Slots)) {
					return invariantError("R%d slot %d field %d contains stale %s", id, index, field, value.Ref())
				}
				targetSlot := &targetRegion.Slots[value.Ref().Slot]
				if !targetSlot.Occupied || targetSlot.Generation != value.Ref().Gen {
					return invariantError("R%d slot %d field %d contains stale %s", id, index, field, value.Ref())
				}
				objectKey := objectEdge{from: slot.object, to: targetSlot.object}
				if expectedObjectEdges[objectKey] == math.MaxUint32 {
					return invariantError("object edge %d -> %d overflows", slot.object, targetSlot.object)
				}
				expectedObjectEdges[objectKey]++
				if expectedLedgerEdges[slot.object] == nil {
					expectedLedgerEdges[slot.object] = make(map[ownership.ObjectID]struct{})
				}
				expectedLedgerEdges[slot.object][targetSlot.object] = struct{}{}
				if id != targetRegion.ID {
					regionKey := edgeKey{from: id, to: targetRegion.ID}
					if expectedRegionEdges[regionKey] == math.MaxUint32 {
						return invariantError("region edge R%d -> R%d overflows", id, targetRegion.ID)
					}
					expectedRegionEdges[regionKey]++
				}
			}
		}
	}

	if err := compareObjectEdges(store.objectEdges, expectedObjectEdges); err != nil {
		return err
	}
	if err := compareRegionEdges(store.barrier.edges, expectedRegionEdges); err != nil {
		return err
	}
	for object, location := range objects {
		snapshot, err := store.ledger.Object(object)
		if err != nil {
			return invariantError("R%d slot %d ledger object %d: %v", location.region, location.slot, object, err)
		}
		region := store.regions[location.region]
		if !snapshot.Alive || snapshot.References != 1 || len(snapshot.Owners) != 1 || snapshot.Owners[region.Owner] != 1 {
			return invariantError("R%d slot %d has invalid ledger object %#v", location.region, location.slot, snapshot)
		}
		wantTargets := sortedObjectSet(expectedLedgerEdges[object])
		if !equalObjectIDs(snapshot.Edges, wantTargets) {
			return invariantError("ledger object %d edges %v, want %v", object, snapshot.Edges, wantTargets)
		}
	}
	if store.stats.LiveSlots != liveSlots || store.stats.LiveCells != liveCells || store.stats.LiveStrings != liveStrings || store.stats.LiveObjects != liveObjects || store.stats.LiveArrays != liveArrays || store.stats.LiveContexts != liveContexts || store.stats.LiveFunctions != liveFunctions || store.stats.LivePromises != livePromises || store.stats.LiveBigInts != liveBigInts || store.stats.LiveSymbols != liveSymbols || store.stats.LiveArrayBuffers != liveArrayBuffers || store.stats.LiveTypedArrays != liveTypedArrays || store.stats.LiveMaps != liveMaps || store.stats.LiveSets != liveSets || store.stats.LiveDates != liveDates || store.stats.LiveRegExps != liveRegExps || store.stats.LiveErrors != liveErrors || store.stats.LiveBytes != liveBytes || store.stats.LiveRegions != liveRegions {
		return invariantError("stats slots/cells/strings/objects/arrays/contexts/functions/promises/bigints/symbols/buffers/views/maps/sets/dates/regexps/errors/bytes/regions = %d/%d/%d/%d/%d/%d/%d/%d/%d/%d/%d/%d/%d/%d/%d/%d/%d/%d/%d, derived %d/%d/%d/%d/%d/%d/%d/%d/%d/%d/%d/%d/%d/%d/%d/%d/%d/%d/%d", store.stats.LiveSlots, store.stats.LiveCells, store.stats.LiveStrings, store.stats.LiveObjects, store.stats.LiveArrays, store.stats.LiveContexts, store.stats.LiveFunctions, store.stats.LivePromises, store.stats.LiveBigInts, store.stats.LiveSymbols, store.stats.LiveArrayBuffers, store.stats.LiveTypedArrays, store.stats.LiveMaps, store.stats.LiveSets, store.stats.LiveDates, store.stats.LiveRegExps, store.stats.LiveErrors, store.stats.LiveBytes, store.stats.LiveRegions, liveSlots, liveCells, liveStrings, liveObjects, liveArrays, liveContexts, liveFunctions, livePromises, liveBigInts, liveSymbols, liveArrayBuffers, liveTypedArrays, liveMaps, liveSets, liveDates, liveRegExps, liveErrors, liveBytes, liveRegions)
	}
	if store.stats.LiveWeakMaps != liveWeakMaps || store.stats.LiveWeakSets != liveWeakSets {
		return invariantError("stats weak maps/sets = %d/%d, derived %d/%d", store.stats.LiveWeakMaps, store.stats.LiveWeakSets, liveWeakMaps, liveWeakSets)
	}
	if store.stats.LiveIterators != liveIterators {
		return invariantError("stats iterators = %d, derived %d", store.stats.LiveIterators, liveIterators)
	}
	if store.stats.LiveHostObjects != liveHostObjects {
		return invariantError("stats host objects = %d, derived %d", store.stats.LiveHostObjects, liveHostObjects)
	}
	if store.stats.PooledSlotBuffers != uint64(len(store.slotBuffers)) || store.stats.PooledSlotCapacity != pooledSlotCapacity || store.stats.ReservedSlotCapacity != reservedSlotCapacity {
		return invariantError("stats pooled buffers/capacity/reserved = %d/%d/%d, derived %d/%d/%d", store.stats.PooledSlotBuffers, store.stats.PooledSlotCapacity, store.stats.ReservedSlotCapacity, len(store.slotBuffers), pooledSlotCapacity, reservedSlotCapacity)
	}
	if store.stats.PeakReservedSlotCapacity < store.stats.ReservedSlotCapacity {
		return invariantError("peak reserved slot capacity %d is below current %d", store.stats.PeakReservedSlotCapacity, store.stats.ReservedSlotCapacity)
	}
	if store.closed && (liveSlots != 0 || liveRegions != 0) {
		return invariantError("closed store retains %d slots in %d regions", liveSlots, liveRegions)
	}
	if store.sharedOwner.Value == 0 {
		if store.sharedClaim != 0 {
			return invariantError("shared claim %d exists without a shared owner", store.sharedClaim)
		}
	} else if store.sharedOwner.Kind != ownership.OwnerShared || store.ownerClaims[store.sharedOwner] != store.sharedClaim {
		return invariantError("shared owner %s and claim %d are inconsistent", store.sharedOwner, store.sharedClaim)
	}
	return nil
}

func compareObjectEdges(got, want map[objectEdge]uint32) error {
	if len(got) != len(want) {
		return invariantError("object edge map has %d entries, want %d", len(got), len(want))
	}
	for edge, count := range want {
		if got[edge] != count {
			return invariantError("object edge %d -> %d count %d, want %d", edge.from, edge.to, got[edge], count)
		}
	}
	return nil
}

func compareRegionEdges(got, want map[edgeKey]uint32) error {
	if len(got) != len(want) {
		return invariantError("region edge map has %d entries, want %d", len(got), len(want))
	}
	for edge, count := range want {
		if got[edge] != count {
			return invariantError("region edge R%d -> R%d count %d, want %d", edge.from, edge.to, got[edge], count)
		}
	}
	return nil
}

func sortedObjectSet(objects map[ownership.ObjectID]struct{}) []ownership.ObjectID {
	result := make([]ownership.ObjectID, 0, len(objects))
	for object := range objects {
		result = append(result, object)
	}
	sort.Slice(result, func(left, right int) bool { return result[left] < result[right] })
	return result
}

func equalObjectIDs(left, right []ownership.ObjectID) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index] != right[index] {
			return false
		}
	}
	return true
}

func invariantError(format string, arguments ...any) error {
	return fmt.Errorf("%w: %s", ErrInvariantViolation, fmt.Sprintf(format, arguments...))
}

func slotHasOtherPayload(slot *Slot, kind HeapKind) bool {
	if kind != HeapCell && len(slot.Cell.Fields) != 0 {
		return true
	}
	if kind != HeapString && slot.String.Text != "" {
		return true
	}
	if kind != HeapObject && !objectHeaderStorageEmpty(slot.Object.ObjectHeader) {
		return true
	}
	if kind != HeapArray && (!objectHeaderStorageEmpty(slot.Array.ObjectHeader) || slot.Array.Length != 0 || len(slot.Array.Elements) != 0) {
		return true
	}
	if kind != HeapContext && (slot.Context.Parent != (Value{}) || len(slot.Context.Bindings) != 0) {
		return true
	}
	if kind != HeapFunction && (!objectHeaderStorageEmpty(slot.Function.ObjectHeader) || slot.Function.Kind != 0 || slot.Function.Name != (Value{}) || slot.Function.Environment != (Value{}) || slot.Function.Arity != 0 || slot.Function.Constructible || len(slot.Function.Code) != 0 || len(slot.Function.Constants) != 0 || slot.Function.NativeID != 0) {
		return true
	}
	if kind != HeapPromise && (!objectHeaderStorageEmpty(slot.Promise.ObjectHeader) || slot.Promise.State != PromisePending || slot.Promise.Result != (Value{}) || len(slot.Promise.Reactions) != 0 || slot.Promise.Handled) {
		return true
	}
	if kind != HeapBigInt && (slot.BigInt.Negative || len(slot.BigInt.Magnitude) != 0) {
		return true
	}
	if kind != HeapSymbol && (slot.Symbol.ID != 0 || slot.Symbol.Description != (Value{})) {
		return true
	}
	if kind != HeapArrayBuffer && (len(slot.ArrayBuffer.Bytes) != 0 || slot.ArrayBuffer.Detached) {
		return true
	}
	if kind != HeapTypedArray && slot.TypedArray != (TypedArray{}) {
		return true
	}
	if kind != HeapMap && (!objectHeaderStorageEmpty(slot.Map.ObjectHeader) || len(slot.Map.Entries) != 0) {
		return true
	}
	if kind != HeapSet && (!objectHeaderStorageEmpty(slot.Set.ObjectHeader) || len(slot.Set.Values) != 0) {
		return true
	}
	if kind != HeapDate && slot.Date.Milliseconds != 0 {
		return true
	}
	if kind != HeapRegExp && slot.RegExp != (RegExp{}) {
		return true
	}
	if kind != HeapError && (!objectHeaderStorageEmpty(slot.Error.ObjectHeader) || slot.Error.Kind != 0 || slot.Error.Message != (Value{}) || slot.Error.Stack != (Value{}) || slot.Error.Cause != (Value{}) || slot.Error.HasCause || len(slot.Error.Errors) != 0) {
		return true
	}
	if kind != HeapWeakMap && len(slot.WeakMap.Entries) != 0 {
		return true
	}
	if kind != HeapWeakSet && len(slot.WeakSet.Keys) != 0 {
		return true
	}
	if kind != HeapIterator && (!objectHeaderStorageEmpty(slot.Iterator.ObjectHeader) || slot.Iterator.Target != (Ref{}) || slot.Iterator.Kind != 0 || slot.Iterator.Next != 0) {
		return true
	}
	if kind != HeapHostObject && slot.HostObject != (HostObject{}) {
		return true
	}
	return false
}

func (store *Store) checkObjectHeaderLocked(ref Ref, slot *Slot) error {
	header, ok := objectHeaderForSlot(slot)
	if !ok {
		return invariantError("%s is not object-like", ref)
	}
	if header.Prototype.Kind() != ValueNull && !header.Prototype.IsRef() {
		return invariantError("%s has invalid prototype kind %d", ref, header.Prototype.Kind())
	}
	if header.Prototype.IsRef() {
		prototype := header.Prototype.Ref()
		prototypeRegion := store.regions[prototype.Region]
		if prototypeRegion == nil || prototypeRegion.State == RegionDestroyed || uint64(prototype.Slot) >= uint64(len(prototypeRegion.Slots)) {
			return invariantError("%s has stale prototype %s", ref, prototype)
		}
		prototypeSlot := &prototypeRegion.Slots[prototype.Slot]
		_, objectLike := objectHeaderForSlot(prototypeSlot)
		if !prototypeSlot.Occupied || prototypeSlot.Generation != prototype.Gen || !objectLike {
			return invariantError("%s has non-Object prototype %s", ref, prototype)
		}
	}
	names := make(map[string]struct{}, len(header.Properties))
	for propertyIndex, property := range header.Properties {
		if property.Kind != PropertyData && property.Kind != PropertyAccessor {
			return invariantError("%s property %d has invalid descriptor kind %d", ref, propertyIndex, property.Kind)
		}
		if property.Kind == PropertyData && (property.Getter.Kind() != ValueUndefined || property.Setter.Kind() != ValueUndefined) {
			return invariantError("%s data property %d retains accessors", ref, propertyIndex)
		}
		if property.Kind == PropertyAccessor && (property.Value.Kind() != ValueUndefined || property.Writable) {
			return invariantError("%s accessor property %d retains data state", ref, propertyIndex)
		}
		if property.Kind == PropertyAccessor {
			for _, callable := range []Value{property.Getter, property.Setter} {
				if callable.Kind() == ValueUndefined {
					continue
				}
				callableRegion := store.regions[callable.Ref().Region]
				if callableRegion == nil || uint64(callable.Ref().Slot) >= uint64(len(callableRegion.Slots)) {
					return invariantError("%s accessor property %d has stale Function %s", ref, propertyIndex, callable.Ref())
				}
				callableSlot := &callableRegion.Slots[callable.Ref().Slot]
				if !callableSlot.Occupied || callableSlot.Generation != callable.Ref().Gen || callableSlot.Kind != HeapFunction {
					return invariantError("%s accessor property %d has non-Function %s", ref, propertyIndex, callable.Ref())
				}
			}
		}
		targetRegion := store.regions[property.Name.Region]
		if targetRegion == nil || targetRegion.State == RegionDestroyed || uint64(property.Name.Slot) >= uint64(len(targetRegion.Slots)) {
			return invariantError("%s property %d has stale name %s", ref, propertyIndex, property.Name)
		}
		nameSlot := &targetRegion.Slots[property.Name.Slot]
		if !nameSlot.Occupied || nameSlot.Generation != property.Name.Gen || nameSlot.Kind != HeapString {
			return invariantError("%s property %d has non-String name %s", ref, propertyIndex, property.Name)
		}
		if _, duplicate := names[nameSlot.String.Text]; duplicate {
			return invariantError("%s has duplicate property %q", ref, nameSlot.String.Text)
		}
		names[nameSlot.String.Text] = struct{}{}
	}
	return nil
}

func (store *Store) checkOptionalTypedRefLocked(value Value, kind HeapKind, label string) error {
	if value.Kind() == ValueNull {
		return nil
	}
	if !value.IsRef() {
		return invariantError("%s has invalid kind %d", label, value.Kind())
	}
	_, slot, err := store.slotLocked(value.Ref())
	if err != nil {
		return invariantError("%s %s: %v", label, value.Ref(), err)
	}
	if slot.Kind != kind {
		return invariantError("%s %s is %s, want %s", label, value.Ref(), slot.Kind, kind)
	}
	return nil
}

func (store *Store) checkContextChainLocked(start Ref) error {
	seen := make(map[Ref]struct{})
	current := start
	for {
		if _, duplicate := seen[current]; duplicate {
			return invariantError("Context %s has a parent cycle", start)
		}
		seen[current] = struct{}{}
		_, slot, err := store.slotLocked(current)
		if err != nil {
			return invariantError("Context %s parent chain: %v", start, err)
		}
		if slot.Kind != HeapContext {
			return invariantError("Context %s parent chain reaches %s", start, slot.Kind)
		}
		if slot.Context.Parent.Kind() == ValueNull {
			return nil
		}
		if !slot.Context.Parent.IsRef() {
			return invariantError("Context %s parent chain has kind %d", start, slot.Context.Parent.Kind())
		}
		current = slot.Context.Parent.Ref()
	}
}
