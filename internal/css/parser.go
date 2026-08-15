package css

import (
	"fmt"
	"strings"
	"unicode"
	"unicode/utf8"
)

// commentBoundary records a removed comment without turning it into CSS
// whitespace. Keeping a zero-width token boundary prevents comments from
// accidentally merging identifiers or creating function tokens.
const commentBoundary byte = 0

const maxGroupRuleNesting = 128

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

// ParseRawDeclarationList parses the contents of a CSS declaration block, such
// as an HTML style attribute, without collapsing duplicate properties. The raw
// ordering is required by the cascade and is intentionally distinct from a
// normalized CSSOM declaration-block view. Malformed declarations are skipped.
// Safely parsed declarations are returned alongside an error when an
// unterminated comment or string prevents further recovery.
func ParseRawDeclarationList(source string) ([]Declaration, error) {
	cleaned, commentErr := stripComments(source)
	declarations, stringErr := parseCleanDeclarationList(cleaned)
	if stringErr != nil {
		return declarations, stringErr
	}
	return declarations, commentErr
}

type stylesheetParser struct {
	source     string
	pos        int
	stylesheet *Stylesheet
	context    ruleContext
	nesting    int
}

type ruleContext struct {
	layer string
	media []string
}

func (parser *stylesheetParser) parse() (Stylesheet, error) {
	stylesheet := Stylesheet{}
	parser.stylesheet = &stylesheet
	if err := parser.parseRuleList(); err != nil {
		return stylesheet, err
	}
	return stylesheet, nil
}

func (parser *stylesheetParser) parseRuleList() error {
	for parser.pos < len(parser.source) {
		parser.skipIgnorable()
		if parser.pos >= len(parser.source) {
			break
		}

		if parser.source[parser.pos] == '@' {
			if err := parser.parseAtRule(); err != nil {
				return err
			}
			continue
		}

		prelude, delimiter, err := parser.readRulePrelude()
		if err != nil {
			return err
		}
		if delimiter != '{' {
			// A semicolon terminates a malformed qualified rule. A stray closing
			// brace is consumed by readRulePrelude. EOF simply ends the sheet.
			continue
		}

		block, err := parser.readBlock()
		if err != nil {
			return err
		}
		selectors, valid := parseSelectorList(prelude)
		if !valid {
			continue
		}
		declarations := parseDeclarations(block)

		parser.stylesheet.Rules = append(parser.stylesheet.Rules, Rule{
			Selectors:    selectors,
			Declarations: declarations,
			Order:        len(parser.stylesheet.Rules),
			Layer:        parser.context.layer,
			Media:        append([]string(nil), parser.context.media...),
		})
	}

	return nil
}

func (parser *stylesheetParser) parseAtRule() error {
	parser.pos++ // '@'
	nameStart := parser.pos
	_, parser.pos = consumeIdentifier(parser.source, parser.pos)
	if parser.pos == nameStart {
		return parser.skipAtRuleTail()
	}
	name := strings.ToLower(parser.source[nameStart:parser.pos])
	prelude, delimiter, err := parser.readRulePrelude()
	if err != nil {
		return err
	}
	prelude = strings.TrimSpace(normalizeCommentBoundaries(prelude))

	switch delimiter {
	case ';', 0, '}':
		if name == "layer" && delimiter == ';' && parser.context.layer == "" && len(parser.context.media) == 0 {
			if layers, ok := parseLayerNameList(prelude); ok {
				for _, layer := range layers {
					parser.recordLayer(layer)
				}
			}
		}
		return nil
	case '{':
		block, err := parser.readBlock()
		if err != nil {
			return err
		}
		switch name {
		case "layer":
			if parser.context.layer != "" || len(parser.context.media) != 0 {
				return nil
			}
			layers, ok := parseLayerNameList(prelude)
			if !ok || len(layers) != 1 {
				// Anonymous, nested, dotted, and multi-name layer blocks are deferred
				// until the layer identity model can represent their cascade ordering.
				return nil
			}
			parser.recordLayer(layers[0])
			return parser.parseNestedRuleList(block, ruleContext{
				layer: layers[0],
				media: append([]string(nil), parser.context.media...),
			})
		case "media":
			media := append([]string(nil), parser.context.media...)
			media = append(media, prelude)
			return parser.parseNestedRuleList(block, ruleContext{
				layer: parser.context.layer,
				media: media,
			})
		default:
			return nil
		}
	default:
		return nil
	}
}

func (parser *stylesheetParser) parseNestedRuleList(source string, context ruleContext) error {
	if parser.nesting >= maxGroupRuleNesting {
		return nil
	}
	nested := stylesheetParser{
		source:     source,
		stylesheet: parser.stylesheet,
		context:    context,
		nesting:    parser.nesting + 1,
	}
	return nested.parseRuleList()
}

func (parser *stylesheetParser) recordLayer(name string) {
	for _, existing := range parser.stylesheet.LayerOrder {
		if existing == name {
			return
		}
	}
	parser.stylesheet.LayerOrder = append(parser.stylesheet.LayerOrder, name)
}

