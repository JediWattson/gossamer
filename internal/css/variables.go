package css

import (
	"sort"
	"strings"
	"unicode/utf8"
)

const (
	maxCustomPropertyDepth      = 128
	maxCustomPropertyReferences = 4096
	maxCustomPropertyValueBytes = 1 << 20
	maxCustomPropertyTotalBytes = 16 << 20
)

// CustomProperties is an immutable set of resolved custom-property values.
// Its zero value is an empty set.
type CustomProperties struct {
	layer *customPropertyLayer
}

type customPropertyLayer struct {
	parent  *customPropertyLayer
	changes map[string]customPropertyValue
}

type customPropertyValue struct {
	value   string
	present bool
}

// ResolveCustomProperties applies specified custom properties over an already
// resolved parent set. Custom properties inherit by default. Values are
// resolved without regard to map iteration order, and invalid or cyclic values
// are omitted from the returned set.
func ResolveCustomProperties(parent CustomProperties, specified map[string]string) CustomProperties {
	if len(specified) == 0 {
		return parent
	}

	local := make(map[string]string, len(specified))
	initial := make(map[string]bool)

	specifiedNames := make([]string, 0, len(specified))
	for name := range specified {
		specifiedNames = append(specifiedNames, name)
	}
	sort.Strings(specifiedNames)
	for _, specifiedName := range specifiedNames {
		name, valid := canonicalCustomPropertyName(specifiedName)
		if !valid {
			continue
		}
		source := specified[specifiedName]
		if !ValidCustomPropertyValue(source) {
			// Invalid declaration-value or var() syntax invalidates the declaration
			// at parse time, so it must not shadow a valid inherited declaration.
			// Callers should use the same validator before choosing a winner among
			// local declarations.
			continue
		}
		switch exactCSSWideKeyword(source) {
		case "inherit", "unset":
			// Inheritance is already represented by the parent layer.
			delete(local, name)
			delete(initial, name)
		case "initial":
			delete(local, name)
			initial[name] = true
		default:
			// A local declaration shadows the inherited value even when it later
			// becomes invalid at computed-value time.
			delete(initial, name)
			local[name] = source
		}
	}

	dependencies := make(map[string][]string, len(local))
	invalid := make(map[string]bool)
	for name, source := range local {
		references := 0
		ok := walkVarReferences(source, 0, func(reference string) bool {
			references++
			if references > maxCustomPropertyReferences {
				return false
			}
			dependencies[name] = append(dependencies[name], reference)
			return true
		})
		if !ok {
			invalid[name] = true
		}
	}
	markCyclicCustomProperties(local, dependencies, invalid)

	resolver := customPropertyResolver{
		inherited:      parent,
		initial:        initial,
		local:          local,
		invalid:        invalid,
		state:          make(map[string]customPropertyState, len(local)),
		resolved:       make(map[string]string, len(local)),
		remainingBytes: maxCustomPropertyTotalBytes,
	}
	names := make([]string, 0, len(local))
	for name := range local {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		resolver.resolve(name, 0)
	}

	changes := make(map[string]customPropertyValue, len(initial)+len(local))
	recordChange := func(name, value string, present bool) {
		inheritedValue, inheritedPresent := parent.lookupCanonical(name)
		if inheritedPresent == present && (!present || inheritedValue == value) {
			return
		}
		changes[name] = customPropertyValue{value: value, present: present}
	}
	for name := range initial {
		recordChange(name, "", false)
	}
	for _, name := range names {
		if value, ok := resolver.resolved[name]; ok && !resolver.invalid[name] {
			recordChange(name, value, true)
			continue
		}
		recordChange(name, "", false)
	}
	if len(changes) == 0 {
		return parent
	}
	return CustomProperties{layer: &customPropertyLayer{parent: parent.layer, changes: changes}}
}

// Substitute replaces var() functions in source with values from properties.
// It returns false for a missing value without a fallback, malformed var()
// syntax, or an expansion that exceeds the resolver's safety bounds.
func (properties CustomProperties) Substitute(source string) (string, bool) {
	if !ValidVariableFunctions(source) {
		return "", false
	}
	return substituteCustomPropertyValue(source, 0, func(name string, _ int) (string, bool) {
		return properties.lookupCanonical(name)
	})
}

