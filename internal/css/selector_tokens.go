package css

import (
	"strconv"
	"strings"
)

// parseTokenSelectorListAtDepth parses complex selectors from the shared CSS
// component-value representation. Matching and specificity continue to use the
// existing private selector AST.
func parseTokenSelectorListAtDepth(source string, nesting int) ([]Selector, bool) {
	if nesting > maxSelectorNesting {
		return nil, false
	}
	values, err := ParseComponentValues(source)
	if err != nil {
		return nil, false
	}
	return parseTokenSelectorComponents(source, values, nesting, false)
}

func parseTokenSelectorComponents(source string, values []ComponentValue, nesting int, forgiving bool) ([]Selector, bool) {
	return parseTokenSelectorComponentsWithContext(source, values, nesting, forgiving, false)
}

func parseTokenSelectorComponentsWithContext(source string, values []ComponentValue, nesting int, forgiving, insideHas bool) ([]Selector, bool) {
	if nesting > maxSelectorNesting {
		return nil, false
	}
	groups := splitSelectorComponentGroups(values)
	selectors := make([]Selector, 0, len(groups))
	for _, group := range groups {
		group = trimComponentWhitespace(group)
		if len(group) == 0 {
			if forgiving {
				continue
			}
			return nil, false
		}
		parser := tokenSelectorParser{source: source, values: group, nesting: nesting, insideHas: insideHas}
		selector, ok := parser.parseComplexSelector()
		parser.skipWhitespace()
		if !ok || !parser.done() {
			if forgiving {
				continue
			}
			return nil, false
		}
		selectors = append(selectors, selector)
	}
	if len(selectors) == 0 && !forgiving {
		return nil, false
	}
	return selectors, true
}

func parseTokenRelativeSelectorComponents(source string, values []ComponentValue, nesting int) ([]Selector, bool) {
	if nesting > maxSelectorNesting {
		return nil, false
	}
	groups := splitSelectorComponentGroups(values)
	selectors := make([]Selector, 0, len(groups))
	for _, group := range groups {
		group = trimComponentWhitespace(group)
		if len(group) == 0 {
			return nil, false
		}
		parser := tokenSelectorParser{source: source, values: group, nesting: nesting, insideHas: true}
		selector, ok := parser.parseRelativeSelector()
		parser.skipWhitespace()
		if !ok || !parser.done() {
			return nil, false
		}
		selectors = append(selectors, selector)
	}
	return selectors, len(selectors) != 0
}

func splitSelectorComponentGroups(values []ComponentValue) [][]ComponentValue {
	groups := make([][]ComponentValue, 0, 1)
	start := 0
	for index, value := range values {
		if value.Kind == ComponentToken && value.Token.Kind == TokenComma {
			groups = append(groups, values[start:index])
			start = index + 1
		}
	}
	return append(groups, values[start:])
}

type tokenSelectorParser struct {
	source    string
	values    []ComponentValue
	pos       int
	nesting   int
	insideHas bool
}

func (parser *tokenSelectorParser) parseRelativeSelector() (Selector, bool) {
	parser.skipWhitespace()
	leading := descendantCombinator
	switch {
	case parser.peekDelim(">"):
		leading = childCombinator
		parser.pos++
	case parser.peekDelim("+"):
		leading = adjacentSiblingCombinator
		parser.pos++
	case parser.peekDelim("~"):
		leading = generalSiblingCombinator
		parser.pos++
	}
	parser.skipWhitespace()
	if parser.done() {
		return Selector{}, false
	}
	selector, ok := parser.parseComplexSelector()
	if !ok {
		return Selector{}, false
	}
	selector.leading = leading
	return selector, true
}

