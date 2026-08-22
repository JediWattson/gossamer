package memory

import (
	"fmt"

	"github.com/JediWattson/gossamer/internal/runtime/ownership"
)

// AllocObject creates an empty native object with a null prototype.
func (store *Store) AllocObject(owner ownership.OwnerID, regionID RegionID) (Ref, error) {
	if store == nil {
		return Ref{}, fmt.Errorf("memory: nil store")
	}
	store.mutex.Lock()
	defer store.mutex.Unlock()
	ref, err := store.allocKindLocked(owner, regionID, HeapObject, false)
	if err != nil {
		return Ref{}, err
	}
	_, slot, _ := store.slotLocked(ref)
	slot.Object.Prototype = NullValue()
	return ref, nil
}

func (store *Store) DerefObject(owner ownership.OwnerID, ref Ref) (Object, error) {
	if store == nil {
		return Object{}, fmt.Errorf("memory: nil store")
	}
	store.mutex.Lock()
	defer store.mutex.Unlock()
	_, slot, err := store.readSlotLocked(owner, ref)
	if err != nil {
		return Object{}, err
	}
	if slot.Kind != HeapObject {
		return Object{}, typeError(ref, slot.Kind, HeapObject)
	}
	return cloneObject(*slot.Object), nil
}

// DerefObjectHeader returns the prototype and named own properties for any
// JavaScript object-like payload.
func (store *Store) DerefObjectHeader(owner ownership.OwnerID, ref Ref) (ObjectHeader, error) {
	if store == nil {
		return ObjectHeader{}, fmt.Errorf("memory: nil store")
	}
	store.mutex.Lock()
	defer store.mutex.Unlock()
	_, slot, err := store.readSlotLocked(owner, ref)
	if err != nil {
		return ObjectHeader{}, err
	}
	header, ok := objectHeaderForSlot(slot)
	if !ok {
		return ObjectHeader{}, objectHeaderTypeError(ref, slot.Kind)
	}
	return cloneObjectHeader(*header), nil
}

// SetPrototype replaces object's direct prototype. The value must be null or
// reference another native Object.
func (store *Store) SetPrototype(owner ownership.OwnerID, object Ref, prototype Value) error {
	if store == nil {
		return fmt.Errorf("memory: nil store")
	}
	store.mutex.Lock()
	defer store.mutex.Unlock()
	return store.setPrototypeLocked(owner, object, prototype, false)
}

func (store *Store) setPrototypeLocked(owner ownership.OwnerID, object Ref, prototype Value, internal bool) error {
	region, slot, err := store.writeSlotLocked(owner, object, internal)
	if err != nil {
		return err
	}
	header, ok := objectHeaderForSlot(slot)
	if !ok {
		return objectHeaderTypeError(object, slot.Kind)
	}
	if header.ImmutablePrototype && !internal {
		if prototype == header.Prototype {
			return nil
		}
		return fmt.Errorf("%w: immutable Object prototype", ErrReadOnlyProperty)
	}
	if prototype.Kind() != ValueNull {
		if !prototype.IsRef() {
			return fmt.Errorf("%w: Object prototype must be null or an Object Ref", ErrTypeMismatch)
		}
		_, prototypeSlot, lookupErr := store.slotLocked(prototype.Ref())
		if lookupErr != nil {
			return lookupErr
		}
		if _, ok := objectHeaderForSlot(prototypeSlot); !ok {
			return objectHeaderTypeError(prototype.Ref(), prototypeSlot.Kind)
		}
		if err := store.rejectPrototypeCycleLocked(object, prototype.Ref()); err != nil {
			return err
		}
	}
	prototype, err = store.replaceValueLocked(owner, region, slot, header.Prototype, prototype, internal)
	if err != nil {
		return err
	}
	header.Prototype = prototype
	return nil
}

// SetObjectIntegrity applies the storage flags needed by exotic immutable
// objects after their initial properties and prototype have been installed.
func (store *Store) SetObjectIntegrity(owner ownership.OwnerID, object Ref, nonExtensible, immutablePrototype bool) error {
	if store == nil {
		return fmt.Errorf("memory: nil store")
	}
	store.mutex.Lock()
	defer store.mutex.Unlock()
	return store.setObjectIntegrityLocked(owner, object, nonExtensible, immutablePrototype, false)
}