// Value returns a resolved custom-property value. Empty values are reported as
// present with ok set to true.
func (properties CustomProperties) Value(name string) (value string, ok bool) {
	canonical, valid := canonicalCustomPropertyName(name)
	if !valid {
		return "", false
	}
	return properties.lookupCanonical(canonical)
}

// Names returns the canonical names of every effective custom property in
// ascending byte order. Names shadowed by an absent local value are omitted.
// The returned slice is owned by the caller.
func (properties CustomProperties) Names() []string {
	seen := make(map[string]struct{})
	names := make([]string, 0)
	for layer := properties.layer; layer != nil; layer = layer.parent {
		for name, change := range layer.changes {
			if _, ok := seen[name]; ok {
				continue
			}
			seen[name] = struct{}{}
			if change.present {
				names = append(names, name)
			}
		}
	}
	sort.Strings(names)
	return names
}

func (properties CustomProperties) lookupCanonical(name string) (string, bool) {
	for layer := properties.layer; layer != nil; layer = layer.parent {
		if change, ok := layer.changes[name]; ok {
			return change.value, change.present
		}
	}
	return "", false
}

// ValidVariableFunctions reports whether every var() function in source has
// valid variable-function syntax. It is intended for declaration validation
// before the cascade chooses a winning custom-property declaration.
func ValidVariableFunctions(source string) bool {
	type pendingValue struct {
		source string
		depth  int
	}
	pending := []pendingValue{{source: source}}
	for len(pending) > 0 {
		last := len(pending) - 1
		current := pending[last]
		pending = pending[:last]
		if current.depth > maxCustomPropertyDepth {
			return false
		}
		for position := 0; position < len(current.source); {
			_, open, _, found, valid := nextVarFunction(current.source, position)
			if !valid {
				return false
			}
			if !found {
				break
			}
			invocation, ok := parseVarInvocation(current.source, open)
			if !ok {
				return false
			}
			if invocation.hasFallback {
				// A var() fallback has its own <declaration-value> grammar. Tokens
				// such as ! and ; are allowed inside nested blocks, but not at the
				// fallback's top level.
				if !validDeclarationValueSyntax(invocation.fallback) {
					return false
				}
				pending = append(pending, pendingValue{source: invocation.fallback, depth: current.depth + 1})
			}
			position = invocation.end
		}
	}
	return true
}

// ValidDeclarationValue reports whether source is a syntactically valid CSS
// declaration value. Empty values are valid at this layer. Top-level semicolons
// and exclamation marks, unmatched closing blocks, bad strings, bad URLs, and
// malformed var() functions are rejected.
func ValidDeclarationValue(source string) bool {
	if len(source) > maxCustomPropertyValueBytes || !validDeclarationValueSyntax(source) {
		return false
	}
	return ValidVariableFunctions(source)
}

// ValidCustomPropertyValue reports whether source can be used as the value of
// an unregistered custom property. Empty values are valid.
func ValidCustomPropertyValue(source string) bool {
	return ValidDeclarationValue(source)
}

// CustomPropertyReferences returns every custom-property name referenced by a
// var() function in source. References in nested fallbacks are included in
// source order and duplicates are preserved. The result is false for malformed
// values or when the resolver's depth or reference limits are exceeded.
func CustomPropertyReferences(source string) ([]string, bool) {
	if !ValidDeclarationValue(source) {
		return nil, false
	}
	references := make([]string, 0)
	ok := walkVarReferences(source, 0, func(name string) bool {
		if len(references) >= maxCustomPropertyReferences {
			return false
		}
		references = append(references, name)
		return true
	})
	if !ok {
		return nil, false
	}
	return references, true
}

// ContainsVarFunction reports whether source contains a var() function token
// outside strings and comments. The function need not be syntactically
// complete for this lexical check to return true.
func ContainsVarFunction(source string) bool {
	position := 0
	for position < len(source) {
		start, _, next, found, _ := nextVarFunction(source, position)
		if found {
			return start >= 0
		}
		if next <= position {
			return false
		}
		position = next
	}
	return false
}

type customPropertyState uint8

const (
	customPropertyUnresolved customPropertyState = iota
	customPropertyResolving
	customPropertyResolved
)

