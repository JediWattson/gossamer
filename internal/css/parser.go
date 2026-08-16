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
	parser := stylesheetParser{source: cleaned, original: source}
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
	sourced, err := ParseRawDeclarationListWithSources(source)
	declarations := make([]Declaration, len(sourced))
	for index := range sourced {
		declarations[index] = sourced[index].Declaration
	}
	return declarations, err
}

// ParseRawDeclarationListWithSources is ParseRawDeclarationList with original
// byte ranges for diagnostics and cascade provenance.
func ParseRawDeclarationListWithSources(source string) ([]SourcedDeclaration, error) {
	return parseSourcedDeclarationList(source, 0)
}

type stylesheetParser struct {
	source         string
	original       string
	baseOffset     int
	pos            int
	stylesheet     *Stylesheet
	context        ruleContext
	nesting        int
	importsAllowed bool
	seenImport     bool
	anonymousLayer *int
	appearance     *int
}

type ruleContext struct {
	layer           string
	media           []string
	supports        []string
	parentSelectors []Selector
}

func (parser *stylesheetParser) parse() (Stylesheet, error) {
	stylesheet := Stylesheet{}
	parser.stylesheet = &stylesheet
	parser.importsAllowed = true
	anonymousLayer := 0
	parser.anonymousLayer = &anonymousLayer
	appearance := 0
	parser.appearance = &appearance
	err := parser.parseRuleList()
	stylesheet = stylesheet.WithSelectorIndex()
	return stylesheet, err
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
		parser.importsAllowed = false

		preludeStart := parser.pos
		prelude, delimiter, err := parser.readRulePrelude()
		if err != nil {
			return err
		}
		if delimiter != '{' {
			// A semicolon terminates a malformed qualified rule. A stray closing
			// brace is consumed by readRulePrelude. EOF simply ends the sheet.
			continue
		}

		blockStart := parser.pos
		block, err := parser.readBlock()
		if err != nil {
			return err
		}
		originalPrelude := parser.original[preludeStart : preludeStart+len(prelude)]
		var selectors []Selector
		var valid bool
		if len(parser.context.parentSelectors) > 0 {
			selectors, valid = parseNestedSelectorList(originalPrelude, parser.context.parentSelectors)
		} else {
			selectors, valid = parseSelectorList(originalPrelude)
		}
		if !valid {
			continue
		}
		originalBlock := parser.original[blockStart : blockStart+len(block)]
		nestedContext := parser.context
		nestedContext.parentSelectors = selectors
		before := len(parser.stylesheet.Rules)
		direct, err := parser.parseNestedStyleContents(block, originalBlock, parser.baseOffset+blockStart, nestedContext)
		if err != nil {
			return err
		}
		if !direct && len(parser.stylesheet.Rules) == before {
			parser.appendRule(selectors, nil, nil, parser.context)
		}
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
	canImport := parser.importsAllowed
	if name == "layer" && delimiter == ';' && parser.seenImport {
		parser.importsAllowed = false
	}
	if name != "import" && name != "charset" && !(name == "layer" && delimiter == ';') {
		parser.importsAllowed = false
	}

	switch delimiter {
	case ';', 0, '}':
		if name == "import" && delimiter == ';' && canImport && parser.context.layer == "" && len(parser.context.media) == 0 && len(parser.context.supports) == 0 {
			if imported, ok := parseImportRule(prelude); ok {
				imported.Order = len(parser.stylesheet.Imports)
				imported.AppearanceOrder = parser.nextAppearanceOrder()
				parser.stylesheet.Imports = append(parser.stylesheet.Imports, imported)
				parser.seenImport = true
			}
			return nil
		}
		if name == "layer" && delimiter == ';' {
			if layers, ok := parseLayerNameList(prelude); ok {
				for _, layer := range layers {
					parser.recordLayer(qualifyLayerName(parser.context.layer, layer))
				}
			}
		}
		return nil
	case '{':
		blockStart := parser.pos
		block, err := parser.readBlock()
		if err != nil {
			return err
		}
		switch name {
		case "layer":
			layer := ""
			if strings.TrimSpace(prelude) == "" {
				layer = parser.newAnonymousLayer()
			} else {
				layers, ok := parseLayerNameList(prelude)
				if !ok || len(layers) != 1 {
					return nil
				}
				layer = layers[0]
			}
			layer = qualifyLayerName(parser.context.layer, layer)
			parser.recordLayer(layer)
			return parser.parseNestedGroupRuleList(block, parser.original[blockStart:blockStart+len(block)], parser.baseOffset+blockStart, ruleContext{
				layer:           layer,
				media:           append([]string(nil), parser.context.media...),
				supports:        append([]string(nil), parser.context.supports...),
				parentSelectors: append([]Selector(nil), parser.context.parentSelectors...),
			})
		case "media":
			media := append([]string(nil), parser.context.media...)
			media = append(media, prelude)
			return parser.parseNestedGroupRuleList(block, parser.original[blockStart:blockStart+len(block)], parser.baseOffset+blockStart, ruleContext{
				layer:           parser.context.layer,
				media:           media,
				supports:        append([]string(nil), parser.context.supports...),
				parentSelectors: append([]Selector(nil), parser.context.parentSelectors...),
			})
		case "supports":
			supports := append([]string(nil), parser.context.supports...)
			supports = append(supports, prelude)
			return parser.parseNestedGroupRuleList(block, parser.original[blockStart:blockStart+len(block)], parser.baseOffset+blockStart, ruleContext{
				layer:           parser.context.layer,
				media:           append([]string(nil), parser.context.media...),
				supports:        supports,
				parentSelectors: append([]Selector(nil), parser.context.parentSelectors...),
			})
		default:
			return nil
		}
	default:
		return nil
	}
}

