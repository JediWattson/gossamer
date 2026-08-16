package css

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
	"unicode/utf8"
)

const (
	maxCSSSyntaxInputBytes = 16 << 20
	maxCSSSyntaxTokens     = 1 << 20
	maxCSSComponentDepth   = 128
)

var (
	ErrCSSSyntaxInputLimit   = errors.New("css: syntax input limit exceeded")
	ErrCSSSyntaxTokenLimit   = errors.New("css: syntax token limit exceeded")
	ErrCSSSyntaxNestingLimit = errors.New("css: component-value nesting limit exceeded")
)

// Span is a half-open byte range in the original CSS source. Byte offsets are
// retained even when token values contain decoded escapes or preprocessed
// replacement characters.
type Span struct {
	Start int
	End   int
}

// Valid reports whether span is a well-formed range within a source of size.
func (span Span) Valid(size int) bool {
	return span.Start >= 0 && span.End >= span.Start && span.End <= size
}

// Slice returns the source covered by span, or an empty string for an invalid
// range.
func (span Span) Slice(source string) string {
	if !span.Valid(len(source)) {
		return ""
	}
	return source[span.Start:span.End]
}

// TokenKind identifies one CSS Syntax token.
type TokenKind uint8

const (
	TokenWhitespace TokenKind = iota + 1
	TokenIdent
	TokenFunction
	TokenAtKeyword
	TokenHash
	TokenString
	TokenBadString
	TokenURL
	TokenBadURL
	TokenDelim
	TokenNumber
	TokenPercentage
	TokenDimension
	TokenColon
	TokenSemicolon
	TokenComma
	TokenOpenParen
	TokenCloseParen
	TokenOpenSquare
	TokenCloseSquare
	TokenOpenCurly
	TokenCloseCurly
	TokenCDO
	TokenCDC
)

func (kind TokenKind) String() string {
	switch kind {
	case TokenWhitespace:
		return "whitespace"
	case TokenIdent:
		return "ident"
	case TokenFunction:
		return "function"
	case TokenAtKeyword:
		return "at-keyword"
	case TokenHash:
		return "hash"
	case TokenString:
		return "string"
	case TokenBadString:
		return "bad-string"
	case TokenURL:
		return "url"
	case TokenBadURL:
		return "bad-url"
	case TokenDelim:
		return "delim"
	case TokenNumber:
		return "number"
	case TokenPercentage:
		return "percentage"
	case TokenDimension:
		return "dimension"
	case TokenColon:
		return "colon"
	case TokenSemicolon:
		return "semicolon"
	case TokenComma:
		return "comma"
	case TokenOpenParen:
		return "open-paren"
	case TokenCloseParen:
		return "close-paren"
	case TokenOpenSquare:
		return "open-square"
	case TokenCloseSquare:
		return "close-square"
	case TokenOpenCurly:
		return "open-curly"
	case TokenCloseCurly:
		return "close-curly"
	case TokenCDO:
		return "CDO"
	case TokenCDC:
		return "CDC"
	default:
		return "unknown"
	}
}

// Token is one decoded CSS Syntax token. Value contains the decoded identifier,
// string, URL, hash, dimension unit, or delimiter. Representation retains a
// numeric token's source spelling; Number is its parsed value. Identifier is
// the CSS Syntax id flag for hash tokens. Incomplete marks an EOF-closed string
// while preserving its spec token kind for recovery.
type Token struct {
	Kind           TokenKind
	Span           Span
	Value          string
	Representation string
	Number         float64
	Integer        bool
	Identifier     bool
	Incomplete     bool
}

// ComponentValueKind distinguishes preserved tokens from grouped functions
// and simple blocks.
type ComponentValueKind uint8

const (
	ComponentToken ComponentValueKind = iota + 1
	ComponentFunction
	ComponentBlock
)

// ComponentValue is one recursively grouped CSS component value. Token is the
// original token for token values, the function token for functions, or the
// opening token for blocks. EOF implicitly closes functions and blocks.
type ComponentValue struct {
	Kind   ComponentValueKind
	Span   Span
	Token  Token
	Values []ComponentValue
}

// Tokenize converts CSS source into decoded tokens with original byte spans.
// Comments are consumed without producing whitespace, preserving token
// boundaries naturally. Safely produced prefix tokens accompany a lexical
// error so callers can recover earlier declarations.
func Tokenize(source string) ([]Token, error) {
	if len(source) > maxCSSSyntaxInputBytes {
		return nil, ErrCSSSyntaxInputLimit
	}
	tokenizer := cssTokenizer{source: source}
	tokenizer.run()
	return tokenizer.tokens, tokenizer.err
}