type customPropertyResolver struct {
	inherited      CustomProperties
	initial        map[string]bool
	local          map[string]string
	invalid        map[string]bool
	state          map[string]customPropertyState
	resolved       map[string]string
	remainingBytes int
}

func (resolver *customPropertyResolver) resolve(name string, depth int) (string, bool) {
	if depth > maxCustomPropertyDepth || resolver.invalid[name] {
		resolver.invalid[name] = true
		return "", false
	}
	if value, ok := resolver.resolved[name]; ok && resolver.state[name] == customPropertyResolved {
		return value, true
	}
	switch resolver.state[name] {
	case customPropertyResolving:
		resolver.invalid[name] = true
		return "", false
	case customPropertyResolved:
		return "", false
	}

	source, local := resolver.local[name]
	if !local {
		return resolver.lookupInherited(name)
	}

	resolver.state[name] = customPropertyResolving
	value, ok := substituteCustomPropertyValue(source, depth+1, func(reference string, lookupDepth int) (string, bool) {
		if _, isLocal := resolver.local[reference]; isLocal {
			return resolver.resolve(reference, lookupDepth)
		}
		return resolver.lookupInherited(reference)
	})
	resolver.state[name] = customPropertyResolved
	if !ok {
		resolver.invalid[name] = true
		return "", false
	}
	if len(value) > resolver.remainingBytes {
		resolver.invalid[name] = true
		return "", false
	}
	resolver.remainingBytes -= len(value)
	resolver.resolved[name] = value
	return value, true
}

func (resolver *customPropertyResolver) lookupInherited(name string) (string, bool) {
	if resolver.initial[name] {
		return "", false
	}
	return resolver.inherited.lookupCanonical(name)
}

func canonicalCustomPropertyName(source string) (string, bool) {
	if source == "" || !wouldStartCSSIdentifier(source, 0) {
		return "", false
	}
	name, end := consumeCSSIdentifier(source, 0)
	if end != len(source) || len(name) <= 2 || !strings.HasPrefix(name, "--") {
		return "", false
	}
	return name, true
}

func exactCSSWideKeyword(source string) string {
	switch keyword := CSSWideKeyword(source); keyword {
	case "initial", "inherit", "unset":
		return keyword
	default:
		return ""
	}
}

// CSSWideKeyword returns the canonical spelling of a CSS-wide keyword when
// source consists of exactly one identifier token, ignoring surrounding CSS
// whitespace and comments. Escapes in the identifier are decoded.
func CSSWideKeyword(source string) string {
	identifier, ok := parseSingleCSSIdentifier(source)
	if !ok {
		return ""
	}
	for _, keyword := range []string{"initial", "inherit", "unset", "revert", "revert-layer"} {
		if equalASCIIFold(identifier, keyword) {
			return keyword
		}
	}
	return ""
}

func parseSingleCSSIdentifier(source string) (string, bool) {
	position, ok := skipCSSIgnorable(source, 0)
	if !ok || position >= len(source) || !wouldStartCSSIdentifier(source, position) {
		return "", false
	}
	identifier, position := consumeCSSIdentifier(source, position)
	position, ok = skipCSSIgnorable(source, position)
	return identifier, ok && position == len(source)
}

func skipCSSIgnorable(source string, position int) (int, bool) {
	for position < len(source) {
		switch {
		case isCSSWhitespace(source[position]):
			position++
		case startsCSSComment(source, position):
			var closed bool
			position, closed = skipCSSComment(source, position)
			if !closed {
				return len(source), false
			}
		default:
			return position, true
		}
	}
	return position, true
}

