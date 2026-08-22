package runtime

import (
	"fmt"
	"strings"

	"github.com/JediWattson/gossamer/internal/runtime/memory"
	"golang.org/x/text/language"
	"golang.org/x/text/language/display"
)

const intlDisplayNamesTypeProperty = "[[Intl.DisplayNamesType]]"

func (intrinsics *Intrinsics) installIntlBuiltins(context *TaskContext) error {
	intlObject, err := context.NewHeapObject()
	if err != nil {
		return err
	}
	if err := context.SetPrototype(intlObject, memory.RefValue(intrinsics.ObjectPrototype)); err != nil {
		return err
	}
	displayNamesPrototype, err := context.NewHeapObject()
	if err != nil {
		return err
	}
	if err := context.SetPrototype(displayNamesPrototype, memory.RefValue(intrinsics.ObjectPrototype)); err != nil {
		return err
	}
	name, err := context.NewString("DisplayNames")
	if err != nil {
		return err
	}
	displayNames, err := context.Realm.store.AllocNativeConstructor(
		context.Owner, context.MemoryRegion, memory.RefValue(name), memory.NullValue(), 2, nativeIntlDisplayNamesConstructor,
	)
	if err != nil {
		return err
	}
	if err := intrinsics.initializeFunctionWithPrototype(
		context, displayNames, memory.RefValue(name), 2, displayNamesPrototype,
	); err != nil {
		return err
	}
	if err := installMethods(intrinsics, context, displayNamesPrototype, []builtinMethod{
		{"of", 1, nativeIntlDisplayNamesOf},
	}); err != nil {
		return err
	}
	if err := defineData(context, intlObject, "DisplayNames", memory.RefValue(displayNames), true, false, true); err != nil {
		return err
	}
	return intrinsics.defineGlobal(context, "Intl", memory.RefValue(intlObject))
}

func builtinIntlDisplayNamesConstructor(execution *execution, _ memory.Ref, _ memory.Function, this memory.Value, arguments []memory.Value) (memory.Value, error) {
	if !this.IsRef() {
		return memory.Value{}, ErrNotConstructor
	}
	options, err := requireObjectLike(execution.context, argument(arguments, 1), "Intl.DisplayNames options")
	if err != nil {
		return memory.Value{}, err
	}
	typeValue, found, err := execution.namedProperty(memory.RefValue(options), "type")
	if err != nil {
		return memory.Value{}, err
	}
	if !found {
		return memory.Value{}, fmt.Errorf("%w: Intl.DisplayNames type is required", memory.ErrInvalidIndex)
	}
	displayType, err := execution.toString(typeValue)
	if err != nil {
		return memory.Value{}, err
	}
	if displayType != "region" {
		return memory.Value{}, fmt.Errorf("%w: unsupported Intl.DisplayNames type %q", memory.ErrInvalidIndex, displayType)
	}
	if err := defineData(execution.context, this.Ref(), intlDisplayNamesTypeProperty, typeValue, false, false, false); err != nil {
		return memory.Value{}, err
	}
	return this, nil
}

func builtinIntlDisplayNamesOf(execution *execution, _ memory.Ref, _ memory.Function, this memory.Value, arguments []memory.Value) (memory.Value, error) {
	if !this.IsRef() {
		return memory.Value{}, fmt.Errorf("%w: Intl.DisplayNames receiver", ErrOperandType)
	}
	displayType, found, err := execution.namedProperty(this, intlDisplayNamesTypeProperty)
	if err != nil {
		return memory.Value{}, err
	}
	if !found {
		return memory.Value{}, fmt.Errorf("%w: Intl.DisplayNames receiver", ErrOperandType)
	}
	typeText, err := execution.toString(displayType)
	if err != nil || typeText != "region" {
		if err != nil {
			return memory.Value{}, err
		}
		return memory.Value{}, fmt.Errorf("%w: unsupported Intl.DisplayNames receiver", ErrOperandType)
	}
	code, err := execution.toString(argument(arguments, 0))
	if err != nil {
		return memory.Value{}, err
	}
	if code != strings.ToUpper(code) {
		return newStringValue(execution.context, code)
	}
	region, err := language.ParseRegion(code)
	if err != nil {
		return memory.Value{}, fmt.Errorf("%w: invalid region %q", memory.ErrInvalidIndex, code)
	}
	return newStringValue(execution.context, display.Regions(language.English).Name(region))
}

func (execution *execution) namedProperty(base memory.Value, name string) (memory.Value, bool, error) {
	key, err := execution.context.NewString(name)
	if err != nil {
		return memory.Value{}, false, err
	}
	return execution.getProperty(base, memory.RefValue(key))
}
