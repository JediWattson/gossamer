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
		_, parentSlot, lookupErr := store.slotLocked(parent.Ref())
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
	parent, err = store.replaceValueLocked(owner, region, slot, slot.Context.Parent, parent, internal)
	if err != nil {
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

// DeclareIndirectBinding creates an immutable live alias. Reads follow the
// target Context slot every time; assignment through the importing binding is
// rejected without mutating the exporter.
func (store *Store) DeclareIndirectBinding(owner ownership.OwnerID, context, name, targetContext, targetName Ref) error {
	if store == nil {
		return fmt.Errorf("memory: nil store")
	}
	store.mutex.Lock()
	defer store.mutex.Unlock()
	return store.declareIndirectBindingLocked(owner, context, name, targetContext, targetName, false)
}

func (store *Store) declareIndirectBindingLocked(owner ownership.OwnerID, context, name, targetContext, targetName Ref, internal bool) error {
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
	_, targetSlot, err := store.readSlotLocked(owner, targetContext)
	if err != nil {
		return err
	}
	if targetSlot.Kind != HeapContext {
		return typeError(targetContext, targetSlot.Kind, HeapContext)
	}
	if _, err := store.stringTextLocked(owner, targetName, internal); err != nil {
		return err
	}

	prepared := make([]Value, 0, 3)
	for _, value := range []Value{RefValue(name), RefValue(targetContext), RefValue(targetName)} {
		linked, linkErr := store.replaceValueLocked(owner, region, slot, Value{}, value, internal)
		if linkErr != nil {
			for index := len(prepared) - 1; index >= 0; index-- {
				_, _ = store.replaceValueLocked(owner, region, slot, prepared[index], Value{}, internal)
			}
			return linkErr
		}
		prepared = append(prepared, linked)
	}
	slot.Context.Bindings = append(slot.Context.Bindings, Binding{
		Name: prepared[0].Ref(), Initialized: true, Indirect: true,
		Target: prepared[1].Ref(), TargetName: prepared[2].Ref(),
	})
	return nil
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
	preparedName, err := store.replaceValueLocked(owner, region, slot, Value{}, RefValue(name), internal)
	if err != nil {
		return err
	}
	slot.Context.Bindings = append(slot.Context.Bindings, Binding{Name: preparedName.Ref(), Mutable: mutable})
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
	if slot.Context.Bindings[index].Indirect {
		return fmt.Errorf("%w: %q is an indirect binding", ErrImmutableBinding, nameText)
	}
	if slot.Context.Bindings[index].Initialized {
		return fmt.Errorf("%w: %q is already initialized", ErrBindingExists, nameText)
	}
	value, err = store.replaceValueLocked(owner, region, slot, Value{}, value, internal)
	if err != nil {
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
	if binding.Indirect {
		return fmt.Errorf("%w: %q", ErrImmutableBinding, nameText)
	}
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
	value, err = store.replaceValueLocked(owner, region, slot, binding.Value, value, false)
	if err != nil {
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
	contextRef, _, slot, index, err := store.resolveBindingSlotLocked(owner, context, nameText)
	if errors.Is(err, ErrBindingNotFound) {
		return Value{}, false, nil
	}
	if err != nil {
		return Value{}, false, err
	}
	value, err := store.resolveBindingValueLocked(owner, contextRef, slot, index, make(map[bindingLocation]struct{}))
	return value, true, err
}

type bindingLocation struct {
	context Ref
	index   int
}

func (store *Store) resolveBindingValueLocked(owner ownership.OwnerID, context Ref, slot *Slot, index int, seen map[bindingLocation]struct{}) (Value, error) {
	location := bindingLocation{context: context, index: index}
	if _, duplicate := seen[location]; duplicate {
		return Value{}, ErrBindingCycle
	}
	seen[location] = struct{}{}
	binding := slot.Context.Bindings[index]
	if !binding.Initialized {
		name, _ := store.stringTextLocked(owner, binding.Name, false)
		return Value{}, fmt.Errorf("%w: %q", ErrBindingUninitialized, name)
	}
	if !binding.Indirect {
		return binding.Value, nil
	}
	targetName, err := store.stringTextLocked(owner, binding.TargetName, false)
	if err != nil {
		return Value{}, err
	}
	targetContext, _, targetSlot, targetIndex, err := store.resolveBindingSlotLocked(owner, binding.Target, targetName)
	if err != nil {
		return Value{}, err
	}
	return store.resolveBindingValueLocked(owner, targetContext, targetSlot, targetIndex, seen)
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
