package css

const maxNestedSelectorExpansion = 4096

// parseNestedSelectorList expands the supported CSS nesting selector forms
// against every selector in the parent list. The current boundary supports an
// omitted nesting selector (implicit descendant), leading combinators, and a
// single leading & with compound suffixes and following combinators.
func parseNestedSelectorList(source string, parents []Selector) ([]Selector, bool) {
	values, err := ParseComponentValues(source)
	if err != nil || len(parents) == 0 {
		return nil, false
	}
	groups := splitSelectorComponentGroups(values)
	if len(groups) == 0 || len(groups) > maxNestedSelectorExpansion/len(parents) {
		return nil, false
	}
	greatestParentSpecificity := parents[0].specificity
	for _, parent := range parents[1:] {
		if parent.specificity.Compare(greatestParentSpecificity) > 0 {
			greatestParentSpecificity = parent.specificity
		}
	}
	selectors := make([]Selector, 0, len(groups)*len(parents))
	for _, group := range groups {
		group = trimComponentWhitespace(group)
		if len(group) == 0 {
			return nil, false
		}
		for _, parent := range parents {
			parent = cloneSelector(parent)
			// The nesting selector has the specificity of the most specific
			// selector in its associated parent list, matching :is() behavior.
			parent.specificity = greatestParentSpecificity
			selector, ok := expandNestedSelectorGroup(source, group, parent)
			if !ok {
				return nil, false
			}
			selectors = append(selectors, selector)
		}
	}
	return selectors, len(selectors) > 0
}

func expandNestedSelectorGroup(source string, values []ComponentValue, parent Selector) (Selector, bool) {
	position := 0
	for position < len(values) && isWhitespaceComponent(values[position]) {
		position++
	}
	if position >= len(values) {
		return Selector{}, false
	}

	if isNestingSelector(values[position]) {
		position++
		hadWhitespace := false
		for position < len(values) && isWhitespaceComponent(values[position]) {
			hadWhitespace = true
			position++
		}
		if position == len(values) {
			return cloneSelector(parent), true
		}
		if containsNestingSelector(values[position:]) {
			return Selector{}, false
		}
		if combinator, next, ok := leadingNestedCombinator(values, position); ok {
			child, valid := parseNestedRemainder(source, values[next:])
			if !valid {
				return Selector{}, false
			}
			return appendNestedSelector(parent, child, combinator), true
		}
		child, valid := parseNestedRemainder(source, values[position:])
		if !valid {
			return Selector{}, false
		}
		if hadWhitespace {
			return appendNestedSelector(parent, child, descendantCombinator), true
		}
		return mergeNestedSelectorCompound(parent, child)
	}
	if containsNestingSelector(values[position:]) {
		return Selector{}, false
	}

	if combinator, next, ok := leadingNestedCombinator(values, position); ok {
		child, valid := parseNestedRemainder(source, values[next:])
		if !valid {
			return Selector{}, false
		}
		return appendNestedSelector(parent, child, combinator), true
	}
	child, valid := parseNestedRemainder(source, values[position:])
	if !valid {
		return Selector{}, false
	}
	return appendNestedSelector(parent, child, descendantCombinator), true
}

func parseNestedRemainder(source string, values []ComponentValue) (Selector, bool) {
	values = trimComponentWhitespace(values)
	selectors, ok := parseTokenSelectorComponents(source, values, 1, false)
	if !ok || len(selectors) != 1 {
		return Selector{}, false
	}
	return selectors[0], true
}

func leadingNestedCombinator(values []ComponentValue, position int) (selectorCombinator, int, bool) {
	if position >= len(values) || values[position].Kind != ComponentToken || values[position].Token.Kind != TokenDelim {
		return 0, position, false
	}
	var combinator selectorCombinator
	switch values[position].Token.Value {
	case ">":
		combinator = childCombinator
	case "+":
		combinator = adjacentSiblingCombinator
	case "~":
		combinator = generalSiblingCombinator
	default:
		return 0, position, false
	}
	position++
	for position < len(values) && isWhitespaceComponent(values[position]) {
		position++
	}
	return combinator, position, true
}

func isNestingSelector(value ComponentValue) bool {
	return value.Kind == ComponentToken && value.Token.Kind == TokenDelim && value.Token.Value == "&"
}

func containsNestingSelector(values []ComponentValue) bool {
	for _, value := range values {
		if isNestingSelector(value) || containsNestingSelector(value.Values) {
			return true
		}
	}
	return false
}

func appendNestedSelector(parent, child Selector, combinator selectorCombinator) Selector {
	result := cloneSelector(parent)
	result.compounds = append(result.compounds, cloneCompounds(child.compounds)...)
	result.combinators = append(result.combinators, combinator)
	result.combinators = append(result.combinators, child.combinators...)
	result.specificity = result.specificity.add(child.specificity)
	return result
}

func mergeNestedSelectorCompound(parent, suffix Selector) (Selector, bool) {
	if len(parent.compounds) == 0 || len(suffix.compounds) == 0 || suffix.compounds[0].typeName != "" {
		return Selector{}, false
	}
	result := cloneSelector(parent)
	target := &result.compounds[len(result.compounds)-1]
	addition := suffix.compounds[0]
	target.ids = append(target.ids, addition.ids...)
	target.classes = append(target.classes, addition.classes...)
	target.attributes = append(target.attributes, addition.attributes...)
	target.pseudos = append(target.pseudos, addition.pseudos...)
	result.compounds = append(result.compounds, cloneCompounds(suffix.compounds[1:])...)
	result.combinators = append(result.combinators, suffix.combinators...)
	result.specificity = result.specificity.add(suffix.specificity)
	return result, true
}

func cloneSelector(source Selector) Selector {
	return Selector{
		specificity: source.specificity,
		compounds:   cloneCompounds(source.compounds),
		combinators: append([]selectorCombinator(nil), source.combinators...),
	}
}

func cloneCompounds(source []compoundSelector) []compoundSelector {
	result := make([]compoundSelector, len(source))
	for index, compound := range source {
		compound.ids = append([]string(nil), compound.ids...)
		compound.classes = append([]string(nil), compound.classes...)
		compound.attributes = append([]attributeSelector(nil), compound.attributes...)
		compound.pseudos = append([]pseudoClassSelector(nil), compound.pseudos...)
		result[index] = compound
	}
	return result
}
