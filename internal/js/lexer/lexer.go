package lexer

import (
	"errors"
	"fmt"
	"math"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"
)

var ErrInvalidToken = errors.New("js/lexer: invalid token")

type Error struct {
	Span    Span
	Message string
}

func (problem *Error) Error() string {
	if problem == nil {
		return ErrInvalidToken.Error()
	}
	return fmt.Sprintf("%s at %d:%d", problem.Message, problem.Span.Start.Line, problem.Span.Start.Column)
}

func (problem *Error) Unwrap() error { return ErrInvalidToken }

type scanner struct {
	source         string
	offset         int
	line           uint32
	column         uint32
	templateFrames []templateFrame
	tolerant       bool
}

type templateFrame struct {
	start      Position
	braceDepth uint32
}

func Lex(source string) ([]Token, error) {
	return lex(source, false)
}

// LexSurface tokenizes enough JavaScript structure to discover module
// specifiers without requiring the source to fit Strand's executable syntax
// subset. Unsupported punctuators and numeric forms remain opaque tokens.
func LexSurface(source string) ([]Token, error) {
	return lex(source, true)
}

func lex(source string, tolerant bool) ([]Token, error) {
	if uint64(len(source)) > math.MaxUint32 {
		return nil, &Error{Span: Span{Start: Position{Line: 1, Column: 1}}, Message: "source exceeds uint32 offsets"}
	}
	input := &scanner{source: source, line: 1, column: 1, tolerant: tolerant}
	tokens := make([]Token, 0, len(source)/3+1)
	allowRegExp := true
	for {
		if err := input.skipTrivia(); err != nil {
			return nil, err
		}
		if input.offset == len(input.source) {
			if len(input.templateFrames) != 0 {
				frame := input.templateFrames[len(input.templateFrames)-1]
				return nil, input.problem(frame.start, "unterminated template substitution")
			}
			position := input.position()
			tokens = append(tokens, Token{Kind: EOF, Span: Span{Start: position, End: position}})
			return tokens, nil
		}
		start := input.position()
		r, _ := input.peekRune()
		var token Token
		var err error
		switch {
		case r == '}' && input.atTemplateBoundary():
			token, err = input.scanTemplateContinuation(start)
		case isIdentifierStart(r):
			token = input.scanIdentifier(start)
		case unicode.IsDigit(r) || r == '.' && input.nextRuneIsDigit():
			token, err = input.scanNumber(start)
		case r == '\'' || r == '"':
			token, err = input.scanString(start, r)
		case r == '`':
			token, err = input.scanTemplate(start)
		case r == '/' && allowRegExp && !strings.HasPrefix(input.source[input.offset:], "/="):
			token, err = input.scanRegExp(start)
		default:
			token, err = input.scanPunctuator(start)
		}
		if err != nil {
			return nil, err
		}
		input.trackTemplateBrace(token.Kind)
		tokens = append(tokens, token)
		allowRegExp = !tokenEndsExpression(token.Kind)
	}
}

func tokenEndsExpression(kind Kind) bool {
	switch kind {
	case Identifier, Number, String, TemplateTail, RegExp, True, False, Null, This,
		RightParen, RightBracket, RightBrace, PlusPlus, MinusMinus:
		return true
	default:
		return false
	}
}

func (input *scanner) atTemplateBoundary() bool {
	if len(input.templateFrames) == 0 {
		return false
	}
	return input.templateFrames[len(input.templateFrames)-1].braceDepth == 0
}

func (input *scanner) trackTemplateBrace(kind Kind) {
	if len(input.templateFrames) == 0 {
		return
	}
	frame := &input.templateFrames[len(input.templateFrames)-1]
	switch kind {
	case LeftBrace:
		frame.braceDepth++
	case RightBrace:
		if frame.braceDepth > 0 {
			frame.braceDepth--
		}
	}
}

func (input *scanner) position() Position {
	return Position{Offset: uint32(input.offset), Line: input.line, Column: input.column}
}

func (input *scanner) peekRune() (rune, int) {
	if input.offset >= len(input.source) {
		return 0, 0
	}
	r, width := utf8.DecodeRuneInString(input.source[input.offset:])
	return r, width
}

