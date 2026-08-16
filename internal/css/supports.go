package css

// SupportsConditionMatches evaluates the @supports condition subset against
// the supplied declaration capability callback. Declaration grammar remains
// owned by the style engine while the CSS package owns condition tokenization,
// logical grouping, and selector() syntax.
func SupportsConditionMatches(source string, supportsDeclaration func(Declaration) bool) bool {
	values, err := ParseComponentValues(source)
	if err != nil {
		return false
	}
	matched, valid := supportsConditionParser{
		source:              source,
		values:              trimComponentWhitespace(values),
		supportsDeclaration: supportsDeclaration,
	}.parse()
	return valid && matched
}

// SupportsImportConditionMatches evaluates the grammar accepted inside an
// @import supports() function. Unlike an @supports prelude, this form also
// accepts an unwrapped declaration such as supports(display: grid).
func SupportsImportConditionMatches(source string, supportsDeclaration func(Declaration) bool) bool {
	values, err := ParseComponentValues(source)
	if err != nil {
		return false
	}
	values = trimComponentWhitespace(values)
	if declaration, ok := supportsDeclarationFromComponents(source, values); ok {
		return supportsDeclaration != nil && supportsDeclaration(declaration)
	}
	matched, valid := supportsConditionParser{
		source:              source,
		values:              values,
		supportsDeclaration: supportsDeclaration,
	}.parse()
	return valid && matched
}

// MatchesSupports reports whether every enclosing @supports group associated
// with the rule matches. Nested group rules form an implicit conjunction.
func (rule Rule) MatchesSupports(supportsDeclaration func(Declaration) bool) bool {
	for _, condition := range rule.Supports {
		if !SupportsConditionMatches(condition, supportsDeclaration) {
			return false
		}
	}
	return true
}

type supportsConditionParser struct {
	source              string
	values              []ComponentValue
	position            int
	supportsDeclaration func(Declaration) bool
}

func (parser supportsConditionParser) parse() (bool, bool) {
	parser.skipWhitespace()
	if parser.position >= len(parser.values) {
		return false, false
	}
	if parser.consumeIdent("not") {
		matched, valid := parser.parseInParens()
		parser.skipWhitespace()
		valid = valid && parser.position == len(parser.values)
		return !matched && valid, valid
	}

	matched, valid := parser.parseInParens()
	if !valid {
		return false, false
	}
	operator := ""
	for {
		parser.skipWhitespace()
		if parser.position == len(parser.values) {
			return matched, true
		}
		current := parser.identAtPosition()
		if current != "and" && current != "or" {
			return false, false
		}
		if operator != "" && operator != current {
			return false, false
		}
		operator = current
		parser.position++
		right, rightValid := parser.parseInParens()
		if !rightValid {
			return false, false
		}
		if operator == "and" {
			matched = matched && right
		} else {
			matched = matched || right
		}
	}
}

func (parser *supportsConditionParser) parseInParens() (bool, bool) {
	parser.skipWhitespace()
	if parser.position >= len(parser.values) {
		return false, false
	}
	value := parser.values[parser.position]
	parser.position++
	switch {
	case value.Kind == ComponentFunction && equalASCIIFold(value.Token.Value, "selector"):
		selectorSource, ok := componentSource(parser.source, value.Values)
		if !ok {
			return false, true
		}
		_, err := ParseSelectorList(selectorSource)
		return err == nil, true
	case value.Kind == ComponentFunction:
		// Unknown future feature functions are valid general-enclosed syntax,
		// but are unsupported by this engine.
		return false, true
	case value.Kind == ComponentBlock && value.Token.Kind == TokenOpenParen:
		inner := trimComponentWhitespace(value.Values)
		if declaration, ok := supportsDeclarationFromComponents(parser.source, inner); ok {
			return parser.supportsDeclaration != nil && parser.supportsDeclaration(declaration), true
		}
		matched, valid := supportsConditionParser{
			source:              parser.source,
			values:              inner,
			supportsDeclaration: parser.supportsDeclaration,
		}.parse()
		if valid {
			return matched, true
		}
		if supportsGeneralEnclosed(inner) {
			return false, true
		}
	}
	return false, false
}

func (parser *supportsConditionParser) skipWhitespace() {
	for parser.position < len(parser.values) && isWhitespaceComponent(parser.values[parser.position]) {
		parser.position++
	}
}

func (parser *supportsConditionParser) consumeIdent(expected string) bool {
	parser.skipWhitespace()
	if parser.identAtPosition() != expected {
		return false
	}
	parser.position++
	return true
}

func (parser *supportsConditionParser) identAtPosition() string {
	if parser.position >= len(parser.values) {
		return ""
	}
	value := parser.values[parser.position]
	if value.Kind != ComponentToken || value.Token.Kind != TokenIdent {
		return ""
	}
	if equalASCIIFold(value.Token.Value, "not") {
		return "not"
	}
	if equalASCIIFold(value.Token.Value, "and") {
		return "and"
	}
	if equalASCIIFold(value.Token.Value, "or") {
		return "or"
	}
	return ""
}

func supportsDeclarationFromComponents(source string, values []ComponentValue) (Declaration, bool) {
	values = trimComponentWhitespace(values)
	if len(values) == 0 {
		return Declaration{}, false
	}
	hasColon := false
	for _, value := range values {
		if value.Kind != ComponentToken {
			continue
		}
		switch value.Token.Kind {
		case TokenColon:
			hasColon = true
		case TokenSemicolon:
			return Declaration{}, false
		}
	}
	if !hasColon {
		return Declaration{}, false
	}
	declarationSource, ok := componentSource(source, values)
	if !ok {
		return Declaration{}, false
	}
	declarations, err := ParseRawDeclarationList(declarationSource)
	if err != nil || len(declarations) != 1 || declarations[0].Important {
		return Declaration{}, false
	}
	return declarations[0], true
}

func componentSource(source string, values []ComponentValue) (string, bool) {
	values = trimComponentWhitespace(values)
	if len(values) == 0 {
		return "", false
	}
	start := values[0].Span.Start
	end := values[len(values)-1].Span.End
	if start < 0 || end < start || end > len(source) {
		return "", false
	}
	return source[start:end], true
}

func supportsGeneralEnclosed(values []ComponentValue) bool {
	values = trimComponentWhitespace(values)
	if len(values) == 0 {
		return false
	}
	first := values[0]
	return first.Kind == ComponentFunction ||
		(first.Kind == ComponentToken && first.Token.Kind == TokenIdent)
}