// ParseComponentValues tokenizes source and recursively groups functions and
// simple blocks. A lexical error and safely grouped prefix values can be
// returned together.
func ParseComponentValues(source string) ([]ComponentValue, error) {
	tokens, tokenErr := Tokenize(source)
	position := 0
	values, _, componentErr := consumeComponentValues(tokens, &position, 0, 0, len(source))
	return values, errors.Join(tokenErr, componentErr)
}

type cssTokenizer struct {
	source string
	pos    int
	tokens []Token
	err    error
}

func (tokenizer *cssTokenizer) run() {
	for tokenizer.pos < len(tokenizer.source) && tokenizer.err == nil {
		if len(tokenizer.tokens) >= maxCSSSyntaxTokens {
			tokenizer.err = ErrCSSSyntaxTokenLimit
			return
		}
		if startsCSSComment(tokenizer.source, tokenizer.pos) {
			next, closed := skipCSSComment(tokenizer.source, tokenizer.pos)
			if !closed {
				tokenizer.err = fmt.Errorf("css: unterminated comment at byte %d", tokenizer.pos)
				return
			}
			tokenizer.pos = next
			continue
		}
		tokenizer.tokens = append(tokenizer.tokens, tokenizer.consumeToken())
	}
}

func (tokenizer *cssTokenizer) consumeToken() Token {
	start := tokenizer.pos
	if tokenizer.whitespaceAt(tokenizer.pos) {
		for tokenizer.pos < len(tokenizer.source) && tokenizer.whitespaceAt(tokenizer.pos) {
			tokenizer.pos = tokenizer.consumeWhitespace(tokenizer.pos)
		}
		return Token{Kind: TokenWhitespace, Span: Span{Start: start, End: tokenizer.pos}, Value: " "}
	}

	if strings.HasPrefix(tokenizer.source[start:], "<!--") {
		tokenizer.pos += 4
		return tokenizer.token(TokenCDO, start, "")
	}
	if strings.HasPrefix(tokenizer.source[start:], "-->") {
		tokenizer.pos += 3
		return tokenizer.token(TokenCDC, start, "")
	}

	character := tokenizer.source[start]
	switch character {
	case '\'', '"':
		return tokenizer.consumeString(character)
	case '#':
		if tokenizer.wouldStartName(start + 1) {
			identifier := tokenizer.wouldStartIdentifier(start + 1)
			tokenizer.pos++
			value := tokenizer.consumeName()
			token := tokenizer.token(TokenHash, start, value)
			token.Identifier = identifier
			return token
		}
		tokenizer.pos++
		return tokenizer.token(TokenDelim, start, "#")
	case '@':
		if tokenizer.wouldStartIdentifier(start + 1) {
			tokenizer.pos++
			value := tokenizer.consumeName()
			return tokenizer.token(TokenAtKeyword, start, value)
		}
		tokenizer.pos++
		return tokenizer.token(TokenDelim, start, "@")
	case ':':
		tokenizer.pos++
		return tokenizer.token(TokenColon, start, "")
	case ';':
		tokenizer.pos++
		return tokenizer.token(TokenSemicolon, start, "")
	case ',':
		tokenizer.pos++
		return tokenizer.token(TokenComma, start, "")
	case '(':
		tokenizer.pos++
		return tokenizer.token(TokenOpenParen, start, "")
	case ')':
		tokenizer.pos++
		return tokenizer.token(TokenCloseParen, start, "")
	case '[':
		tokenizer.pos++
		return tokenizer.token(TokenOpenSquare, start, "")
	case ']':
		tokenizer.pos++
		return tokenizer.token(TokenCloseSquare, start, "")
	case '{':
		tokenizer.pos++
		return tokenizer.token(TokenOpenCurly, start, "")
	case '}':
		tokenizer.pos++
		return tokenizer.token(TokenCloseCurly, start, "")
	}

	if tokenizer.wouldStartNumber(start) {
		return tokenizer.consumeNumeric()
	}
	if tokenizer.wouldStartIdentifier(start) {
		return tokenizer.consumeIdentLike()
	}

	runeValue, width := tokenizer.preprocessedRune(start)
	tokenizer.pos += width
	return tokenizer.token(TokenDelim, start, string(runeValue))
}