func (input *scanner) advance() (rune, error) {
	if input.offset >= len(input.source) {
		return 0, nil
	}
	r, width := utf8.DecodeRuneInString(input.source[input.offset:])
	if r == utf8.RuneError && width == 1 {
		start := input.position()
		return 0, input.problem(start, "invalid UTF-8")
	}
	input.offset += width
	if r == '\r' {
		if input.offset < len(input.source) && input.source[input.offset] == '\n' {
			input.offset++
		}
		input.line++
		input.column = 1
	} else if r == '\n' {
		input.line++
		input.column = 1
	} else {
		input.column++
	}
	return r, nil
}

func (input *scanner) problem(start Position, message string) error {
	return &Error{Span: Span{Start: start, End: input.position()}, Message: message}
}

func (input *scanner) skipTrivia() error {
	for input.offset < len(input.source) {
		r, width := input.peekRune()
		if r == utf8.RuneError && width == 1 {
			start := input.position()
			return input.problem(start, "invalid UTF-8")
		}
		if unicode.IsSpace(r) {
			if _, err := input.advance(); err != nil {
				return err
			}
			continue
		}
		if !strings.HasPrefix(input.source[input.offset:], "//") && !strings.HasPrefix(input.source[input.offset:], "/*") {
			return nil
		}
		start := input.position()
		if strings.HasPrefix(input.source[input.offset:], "//") {
			_, _ = input.advance()
			_, _ = input.advance()
			for input.offset < len(input.source) {
				r, _ := input.peekRune()
				if r == '\n' || r == '\r' {
					break
				}
				if _, err := input.advance(); err != nil {
					return err
				}
			}
			continue
		}
		_, _ = input.advance()
		_, _ = input.advance()
		closed := false
		for input.offset < len(input.source) {
			if strings.HasPrefix(input.source[input.offset:], "*/") {
				_, _ = input.advance()
				_, _ = input.advance()
				closed = true
				break
			}
			if _, err := input.advance(); err != nil {
				return err
			}
		}
		if !closed {
			return input.problem(start, "unterminated block comment")
		}
	}
	return nil
}

func isIdentifierStart(r rune) bool {
	return r == '$' || r == '_' || unicode.IsLetter(r) || unicode.In(r, unicode.Nl)
}

func isIdentifierContinue(r rune) bool {
	return isIdentifierStart(r) || unicode.IsDigit(r) || unicode.In(r, unicode.Mn, unicode.Mc, unicode.Pc) || r == '\u200c' || r == '\u200d'
}

func (input *scanner) scanIdentifier(start Position) Token {
	for input.offset < len(input.source) {
		r, _ := input.peekRune()
		if !isIdentifierContinue(r) {
			break
		}
		_, _ = input.advance()
	}
	lexeme := input.source[start.Offset:input.offset]
	kind := Identifier
	if keyword, exists := keywords[lexeme]; exists {
		kind = keyword
	}
	return Token{Kind: kind, Lexeme: lexeme, Text: lexeme, Span: Span{Start: start, End: input.position()}}
}

func (input *scanner) nextRuneIsDigit() bool {
	_, width := input.peekRune()
	if width == 0 || input.offset+width >= len(input.source) {
		return false
	}
	r, _ := utf8.DecodeRuneInString(input.source[input.offset+width:])
	return unicode.IsDigit(r)
}

