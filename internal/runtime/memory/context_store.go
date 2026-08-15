package memory

import (
	"errors"
	"fmt"

	"github.com/JediWattson/gossamer/internal/runtime/ownership"
)

func (store *Store) AllocContext(owner ownership.OwnerID, regionID RegionID, parent Value) (Ref, error) {
	if store == nil {
		return Ref{}, fmt.Errorf("memory: nil store")
	}
	store.mutex.Lock()
	defer store.mutex.Unlock()
	ref, err := store.allocKindLocked(owner, regionID, HeapContext, false)
	if err != nil {
		return Ref{}, err
	}
	_, slot, _ := store.slotLocked(ref)
	slot.Context.Parent = NullValue()
	if parent.Kind() == ValueNull {
		return ref, nil
	}
	if err := store.setContextParentLocked(owner, ref, parent, false); err != nil {
		_ = store.freeLocked(owner, ref, true)
		return Ref{}, err
	}
	return ref, nil
}

func (store *Store) DerefContext(owner ownership.OwnerID, ref Ref) (Context, error) {
	if store == nil {
		return Context{}, fmt.Errorf("memory: nil store")
	}
	store.mutex.Lock()
	defer store.mutex.Unlock()
	_, slot, err := store.readSlotLocked(owner, ref)
	if err != nil {
		return Context{}, err
	}
	if slot.Kind != HeapContext {
		return Context{}, typeError(ref, slot.Kind, HeapContext)
	}
	return cloneContext(slot.Context), nil
}

func (store *Store) SetContextParent(owner ownership.OwnerID, context Ref, parent Value) error {
	if store == nil {
		return fmt.Errorf("memory: nil store")
	}
	store.mutex.Lock()
	defer store.mutex.Unlock()
	return store.setContextParentLocked(owner, context, parent, false)
}

func (store *Store) setContextParentLocked(owner ownership.OwnerID, context Ref, parent Value, internal bool) error {
	region, slot, err := store.writeSlotLocked(owner, context, internal)
	if err != nil {
		return err
	}
	if slot.Kind != HeapContext {
		return typeError(context, slot.Kind, HeapContext)
	}
	if parent.Kind() != ValueNull {
		if !parent.IsRef() {
			return fmt.Errorf("%w: Context parent must be null or a Context Ref", ErrTypeMismatch)
		}
		_, parentSlot, lookupErr := store.readSlotLocked(owner, parent.Ref())
		if lookupErr != nil && internal {
			_, parentSlot, lookupErr = store.slotLocked(parent.Ref())
		}
		if lookupErr != nil {
			return lookupErr
		}
		if parentSlot.Kind != HeapContext {
			return typeError(parent.Ref(), parentSlot.Kind, HeapContext)
		}
		if err := store.rejectContextCycleLocked(context, parent.Ref()); err != nil {
			return err
		}
	}
	if err := store.replaceValueLocked(owner, region, slot, slot.Context.Parent, parent, internal); err != nil {
		return err
	}
	slot.Context.Parent = parent
	return nil
}

func (store *Store) DeclareBinding(owner ownership.OwnerID, context, name Ref, mutable bool) error {
	if store == nil {
		return fmt.Errorf("memory: nil store")
	}
	store.mutex.Lock()
	defer store.mutex.Unlock()
	return store.declareBindingLocked(owner, context, name, mutable, false)
}

func (store *Store) declareBindingLocked(owner ownership.OwnerID, context, name Ref, mutable, internal bool) error {
	region, slot, err := store.writeSlotLocked(owner, context, internal)
	if err != nil {
		return err
	}
	if slot.Kind != HeapContext {
		return typeError(context, slot.Kind, HeapContext)
	}
	nameText, err := store.stringTextLocked(owner, name, internal)
	if err != nil {
		return err
	}
	if index, err := store.findBindingLocked(slot, nameText); err != nil {
		return err
	} else if index >= 0 {
		return fmt.Errorf("%w: %q", ErrBindingExists, nameText)
	}
	if err := store.replaceValueLocked(owner, region, slot, Value{}, RefValue(name), internal); err != nil {
		return err
	}
	slot.Context.Bindings = append(slot.Context.Bindings, Binding{Name: name, Mutable: mutable})
	return nil
}

func (store *Store) InitializeBinding(owner ownership.OwnerID, context, name Ref, value Value) error {
	if store == nil {
		return fmt.Errorf("memory: nil store")
	}
	store.mutex.Lock()
	defer store.mutex.Unlock()
	return store.initializeBindingLocked(owner, context, name, value, false)
}