func (tokenizer *cssTokenizer) consumeIdentLike() Token {
	start := tokenizer.pos
	value := tokenizer.consumeName()
	if tokenizer.pos >= len(tokenizer.source) || tokenizer.source[tokenizer.pos] != '(' {
		return tokenizer.token(TokenIdent, start, value)
	}
	tokenizer.pos++
	if equalASCIIFold(value, "url") && tokenizer.unquotedURLStarts(tokenizer.pos) {
		return tokenizer.consumeURL(start)
	}
	return tokenizer.token(TokenFunction, start, value)
}

func (tokenizer *cssTokenizer) consumeString(quote byte) Token {
	start := tokenizer.pos
	tokenizer.pos++
	var value strings.Builder
	for tokenizer.pos < len(tokenizer.source) {
		character := tokenizer.source[tokenizer.pos]
		switch {
		case character == quote:
			tokenizer.pos++
			return tokenizer.token(TokenString, start, value.String())
		case character == '\n' || character == '\r' || character == '\f':
			return tokenizer.token(TokenBadString, start, value.String())
		case character == '\\':
			if tokenizer.pos+1 >= len(tokenizer.source) {
				tokenizer.pos++
				tokenizer.err = fmt.Errorf("css: unterminated string at byte %d", start)
				token := tokenizer.token(TokenString, start, value.String())
				token.Incomplete = true
				return token
			}
			if tokenizer.escapedNewlineAt(tokenizer.pos) {
				tokenizer.pos = tokenizer.consumeEscapedNewline(tokenizer.pos)
				continue
			}
			decoded, next := tokenizer.consumeEscape(tokenizer.pos)
			value.WriteRune(decoded)
			tokenizer.pos = next
		default:
			runeValue, width := tokenizer.preprocessedRune(tokenizer.pos)
			value.WriteRune(runeValue)
			tokenizer.pos += width
		}
	}
	tokenizer.err = fmt.Errorf("css: unterminated string at byte %d", start)
	token := tokenizer.token(TokenString, start, value.String())
	token.Incomplete = true
	return token
}

func (tokenizer *cssTokenizer) consumeNumeric() Token {
	start := tokenizer.pos
	numberEnd, integer := consumeCSSNumber(tokenizer.source, tokenizer.pos)
	representation := tokenizer.source[start:numberEnd]
	number, _ := strconv.ParseFloat(representation, 64)
	tokenizer.pos = numberEnd
	if tokenizer.wouldStartIdentifier(tokenizer.pos) {
		unit := tokenizer.consumeName()
		token := tokenizer.token(TokenDimension, start, unit)
		token.Number = number
		token.Integer = integer
		token.Representation = representation
		return token
	}
	if tokenizer.pos < len(tokenizer.source) && tokenizer.source[tokenizer.pos] == '%' {
		tokenizer.pos++
		token := tokenizer.token(TokenPercentage, start, "")
		token.Number = number
		token.Integer = integer
		token.Representation = representation
		return token
	}
	token := tokenizer.token(TokenNumber, start, "")
	token.Number = number
	token.Integer = integer
	token.Representation = representation
	return token
}

func (tokenizer *cssTokenizer) consumeURL(start int) Token {
	for tokenizer.pos < len(tokenizer.source) && tokenizer.whitespaceAt(tokenizer.pos) {
		tokenizer.pos = tokenizer.consumeWhitespace(tokenizer.pos)
	}
	var value strings.Builder
	for tokenizer.pos < len(tokenizer.source) {
		character := tokenizer.source[tokenizer.pos]
		switch {
		case character == ')':
			tokenizer.pos++
			return tokenizer.token(TokenURL, start, value.String())
		case tokenizer.whitespaceAt(tokenizer.pos):
			for tokenizer.pos < len(tokenizer.source) && tokenizer.whitespaceAt(tokenizer.pos) {
				tokenizer.pos = tokenizer.consumeWhitespace(tokenizer.pos)
			}
			if tokenizer.pos >= len(tokenizer.source) {
				return tokenizer.token(TokenURL, start, value.String())
			}
			if tokenizer.source[tokenizer.pos] == ')' {
				tokenizer.pos++
				return tokenizer.token(TokenURL, start, value.String())
			}
			return tokenizer.consumeBadURL(start)
		case character == '\'' || character == '"' || character == '(' || isCSSNonPrintable(character):
			return tokenizer.consumeBadURL(start)
		case character == '\\':
			if !tokenizer.validEscapeAt(tokenizer.pos) {
				return tokenizer.consumeBadURL(start)
			}
			decoded, next := tokenizer.consumeEscape(tokenizer.pos)
			value.WriteRune(decoded)
			tokenizer.pos = next
		default:
			runeValue, width := tokenizer.preprocessedRune(tokenizer.pos)
			value.WriteRune(runeValue)
			tokenizer.pos += width
		}
	}
	return tokenizer.token(TokenURL, start, value.String())
}

