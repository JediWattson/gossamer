package runtime

import (
	"fmt"
	"strings"
	"unicode"

	"github.com/JediWattson/gossamer/internal/runtime/memory"
	"golang.org/x/text/collate"
	"golang.org/x/text/language"
	"golang.org/x/text/language/display"
)

const (
	intlDisplayNamesTypeProperty      = "[[Intl.DisplayNamesType]]"
	intlCollatorLocaleProperty        = "[[Intl.CollatorLocale]]"
	intlCollatorUsageProperty         = "[[Intl.CollatorUsage]]"
	intlCollatorSensitivityProperty   = "[[Intl.CollatorSensitivity]]"
	intlCollatorNumericProperty       = "[[Intl.CollatorNumeric]]"
	intlCollatorIgnorePunctProperty   = "[[Intl.CollatorIgnorePunctuation]]"
	intlCollatorBoundCompareProperty  = "[[Intl.CollatorBoundCompare]]"
)

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

	collatorPrototype, err := context.NewHeapObject()
	if err != nil {
		return err
	}
	if err := context.SetPrototype(collatorPrototype, memory.RefValue(intrinsics.ObjectPrototype)); err != nil {
		return err
	}
	collatorName, err := context.NewString("Collator")
	if err != nil {
		return err
	}
	collator, err := context.Realm.store.AllocNativeConstructor(
		context.Owner, context.MemoryRegion, memory.RefValue(collatorName), memory.NullValue(), 0, nativeIntlCollatorConstructor,
	)
	if err != nil {
		return err
	}
	if err := intrinsics.initializeFunctionWithPrototype(
		context, collator, memory.RefValue(collatorName), 0, collatorPrototype,
	); err != nil {
		return err
	}
	compareGetterName, err := newStringValue(context, "get compare")
	if err != nil {
		return err
	}
	compareGetter, err := context.Realm.store.AllocNativeFunction(
		context.Owner, context.MemoryRegion, compareGetterName, memory.NullValue(), 0, nativeIntlCollatorCompareGetter,
	)
	if err != nil {
		return err
	}
	if err := intrinsics.initializeFunction(context, compareGetter, compareGetterName, 0, false); err != nil {
		return err
	}
	compareKey, err := context.NewString("compare")
	if err != nil {
		return err
	}
	if err := context.DefineProperty(collatorPrototype, compareKey, memory.AccessorProperty(memory.RefValue(compareGetter), memory.UndefinedValue(), false, true)); err != nil {
		return err
	}
	if err := installMethods(intrinsics, context, collatorPrototype, []builtinMethod{
		{"resolvedOptions", 0, nativeIntlCollatorResolvedOptions},
	}); err != nil {
		return err
	}
	if err := defineData(context, intlObject, "Collator", memory.RefValue(collator), true, false, true); err != nil {
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

type intlCollatorConfig struct {
	locale            string
	tag               language.Tag
	usage             string
	sensitivity       string
	numeric           bool
	ignorePunctuation bool
}

func builtinIntlCollatorConstructor(execution *execution, functionRef memory.Ref, _ memory.Function, this memory.Value, arguments []memory.Value) (memory.Value, error) {
	protoValue, found, err := execution.namedProperty(memory.RefValue(functionRef), "prototype")
	if err != nil || !found || !protoValue.IsRef() {
		if err != nil {
			return memory.Value{}, err
		}
		return memory.Value{}, fmt.Errorf("runtime: Intl.Collator prototype missing")
	}
	targetRef := memory.Ref{}
	if this.IsRef() && inheritsFromPrototype(execution.context, this.Ref(), protoValue.Ref()) {
		targetRef = this.Ref()
	} else {
		obj, err := execution.context.NewHeapObject()
		if err != nil {
			return memory.Value{}, err
		}
		if err := execution.context.SetPrototype(obj, protoValue); err != nil {
			return memory.Value{}, err
		}
		targetRef = obj
	}

	locale, err := resolveIntlCollatorLocale(execution, argument(arguments, 0))
	if err != nil {
		return memory.Value{}, err
	}
	usage := "sort"
	sensitivity := "variant"
	numeric := false
	ignorePunctuation := false

	optionsArg := argument(arguments, 1)
	if optionsArg.Kind() != memory.ValueUndefined {
		options, err := requireObjectLike(execution.context, optionsArg, "Intl.Collator options")
		if err != nil {
			return memory.Value{}, err
		}
		optionsRef := memory.RefValue(options)
		if v, ok, err := execution.namedProperty(optionsRef, "usage"); err != nil {
			return memory.Value{}, err
		} else if ok && v.Kind() != memory.ValueUndefined {
			usage, err = execution.toString(v)
			if err != nil {
				return memory.Value{}, err
			}
			if usage != "sort" && usage != "search" {
				return memory.Value{}, fmt.Errorf("%w: invalid Intl.Collator usage %q", memory.ErrInvalidIndex, usage)
			}
		}
		if v, ok, err := execution.namedProperty(optionsRef, "sensitivity"); err != nil {
			return memory.Value{}, err
		} else if ok && v.Kind() != memory.ValueUndefined {
			sensitivity, err = execution.toString(v)
			if err != nil {
				return memory.Value{}, err
			}
			switch sensitivity {
			case "base", "accent", "case", "variant":
			default:
				return memory.Value{}, fmt.Errorf("%w: invalid Intl.Collator sensitivity %q", memory.ErrInvalidIndex, sensitivity)
			}
		}
		if v, ok, err := execution.namedProperty(optionsRef, "numeric"); err != nil {
			return memory.Value{}, err
		} else if ok && v.Kind() != memory.ValueUndefined {
			numeric, err = valueTruthy(execution.context, v)
			if err != nil {
				return memory.Value{}, err
			}
		}
		if v, ok, err := execution.namedProperty(optionsRef, "ignorePunctuation"); err != nil {
			return memory.Value{}, err
		} else if ok && v.Kind() != memory.ValueUndefined {
			ignorePunctuation, err = valueTruthy(execution.context, v)
			if err != nil {
				return memory.Value{}, err
			}
		}
	}

	localeVal, err := newStringValue(execution.context, locale)
	if err != nil {
		return memory.Value{}, err
	}
	usageVal, err := newStringValue(execution.context, usage)
	if err != nil {
		return memory.Value{}, err
	}
	sensitivityVal, err := newStringValue(execution.context, sensitivity)
	if err != nil {
		return memory.Value{}, err
	}
	for _, prop := range []struct {
		name  string
		value memory.Value
	}{
		{intlCollatorLocaleProperty, localeVal},
		{intlCollatorUsageProperty, usageVal},
		{intlCollatorSensitivityProperty, sensitivityVal},
		{intlCollatorNumericProperty, memory.BoolValue(numeric)},
		{intlCollatorIgnorePunctProperty, memory.BoolValue(ignorePunctuation)},
	} {
		if err := defineData(execution.context, targetRef, prop.name, prop.value, false, false, false); err != nil {
			return memory.Value{}, err
		}
	}
	return memory.RefValue(targetRef), nil
}

func builtinIntlCollatorCompareGetter(execution *execution, _ memory.Ref, _ memory.Function, this memory.Value, _ []memory.Value) (memory.Value, error) {
	if _, err := loadIntlCollatorConfig(execution, this); err != nil {
		return memory.Value{}, err
	}
	if cached, found, err := execution.namedProperty(this, intlCollatorBoundCompareProperty); err != nil {
		return memory.Value{}, err
	} else if found && cached.IsRef() {
		return cached, nil
	}
	name, err := newStringValue(execution.context, "compare")
	if err != nil {
		return memory.Value{}, err
	}
	bound, err := execution.context.NewBoundNativeFunction(name, memory.NullValue(), 2, nativeIntlCollatorCompare, this)
	if err != nil {
		return memory.Value{}, err
	}
	boundVal := memory.RefValue(bound)
	if err := defineData(execution.context, this.Ref(), intlCollatorBoundCompareProperty, boundVal, false, false, false); err != nil {
		return memory.Value{}, err
	}
	return boundVal, nil
}

func builtinIntlCollatorCompare(execution *execution, _ memory.Ref, function memory.Function, this memory.Value, arguments []memory.Value) (memory.Value, error) {
	receiver := this
	if len(function.Captures) > 0 && function.Captures[0].IsRef() {
		receiver = function.Captures[0]
	}
	cfg, err := loadIntlCollatorConfig(execution, receiver)
	if err != nil {
		return memory.Value{}, err
	}
	left, err := execution.toString(argument(arguments, 0))
	if err != nil {
		return memory.Value{}, err
	}
	right, err := execution.toString(argument(arguments, 1))
	if err != nil {
		return memory.Value{}, err
	}
	if cfg.ignorePunctuation {
		left = stripPunctuation(left)
		right = stripPunctuation(right)
	}
	var opts []collate.Option
	switch cfg.sensitivity {
	case "base":
		opts = append(opts, collate.IgnoreCase, collate.IgnoreDiacritics)
	case "accent":
		opts = append(opts, collate.IgnoreCase)
	case "case":
		opts = append(opts, collate.IgnoreDiacritics)
	}
	if cfg.numeric {
		opts = append(opts, collate.Numeric)
	}
	collator := collate.New(cfg.tag, opts...)
	return memory.NumberValue(float64(collator.CompareString(left, right))), nil
}

func builtinIntlCollatorResolvedOptions(execution *execution, _ memory.Ref, _ memory.Function, this memory.Value, _ []memory.Value) (memory.Value, error) {
	cfg, err := loadIntlCollatorConfig(execution, this)
	if err != nil {
		return memory.Value{}, err
	}
	obj, err := execution.context.NewHeapObject()
	if err != nil {
		return memory.Value{}, err
	}
	if err := execution.context.SetPrototype(obj, memory.RefValue(execution.context.intrinsics.ObjectPrototype)); err != nil {
		return memory.Value{}, err
	}
	localeVal, err := newStringValue(execution.context, cfg.locale)
	if err != nil {
		return memory.Value{}, err
	}
	usageVal, err := newStringValue(execution.context, cfg.usage)
	if err != nil {
		return memory.Value{}, err
	}
	sensitivityVal, err := newStringValue(execution.context, cfg.sensitivity)
	if err != nil {
		return memory.Value{}, err
	}
	collationVal, err := newStringValue(execution.context, "default")
	if err != nil {
		return memory.Value{}, err
	}
	caseFirstVal, err := newStringValue(execution.context, "false")
	if err != nil {
		return memory.Value{}, err
	}
	for _, prop := range []struct {
		name  string
		value memory.Value
	}{
		{"locale", localeVal},
		{"usage", usageVal},
		{"sensitivity", sensitivityVal},
		{"ignorePunctuation", memory.BoolValue(cfg.ignorePunctuation)},
		{"collation", collationVal},
		{"numeric", memory.BoolValue(cfg.numeric)},
		{"caseFirst", caseFirstVal},
	} {
		if err := defineData(execution.context, obj, prop.name, prop.value, true, true, true); err != nil {
			return memory.Value{}, err
		}
	}
	return memory.RefValue(obj), nil
}

func loadIntlCollatorConfig(execution *execution, value memory.Value) (intlCollatorConfig, error) {
	if !value.IsRef() {
		return intlCollatorConfig{}, fmt.Errorf("%w: Intl.Collator receiver", ErrOperandType)
	}
	sensitivityVal, found, err := execution.namedProperty(value, intlCollatorSensitivityProperty)
	if err != nil {
		return intlCollatorConfig{}, err
	}
	if !found {
		return intlCollatorConfig{}, fmt.Errorf("%w: Intl.Collator receiver", ErrOperandType)
	}
	sensitivity, err := execution.toString(sensitivityVal)
	if err != nil {
		return intlCollatorConfig{}, err
	}
	locale := "en"
	if localeVal, ok, err := execution.namedProperty(value, intlCollatorLocaleProperty); err != nil {
		return intlCollatorConfig{}, err
	} else if ok {
		locale, err = execution.toString(localeVal)
		if err != nil {
			return intlCollatorConfig{}, err
		}
	}
	tag, err := language.Parse(locale)
	if err != nil {
		tag = language.English
	}
	usage := "sort"
	if usageVal, ok, err := execution.namedProperty(value, intlCollatorUsageProperty); err != nil {
		return intlCollatorConfig{}, err
	} else if ok {
		usage, err = execution.toString(usageVal)
		if err != nil {
			return intlCollatorConfig{}, err
		}
	}
	numeric := false
	if numericVal, ok, err := execution.namedProperty(value, intlCollatorNumericProperty); err != nil {
		return intlCollatorConfig{}, err
	} else if ok {
		numeric, err = valueTruthy(execution.context, numericVal)
		if err != nil {
			return intlCollatorConfig{}, err
		}
	}
	ignorePunct := false
	if punctVal, ok, err := execution.namedProperty(value, intlCollatorIgnorePunctProperty); err != nil {
		return intlCollatorConfig{}, err
	} else if ok {
		ignorePunct, err = valueTruthy(execution.context, punctVal)
		if err != nil {
			return intlCollatorConfig{}, err
		}
	}
	return intlCollatorConfig{
		locale:            locale,
		tag:               tag,
		usage:             usage,
		sensitivity:       sensitivity,
		numeric:           numeric,
		ignorePunctuation: ignorePunct,
	}, nil
}

func resolveIntlCollatorLocale(execution *execution, value memory.Value) (string, error) {
	if value.Kind() == memory.ValueUndefined {
		return "en", nil
	}
	candidate := value
	if value.IsRef() {
		if array, err := execution.context.DerefArray(value.Ref()); err == nil {
			if array.Length == 0 {
				return "en", nil
			}
			firstKey, err := execution.context.NewString("0")
			if err != nil {
				return "", err
			}
			firstVal, ok, err := execution.getProperty(value, memory.RefValue(firstKey))
			if err != nil {
				return "", err
			}
			if ok {
				candidate = firstVal
			}
		}
	}
	tagText, err := execution.toString(candidate)
	if err != nil {
		return "", err
	}
	tag, err := language.Parse(tagText)
	if err != nil {
		return "", fmt.Errorf("%w: invalid language tag %q", memory.ErrInvalidIndex, tagText)
	}
	return tag.String(), nil
}

func inheritsFromPrototype(context *TaskContext, ref, expected memory.Ref) bool {
	current := ref
	for depth := 0; depth < 32 && current != (memory.Ref{}); depth++ {
		header, err := context.DerefObjectHeader(current)
		if err != nil || !header.Prototype.IsRef() {
			return false
		}
		if header.Prototype.Ref() == expected {
			return true
		}
		current = header.Prototype.Ref()
	}
	return false
}

func stripPunctuation(text string) string {
	var builder strings.Builder
	builder.Grow(len(text))
	for _, r := range text {
		if !unicode.IsPunct(r) {
			builder.WriteRune(r)
		}
	}
	return builder.String()
}