func (parser *stylesheetParser) appendRule(selectors []Selector, declarations []Declaration, sources []DeclarationSource, context ruleContext) {
	if len(selectors) == 0 {
		return
	}
	parser.stylesheet.Rules = append(parser.stylesheet.Rules, Rule{
		Selectors:          selectors,
		Declarations:       declarations,
		DeclarationSources: sources,
		Order:              len(parser.stylesheet.Rules),
		Layer:              context.layer,
		Media:              append([]string(nil), context.media...),
		Supports:           append([]string(nil), context.supports...),
	})
}

func (parser *stylesheetParser) parseNestedGroupRuleList(source, original string, baseOffset int, context ruleContext) error {
	if len(context.parentSelectors) == 0 {
		return parser.parseNestedRuleList(source, original, baseOffset, context)
	}
	_, err := parser.parseNestedStyleContents(source, original, baseOffset, context)
	return err
}

func (parser *stylesheetParser) parseNestedStyleContents(source, original string, baseOffset int, context ruleContext) (bool, error) {
	values, err := ParseComponentValues(original)
	if err != nil {
		return false, err
	}
	direct := false
	runStart := 0
	candidateStart := 0
	candidateStartByte := 0
	flushDeclarations := func(end int) {
		if end < runStart || end > len(original) {
			return
		}
		sourced, _ := parseSourcedDeclarationList(original[runStart:end], baseOffset+runStart)
		declarations, sources := splitSourcedDeclarations(sourced)
		if len(declarations) == 0 {
			return
		}
		parser.appendRule(context.parentSelectors, declarations, sources, context)
		direct = true
	}
	for index, value := range values {
		if value.Kind == ComponentToken && value.Token.Kind == TokenSemicolon {
			candidateStart = index + 1
			candidateStartByte = value.Span.End
			continue
		}
		if value.Kind != ComponentBlock || value.Token.Kind != TokenOpenCurly || customDeclarationPrefix(values[candidateStart:index]) {
			continue
		}
		flushDeclarations(candidateStartByte)
		if candidateStartByte < 0 || value.Span.End < candidateStartByte || value.Span.End > len(original) || value.Span.End > len(source) {
			return direct, nil
		}
		nested := stylesheetParser{
			source:         source[candidateStartByte:value.Span.End],
			original:       original[candidateStartByte:value.Span.End],
			baseOffset:     baseOffset + candidateStartByte,
			stylesheet:     parser.stylesheet,
			context:        context,
			nesting:        parser.nesting + 1,
			anonymousLayer: parser.anonymousLayer,
			appearance:     parser.appearance,
		}
		if nested.nesting <= maxGroupRuleNesting {
			if err := nested.parseRuleList(); err != nil {
				return direct, err
			}
		}
		runStart = value.Span.End
		candidateStart = index + 1
		candidateStartByte = value.Span.End
	}
	flushDeclarations(len(original))
	return direct, nil
}

