package css

// SelectorDependencies is an immutable conservative summary of DOM inputs
// read by a stylesheet's selectors. A positive result may over-invalidate;
// a negative result is safe to use when proving that a mutation cannot change
// selector matching.
type SelectorDependencies struct {
	attributes         map[string]struct{}
	characterData      bool
	formState          bool
	emptyText          bool
	directionText      bool
	formText           bool
	valueState         bool
	checkedState       bool
	selectedState      bool
	indeterminateState bool
	userValidityState  bool
	childList          bool
	ancestors          bool
	siblings           bool
	relational         bool
}

// SelectorDependencies returns the cached dependency summary compiled with
// the stylesheet's selector candidate index.
func (stylesheet Stylesheet) SelectorDependencies() SelectorDependencies {
	stylesheet = stylesheet.WithSelectorIndex()
	return stylesheet.selectorIndex.dependencies
}

// DependsOnAttribute reports whether changing the named content attribute can
// affect at least one selector in the stylesheet.
func (dependencies SelectorDependencies) DependsOnAttribute(name string) bool {
	_, found := dependencies.attributes[lowerASCII(name)]
	return found
}

func (dependencies SelectorDependencies) DependsOnCharacterData() bool {
	return dependencies.characterData
}

func (dependencies SelectorDependencies) DependsOnFormState() bool {
	return dependencies.formState
}

// DependsOnState reports whether one native control-state journal category is
// observed by the stylesheet's pseudo-classes. Selection-only changes are not
// selector-visible.
func (dependencies SelectorDependencies) DependsOnState(name string) bool {
	switch name {
	case "value":
		return dependencies.valueState
	case "checked":
		return dependencies.checkedState
	case "selected":
		return dependencies.selectedState
	case "indeterminate":
		return dependencies.indeterminateState
	case "user-validity":
		return dependencies.userValidityState
	case "reset":
		return dependencies.formState
	default:
		return false
	}
}

func (dependencies SelectorDependencies) DependsOnEmptyText() bool {
	return dependencies.emptyText
}

func (dependencies SelectorDependencies) DependsOnDirectionalityText() bool {
	return dependencies.directionText
}

func (dependencies SelectorDependencies) DependsOnFormText() bool {
	return dependencies.formText
}

func (dependencies SelectorDependencies) DependsOnChildList() bool {
	return dependencies.childList
}

// DependsOnAncestors, DependsOnSiblings, and DependsOnDescendants expose the
// directionality needed by the subsequent subtree-restyle invalidator.
func (dependencies SelectorDependencies) DependsOnAncestors() bool {
	return dependencies.ancestors
}

func (dependencies SelectorDependencies) DependsOnSiblings() bool {
	return dependencies.siblings
}

func (dependencies SelectorDependencies) DependsOnDescendants() bool {
	return dependencies.relational
}

func collectSelectorDependencies(selector Selector, dependencies *SelectorDependencies) {
	if dependencies == nil {
		return
	}
	if selector.leading != 0 {
		dependencies.childList = true
		dependencies.relational = true
	}
	for _, combinator := range selector.combinators {
		dependencies.childList = true
		switch combinator {
		case descendantCombinator, childCombinator:
			dependencies.ancestors = true
		case adjacentSiblingCombinator, generalSiblingCombinator:
			dependencies.siblings = true
		}
	}
	for _, compound := range selector.compounds {
		if len(compound.ids) != 0 {
			dependencies.addAttributes("id")
		}
		if len(compound.classes) != 0 {
			dependencies.addAttributes("class")
		}
		for _, attribute := range compound.attributes {
			dependencies.addAttributes(attribute.name)
		}
		for _, pseudo := range compound.pseudos {
			collectPseudoDependencies(pseudo, dependencies)
		}
	}
}

func collectPseudoDependencies(pseudo pseudoClassSelector, dependencies *SelectorDependencies) {
	for _, nested := range pseudo.selectors {
		collectSelectorDependencies(nested, dependencies)
	}
	switch pseudo.name {
	case "root", "first-child", "last-child", "only-child", "first-of-type", "last-of-type", "only-of-type",
		"nth-child", "nth-last-child", "nth-of-type", "nth-last-of-type":
		dependencies.childList = true
	case "empty":
		dependencies.childList = true
		dependencies.characterData = true
		dependencies.emptyText = true
	case "has":
		dependencies.childList = true
		dependencies.relational = true
	case "link", "any-link", "visited":
		dependencies.addAttributes("href")
	case "target":
		dependencies.addAttributes("id")
	case "checked":
		dependencies.addAttributes("checked", "selected", "type")
		dependencies.formState = true
		dependencies.checkedState = true
		dependencies.selectedState = true
	case "disabled", "enabled":
		dependencies.addAttributes("disabled")
		dependencies.childList = true
		dependencies.ancestors = true
	case "required", "optional":
		dependencies.addAttributes("required", "type")
	case "read-only", "read-write":
		dependencies.addAttributes("contenteditable", "disabled", "readonly", "type")
		dependencies.ancestors = true
		dependencies.childList = true
	case "placeholder-shown":
		dependencies.addAttributes("placeholder", "type", "value")
		dependencies.characterData = true
		dependencies.formState = true
		dependencies.formText = true
		dependencies.valueState = true
	case "default":
		dependencies.addAttributes("checked", "selected", "type", "command", "commandfor", "form", "id")
		dependencies.childList = true
		dependencies.ancestors = true
	case "indeterminate":
		dependencies.addAttributes("checked", "form", "id", "name", "type", "value")
		dependencies.formState = true
		dependencies.checkedState = true
		dependencies.indeterminateState = true
		dependencies.childList = true
	case "valid", "invalid", "user-valid", "user-invalid", "in-range", "out-of-range":
		dependencies.addAttributes(
			"checked", "disabled", "form", "id", "max", "maxlength", "min", "minlength", "multiple",
			"name", "pattern", "readonly", "required", "selected", "step", "type", "value",
		)
		dependencies.characterData = true
		dependencies.formState = true
		dependencies.formText = true
		dependencies.valueState = true
		dependencies.checkedState = true
		dependencies.selectedState = true
		if pseudo.name == "user-valid" || pseudo.name == "user-invalid" {
			dependencies.userValidityState = true
		}
		dependencies.childList = true
		dependencies.ancestors = true
	case "lang":
		dependencies.addAttributes("lang", "xml:lang")
		dependencies.ancestors = true
	case "dir":
		dependencies.addAttributes("dir", "type", "value")
		dependencies.characterData = true
		dependencies.formState = true
		dependencies.directionText = true
		dependencies.valueState = true
		dependencies.ancestors = true
	}
}

func (dependencies *SelectorDependencies) addAttributes(names ...string) {
	if dependencies.attributes == nil {
		dependencies.attributes = make(map[string]struct{}, len(names))
	}
	for _, name := range names {
		dependencies.attributes[lowerASCII(name)] = struct{}{}
	}
}
