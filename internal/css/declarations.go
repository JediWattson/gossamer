package css

import "strings"

// ParseDeclarationList parses and normalizes a CSS declaration block such as
// an element's style attribute. Duplicate properties keep their first position
// and their last declared value, matching the observable CSS declaration list.
func ParseDeclarationList(source string) []Declaration {
	return normalizeDeclarationList(parseDeclarations(source))
}

// SerializeDeclarationList returns the normalized CSSOM-style text for a
// declaration block.
func SerializeDeclarationList(declarations []Declaration) string {
	declarations = normalizeDeclarationList(declarations)
	var result strings.Builder
	for _, declaration := range declarations {
		result.WriteString(declaration.Property)
		result.WriteString(": ")
		result.WriteString(declaration.Value)
		if declaration.Important {
			result.WriteString(" !important")
		}
		result.WriteString("; ")
	}
	return strings.TrimSpace(result.String())
}

// DeclarationValue returns the value and priority of the named property.
func DeclarationValue(declarations []Declaration, property string) (string, bool, bool) {
	property, ok := normalizeDeclarationProperty(property)
	if !ok {
		return "", false, false
	}
	for index := len(declarations) - 1; index >= 0; index-- {
		if declarations[index].Property == property {
			return declarations[index].Value, declarations[index].Important, true
		}
	}
	return "", false, false
}

// DeclarationPropertyNames returns the declaration block's property names in
// observable order, without duplicates.
func DeclarationPropertyNames(declarations []Declaration) []string {
	declarations = normalizeDeclarationList(declarations)
	names := make([]string, len(declarations))
	for index, declaration := range declarations {
		names[index] = declaration.Property
	}
	return names
}

// SetDeclaration updates one property without disturbing the order of the
// other declarations. An empty value removes the property. Invalid names are
// ignored and reported through changed=false.
func SetDeclaration(declarations []Declaration, property, value string, important bool) ([]Declaration, bool) {
	property, ok := normalizeDeclarationProperty(property)
	if !ok {
		return normalizeDeclarationList(declarations), false
	}
	value = strings.TrimSpace(value)
	if value == "" {
		updated, _, changed := RemoveDeclaration(declarations, property)
		return updated, changed
	}

	updated := normalizeDeclarationList(declarations)
	for index := range updated {
		if updated[index].Property != property {
			continue
		}
		if updated[index].Value == value && updated[index].Important == important {
			return updated, false
		}
		updated[index].Value = value
		updated[index].Important = important
		return updated, true
	}
	return append(updated, Declaration{Property: property, Value: value, Important: important}), true
}

// RemoveDeclaration removes the named property and returns its previous value.
func RemoveDeclaration(declarations []Declaration, property string) ([]Declaration, string, bool) {
	property, ok := normalizeDeclarationProperty(property)
	if !ok {
		return normalizeDeclarationList(declarations), "", false
	}
	updated := normalizeDeclarationList(declarations)
	for index := range updated {
		if updated[index].Property != property {
			continue
		}
		previous := updated[index].Value
		copy(updated[index:], updated[index+1:])
		updated[len(updated)-1] = Declaration{}
		return updated[:len(updated)-1], previous, true
	}
	return updated, "", false
}

func normalizeDeclarationList(declarations []Declaration) []Declaration {
	result := make([]Declaration, 0, len(declarations))
	indices := make(map[string]int, len(declarations))
	for _, declaration := range declarations {
		property, ok := normalizeDeclarationProperty(declaration.Property)
		if !ok || strings.TrimSpace(declaration.Value) == "" {
			continue
		}
		declaration.Property = property
		declaration.Value = strings.TrimSpace(declaration.Value)
		if index, exists := indices[property]; exists {
			result[index] = declaration
			continue
		}
		indices[property] = len(result)
		result = append(result, declaration)
	}
	return result
}

func normalizeDeclarationProperty(property string) (string, bool) {
	property = trimCSSIgnorable(property)
	if !validPropertyName(property) {
		return "", false
	}
	if !strings.HasPrefix(property, "--") {
		property = strings.ToLower(property)
	}
	return property, true
}