func markCyclicCustomProperties(local map[string]string, dependencies map[string][]string, invalid map[string]bool) {
	names := make([]string, 0, len(local))
	for name := range local {
		names = append(names, name)
	}
	sort.Strings(names)

	type dfsFrame struct {
		name string
		next int
	}
	visited := make(map[string]bool, len(local))
	finishOrder := make([]string, 0, len(local))
	for _, root := range names {
		if visited[root] || invalid[root] {
			continue
		}
		visited[root] = true
		stack := []dfsFrame{{name: root}}
		for len(stack) > 0 {
			frame := &stack[len(stack)-1]
			advanced := false
			for frame.next < len(dependencies[frame.name]) {
				dependency := dependencies[frame.name][frame.next]
				frame.next++
				if _, localDependency := local[dependency]; !localDependency || invalid[dependency] || visited[dependency] {
					continue
				}
				visited[dependency] = true
				stack = append(stack, dfsFrame{name: dependency})
				advanced = true
				break
			}
			if advanced {
				continue
			}
			finishOrder = append(finishOrder, frame.name)
			stack = stack[:len(stack)-1]
		}
	}

	reverse := make(map[string][]string, len(local))
	for _, name := range names {
		if invalid[name] {
			continue
		}
		for _, dependency := range dependencies[name] {
			if _, localDependency := local[dependency]; localDependency && !invalid[dependency] {
				reverse[dependency] = append(reverse[dependency], name)
			}
		}
	}

	visited = make(map[string]bool, len(local))
	for index := len(finishOrder) - 1; index >= 0; index-- {
		root := finishOrder[index]
		if visited[root] {
			continue
		}
		component := make([]string, 0, 1)
		stack := []string{root}
		visited[root] = true
		for len(stack) > 0 {
			last := len(stack) - 1
			name := stack[last]
			stack = stack[:last]
			component = append(component, name)
			for _, dependency := range reverse[name] {
				if !visited[dependency] {
					visited[dependency] = true
					stack = append(stack, dependency)
				}
			}
		}

		cyclic := len(component) > 1
		if !cyclic {
			name := component[0]
			for _, dependency := range dependencies[name] {
				if dependency == name {
					cyclic = true
					break
				}
			}
		}
		if cyclic {
			for _, name := range component {
				invalid[name] = true
			}
		}
	}
}

func walkVarReferences(source string, depth int, visit func(string) bool) bool {
	if depth > maxCustomPropertyDepth || len(source) > maxCustomPropertyValueBytes {
		return false
	}
	for position := 0; position < len(source); {
		_, open, next, found, valid := nextVarFunction(source, position)
		if !valid {
			return false
		}
		if !found {
			return true
		}
		invocation, ok := parseVarInvocation(source, open)
		if !ok || !visit(invocation.name) {
			return false
		}
		if invocation.hasFallback && !walkVarReferences(invocation.fallback, depth+1, visit) {
			return false
		}
		position = invocation.end
		if position <= next {
			position = next
		}
	}
	return true
}

type varInvocation struct {
	name        string
	fallback    string
	hasFallback bool
	end         int
}

func parseVarInvocation(source string, open int) (varInvocation, bool) {
	if open >= len(source) || source[open] != '(' {
		return varInvocation{}, false
	}
	stack := []byte{'('}
	comma := -1
	for position := open + 1; position < len(source); {
		character := source[position]
		switch {
		case startsCSSComment(source, position):
			next, closed := skipCSSComment(source, position)
			if !closed {
				return varInvocation{}, false
			}
			position = next
			continue
		case character == '\'' || character == '"':
			next, closed := skipCSSString(source, position)
			if !closed {
				return varInvocation{}, false
			}
			position = next
			continue
		case character == '\\':
			position = skipCSSEscape(source, position)
			continue
		}

		switch character {
		case '(', '[', '{':
			if len(stack) >= maxCustomPropertyDepth {
				return varInvocation{}, false
			}
			stack = append(stack, character)
		case ')', ']', '}':
			if len(stack) == 0 || !matchingCSSBlock(stack[len(stack)-1], character) {
				return varInvocation{}, false
			}
			stack = stack[:len(stack)-1]
			if len(stack) == 0 {
				return finishVarInvocation(source, open, comma, position)
			}
		case ',':
			if len(stack) == 1 && comma < 0 {
				comma = position
			}
		}
		position++
	}
	// CSS component-value parsing implicitly closes functions and simple blocks
	// at EOF. Preserve that behavior for style-attribute values ending in var().
	return finishVarInvocation(source, open, comma, len(source))
}

func parseVarName(source string) (string, bool) {
	start, ok := skipCSSIgnorable(source, 0)
	if !ok || start >= len(source) || !wouldStartCSSIdentifier(source, start) {
		return "", false
	}
	name, end := consumeCSSIdentifier(source, start)
	end, ok = skipCSSIgnorable(source, end)
	if !ok || end != len(source) || len(name) <= 2 || !strings.HasPrefix(name, "--") {
		return "", false
	}
	return name, true
}

