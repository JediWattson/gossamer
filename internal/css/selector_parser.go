package css

import (
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"
)

const maxSelectorNesting = 128

// parseSelectorList parses an ordinary, unforgiving selector list: one invalid
// member invalidates the entire list.
func parseSelectorList(source string) ([]Selector, bool) {
	return parseSelectorListAtDepth(source, 0)
}

func parseSelectorListAtDepth(source string, nesting int) ([]Selector, bool) {
	if nesting > maxSelectorNesting {
		return nil, false
	}
	parser := selectorParser{source: source, nesting: nesting}
	parser.skipInterTokenIgnorable()
	if parser.done() {
		return nil, false
	}

	var selectors []Selector
	for {
		selector, ok := parser.parseComplexSelector()
		if !ok {
			return nil, false
		}
		selectors = append(selectors, selector)
		parser.skipInterTokenIgnorable()
		if parser.done() {
			return selectors, true
		}
		if parser.peek() != ',' {
			return nil, false
		}
		parser.pos++
		parser.skipInterTokenIgnorable()
		if parser.done() {
			return nil, false
		}
	}
}

// parseForgivingSelectorList drops invalid members. A non-empty source whose
// members are all invalid remains a valid, empty list and therefore never
// matches. This is the Selectors 4 behavior used by :is() and :where().
func parseForgivingSelectorList(source string, nesting int) ([]Selector, bool) {
	if nesting > maxSelectorNesting {
		return nil, false
	}
	if trimSelectorIgnorable(source) == "" {
		return nil, false
	}
	parts := splitTopLevel(source, ',')
	selectors := make([]Selector, 0, len(parts))
	for _, part := range parts {
		trimmed := trimSelectorIgnorable(part)
		if trimmed == "" {
			continue
		}
		parsed, ok := parseSelectorListAtDepth(trimmed, nesting)
		if ok && len(parsed) == 1 {
			selectors = append(selectors, parsed[0])
		}
	}
	return selectors, true
}

type selectorParser struct {
	source  string
	pos     int
	nesting int
}