func (store *Store) setObjectIntegrityLocked(owner ownership.OwnerID, object Ref, nonExtensible, immutablePrototype, internal bool) error {
	_, slot, err := store.writeSlotLocked(owner, object, internal)
	if err != nil {
		return err
	}
	header, ok := objectHeaderForSlot(slot)
	if !ok {
		return objectHeaderTypeError(object, slot.Kind)
	}
	header.NonExtensible = nonExtensible
	header.ImmutablePrototype = immutablePrototype
	return nil
}

// SetProperty inserts or replaces an own property. Name must reference a
// native String or Symbol. Equal String contents address the same property;
// Symbols address properties by semantic identity even after graph copying.
func (store *Store) SetProperty(owner ownership.OwnerID, object, name Ref, value Value) error {
	if store == nil {
		return fmt.Errorf("memory: nil store")
	}
	store.mutex.Lock()
	defer store.mutex.Unlock()
	return store.setPropertyLocked(owner, object, name, value, false)
}

func (store *Store) setPropertyLocked(owner ownership.OwnerID, object, name Ref, value Value, internal bool) error {
	_, slot, err := store.writeSlotLocked(owner, object, internal)
	if err != nil {
		return err
	}
	header, ok := objectHeaderForSlot(slot)
	if !ok {
		return objectHeaderTypeError(object, slot.Kind)
	}
	key, err := store.propertyKeyLocked(name)
	if err != nil {
		return err
	}
	index, err := store.findPropertyLocked(slot, key)
	if err != nil {
		return err
	}
	if index >= 0 {
		property := header.Properties[index]
		if property.Kind == PropertyAccessor {
			return fmt.Errorf("%w: %s", ErrAccessorProperty, key)
		}
		if !property.Writable && !internal {
			return fmt.Errorf("%w: %s", ErrReadOnlyProperty, key)
		}
		property.Value = value
		property.Name = Ref{}
		return store.definePropertyLocked(owner, object, name, property, internal)
	}
	return store.definePropertyLocked(owner, object, name, DataProperty(value, true, true, true), internal)
}

// DefineProperty creates or replaces an own data/accessor descriptor. Getter
// and setter values must be Undefined or Function Refs.
func (store *Store) DefineProperty(owner ownership.OwnerID, object, name Ref, descriptor Property) error {
	if store == nil {
		return fmt.Errorf("memory: nil store")
	}
	store.mutex.Lock()
	defer store.mutex.Unlock()
	return store.definePropertyLocked(owner, object, name, descriptor, false)
}

func (store *Store) definePropertyLocked(owner ownership.OwnerID, object, name Ref, descriptor Property, internal bool) error {
	region, slot, err := store.writeSlotLocked(owner, object, internal)
	if err != nil {
		return err
	}
	header, ok := objectHeaderForSlot(slot)
	if !ok {
		return objectHeaderTypeError(object, slot.Kind)
	}
	key, err := store.propertyKeyLocked(name)
	if err != nil {
		return err
	}
	descriptor.Name = Ref{}
	if descriptor.Kind == PropertyData {
		descriptor.Getter = Value{}
		descriptor.Setter = Value{}
	} else if descriptor.Kind == PropertyAccessor {
		descriptor.Value = Value{}
		descriptor.Writable = false
	}
	if err := store.validatePropertyDescriptorLocked(descriptor); err != nil {
		return err
	}
	index, err := store.findPropertyLocked(slot, key)
	if err != nil {
		return err
	}
	if index >= 0 {
		previous := header.Properties[index]
		if !internal {
			if err := compatiblePropertyDescriptor(previous, descriptor); err != nil {
				return fmt.Errorf("%w: %s", err, key)
			}
		}
		prepared, err := store.replacePropertyValuesLocked(owner, region, slot, previous, descriptor, internal)
		if err != nil {
			return err
		}
		prepared.Name = previous.Name
		header.Properties[index] = prepared
		return nil
	}
	if header.NonExtensible && !internal {
		return fmt.Errorf("%w: Object is not extensible", ErrReadOnlyProperty)
	}
	preparedName, err := store.replaceValueLocked(owner, region, slot, Value{}, RefValue(name), internal)
	if err != nil {
		return err
	}
	prepared, err := store.replacePropertyValuesLocked(owner, region, slot, Property{}, descriptor, internal)
	if err != nil {
		_, _ = store.replaceValueLocked(owner, region, slot, preparedName, Value{}, true)
		return err
	}
	prepared.Name = preparedName.Ref()
	header.Properties = append(header.Properties, prepared)
	return nil
}