func parseImportRule(source string) (ImportRule, bool) {
	values, err := ParseComponentValues(source)
	if err != nil {
		return ImportRule{}, false
	}
	values = trimComponentWhitespace(values)
	if len(values) == 0 {
		return ImportRule{}, false
	}
	imported := ImportRule{}
	first := values[0]
	switch {
	case first.Kind == ComponentToken && (first.Token.Kind == TokenString || first.Token.Kind == TokenURL):
		imported.URL = first.Token.Value
	case first.Kind == ComponentFunction && equalASCIIFold(first.Token.Value, "url"):
		arguments := trimComponentWhitespace(first.Values)
		if len(arguments) != 1 || arguments[0].Kind != ComponentToken || arguments[0].Token.Kind != TokenString {
			return ImportRule{}, false
		}
		imported.URL = arguments[0].Token.Value
	default:
		return ImportRule{}, false
	}
	if imported.URL == "" {
		return ImportRule{}, false
	}

	remaining := trimComponentWhitespace(values[1:])
	if len(remaining) > 0 {
		if remaining[0].Kind == ComponentToken && remaining[0].Token.Kind == TokenIdent && equalASCIIFold(remaining[0].Token.Value, "layer") {
			imported.Layered = true
			remaining = trimComponentWhitespace(remaining[1:])
		} else if remaining[0].Kind == ComponentFunction && equalASCIIFold(remaining[0].Token.Value, "layer") {
			arguments := trimComponentWhitespace(remaining[0].Values)
			name, ok := parseLayerName(arguments)
			if !ok {
				return ImportRule{}, false
			}
			imported.Layered = true
			imported.Layer = name
			remaining = trimComponentWhitespace(remaining[1:])
		}
	}
	if len(remaining) > 0 && remaining[0].Kind == ComponentFunction && equalASCIIFold(remaining[0].Token.Value, "supports") {
		arguments := trimComponentWhitespace(remaining[0].Values)
		if len(arguments) == 0 {
			return ImportRule{}, false
		}
		start := arguments[0].Span.Start
		end := arguments[len(arguments)-1].Span.End
		if start < 0 || end < start || end > len(source) {
			return ImportRule{}, false
		}
		imported.Supports = strings.TrimSpace(source[start:end])
		remaining = trimComponentWhitespace(remaining[1:])
	}
	if len(remaining) > 0 {
		start := remaining[0].Span.Start
		end := remaining[len(remaining)-1].Span.End
		if start < 0 || end < start || end > len(source) {
			return ImportRule{}, false
		}
		imported.Media = strings.TrimSpace(source[start:end])
	}
	return imported, true
}

func (parser *stylesheetParser) parseNestedRuleList(source, original string, baseOffset int, context ruleContext) error {
	if parser.nesting >= maxGroupRuleNesting {
		return nil
	}
	nested := stylesheetParser{
		source:         source,
		original:       original,
		baseOffset:     baseOffset,
		stylesheet:     parser.stylesheet,
		context:        context,
		nesting:        parser.nesting + 1,
		anonymousLayer: parser.anonymousLayer,
		appearance:     parser.appearance,
	}
	return nested.parseRuleList()
}

func (parser *stylesheetParser) recordLayer(name string) {
	if name == "" {
		return
	}
	if separator := strings.LastIndexByte(name, '.'); separator >= 0 {
		parser.recordLayer(name[:separator])
	}
	parser.stylesheet.LayerDeclarations = append(parser.stylesheet.LayerDeclarations, LayerDeclaration{
		Name: name, Media: append([]string(nil), parser.context.media...),
		Supports: append([]string(nil), parser.context.supports...),
		Order:    parser.nextAppearanceOrder(),
	})
	for _, existing := range parser.stylesheet.LayerOrder {
		if existing == name {
			return
		}
	}
	if separator := strings.LastIndexByte(name, '.'); separator >= 0 {
		parent := name[:separator]
		for index, existing := range parser.stylesheet.LayerOrder {
			if existing == parent {
				parser.stylesheet.LayerOrder = append(parser.stylesheet.LayerOrder, "")
				copy(parser.stylesheet.LayerOrder[index+1:], parser.stylesheet.LayerOrder[index:])
				parser.stylesheet.LayerOrder[index] = name
				return
			}
		}
	}
	parser.stylesheet.LayerOrder = append(parser.stylesheet.LayerOrder, name)
}