func finishVarInvocation(source string, open, comma, close int) (varInvocation, bool) {
	nameEnd := close
	if comma >= 0 {
		nameEnd = comma
	}
	name, ok := parseVarName(source[open+1 : nameEnd])
	if !ok {
		return varInvocation{}, false
	}
	invocation := varInvocation{name: name, end: close}
	if close < len(source) {
		invocation.end++
	}
	if comma >= 0 {
		invocation.hasFallback = true
		invocation.fallback = source[comma+1 : close]
	}
	return invocation, true
}

func matchingCSSBlock(open, close byte) bool {
	return open == '(' && close == ')' || open == '[' && close == ']' || open == '{' && close == '}'
}

func substituteCustomPropertyValue(
	source string,
	depth int,
	lookup func(name string, depth int) (string, bool),
) (string, bool) {
	if depth > maxCustomPropertyDepth || len(source) > maxCustomPropertyValueBytes {
		return "", false
	}

	var output strings.Builder
	segmentStart := 0
	for position := 0; position < len(source); {
		start, open, _, foundVar, valid := nextVarFunction(source, position)
		if !valid {
			return "", false
		}
		if !foundVar {
			break
		}
		invocation, ok := parseVarInvocation(source, open)
		if !ok || !writeLimitedPiece(&output, source[segmentStart:start]) {
			return "", false
		}

		replacement, found := lookup(invocation.name, depth+1)
		if !found {
			if !invocation.hasFallback {
				return "", false
			}
			replacement, ok = substituteCustomPropertyValue(invocation.fallback, depth+1, lookup)
			if !ok {
				return "", false
			}
		}
		if !writeLimitedPiece(&output, replacement) {
			return "", false
		}
		position = invocation.end
		segmentStart = position
	}
	if segmentStart == 0 {
		return source, true
	}
	if !writeLimitedPiece(&output, source[segmentStart:]) {
		return "", false
	}
	return output.String(), true
}

func writeLimitedPiece(builder *strings.Builder, value string) bool {
	separator := ""
	if builder.Len() > 0 && value != "" && cssTokensCouldFuse(builder.String(), value) {
		separator = " "
	}
	if len(separator)+len(value) > maxCustomPropertyValueBytes-builder.Len() {
		return false
	}
	builder.WriteString(separator)
	builder.WriteString(value)
	return true
}

func cssTokensCouldFuse(left, right string) bool {
	if left == "" || right == "" {
		return false
	}
	escapedEnd := endsWithCSSEscape(left)
	if isCSSWhitespace(left[len(left)-1]) && !escapedEnd || isCSSWhitespace(right[0]) ||
		strings.HasSuffix(left, "*/") || strings.HasPrefix(right, "/*") {
		return false
	}
	last := left[len(left)-1]
	first := right[0]
	if last == '\\' && endsWithRawCSSBackslash(left) {
		// A trailing backslash is a delimiter token at EOF, but would escape the
		// first code point of an adjacent substitution segment.
		return true
	}
	lastName := isRawCSSNameByte(last)
	firstName := isRawCSSNameByte(first)
	if escapedEnd && (firstName || first == '(') {
		return true
	}
	if lastName && (firstName || first == '(' || first == '%' || first == '+' || first == '.') {
		return true
	}
	if (last == '#' || last == '@' || last == '+' || last == '.' || last == '-') && firstName {
		return true
	}
	if (last == '+' || last == '-' || last == '.') && (isASCIIDigit(first) || first == '.') {
		return true
	}
	if isASCIIDigit(last) && (first == '.' || first == '%') {
		return true
	}
	if last == '/' && first == '*' {
		return true
	}
	if last == '<' && strings.HasPrefix(right, "!--") {
		return true
	}
	if strings.HasSuffix(left, "<!") && strings.HasPrefix(right, "--") {
		return true
	}
	return strings.HasSuffix(left, "--") && first == '>'
}

func endsWithRawCSSBackslash(source string) bool {
	count := 0
	for position := len(source) - 1; position >= 0 && source[position] == '\\'; position-- {
		count++
	}
	return count%2 != 0
}