func (parser *tokenSelectorParser) parseComplexSelector() (Selector, bool) {
	parser.skipWhitespace()
	first, specificity, ok := parser.parseCompoundSelector()
	if !ok {
		return Selector{}, false
	}
	selector := Selector{compounds: []compoundSelector{first}, specificity: specificity}
	for {
		hadWhitespace := parser.skipWhitespace()
		if parser.done() {
			return selector, true
		}

		var combinator selectorCombinator
		switch {
		case parser.peekDelim(">"):
			combinator = childCombinator
			parser.pos++
			parser.skipWhitespace()
		case parser.peekDelim("+"):
			combinator = adjacentSiblingCombinator
			parser.pos++
			parser.skipWhitespace()
		case parser.peekDelim("~"):
			combinator = generalSiblingCombinator
			parser.pos++
			parser.skipWhitespace()
		default:
			if !hadWhitespace {
				return Selector{}, false
			}
			combinator = descendantCombinator
		}
		if parser.done() || parser.peekDelim(">") || parser.peekDelim("+") || parser.peekDelim("~") {
			return Selector{}, false
		}
		compound, compoundSpecificity, ok := parser.parseCompoundSelector()
		if !ok {
			return Selector{}, false
		}
		selector.compounds = append(selector.compounds, compound)
		selector.combinators = append(selector.combinators, combinator)
		selector.specificity = selector.specificity.add(compoundSpecificity)
	}
}

func (parser *tokenSelectorParser) parseCompoundSelector() (compoundSelector, Specificity, bool) {
	var compound compoundSelector
	var specificity Specificity
	found := false

	if parser.peekDelim("*") {
		compound.typeName = "*"
		parser.pos++
		found = true
	} else if token, ok := parser.peekToken(TokenIdent); ok {
		compound.typeName = lowerASCII(token.Value)
		parser.pos++
		specificity.Types++
		found = true
	}

	for !parser.done() {
		value := parser.values[parser.pos]
		switch {
		case value.Kind == ComponentToken && value.Token.Kind == TokenHash:
			if !value.Token.Identifier {
				return compoundSelector{}, Specificity{}, false
			}
			compound.ids = append(compound.ids, value.Token.Value)
			parser.pos++
			specificity.IDs++
			found = true
		case parser.peekDelim("."):
			parser.pos++
			identifier, ok := parser.peekToken(TokenIdent)
			if !ok {
				return compoundSelector{}, Specificity{}, false
			}
			compound.classes = append(compound.classes, identifier.Value)
			parser.pos++
			specificity.Classes++
			found = true
		case value.Kind == ComponentBlock && value.Token.Kind == TokenOpenSquare:
			attribute, ok := parseTokenAttributeSelector(value.Values)
			if !ok {
				return compoundSelector{}, Specificity{}, false
			}
			compound.attributes = append(compound.attributes, attribute)
			parser.pos++
			specificity.Classes++
			found = true
		case value.Kind == ComponentToken && value.Token.Kind == TokenColon:
			parser.pos++
			pseudo, pseudoSpecificity, ok := parser.parsePseudoClass()
			if !ok {
				return compoundSelector{}, Specificity{}, false
			}
			compound.pseudos = append(compound.pseudos, pseudo)
			specificity = specificity.add(pseudoSpecificity)
			found = true
		default:
			return compound, specificity, found
		}
	}
	return compound, specificity, found
}

func parseTokenAttributeSelector(values []ComponentValue) (attributeSelector, bool) {
	parser := tokenSelectorParser{values: trimComponentWhitespace(values)}
	name, ok := parser.peekToken(TokenIdent)
	if !ok {
		return attributeSelector{}, false
	}
	parser.pos++
	attribute := attributeSelector{name: lowerASCII(name.Value)}
	parser.skipWhitespace()
	if parser.done() {
		return attribute, true
	}

	switch {
	case parser.peekDelim("="):
		attribute.operator = attributeEquals
		parser.pos++
	case parser.peekDelim("~"), parser.peekDelim("|"), parser.peekDelim("^"), parser.peekDelim("$"), parser.peekDelim("*"):
		operator := parser.values[parser.pos].Token.Value
		parser.pos++
		if !parser.peekDelim("=") {
			return attributeSelector{}, false
		}
		parser.pos++
		switch operator {
		case "~":
			attribute.operator = attributeIncludes
		case "|":
			attribute.operator = attributeDashMatch
		case "^":
			attribute.operator = attributePrefix
		case "$":
			attribute.operator = attributeSuffix
		case "*":
			attribute.operator = attributeSubstring
		}
	default:
		return attributeSelector{}, false
	}

	parser.skipWhitespace()
	if parser.done() {
		return attributeSelector{}, false
	}
	value := parser.values[parser.pos]
	if value.Kind != ComponentToken || value.Token.Kind != TokenIdent && value.Token.Kind != TokenString || value.Token.Incomplete {
		return attributeSelector{}, false
	}
	attribute.value = value.Token.Value
	parser.pos++
	parser.skipWhitespace()
	if parser.done() {
		return attribute, true
	}
	flag, ok := parser.peekToken(TokenIdent)
	if !ok {
		return attributeSelector{}, false
	}
	switch lowerASCII(flag.Value) {
	case "i":
		attribute.valueCase = attributeCaseInsensitive
	case "s":
		attribute.valueCase = attributeCaseSensitive
	default:
		return attributeSelector{}, false
	}
	parser.pos++
	parser.skipWhitespace()
	return attribute, parser.done()
}