func (input *scanner) scanNumber(start Position) (Token, error) {
	if strings.HasPrefix(input.source[input.offset:], "0x") || strings.HasPrefix(input.source[input.offset:], "0X") {
		return input.scanRadixNumber(start, 16, 2)
	}
	if strings.HasPrefix(input.source[input.offset:], "0b") || strings.HasPrefix(input.source[input.offset:], "0B") {
		return input.scanRadixNumber(start, 2, 2)
	}
	if strings.HasPrefix(input.source[input.offset:], "0o") || strings.HasPrefix(input.source[input.offset:], "0O") {
		return input.scanRadixNumber(start, 8, 2)
	}
	seenDot := false
	if input.source[input.offset] == '.' {
		seenDot = true
		_, _ = input.advance()
	}
	for input.offset < len(input.source) {
		r, _ := input.peekRune()
		if !unicode.IsDigit(r) {
			break
		}
		_, _ = input.advance()
	}
	if !seenDot && input.offset < len(input.source) && input.source[input.offset] == '.' {
		seenDot = true
		_, _ = input.advance()
		for input.offset < len(input.source) {
			r, _ := input.peekRune()
			if !unicode.IsDigit(r) {
				break
			}
			_, _ = input.advance()
		}
	}
	if input.offset < len(input.source) && (input.source[input.offset] == 'e' || input.source[input.offset] == 'E') {
		_, _ = input.advance()
		if input.offset < len(input.source) && (input.source[input.offset] == '+' || input.source[input.offset] == '-') {
			_, _ = input.advance()
		}
		digitStart := input.offset
		for input.offset < len(input.source) {
			r, _ := input.peekRune()
			if !unicode.IsDigit(r) {
				break
			}
			_, _ = input.advance()
		}
		if digitStart == input.offset {
			return Token{}, input.problem(start, "malformed numeric exponent")
		}
	}
	if input.offset < len(input.source) {
		r, _ := input.peekRune()
		if isIdentifierStart(r) {
			if input.tolerant {
				for input.offset < len(input.source) {
					r, _ = input.peekRune()
					if !isIdentifierContinue(r) && !unicode.IsDigit(r) {
						break
					}
					_, _ = input.advance()
				}
				return Token{Kind: Number, Lexeme: input.source[start.Offset:input.offset], Span: Span{Start: start, End: input.position()}}, nil
			}
			return Token{}, input.problem(start, "identifier cannot immediately follow a number")
		}
	}
	lexeme := input.source[start.Offset:input.offset]
	value, err := strconv.ParseFloat(lexeme, 64)
	if err != nil {
		return Token{}, input.problem(start, "malformed number")
	}
	return Token{Kind: Number, Lexeme: lexeme, Number: value, Span: Span{Start: start, End: input.position()}}, nil
}

func (input *scanner) scanRadixNumber(start Position, base, prefix int) (Token, error) {
	for index := 0; index < prefix; index++ {
		_, _ = input.advance()
	}
	digitStart := input.offset
	for input.offset < len(input.source) {
		r, _ := input.peekRune()
		if digitValue(r) < 0 || digitValue(r) >= base {
			break
		}
		_, _ = input.advance()
	}
	if digitStart == input.offset {
		return Token{}, input.problem(start, "radix literal requires digits")
	}
	if input.offset < len(input.source) {
		r, _ := input.peekRune()
		if isIdentifierContinue(r) || unicode.IsDigit(r) {
			if input.tolerant {
				for input.offset < len(input.source) {
					r, _ = input.peekRune()
					if !isIdentifierContinue(r) && !unicode.IsDigit(r) {
						break
					}
					_, _ = input.advance()
				}
				return Token{Kind: Number, Lexeme: input.source[start.Offset:input.offset], Span: Span{Start: start, End: input.position()}}, nil
			}
			return Token{}, input.problem(start, "invalid radix digit")
		}
	}
	lexeme := input.source[start.Offset:input.offset]
	integer, err := strconv.ParseUint(lexeme[prefix:], base, 64)
	if err != nil {
		return Token{}, input.problem(start, "radix literal is out of range")
	}
	return Token{Kind: Number, Lexeme: lexeme, Number: float64(integer), Span: Span{Start: start, End: input.position()}}, nil
}

func digitValue(r rune) int {
	switch {
	case r >= '0' && r <= '9':
		return int(r - '0')
	case r >= 'a' && r <= 'f':
		return int(r-'a') + 10
	case r >= 'A' && r <= 'F':
		return int(r-'A') + 10
	default:
		return -1
	}
}

func (input *scanner) scanString(start Position, quote rune) (Token, error) {
	_, _ = input.advance()
	var decoded strings.Builder
	for input.offset < len(input.source) {
		r, err := input.advance()
		if err != nil {
			return Token{}, err
		}
		if r == quote {
			return Token{
				Kind:   String,
				Lexeme: input.source[start.Offset:input.offset],
				Text:   decoded.String(),
				Span:   Span{Start: start, End: input.position()},
			}, nil
		}
		if r == '\n' || r == '\r' {
			return Token{}, input.problem(start, "unterminated string literal")
		}
		if r != '\\' {
			decoded.WriteRune(r)
			continue
		}
		escapeStart := input.position()
		escape, err := input.advance()
		if err != nil {
			return Token{}, err
		}
		switch escape {
		case '\n', '\r':
			// Line continuation contributes no character.
		case '\'', '"', '\\', '/':
			decoded.WriteRune(escape)
		case 'n':
			decoded.WriteByte('\n')
		case 'r':
			decoded.WriteByte('\r')
		case 't':
			decoded.WriteByte('\t')
		case 'b':
			decoded.WriteByte('\b')
		case 'f':
			decoded.WriteByte('\f')
		case 'v':
			decoded.WriteByte('\v')
		case '0':
			decoded.WriteByte(0)
		case 'x':
			value, err := input.consumeHex(escapeStart, 2)
			if err != nil {
				return Token{}, err
			}
			decoded.WriteRune(rune(value))
		case 'u':
			value, err := input.consumeUnicodeEscape(escapeStart)
			if err != nil {
				return Token{}, err
			}
			decoded.WriteRune(value)
		default:
			return Token{}, input.problem(escapeStart, fmt.Sprintf("unsupported escape \\%c", escape))
		}
	}
	return Token{}, input.problem(start, "unterminated string literal")
}