func endsWithCSSEscape(source string) bool {
	start := max(0, len(source)-10)
	for position := start; position < len(source); position++ {
		if source[position] != '\\' || !validCSSEscapeAt(source, position) {
			continue
		}
		_, end := consumeCSSEscapedRune(source, position)
		if end == len(source) {
			return true
		}
	}
	return false
}

func isRawCSSNameByte(character byte) bool {
	return character == '-' || character == '_' || character == '\\' || isASCIIAlpha(character) ||
		isASCIIDigit(character) || character >= utf8.RuneSelf
}

func validDeclarationValueSyntax(source string) bool {
	stack := make([]byte, 0, 8)
	for position := 0; position < len(source); {
		character := source[position]
		switch {
		case strings.HasPrefix(source[position:], "<!--"):
			position += 4
			continue
		case strings.HasPrefix(source[position:], "-->"):
			position += 3
			continue
		case startsCSSComment(source, position):
			next, closed := skipCSSComment(source, position)
			if !closed {
				return false
			}
			position = next
			continue
		case character == '\'' || character == '"':
			next, closed := skipCSSString(source, position)
			if !closed {
				return false
			}
			position = next
			continue
		case len(stack) == 0 && (character == ';' || character == '!'):
			return false
		case character == '#' && wouldStartCSSName(source, position+1):
			_, position = consumeCSSIdentifier(source, position+1)
			continue
		case character == '@' && wouldStartCSSIdentifier(source, position+1):
			_, position = consumeCSSIdentifier(source, position+1)
			continue
		case wouldStartCSSNumber(source, position):
			position = consumeCSSNumericToken(source, position)
			continue
		case wouldStartCSSIdentifier(source, position):
			identifier, end := consumeCSSIdentifier(source, position)
			if end < len(source) && source[end] == '(' {
				if equalASCIIFold(identifier, "url") {
					if isUnquotedCSSURL(source, end) {
						var ok bool
						position, ok = consumeCSSURLToken(source, end)
						if !ok {
							return false
						}
						continue
					}
				}
				if len(stack) >= maxCustomPropertyDepth {
					return false
				}
				stack = append(stack, '(')
				position = end + 1
				continue
			}
			position = end
			continue
		}

		switch character {
		case '(', '[', '{':
			if len(stack) >= maxCustomPropertyDepth {
				return false
			}
			stack = append(stack, character)
		case ')', ']', '}':
			if len(stack) == 0 || !matchingCSSBlock(stack[len(stack)-1], character) {
				return false
			}
			stack = stack[:len(stack)-1]
		case '\\':
			if validCSSEscapeAt(source, position) {
				position = skipCSSEscape(source, position)
				continue
			}
		}
		position++
	}
	// CSS component-value parsing implicitly closes functions and simple blocks
	// at EOF, so unmatched open blocks do not make a declaration value invalid.
	return true
}

// nextVarFunction scans CSS tokens beginning at position. start and open
// identify a decoded var function token when found. next is always a safe
// position from which to continue scanning when no function is found.
func nextVarFunction(source string, position int) (start, open, next int, found, valid bool) {
	for position < len(source) {
		switch {
		case strings.HasPrefix(source[position:], "<!--"):
			position += 4
		case strings.HasPrefix(source[position:], "-->"):
			position += 3
		case startsCSSComment(source, position):
			var closed bool
			position, closed = skipCSSComment(source, position)
			if !closed {
				return -1, -1, len(source), false, false
			}
		case source[position] == '\'' || source[position] == '"':
			var closed bool
			position, closed = skipCSSString(source, position)
			if !closed {
				return -1, -1, len(source), false, false
			}
		case source[position] == '#' && wouldStartCSSName(source, position+1):
			_, position = consumeCSSIdentifier(source, position+1)
		case source[position] == '@' && wouldStartCSSIdentifier(source, position+1):
			_, position = consumeCSSIdentifier(source, position+1)
		case wouldStartCSSNumber(source, position):
			position = consumeCSSNumericToken(source, position)
		case wouldStartCSSIdentifier(source, position):
			tokenStart := position
			identifier, end := consumeCSSIdentifier(source, position)
			if end < len(source) && source[end] == '(' {
				if equalASCIIFold(identifier, "var") {
					return tokenStart, end, end + 1, true, true
				}
				if equalASCIIFold(identifier, "url") {
					if isUnquotedCSSURL(source, end) {
						var ok bool
						position, ok = consumeCSSURLToken(source, end)
						if !ok {
							return -1, -1, len(source), false, false
						}
						continue
					}
				}
				position = end + 1
				continue
			}
			position = end
		case source[position] == '\\':
			position = skipCSSEscape(source, position)
		default:
			position++
		}
	}
	return -1, -1, len(source), false, true
}