func (tokenizer *cssTokenizer) consumeBadURL(start int) Token {
	for tokenizer.pos < len(tokenizer.source) {
		if tokenizer.source[tokenizer.pos] == ')' {
			tokenizer.pos++
			break
		}
		if tokenizer.source[tokenizer.pos] == '\\' && tokenizer.validEscapeAt(tokenizer.pos) {
			_, tokenizer.pos = tokenizer.consumeEscape(tokenizer.pos)
			continue
		}
		_, width := tokenizer.preprocessedRune(tokenizer.pos)
		tokenizer.pos += width
	}
	return tokenizer.token(TokenBadURL, start, "")
}

func (tokenizer *cssTokenizer) consumeName() string {
	var value strings.Builder
	for tokenizer.pos < len(tokenizer.source) {
		character, width := tokenizer.preprocessedRune(tokenizer.pos)
		if isCSSNameRune(character) || tokenizer.source[tokenizer.pos] == 0 {
			value.WriteRune(character)
			tokenizer.pos += width
			continue
		}
		if tokenizer.source[tokenizer.pos] != '\\' || !tokenizer.validEscapeAt(tokenizer.pos) {
			break
		}
		decoded, next := tokenizer.consumeEscape(tokenizer.pos)
		value.WriteRune(decoded)
		tokenizer.pos = next
	}
	return value.String()
}

func (tokenizer *cssTokenizer) wouldStartIdentifier(position int) bool {
	if position >= len(tokenizer.source) {
		return false
	}
	first, firstWidth := tokenizer.preprocessedRune(position)
	if isCSSNameStartRune(first) || tokenizer.source[position] == 0 {
		return true
	}
	if first == '\\' {
		return tokenizer.validEscapeAt(position)
	}
	if first != '-' {
		return false
	}
	next := position + firstWidth
	if next >= len(tokenizer.source) {
		return false
	}
	second, _ := tokenizer.preprocessedRune(next)
	return second == '-' || isCSSNameStartRune(second) || tokenizer.source[next] == 0 ||
		second == '\\' && tokenizer.validEscapeAt(next)
}

func (tokenizer *cssTokenizer) wouldStartName(position int) bool {
	if position >= len(tokenizer.source) {
		return false
	}
	character, _ := tokenizer.preprocessedRune(position)
	return isCSSNameRune(character) || tokenizer.source[position] == 0 ||
		character == '\\' && tokenizer.validEscapeAt(position)
}

func (tokenizer *cssTokenizer) wouldStartNumber(position int) bool {
	return wouldStartCSSNumber(tokenizer.source, position)
}

func (tokenizer *cssTokenizer) unquotedURLStarts(position int) bool {
	for position < len(tokenizer.source) && tokenizer.whitespaceAt(position) {
		position = tokenizer.consumeWhitespace(position)
	}
	return position >= len(tokenizer.source) || tokenizer.source[position] != '\'' && tokenizer.source[position] != '"'
}

func (tokenizer *cssTokenizer) validEscapeAt(position int) bool {
	if position < 0 || position >= len(tokenizer.source) || tokenizer.source[position] != '\\' || position+1 >= len(tokenizer.source) {
		return false
	}
	return !tokenizer.newlineAt(position + 1)
}

func (tokenizer *cssTokenizer) consumeEscape(position int) (rune, int) {
	position++
	if position >= len(tokenizer.source) {
		return utf8.RuneError, position
	}
	if isHexDigit(tokenizer.source[position]) {
		value := rune(0)
		count := 0
		for position < len(tokenizer.source) && count < 6 && isHexDigit(tokenizer.source[position]) {
			value = value*16 + rune(hexDigitValue(tokenizer.source[position]))
			position++
			count++
		}
		if position < len(tokenizer.source) && tokenizer.whitespaceAt(position) {
			position = tokenizer.consumeWhitespace(position)
		}
		if value == 0 || value > utf8.MaxRune || value >= 0xd800 && value <= 0xdfff {
			value = utf8.RuneError
		}
		return value, position
	}
	character, width := tokenizer.preprocessedRune(position)
	return character, position + width
}

