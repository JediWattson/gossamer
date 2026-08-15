package css

import (
	"fmt"
	"strings"
	"unicode"
	"unicode/utf8"
)

// Parse parses the supported subset of a CSS stylesheet. Unsupported or
// malformed rules and declarations are skipped so a bad rule does not discard
// the rest of the sheet. An error is returned only when an unterminated comment
// or string makes it unsafe to locate the next rule boundary.
func Parse(source string) (Stylesheet, error) {
	cleaned, commentErr := stripComments(source)
	parser := stylesheetParser{source: cleaned}
	stylesheet, parseErr := parser.parse()
	if parseErr != nil {
		return stylesheet, parseErr
	}
	return stylesheet, commentErr
}

type stylesheetParser struct {
	source string
	pos    int
}

func (parser *stylesheetParser) parse() (Stylesheet, error) {
	var stylesheet Stylesheet

	for parser.pos < len(parser.source) {
		parser.skipIgnorable()
		if parser.pos >= len(parser.source) {
			break
		}

		if parser.source[parser.pos] == '@' {
			if err := parser.skipAtRule(); err != nil {
				return stylesheet, err
			}
			continue
		}

		prelude, delimiter, err := parser.readRulePrelude()
		if err != nil {
			return stylesheet, err
		}
		if delimiter != '{' {
			// A semicolon terminates a malformed qualified rule. A stray closing
			// brace is consumed by readRulePrelude. EOF simply ends the sheet.
			continue
		}

		block, err := parser.readBlock()
		if err != nil {
			return stylesheet, err
		}
		selectors, valid := parseSelectorList(prelude)
		if !valid {
			continue
		}
		declarations := parseDeclarations(block)

		stylesheet.Rules = append(stylesheet.Rules, Rule{
			Selectors:    selectors,
			Declarations: declarations,
			Order:        len(stylesheet.Rules),
		})
	}

	return stylesheet, nil
}

func (parser *stylesheetParser) skipIgnorable() {
	for parser.pos < len(parser.source) {
		switch parser.source[parser.pos] {
		case ' ', '\t', '\n', '\r', '\f', ';', '}':
			parser.pos++
		default:
			return
		}
	}
}

func (parser *stylesheetParser) readRulePrelude() (string, byte, error) {
	start := parser.pos
	quote := byte(0)
	escaped := false
	parenDepth := 0
	bracketDepth := 0

	for parser.pos < len(parser.source) {
		character := parser.source[parser.pos]
		if quote != 0 {
			parser.pos++
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
			parser.pos++
		case '(':
			parenDepth++
			parser.pos++
		case ')':
			if parenDepth > 0 {
				parenDepth--
			}
			parser.pos++
		case '[':
			bracketDepth++
			parser.pos++
		case ']':
			if bracketDepth > 0 {
				bracketDepth--
			}
			parser.pos++
		case '{', ';', '}':
			if parenDepth == 0 && bracketDepth == 0 {
				prelude := parser.source[start:parser.pos]
				parser.pos++
				return prelude, character, nil
			}
			parser.pos++
		default:
			parser.pos++
		}
	}

	if quote != 0 {
		return "", 0, fmt.Errorf("css: unterminated string in rule prelude")
	}
	return parser.source[start:parser.pos], 0, nil
}

// readBlock starts immediately after an opening brace. It returns the contents
// without the outer braces. An unclosed block at EOF is still useful and is
// treated as an implicit close; an unclosed string is not safely recoverable.
func (parser *stylesheetParser) readBlock() (string, error) {
	start := parser.pos
	depth := 1
	quote := byte(0)
	escaped := false

	for parser.pos < len(parser.source) {
		character := parser.source[parser.pos]
		if quote != 0 {
			parser.pos++
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
			parser.pos++
		case '{':
			depth++
			parser.pos++
		case '}':
			depth--
			if depth == 0 {
				block := parser.source[start:parser.pos]
				parser.pos++
				return block, nil
			}
			parser.pos++
		default:
			parser.pos++
		}
	}

	if quote != 0 {
		return "", fmt.Errorf("css: unterminated string in declaration block")
	}
	return parser.source[start:parser.pos], nil
}