func (parser *tokenSelectorParser) parsePseudoClass() (pseudoClassSelector, Specificity, bool) {
	if parser.done() {
		return pseudoClassSelector{}, Specificity{}, false
	}
	value := parser.values[parser.pos]
	if value.Kind == ComponentToken && value.Token.Kind == TokenColon {
		return pseudoClassSelector{}, Specificity{}, false
	}
	if value.Kind == ComponentToken && value.Token.Kind == TokenIdent {
		name := lowerASCII(value.Token.Value)
		if !supportedSimplePseudoClass(name) {
			return pseudoClassSelector{}, Specificity{}, false
		}
		parser.pos++
		return pseudoClassSelector{name: name}, Specificity{Classes: 1}, true
	}
	if value.Kind != ComponentFunction {
		return pseudoClassSelector{}, Specificity{}, false
	}
	parser.pos++
	name := lowerASCII(value.Token.Value)
	pseudo := pseudoClassSelector{name: name}
	switch name {
	case "is", "where":
		var ok bool
		pseudo.selectors, ok = parseTokenSelectorComponentsWithContext(parser.source, value.Values, parser.nesting+1, true, parser.insideHas)
		if !ok {
			return pseudoClassSelector{}, Specificity{}, false
		}
		if name == "where" {
			return pseudo, Specificity{}, true
		}
		return pseudo, greatestSpecificity(pseudo.selectors), true
	case "not":
		var ok bool
		pseudo.selectors, ok = parseTokenSelectorComponentsWithContext(parser.source, value.Values, parser.nesting+1, false, parser.insideHas)
		if !ok {
			return pseudoClassSelector{}, Specificity{}, false
		}
		return pseudo, greatestSpecificity(pseudo.selectors), true
	case "has":
		if parser.insideHas {
			return pseudoClassSelector{}, Specificity{}, false
		}
		var ok bool
		pseudo.selectors, ok = parseTokenRelativeSelectorComponents(parser.source, value.Values, parser.nesting+1)
		if !ok {
			return pseudoClassSelector{}, Specificity{}, false
		}
		return pseudo, greatestSpecificity(pseudo.selectors), true
	case "lang":
		var ok bool
		pseudo.arguments, ok = parseLanguageRanges(value.Values)
		if !ok {
			return pseudoClassSelector{}, Specificity{}, false
		}
		return pseudo, Specificity{Classes: 1}, true
	case "dir":
		argument := trimComponentWhitespace(value.Values)
		if len(argument) != 1 || argument[0].Kind != ComponentToken ||
			argument[0].Token.Kind != TokenIdent || argument[0].Token.Incomplete {
			return pseudoClassSelector{}, Specificity{}, false
		}
		pseudo.arguments = []string{lowerASCII(argument[0].Token.Value)}
		return pseudo, Specificity{Classes: 1}, true
	case "nth-child", "nth-last-child", "nth-of-type", "nth-last-of-type":
		allowOf := name == "nth-child" || name == "nth-last-child"
		var ok bool
		pseudo.nth, pseudo.selectors, ok = parseTokenNthArgument(parser.source, value.Values, allowOf, parser.nesting+1, parser.insideHas)
		if !ok {
			return pseudoClassSelector{}, Specificity{}, false
		}
		if allowOf {
			return pseudo, (Specificity{Classes: 1}).add(greatestSpecificity(pseudo.selectors)), true
		}
		return pseudo, Specificity{Classes: 1}, true
	default:
		return pseudoClassSelector{}, Specificity{}, false
	}
}