func wouldStartCSSIdentifier(source string, position int) bool {
	if position >= len(source) {
		return false
	}
	character, width := utf8.DecodeRuneInString(source[position:])
	if isCSSNameStartRune(character) {
		return true
	}
	if character == '\\' {
		return validCSSEscapeAt(source, position)
	}
	if character != '-' {
		return false
	}
	next := position + width
	if next >= len(source) {
		return false
	}
	nextCharacter, _ := utf8.DecodeRuneInString(source[next:])
	return nextCharacter == '-' || isCSSNameStartRune(nextCharacter) ||
		nextCharacter == '\\' && validCSSEscapeAt(source, next)
}

func wouldStartCSSName(source string, position int) bool {
	if position >= len(source) {
		return false
	}
	character, _ := utf8.DecodeRuneInString(source[position:])
	return isCSSNameRune(character) || character == '\\' && validCSSEscapeAt(source, position)
}

func wouldStartCSSNumber(source string, position int) bool {
	if position >= len(source) {
		return false
	}
	switch source[position] {
	case '+', '-':
		position++
		if position < len(source) && isASCIIDigit(source[position]) {
			return true
		}
		return position+1 < len(source) && source[position] == '.' && isASCIIDigit(source[position+1])
	case '.':
		return position+1 < len(source) && isASCIIDigit(source[position+1])
	default:
		return isASCIIDigit(source[position])
	}
}

func consumeCSSNumericToken(source string, position int) int {
	if position < len(source) && (source[position] == '+' || source[position] == '-') {
		position++
	}
	for position < len(source) && isASCIIDigit(source[position]) {
		position++
	}
	if position+1 < len(source) && source[position] == '.' && isASCIIDigit(source[position+1]) {
		position += 2
		for position < len(source) && isASCIIDigit(source[position]) {
			position++
		}
	}
	if position < len(source) && (source[position] == 'e' || source[position] == 'E') {
		exponent := position + 1
		if exponent < len(source) && (source[exponent] == '+' || source[exponent] == '-') {
			exponent++
		}
		if exponent < len(source) && isASCIIDigit(source[exponent]) {
			position = exponent + 1
			for position < len(source) && isASCIIDigit(source[position]) {
				position++
			}
		}
	}
	if wouldStartCSSIdentifier(source, position) {
		_, position = consumeCSSIdentifier(source, position)
	} else if position < len(source) && source[position] == '%' {
		position++
	}
	return position
}

func consumeCSSIdentifier(source string, position int) (string, int) {
	start := position
	segmentStart := position
	escaped := false
	var decoded strings.Builder
	for position < len(source) {
		character, width := utf8.DecodeRuneInString(source[position:])
		if isCSSNameRune(character) {
			position += width
			continue
		}
		if character != '\\' || !validCSSEscapeAt(source, position) {
			break
		}
		if !escaped {
			decoded.Grow(len(source) - start)
			escaped = true
		}
		decoded.WriteString(source[segmentStart:position])
		decodedRune, next := consumeCSSEscapedRune(source, position)
		decoded.WriteRune(decodedRune)
		position = next
		segmentStart = position
	}
	if !escaped {
		return source[start:position], position
	}
	decoded.WriteString(source[segmentStart:position])
	return decoded.String(), position
}

func isCSSNameStartRune(character rune) bool {
	return character == '_' || character >= utf8.RuneSelf ||
		character >= 'a' && character <= 'z' || character >= 'A' && character <= 'Z'
}

func isCSSNameRune(character rune) bool {
	return isCSSNameStartRune(character) || character == '-' || character >= '0' && character <= '9'
}

func validCSSEscapeAt(source string, position int) bool {
	if position < 0 || position >= len(source) || source[position] != '\\' || position+1 >= len(source) {
		return false
	}
	next := source[position+1]
	return next != '\n' && next != '\r' && next != '\f'
}