func (parser *stylesheetParser) skipAtRule() error {
	// Skip an at-rule through its top-level semicolon or its complete block.
	// Nested blocks (for example @media containing qualified rules) are skipped
	// as a unit until conditional rule evaluation is implemented.
	quote := byte(0)
	escaped := false
	parenDepth := 0
	bracketDepth := 0
	parser.pos++ // '@'

	for parser.pos < len(parser.source) {
		character := parser.source[parser.pos]
		if quote != 0 {
			parser.pos++
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
			parser.pos++
		case '(':
			parenDepth++
			parser.pos++
		case ')':
			if parenDepth > 0 {
				parenDepth--
			}
			parser.pos++
		case '[':
			bracketDepth++
			parser.pos++
		case ']':
			if bracketDepth > 0 {
				bracketDepth--
			}
			parser.pos++
		case ';':
			parser.pos++
			if parenDepth == 0 && bracketDepth == 0 {
				return nil
			}
		case '{':
			if parenDepth == 0 && bracketDepth == 0 {
				parser.pos++
				_, err := parser.readBlock()
				return err
			}
			parser.pos++
		default:
			parser.pos++
		}
	}

	if quote != 0 {
		return fmt.Errorf("css: unterminated string in at-rule")
	}
	return nil
}

func parseSelectorList(source string) ([]Selector, bool) {
	parts := splitTopLevel(source, ',')
	selectors := make([]Selector, 0, len(parts))
	for _, part := range parts {
		selector, ok := parseSelector(strings.TrimSpace(part))
		if !ok {
			// CSS selector lists are not forgiving: one invalid selector makes
			// the entire qualified rule invalid.
			return nil, false
		}
		selectors = append(selectors, selector)
	}
	return selectors, len(selectors) > 0
}

func parseSelector(source string) (Selector, bool) {
	if source == "" {
		return Selector{}, false
	}

	var selector Selector
	position := 0
	if source[position] == '*' {
		selector.Tag = "*"
		position++
	} else if isIdentifierStartAt(source, position) {
		identifier, next := consumeIdentifier(source, position)
		selector.Tag = lowerASCII(identifier)
		selector.Specificity.Types++
		position = next
	}

	for position < len(source) {
		prefix := source[position]
		if isCSSWhitespace(prefix) {
			// Outer whitespace was trimmed; whitespace here would introduce an
			// unsupported descendant combinator.
			return Selector{}, false
		}
		if prefix != '#' && prefix != '.' && prefix != ':' {
			return Selector{}, false
		}
		position++
		if prefix == ':' && position < len(source) && source[position] == ':' {
			return Selector{}, false
		}
		if !isIdentifierStartAt(source, position) {
			return Selector{}, false
		}
		identifier, next := consumeIdentifier(source, position)
		position = next

		switch prefix {
		case '#':
			if selector.ID != "" {
				return Selector{}, false
			}
			selector.ID = identifier
			selector.Specificity.IDs++
		case '.':
			selector.Classes = append(selector.Classes, identifier)
			selector.Specificity.Classes++
		case ':':
			// Functional pseudo-classes are deliberately outside the first
			// subset; accepting their arguments would require their distinct
			// matching and specificity rules.
			if position < len(source) && source[position] == '(' {
				return Selector{}, false
			}
			identifier = strings.ToLower(identifier)
			if !supportedPseudoClass(identifier) {
				return Selector{}, false
			}
			selector.PseudoClasses = append(selector.PseudoClasses, identifier)
			selector.Specificity.Classes++
		}
	}

	if selector.Tag == "" && selector.ID == "" && len(selector.Classes) == 0 && len(selector.PseudoClasses) == 0 {
		return Selector{}, false
	}
	return selector, true
}

func parseDeclarations(block string) []Declaration {
	parts := splitTopLevel(block, ';')
	declarations := make([]Declaration, 0, len(parts))
	for _, part := range parts {
		colon := indexTopLevel(part, ':')
		if colon < 0 {
			continue
		}

		property := strings.TrimSpace(part[:colon])
		if !validPropertyName(property) {
			continue
		}
		if !strings.HasPrefix(property, "--") {
			property = strings.ToLower(property)
		}

		value := strings.TrimSpace(part[colon+1:])
		value, important := removeImportant(value)
		if value == "" {
			continue
		}
		declarations = append(declarations, Declaration{
			Property:  property,
			Value:     value,
			Important: important,
		})
	}
	return declarations
}