func parseLanguageRanges(values []ComponentValue) ([]string, bool) {
	groups := splitSelectorComponentGroups(values)
	ranges := make([]string, 0, len(groups))
	for _, group := range groups {
		group = trimComponentWhitespace(group)
		if len(group) != 1 || group[0].Kind != ComponentToken {
			return nil, false
		}
		token := group[0].Token
		if token.Incomplete || token.Kind != TokenIdent && token.Kind != TokenString {
			return nil, false
		}
		ranges = append(ranges, token.Value)
	}
	return ranges, len(ranges) != 0
}

func parseTokenNthArgument(source string, values []ComponentValue, allowOf bool, nesting int, insideHas bool) (nthExpression, []Selector, bool) {
	values = trimComponentWhitespace(values)
	if len(values) == 0 {
		return nthExpression{}, nil, false
	}
	ofIndex := -1
	for index, value := range values {
		if index == 0 || value.Kind != ComponentToken || value.Token.Kind != TokenIdent || !equalASCIIFold(value.Token.Value, "of") {
			continue
		}
		ofIndex = index
		break
	}
	formulaValues := values
	var selectorValues []ComponentValue
	if ofIndex >= 0 {
		if !allowOf {
			return nthExpression{}, nil, false
		}
		formulaValues = trimComponentWhitespace(values[:ofIndex])
		selectorValues = trimComponentWhitespace(values[ofIndex+1:])
		if len(formulaValues) == 0 || len(selectorValues) == 0 {
			return nthExpression{}, nil, false
		}
		lastFormula := formulaValues[len(formulaValues)-1]
		firstSelector := selectorValues[0]
		if !componentsSeparated(source, lastFormula, values[ofIndex]) || !componentsSeparated(source, values[ofIndex], firstSelector) {
			return nthExpression{}, nil, false
		}
	}
	expression, ok := parseTokenAnPlusB(formulaValues)
	if !ok {
		return nthExpression{}, nil, false
	}
	if ofIndex < 0 {
		return expression, nil, true
	}
	selectors, ok := parseTokenSelectorComponentsWithContext(source, selectorValues, nesting, false, insideHas)
	if !ok {
		return nthExpression{}, nil, false
	}
	return expression, selectors, true
}

type nthToken struct {
	token            Token
	whitespaceBefore bool
}

func parseTokenAnPlusB(values []ComponentValue) (nthExpression, bool) {
	tokens := make([]nthToken, 0, len(values))
	whitespace := false
	for _, value := range values {
		if isWhitespaceComponent(value) {
			whitespace = true
			continue
		}
		if value.Kind != ComponentToken {
			return nthExpression{}, false
		}
		tokens = append(tokens, nthToken{token: value.Token, whitespaceBefore: whitespace})
		whitespace = false
	}
	if len(tokens) == 0 {
		return nthExpression{}, false
	}

	first := tokens[0].token
	if first.Kind == TokenIdent {
		switch lowerASCII(first.Value) {
		case "odd":
			return nthExpression{a: 2, b: 1}, len(tokens) == 1
		case "even":
			return nthExpression{a: 2}, len(tokens) == 1
		}
	}
	if first.Kind == TokenNumber {
		integer, ok := nthInteger(first)
		return nthExpression{b: integer}, ok && len(tokens) == 1
	}

	position := 1
	coefficient := 0
	offset := 0
	needsNegativeOffset := false
	offsetSpecified := false
	switch first.Kind {
	case TokenDimension:
		var ok bool
		coefficient, ok = nthInteger(first)
		if !ok {
			return nthExpression{}, false
		}
		offset, needsNegativeOffset, offsetSpecified, ok = parseNthUnit(first.Value)
		if !ok {
			return nthExpression{}, false
		}
	case TokenIdent:
		var ok bool
		coefficient, offset, needsNegativeOffset, offsetSpecified, ok = parseNthIdent(first.Value)
		if !ok {
			return nthExpression{}, false
		}
	case TokenDelim:
		if first.Value != "+" || len(tokens) < 2 || tokens[1].whitespaceBefore || tokens[1].token.Kind != TokenIdent {
			return nthExpression{}, false
		}
		var ok bool
		coefficient, offset, needsNegativeOffset, offsetSpecified, ok = parseNthIdent(tokens[1].token.Value)
		if !ok || coefficient < 0 {
			return nthExpression{}, false
		}
		position = 2
	default:
		return nthExpression{}, false
	}

	if needsNegativeOffset {
		if position >= len(tokens) {
			return nthExpression{}, false
		}
		integer, ok := nthUnsignedInteger(tokens[position].token)
		if !ok {
			return nthExpression{}, false
		}
		offset = -integer
		position++
	}
	if position == len(tokens) {
		return nthExpression{a: coefficient, b: offset}, true
	}
	if offsetSpecified || position+2 < len(tokens) {
		return nthExpression{}, false
	}
	if position+1 == len(tokens) {
		integer, ok := nthSignedInteger(tokens[position].token)
		if !ok {
			return nthExpression{}, false
		}
		return nthExpression{a: coefficient, b: integer}, true
	}
	sign := tokens[position].token
	if sign.Kind != TokenDelim || sign.Value != "+" && sign.Value != "-" {
		return nthExpression{}, false
	}
	integer, ok := nthUnsignedInteger(tokens[position+1].token)
	if !ok || position+2 != len(tokens) {
		return nthExpression{}, false
	}
	if sign.Value == "-" {
		integer = -integer
	}
	return nthExpression{a: coefficient, b: integer}, true
}