func consumeCSSEscapedRune(source string, position int) (rune, int) {
	position++
	if position >= len(source) {
		return utf8.RuneError, position
	}
	if isHexDigit(source[position]) {
		value := rune(0)
		count := 0
		for position < len(source) && count < 6 && isHexDigit(source[position]) {
			value = value*16 + rune(cssHexDigitValue(source[position]))
			position++
			count++
		}
		if position < len(source) && isCSSWhitespace(source[position]) {
			if source[position] == '\r' && position+1 < len(source) && source[position+1] == '\n' {
				position += 2
			} else {
				position++
			}
		}
		if value == 0 || value > utf8.MaxRune || value >= 0xd800 && value <= 0xdfff {
			value = utf8.RuneError
		}
		return value, position
	}
	character, width := utf8.DecodeRuneInString(source[position:])
	return character, position + width
}

func cssHexDigitValue(character byte) int {
	switch {
	case character >= '0' && character <= '9':
		return int(character - '0')
	case character >= 'a' && character <= 'f':
		return int(character-'a') + 10
	default:
		return int(character-'A') + 10
	}
}

func isASCIIAlpha(character byte) bool {
	return character >= 'a' && character <= 'z' || character >= 'A' && character <= 'Z'
}

func isASCIIDigit(character byte) bool {
	return character >= '0' && character <= '9'
}

func isUnquotedCSSURL(source string, open int) bool {
	position := open + 1
	for position < len(source) && isCSSWhitespace(source[position]) {
		position++
	}
	return position >= len(source) || source[position] != '\'' && source[position] != '"'
}

func consumeCSSURLToken(source string, open int) (int, bool) {
	position := open + 1
	for position < len(source) && isCSSWhitespace(source[position]) {
		position++
	}
	for position < len(source) {
		character := source[position]
		switch {
		case character == ')':
			return position + 1, true
		case isCSSWhitespace(character):
			for position < len(source) && isCSSWhitespace(source[position]) {
				position++
			}
			if position >= len(source) {
				return position, true
			}
			if source[position] != ')' {
				return len(source), false
			}
			return position + 1, true
		case character == '\'' || character == '"' || character == '(' || isCSSNonPrintable(character):
			return len(source), false
		case character == '\\':
			if !validCSSEscapeAt(source, position) {
				return len(source), false
			}
			position = skipCSSEscape(source, position)
		default:
			_, width := utf8.DecodeRuneInString(source[position:])
			position += width
		}
	}
	// EOF implicitly closes an otherwise valid URL token.
	return position, true
}

func isCSSNonPrintable(character byte) bool {
	return character <= 0x08 || character == 0x0b || character >= 0x0e && character <= 0x1f || character == 0x7f
}

func startsCSSComment(source string, position int) bool {
	return position >= 0 && position+1 < len(source) && source[position] == '/' && source[position+1] == '*'
}

func skipCSSComment(source string, position int) (int, bool) {
	end := strings.Index(source[position+2:], "*/")
	if end < 0 {
		return len(source), false
	}
	return position + end + 4, true
}

func skipCSSString(source string, position int) (int, bool) {
	quote := source[position]
	position++
	for position < len(source) {
		switch source[position] {
		case quote:
			return position + 1, true
		case '\\':
			position = skipCSSEscape(source, position)
		case '\n', '\r', '\f':
			return position, false
		default:
			position++
		}
	}
	return len(source), false
}

func skipCSSEscape(source string, position int) int {
	position++
	if position >= len(source) {
		return position
	}
	if source[position] == '\r' && position+1 < len(source) && source[position+1] == '\n' {
		return position + 2
	}
	if source[position] == '\n' || source[position] == '\r' || source[position] == '\f' {
		return position + 1
	}
	if isHexDigit(source[position]) {
		count := 0
		for position < len(source) && count < 6 && isHexDigit(source[position]) {
			position++
			count++
		}
		if position < len(source) && isCSSWhitespace(source[position]) {
			if source[position] == '\r' && position+1 < len(source) && source[position+1] == '\n' {
				return position + 2
			}
			return position + 1
		}
		return position
	}
	return position + 1
}