func (store *Store) initializeBindingLocked(owner ownership.OwnerID, context, name Ref, value Value, internal bool) error {
	region, slot, err := store.writeSlotLocked(owner, context, internal)
	if err != nil {
		return err
	}
	if slot.Kind != HeapContext {
		return typeError(context, slot.Kind, HeapContext)
	}
	nameText, err := store.stringTextLocked(owner, name, internal)
	if err != nil {
		return err
	}
	index, err := store.findBindingLocked(slot, nameText)
	if err != nil {
		return err
	}
	if index < 0 {
		return fmt.Errorf("%w: %q", ErrBindingNotFound, nameText)
	}
	if slot.Context.Bindings[index].Initialized {
		return fmt.Errorf("%w: %q is already initialized", ErrBindingExists, nameText)
	}
	if err := store.replaceValueLocked(owner, region, slot, Value{}, value, internal); err != nil {
		return err
	}
	slot.Context.Bindings[index].Value = value
	slot.Context.Bindings[index].Initialized = true
	return nil
}

// SetBinding updates the nearest initialized mutable binding in the parent
// chain.
func (store *Store) SetBinding(owner ownership.OwnerID, context, name Ref, value Value) error {
	if store == nil {
		return fmt.Errorf("memory: nil store")
	}
	store.mutex.Lock()
	defer store.mutex.Unlock()
	nameText, err := store.stringTextLocked(owner, name, false)
	if err != nil {
		return err
	}
	_, region, slot, index, err := store.resolveBindingSlotLocked(owner, context, nameText)
	if err != nil {
		return err
	}
	binding := &slot.Context.Bindings[index]
	if !binding.Initialized {
		return fmt.Errorf("%w: %q", ErrBindingUninitialized, nameText)
	}
	if !binding.Mutable {
		return fmt.Errorf("%w: %q", ErrImmutableBinding, nameText)
	}
	if region.State == RegionPublished {
		return fmt.Errorf("%w: R%d", ErrImmutableRegion, region.ID)
	}
	if region.Owner != owner {
		return store.accessError(region, owner)
	}
	if err := store.replaceValueLocked(owner, region, slot, binding.Value, value, false); err != nil {
		return err
	}
	binding.Value = value
	return nil
}

func (store *Store) ResolveBinding(owner ownership.OwnerID, context, name Ref) (Value, bool, error) {
	if store == nil {
		return Value{}, false, fmt.Errorf("memory: nil store")
	}
	store.mutex.Lock()
	defer store.mutex.Unlock()
	nameText, err := store.stringTextLocked(owner, name, false)
	if err != nil {
		return Value{}, false, err
	}
	_, _, slot, index, err := store.resolveBindingSlotLocked(owner, context, nameText)
	if errors.Is(err, ErrBindingNotFound) {
		return Value{}, false, nil
	}
	if err != nil {
		return Value{}, false, err
	}
	binding := slot.Context.Bindings[index]
	if !binding.Initialized {
		return Value{}, true, fmt.Errorf("%w: %q", ErrBindingUninitialized, nameText)
	}
	return binding.Value, true, nil
}

func (store *Store) resolveBindingSlotLocked(owner ownership.OwnerID, context Ref, name string) (Ref, *Region, *Slot, int, error) {
	seen := make(map[Ref]struct{})
	current := context
	for {
		if _, duplicate := seen[current]; duplicate {
			return Ref{}, nil, nil, -1, ErrContextCycle
		}
		seen[current] = struct{}{}
		region, slot, err := store.readSlotLocked(owner, current)
		if err != nil {
			return Ref{}, nil, nil, -1, err
		}
		if slot.Kind != HeapContext {
			return Ref{}, nil, nil, -1, typeError(current, slot.Kind, HeapContext)
		}
		index, err := store.findBindingLocked(slot, name)
		if err != nil {
			return Ref{}, nil, nil, -1, err
		}
		if index >= 0 {
			return current, region, slot, index, nil
		}
		if slot.Context.Parent.Kind() == ValueNull {
			return Ref{}, nil, nil, -1, fmt.Errorf("%w: %q", ErrBindingNotFound, name)
		}
		current = slot.Context.Parent.Ref()
	}
}

func (store *Store) rejectContextCycleLocked(context, parent Ref) error {
	seen := make(map[Ref]struct{})
	current := parent
	for {
		if current == context {
			return fmt.Errorf("%w: %s -> %s", ErrContextCycle, context, parent)
		}
		if _, duplicate := seen[current]; duplicate {
			return ErrContextCycle
		}
		seen[current] = struct{}{}
		_, slot, err := store.slotLocked(current)
		if err != nil {
			return err
		}
		if slot.Kind != HeapContext {
			return typeError(current, slot.Kind, HeapContext)
		}
		if slot.Context.Parent.Kind() == ValueNull {
			return nil
		}
		current = slot.Context.Parent.Ref()
	}
}

func (store *Store) findBindingLocked(slot *Slot, name string) (int, error) {
	for index, binding := range slot.Context.Bindings {
		_, nameSlot, err := store.slotLocked(binding.Name)
		if err != nil {
			return -1, err
		}
		if nameSlot.Kind != HeapString {
			return -1, typeError(binding.Name, nameSlot.Kind, HeapString)
		}
		if nameSlot.String.Text == name {
			return index, nil
		}
	}
	return -1, nil
}