func (store *Store) validatePropertyDescriptorLocked(descriptor Property) error {
	switch descriptor.Kind {
	case PropertyData:
	case PropertyAccessor:
		for _, callable := range []Value{descriptor.Getter, descriptor.Setter} {
			if callable.Kind() == ValueUndefined {
				continue
			}
			if !callable.IsRef() {
				return fmt.Errorf("%w: accessor must be Undefined or a Function Ref", ErrTypeMismatch)
			}
			_, callableSlot, err := store.slotLocked(callable.Ref())
			if err != nil {
				return err
			}
			if callableSlot.Kind != HeapFunction {
				return typeError(callable.Ref(), callableSlot.Kind, HeapFunction)
			}
		}
	default:
		return fmt.Errorf("%w: property descriptor kind %d", ErrTypeMismatch, descriptor.Kind)
	}
	return nil
}

func compatiblePropertyDescriptor(previous, next Property) error {
	if previous.Configurable {
		return nil
	}
	if next.Configurable || next.Enumerable != previous.Enumerable || next.Kind != previous.Kind {
		return ErrNonConfigurable
	}
	if previous.Kind == PropertyData && !previous.Writable {
		if next.Writable || next.Value != previous.Value {
			return ErrReadOnlyProperty
		}
	}
	if previous.Kind == PropertyAccessor && (next.Getter != previous.Getter || next.Setter != previous.Setter) {
		return ErrNonConfigurable
	}
	return nil
}

func (store *Store) replacePropertyValuesLocked(owner ownership.OwnerID, region *Region, slot *Slot, previous, next Property, internal bool) (Property, error) {
	oldValues := []Value{previous.Value, previous.Getter, previous.Setter}
	newValues := []Value{next.Value, next.Getter, next.Setter}
	prepared := make([]Value, len(newValues))
	for index := range newValues {
		value, err := store.replaceValueLocked(owner, region, slot, oldValues[index], newValues[index], internal)
		if err != nil {
			for rollback := index - 1; rollback >= 0; rollback-- {
				_, _ = store.replaceValueLocked(owner, region, slot, prepared[rollback], oldValues[rollback], true)
			}
			return Property{}, err
		}
		prepared[index] = value
	}
	next.Value, next.Getter, next.Setter = prepared[0], prepared[1], prepared[2]
	return next, nil
}

func (store *Store) GetOwnProperty(owner ownership.OwnerID, object, name Ref) (Value, bool, error) {
	property, found, err := store.GetOwnPropertyDescriptor(owner, object, name)
	if err != nil || !found {
		return Value{}, found, err
	}
	if property.Kind != PropertyData {
		return Value{}, true, ErrAccessorProperty
	}
	return property.Value, true, nil
}

func (store *Store) GetOwnPropertyDescriptor(owner ownership.OwnerID, object, name Ref) (Property, bool, error) {
	if store == nil {
		return Property{}, false, fmt.Errorf("memory: nil store")
	}
	store.mutex.Lock()
	defer store.mutex.Unlock()
	_, slot, err := store.readSlotLocked(owner, object)
	if err != nil {
		return Property{}, false, err
	}
	header, ok := objectHeaderForSlot(slot)
	if !ok {
		return Property{}, false, objectHeaderTypeError(object, slot.Kind)
	}
	key, err := store.propertyKeyLocked(name)
	if err != nil {
		return Property{}, false, err
	}
	index, err := store.findPropertyLocked(slot, key)
	if err != nil {
		return Property{}, false, err
	}
	if index < 0 {
		return Property{}, false, nil
	}
	return header.Properties[index], true, nil
}