func removeImportant(value string) (string, bool) {
	importantAt := -1
	quote := byte(0)
	escaped := false
	parenDepth := 0
	bracketDepth := 0
	braceDepth := 0

	for position := 0; position < len(value); position++ {
		character := value[position]
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
		case '(':
			parenDepth++
		case ')':
			if parenDepth > 0 {
				parenDepth--
			}
		case '[':
			bracketDepth++
		case ']':
			if bracketDepth > 0 {
				bracketDepth--
			}
		case '{':
			braceDepth++
		case '}':
			if braceDepth > 0 {
				braceDepth--
			}
		case '!':
			if parenDepth == 0 && bracketDepth == 0 && braceDepth == 0 {
				importantAt = position
			}
		}
	}

	if importantAt < 0 {
		return strings.TrimSpace(value), false
	}
	suffix := strings.TrimSpace(value[importantAt+1:])
	if !strings.EqualFold(suffix, "important") {
		return strings.TrimSpace(value), false
	}
	return strings.TrimSpace(value[:importantAt]), true
}

func splitTopLevel(source string, delimiter byte) []string {
	var parts []string
	start := 0
	quote := byte(0)
	escaped := false
	parenDepth := 0
	bracketDepth := 0
	braceDepth := 0

	for position := 0; position < len(source); position++ {
		character := source[position]
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
		case '(':
			parenDepth++
		case ')':
			if parenDepth > 0 {
				parenDepth--
			}
		case '[':
			bracketDepth++
		case ']':
			if bracketDepth > 0 {
				bracketDepth--
			}
		case '{':
			braceDepth++
		case '}':
			if braceDepth > 0 {
				braceDepth--
			}
		default:
			if character == delimiter && parenDepth == 0 && bracketDepth == 0 && braceDepth == 0 {
				parts = append(parts, source[start:position])
				start = position + 1
			}
		}
	}
	return append(parts, source[start:])
}

func indexTopLevel(source string, delimiter byte) int {
	parts := splitTopLevelWithOffsets(source, delimiter)
	if len(parts) == 0 {
		return -1
	}
	return parts[0]
}

func splitTopLevelWithOffsets(source string, delimiter byte) []int {
	quote := byte(0)
	escaped := false
	parenDepth := 0
	bracketDepth := 0
	braceDepth := 0
	for position := 0; position < len(source); position++ {
		character := source[position]
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
		case '(':
			parenDepth++
		case ')':
			if parenDepth > 0 {
				parenDepth--
			}
		case '[':
			bracketDepth++
		case ']':
			if bracketDepth > 0 {
				bracketDepth--
			}
		case '{':
			braceDepth++
		case '}':
			if braceDepth > 0 {
				braceDepth--
			}
		default:
			if character == delimiter && parenDepth == 0 && bracketDepth == 0 && braceDepth == 0 {
				return []int{position}
			}
		}
	}
	return nil
}

func validPropertyName(property string) bool {
	if property == "" || !isIdentifierStartAt(property, 0) {
		return false
	}
	_, end := consumeIdentifier(property, 0)
	return end == len(property)
}

func isIdentifierStartAt(source string, position int) bool {
	if position >= len(source) {
		return false
	}
	runeValue, _ := utf8.DecodeRuneInString(source[position:])
	return runeValue == '_' || runeValue == '-' || unicode.IsLetter(runeValue) || runeValue >= utf8.RuneSelf
}

func consumeIdentifier(source string, position int) (string, int) {
	start := position
	for position < len(source) {
		runeValue, width := utf8.DecodeRuneInString(source[position:])
		if runeValue != '_' && runeValue != '-' && !unicode.IsLetter(runeValue) && !unicode.IsDigit(runeValue) && runeValue < utf8.RuneSelf {
			break
		}
		position += width
	}
	return source[start:position], position
}

func isCSSWhitespace(character byte) bool {
	switch character {
	case ' ', '\t', '\n', '\r', '\f':
		return true
	default:
		return false
	}
}

func stripComments(source string) (string, error) {
	var cleaned strings.Builder
	cleaned.Grow(len(source))
	quote := byte(0)
	escaped := false

	for position := 0; position < len(source); {
		character := source[position]
		if quote != 0 {
			cleaned.WriteByte(character)
			position++
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

		if character == '\'' || character == '"' {
			quote = character
			cleaned.WriteByte(character)
			position++
			continue
		}
		if character == '/' && position+1 < len(source) && source[position+1] == '*' {
			end := strings.Index(source[position+2:], "*/")
			if end < 0 {
				return cleaned.String(), fmt.Errorf("css: unterminated comment")
			}
			position += end + 4
			continue
		}
		cleaned.WriteByte(character)
		position++
	}

	return cleaned.String(), nil
}