func (parser *stylesheetParser) nextAppearanceOrder() int {
	if parser.appearance == nil {
		appearance := 0
		parser.appearance = &appearance
	}
	order := *parser.appearance
	*parser.appearance = order + 1
	return order
}

func parseLayerNameList(source string) ([]string, bool) {
	values, err := ParseComponentValues(source)
	if err != nil {
		return nil, false
	}
	groups := splitLayerNameGroups(values)
	if len(groups) == 0 {
		return nil, false
	}
	layers := make([]string, 0, len(groups))
	for _, group := range groups {
		name, ok := parseLayerName(group)
		if !ok {
			return nil, false
		}
		layers = append(layers, name)
	}
	return layers, true
}

func splitLayerNameGroups(values []ComponentValue) [][]ComponentValue {
	groups := make([][]ComponentValue, 0, 1)
	start := 0
	for index, value := range values {
		if value.Kind == ComponentToken && value.Token.Kind == TokenComma {
			groups = append(groups, trimComponentWhitespace(values[start:index]))
			start = index + 1
		}
	}
	groups = append(groups, trimComponentWhitespace(values[start:]))
	return groups
}

func parseLayerName(values []ComponentValue) (string, bool) {
	values = trimComponentWhitespace(values)
	if len(values) == 0 {
		return "", false
	}
	parts := make([]string, 0, (len(values)+1)/2)
	expectIdentifier := true
	for _, value := range values {
		if isWhitespaceComponent(value) {
			return "", false
		}
		if expectIdentifier {
			if value.Kind != ComponentToken || value.Token.Kind != TokenIdent || !validLayerIdentifier(value.Token.Value) {
				return "", false
			}
			parts = append(parts, value.Token.Value)
			expectIdentifier = false
			continue
		}
		if value.Kind != ComponentToken || value.Token.Kind != TokenDelim || value.Token.Value != "." {
			return "", false
		}
		expectIdentifier = true
	}
	if expectIdentifier {
		return "", false
	}
	return strings.Join(parts, "."), true
}

func validLayerIdentifier(source string) bool {
	if source == "" || strings.Contains(source, ".") {
		// The flattened public layer identity uses dots as path separators.
		// Escaped literal dots wait for a segment-preserving layer-path type.
		return false
	}
	switch {
	case equalASCIIFold(source, "initial"), equalASCIIFold(source, "inherit"),
		equalASCIIFold(source, "unset"), equalASCIIFold(source, "revert"),
		equalASCIIFold(source, "revert-layer"):
		return false
	}
	return true
}

func qualifyLayerName(parent, child string) string {
	if parent == "" {
		return child
	}
	return parent + "." + child
}

func (parser *stylesheetParser) newAnonymousLayer() string {
	if parser.anonymousLayer == nil {
		counter := 0
		parser.anonymousLayer = &counter
	}
	*parser.anonymousLayer = *parser.anonymousLayer + 1
	return fmt.Sprintf("\x00layer-%d", *parser.anonymousLayer)
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
	sourced, _ := parseSourcedDeclarationList(block, 0)
	declarations, _ := splitSourcedDeclarations(sourced)
	return declarations
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
			// Preserve byte offsets. Shared CSS Syntax preprocessing handles raw
			// NULs when declaration values are tokenized; legacy parsers treat this
			// byte as a non-merging boundary until they migrate to tokens.
			cleaned.WriteByte(commentBoundary)
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
			commentLength := end + 4
			for range commentLength {
				cleaned.WriteByte(commentBoundary)
			}
			position += commentLength
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
	boundary := false
	for position := 0; position < len(source); position++ {
		if source[position] == commentBoundary {
			if !boundary {
				normalized.WriteByte(' ')
				boundary = true
			}
			continue
		}
		boundary = false
		normalized.WriteByte(source[position])
	}
	return normalized.String()
}