func (parser *selectorParser) parseComplexSelector() (Selector, bool) {
	parser.skipInterTokenIgnorable()
	first, specificity, ok := parser.parseCompoundSelector()
	if !ok {
		return Selector{}, false
	}

	selector := Selector{
		compounds:   []compoundSelector{first},
		specificity: specificity,
	}
	for {
		hadWhitespace := parser.skipInterTokenIgnorable()
		if parser.done() || parser.peek() == ',' {
			return selector, true
		}

		var combinator selectorCombinator
		switch parser.peek() {
		case '>':
			combinator = childCombinator
			parser.pos++
			parser.skipInterTokenIgnorable()
		case '+':
			combinator = adjacentSiblingCombinator
			parser.pos++
			parser.skipInterTokenIgnorable()
		case '~':
			combinator = generalSiblingCombinator
			parser.pos++
			parser.skipInterTokenIgnorable()
		default:
			if !hadWhitespace {
				return Selector{}, false
			}
			combinator = descendantCombinator
		}
		if parser.done() || parser.peek() == ',' || parser.peek() == '>' || parser.peek() == '+' || parser.peek() == '~' {
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

func (parser *selectorParser) parseCompoundSelector() (compoundSelector, Specificity, bool) {
	var compound compoundSelector
	var specificity Specificity
	found := false
	parser.skipCommentBoundaries()

	if !parser.done() && parser.peek() == '*' {
		compound.typeName = "*"
		parser.pos++
		found = true
	} else if parser.identifierStartsHere() {
		identifier, ok := parser.consumeIdentifier()
		if !ok {
			return compoundSelector{}, Specificity{}, false
		}
		compound.typeName = lowerASCII(identifier)
		specificity.Types++
		found = true
	}

	for !parser.done() {
		parser.skipCommentBoundaries()
		if parser.done() {
			break
		}
		switch parser.peek() {
		case '#':
			parser.pos++
			identifier, ok := parser.consumeIdentifier()
			if !ok {
				return compoundSelector{}, Specificity{}, false
			}
			compound.ids = append(compound.ids, identifier)
			specificity.IDs++
			found = true
		case '.':
			parser.pos++
			parser.skipCommentBoundaries()
			identifier, ok := parser.consumeIdentifier()
			if !ok {
				return compoundSelector{}, Specificity{}, false
			}
			compound.classes = append(compound.classes, identifier)
			specificity.Classes++
			found = true
		case '[':
			attribute, ok := parser.parseAttributeSelector()
			if !ok {
				return compoundSelector{}, Specificity{}, false
			}
			compound.attributes = append(compound.attributes, attribute)
			specificity.Classes++
			found = true
		case ':':
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

func (parser *selectorParser) parseAttributeSelector() (attributeSelector, bool) {
	parser.pos++ // '['
	parser.skipInterTokenIgnorable()
	name, ok := parser.consumeIdentifier()
	if !ok {
		return attributeSelector{}, false
	}
	attribute := attributeSelector{name: lowerASCII(name)}
	parser.skipInterTokenIgnorable()
	if parser.done() {
		return attributeSelector{}, false
	}
	if parser.peek() == ']' {
		parser.pos++
		return attribute, true
	}

	switch parser.peek() {
	case '=':
		attribute.operator = attributeEquals
		parser.pos++
	case '~', '|', '^', '$', '*':
		if parser.pos+1 >= len(parser.source) || parser.source[parser.pos+1] != '=' {
			return attributeSelector{}, false
		}
		switch parser.peek() {
		case '~':
			attribute.operator = attributeIncludes
		case '|':
			attribute.operator = attributeDashMatch
		case '^':
			attribute.operator = attributePrefix
		case '$':
			attribute.operator = attributeSuffix
		case '*':
			attribute.operator = attributeSubstring
		}
		parser.pos += 2
	default:
		return attributeSelector{}, false
	}

	parser.skipInterTokenIgnorable()
	if parser.done() {
		return attributeSelector{}, false
	}
	if parser.peek() == '\'' || parser.peek() == '"' {
		attribute.value, ok = parser.consumeString()
	} else {
		attribute.value, ok = parser.consumeIdentifier()
	}
	if !ok {
		return attributeSelector{}, false
	}

	parser.skipInterTokenIgnorable()
	if parser.done() {
		return attributeSelector{}, false
	}
	if parser.peek() != ']' {
		flag, ok := parser.consumeIdentifier()
		if !ok {
			return attributeSelector{}, false
		}
		switch lowerASCII(flag) {
		case "i":
			attribute.valueCase = attributeCaseInsensitive
		case "s":
			attribute.valueCase = attributeCaseSensitive
		default:
			return attributeSelector{}, false
		}
		parser.skipInterTokenIgnorable()
		if parser.done() || parser.peek() != ']' {
			return attributeSelector{}, false
		}
	}
	parser.pos++
	return attribute, true
}

func (parser *selectorParser) parsePseudoClass() (pseudoClassSelector, Specificity, bool) {
	parser.pos++ // ':'
	parser.skipCommentBoundaries()
	if parser.done() || parser.peek() == ':' {
		return pseudoClassSelector{}, Specificity{}, false
	}
	name, ok := parser.consumeIdentifier()
	if !ok {
		return pseudoClassSelector{}, Specificity{}, false
	}
	name = lowerASCII(name)
	pseudo := pseudoClassSelector{name: name}

	if parser.done() || parser.peek() != '(' {
		if !supportedSimplePseudoClass(name) {
			return pseudoClassSelector{}, Specificity{}, false
		}
		return pseudo, Specificity{Classes: 1}, true
	}

	argument, ok := parser.consumeParenthesized()
	if !ok {
		return pseudoClassSelector{}, Specificity{}, false
	}
	switch name {
	case "is", "where":
		pseudo.selectors, ok = parseForgivingSelectorList(argument, parser.nesting+1)
		if !ok {
			return pseudoClassSelector{}, Specificity{}, false
		}
		if name == "where" {
			return pseudo, Specificity{}, true
		}
		return pseudo, greatestSpecificity(pseudo.selectors), true
	case "not":
		pseudo.selectors, ok = parseSelectorListAtDepth(argument, parser.nesting+1)
		if !ok {
			return pseudoClassSelector{}, Specificity{}, false
		}
		return pseudo, greatestSpecificity(pseudo.selectors), true
	case "nth-child", "nth-last-child":
		pseudo.nth, pseudo.selectors, ok = parseNthArgument(argument, true, parser.nesting+1)
		if !ok {
			return pseudoClassSelector{}, Specificity{}, false
		}
		return pseudo, (Specificity{Classes: 1}).add(greatestSpecificity(pseudo.selectors)), true
	case "nth-of-type", "nth-last-of-type":
		pseudo.nth, pseudo.selectors, ok = parseNthArgument(argument, false, parser.nesting+1)
		if !ok {
			return pseudoClassSelector{}, Specificity{}, false
		}
		return pseudo, Specificity{Classes: 1}, true
	default:
		return pseudoClassSelector{}, Specificity{}, false
	}
}

func parseNthArgument(source string, allowOf bool, nesting int) (nthExpression, []Selector, bool) {
	position := skipCSSWhitespaceAt(source, 0)
	if position >= len(source) {
		return nthExpression{}, nil, false
	}

	var expression nthExpression
	lower := lowerASCII(source[position:])
	switch {
	case hasIdentifierPrefix(lower, "odd"):
		expression = nthExpression{a: 2, b: 1}
		position += 3
	case hasIdentifierPrefix(lower, "even"):
		expression = nthExpression{a: 2}
		position += 4
	default:
		var ok bool
		expression, position, ok = parseAnPlusB(source, position)
		if !ok {
			return nthExpression{}, nil, false
		}
	}

	formulaEnd := position
	position = skipCSSWhitespaceAt(source, position)
	if position == len(source) {
		return expression, nil, true
	}
	if !allowOf || position == formulaEnd {
		return nthExpression{}, nil, false
	}
	if position+2 > len(source) || !equalASCIIFold(source[position:position+2], "of") ||
		position+2 < len(source) && isSelectorIdentifierContinueAt(source, position+2) {
		return nthExpression{}, nil, false
	}
	position += 2
	if next := skipCSSWhitespaceAt(source, position); next == position {
		return nthExpression{}, nil, false
	} else {
		position = next
	}
	selectors, ok := parseSelectorListAtDepth(source[position:], nesting)
	if !ok {
		return nthExpression{}, nil, false
	}
	return expression, selectors, true
}

func parseAnPlusB(source string, position int) (nthExpression, int, bool) {
	sign := 1
	if source[position] == '+' || source[position] == '-' {
		if source[position] == '-' {
			sign = -1
		}
		position++
		if position >= len(source) || isCSSWhitespace(source[position]) {
			return nthExpression{}, 0, false
		}
	}

	if position < len(source) && (source[position] == 'n' || source[position] == 'N') {
		return parseNthOffset(source, position+1, sign)
	}
	digitStart := position
	for position < len(source) && source[position] >= '0' && source[position] <= '9' {
		position++
	}
	if digitStart == position {
		return nthExpression{}, 0, false
	}
	number, err := strconv.Atoi(source[digitStart:position])
	if err != nil {
		return nthExpression{}, 0, false
	}
	if position < len(source) && (source[position] == 'n' || source[position] == 'N') {
		return parseNthOffset(source, position+1, sign*number)
	}
	return nthExpression{b: sign * number}, position, true
}

func parseNthOffset(source string, position, coefficient int) (nthExpression, int, bool) {
	expression := nthExpression{a: coefficient}
	offsetStart := position
	position = skipCSSWhitespaceAt(source, position)
	if position >= len(source) || source[position] != '+' && source[position] != '-' {
		return expression, offsetStart, true
	}
	sign := 1
	if source[position] == '-' {
		sign = -1
	}
	position++
	position = skipCSSWhitespaceAt(source, position)
	if position >= len(source) || source[position] < '0' || source[position] > '9' {
		return nthExpression{}, 0, false
	}
	digitStart := position
	for position < len(source) && source[position] >= '0' && source[position] <= '9' {
		position++
	}
	offset, err := strconv.Atoi(source[digitStart:position])
	if err != nil {
		return nthExpression{}, 0, false
	}
	expression.b = sign * offset
	return expression, position, true
}

func hasIdentifierPrefix(source, prefix string) bool {
	if !strings.HasPrefix(source, prefix) {
		return false
	}
	return len(source) == len(prefix) || !isSelectorIdentifierContinueAt(source, len(prefix))
}

func (parser *selectorParser) consumeParenthesized() (string, bool) {
	if parser.done() || parser.peek() != '(' {
		return "", false
	}
	start := parser.pos + 1
	depth := 1
	quote := byte(0)
	escaped := false
	bracketDepth := 0
	for position := start; position < len(parser.source); position++ {
		character := parser.source[position]
		if quote != 0 {
			if escaped {
				escaped = false
				continue
			}
			if character == '\\' {
				escaped = true
				continue
			}
			if character == quote {
				quote = 0
			}
			continue
		}
		switch character {
		case '\'', '"':
			quote = character
		case '[':
			bracketDepth++
		case ']':
			if bracketDepth > 0 {
				bracketDepth--
			}
		case '(':
			if bracketDepth == 0 {
				depth++
			}
		case ')':
			if bracketDepth == 0 {
				depth--
				if depth == 0 {
					argument := parser.source[start:position]
					parser.pos = position + 1
					return argument, true
				}
			}
		}
	}
	return "", false
}

func (parser *selectorParser) consumeString() (string, bool) {
	if parser.done() || parser.peek() != '\'' && parser.peek() != '"' {
		return "", false
	}
	quote := parser.peek()
	parser.pos++
	var value strings.Builder
	for !parser.done() {
		character := parser.peek()
		parser.pos++
		if character == quote {
			return value.String(), true
		}
		if character == '\n' || character == '\r' || character == '\f' {
			return "", false
		}
		if character == '\\' {
			if parser.done() {
				return "", false
			}
			if parser.peek() == '\n' || parser.peek() == '\r' || parser.peek() == '\f' {
				parser.consumeEscapedNewline()
				continue
			}
			if isHexDigit(parser.peek()) {
				codePoint := 0
				for count := 0; count < 6 && !parser.done() && isHexDigit(parser.peek()); count++ {
					codePoint = codePoint*16 + hexDigitValue(parser.peek())
					parser.pos++
				}
				if !parser.done() && isCSSWhitespace(parser.peek()) {
					parser.consumeEscapedNewline()
				}
				if codePoint == 0 || codePoint > utf8.MaxRune || codePoint >= 0xd800 && codePoint <= 0xdfff {
					codePoint = utf8.RuneError
				}
				value.WriteRune(rune(codePoint))
				continue
			}
			runeValue, width := utf8.DecodeRuneInString(parser.source[parser.pos:])
			parser.pos += width
			value.WriteRune(runeValue)
			continue
		}
		value.WriteByte(character)
	}
	return "", false
}

func (parser *selectorParser) consumeEscapedNewline() {
	if parser.done() {
		return
	}
	if parser.peek() == '\r' && parser.pos+1 < len(parser.source) && parser.source[parser.pos+1] == '\n' {
		parser.pos += 2
		return
	}
	parser.pos++
}

func isHexDigit(character byte) bool {
	return character >= '0' && character <= '9' ||
		character >= 'a' && character <= 'f' ||
		character >= 'A' && character <= 'F'
}

func hexDigitValue(character byte) int {
	switch {
	case character >= '0' && character <= '9':
		return int(character - '0')
	case character >= 'a' && character <= 'f':
		return int(character-'a') + 10
	default:
		return int(character-'A') + 10
	}
}

func (parser *selectorParser) consumeIdentifier() (string, bool) {
	if !parser.identifierStartsHere() {
		return "", false
	}
	start := parser.pos
	_, parser.pos = consumeIdentifier(parser.source, parser.pos)
	return parser.source[start:parser.pos], parser.pos > start
}

func (parser *selectorParser) identifierStartsHere() bool {
	if parser.done() {
		return false
	}
	runeValue, width := utf8.DecodeRuneInString(parser.source[parser.pos:])
	if runeValue == '\\' || unicode.IsDigit(runeValue) {
		return false
	}
	if runeValue != '-' {
		return runeValue == '_' || unicode.IsLetter(runeValue) || runeValue >= utf8.RuneSelf
	}
	next := parser.pos + width
	if next >= len(parser.source) {
		return false
	}
	nextRune, _ := utf8.DecodeRuneInString(parser.source[next:])
	return nextRune == '-' || nextRune == '_' || unicode.IsLetter(nextRune) || nextRune >= utf8.RuneSelf
}

func isSelectorIdentifierContinueAt(source string, position int) bool {
	if position >= len(source) {
		return false
	}
	runeValue, _ := utf8.DecodeRuneInString(source[position:])
	return runeValue == '-' || runeValue == '_' || unicode.IsLetter(runeValue) || unicode.IsDigit(runeValue) || runeValue >= utf8.RuneSelf
}

func (parser *selectorParser) skipCommentBoundaries() {
	for !parser.done() && parser.peek() == commentBoundary {
		parser.pos++
	}
}

// skipInterTokenIgnorable consumes comments and CSS whitespace while reporting
// only real whitespace. Comments create token boundaries but are not descendant
// combinators.
func (parser *selectorParser) skipInterTokenIgnorable() bool {
	hadWhitespace := false
	for !parser.done() {
		if parser.peek() == commentBoundary {
			parser.pos++
			continue
		}
		if isCSSWhitespace(parser.peek()) {
			hadWhitespace = true
			parser.pos++
			continue
		}
		break
	}
	return hadWhitespace
}

func skipCSSWhitespaceAt(source string, position int) int {
	for position < len(source) && isCSSWhitespace(source[position]) {
		position++
	}
	return position
}

func trimSelectorIgnorable(source string) string {
	start := 0
	for start < len(source) && (isCSSWhitespace(source[start]) || source[start] == commentBoundary) {
		start++
	}
	end := len(source)
	for end > start && (isCSSWhitespace(source[end-1]) || source[end-1] == commentBoundary) {
		end--
	}
	return source[start:end]
}

func (parser *selectorParser) done() bool {
	return parser.pos >= len(parser.source)
}

func (parser *selectorParser) peek() byte {
	return parser.source[parser.pos]
}