func (input *scanner) scanTemplate(start Position) (Token, error) {
	_, _ = input.advance()
	return input.scanTemplateChunk(start, true)
}

func (input *scanner) scanTemplateContinuation(start Position) (Token, error) {
	_, _ = input.advance()
	return input.scanTemplateChunk(start, false)
}

func (input *scanner) scanTemplateChunk(start Position, first bool) (Token, error) {
	var decoded strings.Builder
	for input.offset < len(input.source) {
		if strings.HasPrefix(input.source[input.offset:], "${") {
			_, _ = input.advance()
			_, _ = input.advance()
			kind := TemplateMiddle
			if first {
				kind = TemplateHead
				input.templateFrames = append(input.templateFrames, templateFrame{start: start})
			}
			return Token{
				Kind: kind, Lexeme: input.source[start.Offset:input.offset], Text: decoded.String(),
				Span: Span{Start: start, End: input.position()},
			}, nil
		}
		r, err := input.advance()
		if err != nil {
			return Token{}, err
		}
		if r == '`' {
			kind := String
			if !first {
				kind = TemplateTail
				input.templateFrames = input.templateFrames[:len(input.templateFrames)-1]
			}
			return Token{Kind: kind, Lexeme: input.source[start.Offset:input.offset], Text: decoded.String(), Span: Span{Start: start, End: input.position()}}, nil
		}
		if r != '\\' {
			decoded.WriteRune(r)
			continue
		}
		escapeStart := input.position()
		escape, err := input.advance()
		if err != nil {
			return Token{}, err
		}
		switch escape {
		case '\n', '\r':
		case '`', '\\', '$', '/', '\'', '"':
			decoded.WriteRune(escape)
		case 'n':
			decoded.WriteByte('\n')
		case 'r':
			decoded.WriteByte('\r')
		case 't':
			decoded.WriteByte('\t')
		case 'b':
			decoded.WriteByte('\b')
		case 'f':
			decoded.WriteByte('\f')
		case 'v':
			decoded.WriteByte('\v')
		case '0':
			decoded.WriteByte(0)
		case 'x':
			value, err := input.consumeHex(escapeStart, 2)
			if err != nil {
				return Token{}, err
			}
			decoded.WriteRune(rune(value))
		case 'u':
			value, err := input.consumeUnicodeEscape(escapeStart)
			if err != nil {
				return Token{}, err
			}
			decoded.WriteRune(value)
		default:
			return Token{}, input.problem(escapeStart, fmt.Sprintf("unsupported template escape \\%c", escape))
		}
	}
	message := "unterminated template literal"
	if !first {
		message = "unterminated template substitution"
	}
	return Token{}, input.problem(start, message)
}

func (input *scanner) scanRegExp(start Position) (Token, error) {
	_, _ = input.advance()
	patternStart := input.offset
	escaped := false
	inClass := false
	for input.offset < len(input.source) {
		r, err := input.advance()
		if err != nil {
			return Token{}, err
		}
		if r == '\n' || r == '\r' {
			return Token{}, input.problem(start, "unterminated regular expression literal")
		}
		if escaped {
			escaped = false
			continue
		}
		if r == '\\' {
			escaped = true
			continue
		}
		if r == '[' {
			inClass = true
			continue
		}
		if r == ']' {
			inClass = false
			continue
		}
		if r != '/' || inClass {
			continue
		}
		patternEnd := input.offset - 1
		flagsStart := input.offset
		for input.offset < len(input.source) {
			flag, _ := input.peekRune()
			if !unicode.IsLetter(flag) {
				break
			}
			_, _ = input.advance()
		}
		return Token{
			Kind: RegExp, Lexeme: input.source[start.Offset:input.offset],
			Text: input.source[patternStart:patternEnd], Flags: input.source[flagsStart:input.offset],
			Span: Span{Start: start, End: input.position()},
		}, nil
	}
	return Token{}, input.problem(start, "unterminated regular expression literal")
}