func (tokenizer *cssTokenizer) preprocessedRune(position int) (rune, int) {
	if position >= len(tokenizer.source) {
		return utf8.RuneError, 0
	}
	if tokenizer.source[position] == 0 {
		return utf8.RuneError, 1
	}
	if tokenizer.source[position] == '\r' {
		if position+1 < len(tokenizer.source) && tokenizer.source[position+1] == '\n' {
			return '\n', 2
		}
		return '\n', 1
	}
	if tokenizer.source[position] == '\f' {
		return '\n', 1
	}
	character, width := utf8.DecodeRuneInString(tokenizer.source[position:])
	return character, width
}

func (tokenizer *cssTokenizer) whitespaceAt(position int) bool {
	if position >= len(tokenizer.source) {
		return false
	}
	switch tokenizer.source[position] {
	case ' ', '\t', '\n', '\r', '\f':
		return true
	default:
		return false
	}
}

func (tokenizer *cssTokenizer) consumeWhitespace(position int) int {
	if tokenizer.source[position] == '\r' && position+1 < len(tokenizer.source) && tokenizer.source[position+1] == '\n' {
		return position + 2
	}
	return position + 1
}

func (tokenizer *cssTokenizer) newlineAt(position int) bool {
	if position >= len(tokenizer.source) {
		return false
	}
	switch tokenizer.source[position] {
	case '\n', '\r', '\f':
		return true
	default:
		return false
	}
}

func (tokenizer *cssTokenizer) escapedNewlineAt(position int) bool {
	return position+1 < len(tokenizer.source) && tokenizer.source[position] == '\\' && tokenizer.newlineAt(position+1)
}

func (tokenizer *cssTokenizer) consumeEscapedNewline(position int) int {
	position++
	if tokenizer.source[position] == '\r' && position+1 < len(tokenizer.source) && tokenizer.source[position+1] == '\n' {
		return position + 2
	}
	return position + 1
}

func (tokenizer *cssTokenizer) token(kind TokenKind, start int, value string) Token {
	return Token{Kind: kind, Span: Span{Start: start, End: tokenizer.pos}, Value: value}
}

func consumeCSSNumber(source string, position int) (int, bool) {
	integer := true
	if position < len(source) && (source[position] == '+' || source[position] == '-') {
		position++
	}
	for position < len(source) && isASCIIDigit(source[position]) {
		position++
	}
	if position+1 < len(source) && source[position] == '.' && isASCIIDigit(source[position+1]) {
		integer = false
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
			integer = false
			position = exponent + 1
			for position < len(source) && isASCIIDigit(source[position]) {
				position++
			}
		}
	}
	return position, integer
}

func consumeComponentValues(tokens []Token, position *int, ending TokenKind, depth, sourceSize int) ([]ComponentValue, bool, error) {
	if depth > maxCSSComponentDepth {
		return nil, false, ErrCSSSyntaxNestingLimit
	}
	values := make([]ComponentValue, 0)
	for *position < len(tokens) {
		token := tokens[*position]
		if ending != 0 && token.Kind == ending {
			*position++
			return values, true, nil
		}
		*position++
		closing := matchingComponentClose(token.Kind)
		if token.Kind == TokenFunction {
			closing = TokenCloseParen
		}
		if closing == 0 {
			values = append(values, ComponentValue{Kind: ComponentToken, Span: token.Span, Token: token})
			continue
		}
		children, closed, err := consumeComponentValues(tokens, position, closing, depth+1, sourceSize)
		end := sourceSize
		if closed {
			end = tokens[*position-1].Span.End
		}
		kind := ComponentBlock
		if token.Kind == TokenFunction {
			kind = ComponentFunction
		}
		values = append(values, ComponentValue{
			Kind: kind, Span: Span{Start: token.Span.Start, End: end}, Token: token, Values: children,
		})
		if err != nil {
			return values, false, err
		}
	}
	return values, false, nil
}

func matchingComponentClose(kind TokenKind) TokenKind {
	switch kind {
	case TokenOpenParen:
		return TokenCloseParen
	case TokenOpenSquare:
		return TokenCloseSquare
	case TokenOpenCurly:
		return TokenCloseCurly
	default:
		return 0
	}
}