func parseLayerNameList(source string) ([]string, bool) {
	parts := splitTopLevel(source, ',')
	if len(parts) == 0 {
		return nil, false
	}
	layers := make([]string, 0, len(parts))
	for _, part := range parts {
		name := trimCSSIgnorable(part)
		if name == "" || !validLayerName(name) {
			return nil, false
		}
		layers = append(layers, name)
	}
	return layers, true
}

func validLayerName(source string) bool {
	if strings.Contains(source, ".") || !validPropertyName(source) {
		return false
	}
	switch strings.ToLower(source) {
	case "initial", "inherit", "unset", "revert", "revert-layer":
		return false
	}
	return true
}

func (parser *stylesheetParser) skipIgnorable() {
	for parser.pos < len(parser.source) {
		// CSS Syntax tokenizes legacy HTML comment delimiters as CDO/CDC and
		// discards them at the top level of a stylesheet. Older sites still wrap
		// inline CSS in these markers.
		if strings.HasPrefix(parser.source[parser.pos:], "<!--") {
			parser.pos += len("<!--")
			continue
		}
		if strings.HasPrefix(parser.source[parser.pos:], "-->") {
			parser.pos += len("-->")
			continue
		}
		switch parser.source[parser.pos] {
		case ' ', '\t', '\n', '\r', '\f', commentBoundary, ';', '}':
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
		if character == '\\' && validCSSEscapeAt(parser.source, parser.pos) {
			parser.pos = skipCSSEscape(parser.source, parser.pos)
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
		if character == '\\' && validCSSEscapeAt(parser.source, parser.pos) {
			parser.pos = skipCSSEscape(parser.source, parser.pos)
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

func (parser *stylesheetParser) skipAtRuleTail() error {
	// Skip an at-rule through its top-level semicolon or its complete block.
	// This is used only after a malformed at-keyword; recognized at-rules are
	// handled by parseAtRule.
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
		if character == '\\' && validCSSEscapeAt(parser.source, parser.pos) {
			parser.pos = skipCSSEscape(parser.source, parser.pos)
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

func parseDeclarations(block string) []Declaration {
	declarations, _ := parseCleanDeclarationList(block)
	return declarations
}

func parseCleanDeclarationList(source string) ([]Declaration, error) {
	parts, err := splitDeclarationList(source)
	declarations := make([]Declaration, 0, len(parts))
	for _, part := range parts {
		colon := indexTopLevel(part, ':')
		if colon < 0 {
			continue
		}

		property := trimCSSIgnorable(part[:colon])
		if !validPropertyName(property) {
			continue
		}
		custom := strings.HasPrefix(property, "--")
		if !custom {
			property = strings.ToLower(property)
		}

		value := normalizeCommentBoundaries(part[colon+1:])
		value = strings.TrimSpace(value)
		value, important := removeImportant(value)
		if value == "" && !custom {
			continue
		}
		if custom && !ValidCustomPropertyValue(value) {
			continue
		}
		declarations = append(declarations, Declaration{
			Property:  property,
			Value:     value,
			Important: important,
		})
	}
	return declarations, err
}

func splitDeclarationList(source string) ([]string, error) {
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
		if character == '\\' && validCSSEscapeAt(source, position) {
			position = skipCSSEscape(source, position) - 1
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
		case ';':
			if parenDepth == 0 && bracketDepth == 0 && braceDepth == 0 {
				parts = append(parts, source[start:position])
				start = position + 1
			}
		}
	}

	if quote != 0 {
		return parts, fmt.Errorf("css: unterminated string in declaration list")
	}
	return append(parts, source[start:]), nil
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
		if character == '\\' && validCSSEscapeAt(value, position) {
			// An escaped exclamation mark is part of an identifier token, not
			// the delimiter token that can begin an !important annotation.
			position = skipCSSEscape(value, position) - 1
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
	suffix, ok := parseSingleCSSIdentifier(value[importantAt+1:])
	if !ok || !equalASCIIFold(suffix, "important") {
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
		if character == '\\' && validCSSEscapeAt(source, position) {
			position = skipCSSEscape(source, position) - 1
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
		if character == '\\' && validCSSEscapeAt(source, position) {
			position = skipCSSEscape(source, position) - 1
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
		if character == 0 {
			cleaned.WriteRune(utf8.RuneError)
			position++
			continue
		}
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
		if character == '\\' && validCSSEscapeAt(source, position) {
			end := skipCSSEscape(source, position)
			cleaned.WriteString(source[position:end])
			position = end
			continue
		}
		if character == '/' && position+1 < len(source) && source[position+1] == '*' {
			end := strings.Index(source[position+2:], "*/")
			if end < 0 {
				return cleaned.String(), fmt.Errorf("css: unterminated comment")
			}
			cleaned.WriteByte(commentBoundary)
			position += end + 4
			continue
		}
		cleaned.WriteByte(character)
		position++
	}

	return cleaned.String(), nil
}

func trimCSSIgnorable(source string) string {
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

func normalizeCommentBoundaries(source string) string {
	if !strings.ContainsRune(source, rune(commentBoundary)) {
		return source
	}
	var normalized strings.Builder
	normalized.Grow(len(source))
	for position := 0; position < len(source); position++ {
		if source[position] == commentBoundary {
			normalized.WriteByte(' ')
			continue
		}
		normalized.WriteByte(source[position])
	}
	return normalized.String()
}