func (input *scanner) consumeHex(start Position, count int) (uint64, error) {
	var value uint64
	for index := 0; index < count; index++ {
		r, _ := input.peekRune()
		digit := digitValue(r)
		if digit < 0 || digit >= 16 {
			return 0, input.problem(start, "malformed hexadecimal escape")
		}
		_, _ = input.advance()
		value = value*16 + uint64(digit)
	}
	return value, nil
}

func (input *scanner) consumeUnicodeEscape(start Position) (rune, error) {
	if input.offset < len(input.source) && input.source[input.offset] == '{' {
		_, _ = input.advance()
		digits := 0
		var value uint64
		for input.offset < len(input.source) && input.source[input.offset] != '}' {
			r, _ := input.peekRune()
			digit := digitValue(r)
			if digit < 0 || digit >= 16 || digits == 6 {
				return 0, input.problem(start, "malformed Unicode escape")
			}
			_, _ = input.advance()
			value = value*16 + uint64(digit)
			digits++
		}
		if digits == 0 || input.offset >= len(input.source) || input.source[input.offset] != '}' || value > utf8.MaxRune || value >= 0xd800 && value <= 0xdfff {
			return 0, input.problem(start, "invalid Unicode code point")
		}
		_, _ = input.advance()
		return rune(value), nil
	}
	value, err := input.consumeHex(start, 4)
	if err != nil {
		return 0, err
	}
	if value >= 0xd800 && value <= 0xdfff {
		return 0, input.problem(start, "surrogate escape requires a code-point escape")
	}
	return rune(value), nil
}

func (input *scanner) scanPunctuator(start Position) (Token, error) {
	table := []struct {
		text string
		kind Kind
	}{
		{">>>=", UnsignedShiftRightAssign}, {"===", StrictEqual}, {"!==", StrictNotEqual}, {"...", Ellipsis},
		{"<<=", ShiftLeftAssign}, {">>=", ShiftRightAssign}, {">>>", UnsignedShiftRight},
		{"+=", PlusAssign}, {"-=", MinusAssign}, {"*=", StarAssign}, {"/=", SlashAssign},
		{"%=", PercentAssign}, {"&=", AmpersandAssign}, {"|=", PipeAssign}, {"^=", CaretAssign},
		{"++", PlusPlus}, {"--", MinusMinus}, {"==", EqualEqual}, {"!=", BangEqual},
		{"<=", LessEqual}, {">=", GreaterEqual}, {"<<", ShiftLeft}, {">>", ShiftRight},
		{"&&", AndAnd}, {"||", OrOr}, {"??", Nullish}, {"?.", OptionalChain}, {"=>", Arrow},
		{"(", LeftParen}, {")", RightParen}, {"{", LeftBrace}, {"}", RightBrace},
		{"[", LeftBracket}, {"]", RightBracket}, {";", Semicolon}, {",", Comma},
		{".", Dot}, {":", Colon}, {"?", Question}, {"=", Assign}, {"+", Plus},
		{"-", Minus}, {"*", Star}, {"/", Slash}, {"%", Percent}, {"!", Bang},
		{"<", Less}, {">", Greater}, {"&", Ampersand}, {"|", Pipe}, {"^", Caret}, {"~", Tilde},
	}
	for _, candidate := range table {
		if strings.HasPrefix(input.source[input.offset:], candidate.text) {
			for range candidate.text {
				_, _ = input.advance()
			}
			return Token{Kind: candidate.kind, Lexeme: candidate.text, Span: Span{Start: start, End: input.position()}}, nil
		}
	}
	r, err := input.advance()
	if err != nil {
		return Token{}, err
	}
	if input.tolerant {
		return Token{Kind: Unknown, Lexeme: string(r), Span: Span{Start: start, End: input.position()}}, nil
	}
	return Token{}, input.problem(start, fmt.Sprintf("unexpected character %q", r))
}