func (store *Store) DeleteProperty(owner ownership.OwnerID, object, name Ref) (bool, error) {
	if store == nil {
		return false, fmt.Errorf("memory: nil store")
	}
	store.mutex.Lock()
	defer store.mutex.Unlock()
	region, slot, err := store.writeSlotLocked(owner, object, false)
	if err != nil {
		return false, err
	}
	header, ok := objectHeaderForSlot(slot)
	if !ok {
		return false, objectHeaderTypeError(object, slot.Kind)
	}
	key, err := store.propertyKeyLocked(name)
	if err != nil {
		return false, err
	}
	index, err := store.findPropertyLocked(slot, key)
	if err != nil || index < 0 {
		return false, err
	}
	property := header.Properties[index]
	if !property.Configurable {
		return false, nil
	}
	if _, err := store.replaceValueLocked(owner, region, slot, RefValue(property.Name), Value{}, false); err != nil {
		return false, err
	}
	for _, value := range []Value{property.Value, property.Getter, property.Setter} {
		if _, err := store.replaceValueLocked(owner, region, slot, value, Value{}, false); err != nil {
			return false, err
		}
	}
	copy(header.Properties[index:], header.Properties[index+1:])
	header.Properties[len(header.Properties)-1] = Property{}
	header.Properties = header.Properties[:len(header.Properties)-1]
	return true, nil
}

func (store *Store) rejectPrototypeCycleLocked(object, prototype Ref) error {
	seen := make(map[Ref]struct{})
	current := prototype
	for {
		if current == object {
			return fmt.Errorf("%w: %s -> %s", ErrPrototypeCycle, object, prototype)
		}
		if _, duplicate := seen[current]; duplicate {
			return ErrPrototypeCycle
		}
		seen[current] = struct{}{}
		_, slot, err := store.slotLocked(current)
		if err != nil {
			return err
		}
		header, ok := objectHeaderForSlot(slot)
		if !ok {
			return objectHeaderTypeError(current, slot.Kind)
		}
		if header.Prototype.Kind() == ValueNull {
			return nil
		}
		current = header.Prototype.Ref()
	}
}

func (store *Store) stringTextLocked(owner ownership.OwnerID, ref Ref, internal bool) (string, error) {
	_, slot, err := store.slotLocked(ref)
	if err != nil {
		return "", err
	}
	if slot.Kind != HeapString {
		return "", typeError(ref, slot.Kind, HeapString)
	}
	return slot.String.Text, nil
}

type propertyKey struct {
	kind   HeapKind
	text   string
	symbol SymbolID
}

func (key propertyKey) String() string {
	if key.kind == HeapSymbol {
		return fmt.Sprintf("Symbol(%d)", key.symbol)
	}
	return fmt.Sprintf("%q", key.text)
}

func (store *Store) propertyKeyLocked(ref Ref) (propertyKey, error) {
	_, slot, err := store.slotLocked(ref)
	if err != nil {
		return propertyKey{}, err
	}
	switch slot.Kind {
	case HeapString:
		return propertyKey{kind: HeapString, text: slot.String.Text}, nil
	case HeapSymbol:
		return propertyKey{kind: HeapSymbol, symbol: slot.Symbol.ID}, nil
	default:
		return propertyKey{}, fmt.Errorf("%w: property key %s is a %s, want String or Symbol", ErrTypeMismatch, ref, slot.Kind)
	}
}

func (store *Store) findPropertyLocked(slot *Slot, key propertyKey) (int, error) {
	header, ok := objectHeaderForSlot(slot)
	if !ok {
		return -1, objectHeaderTypeError(Ref{}, slot.Kind)
	}
	for index, property := range header.Properties {
		propertyKey, err := store.propertyKeyLocked(property.Name)
		if err != nil {
			return -1, err
		}
		if propertyKey == key {
			return index, nil
		}
	}
	return -1, nil
}