func parseNthUnit(unit string) (offset int, needsNegativeOffset, offsetSpecified, ok bool) {
	unit = lowerASCII(unit)
	if unit == "n" {
		return 0, false, false, true
	}
	if unit == "n-" {
		return 0, true, true, true
	}
	if strings.HasPrefix(unit, "n-") {
		integer, err := strconv.Atoi(unit[2:])
		return -integer, false, true, err == nil
	}
	return 0, false, false, false
}

func parseNthIdent(identifier string) (coefficient, offset int, needsNegativeOffset, offsetSpecified, ok bool) {
	identifier = lowerASCII(identifier)
	sign := 1
	if strings.HasPrefix(identifier, "-") {
		sign = -1
		identifier = identifier[1:]
	}
	offset, needsNegativeOffset, offsetSpecified, ok = parseNthUnit(identifier)
	if !ok {
		return 0, 0, false, false, false
	}
	return sign, offset, needsNegativeOffset, offsetSpecified, true
}

func nthInteger(token Token) (int, bool) {
	if !token.Integer || token.Representation == "" {
		return 0, false
	}
	integer, err := strconv.Atoi(token.Representation)
	return integer, err == nil
}

func nthUnsignedInteger(token Token) (int, bool) {
	if token.Kind != TokenNumber || strings.HasPrefix(token.Representation, "+") || strings.HasPrefix(token.Representation, "-") {
		return 0, false
	}
	return nthInteger(token)
}

func nthSignedInteger(token Token) (int, bool) {
	if token.Kind != TokenNumber || !strings.HasPrefix(token.Representation, "+") && !strings.HasPrefix(token.Representation, "-") {
		return 0, false
	}
	return nthInteger(token)
}

func componentsSeparated(source string, left, right ComponentValue) bool {
	return left.Span.End >= 0 && right.Span.Start <= len(source) && left.Span.End < right.Span.Start
}

func (parser *tokenSelectorParser) skipWhitespace() bool {
	start := parser.pos
	for !parser.done() && isWhitespaceComponent(parser.values[parser.pos]) {
		parser.pos++
	}
	return parser.pos != start
}

func (parser *tokenSelectorParser) peekToken(kind TokenKind) (Token, bool) {
	if parser.done() {
		return Token{}, false
	}
	value := parser.values[parser.pos]
	return value.Token, value.Kind == ComponentToken && value.Token.Kind == kind
}

func (parser *tokenSelectorParser) peekDelim(value string) bool {
	token, ok := parser.peekToken(TokenDelim)
	return ok && token.Value == value
}

func (parser *tokenSelectorParser) done() bool {
	return parser.pos >= len(parser.values)
}
