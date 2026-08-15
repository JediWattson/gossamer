// Package html tokenizes HTML and constructs DOM documents.
package html

import (
	"bufio"
	stdhtml "html"
	"io"
	"strings"
	"unicode/utf8"
)

// TokenType identifies a token emitted by the HTML tokenizer.
type TokenType uint8

const (
	DoctypeToken TokenType = iota
	StartTagToken
	EndTagToken
	CommentToken
	CharacterToken
	ProcessingInstructionToken
)

// Attribute is a name-value pair on a tag token.
type Attribute struct {
	Name  string
	Value string
}

// Token is one item emitted from an HTML input stream.
type Token struct {
	Type        TokenType
	Data        string
	Target      string
	Attributes  []Attribute
	SelfClosing bool
}

type tokenizerState uint8

const (
	dataState tokenizerState = iota
	tagOpenState
	endTagOpenState
	tagNameState
	beforeAttributeNameState
	attributeNameState
	afterAttributeNameState
	beforeAttributeValueState
	attributeValueDoubleQuotedState
	attributeValueSingleQuotedState
	attributeValueUnquotedState
	afterAttributeValueQuotedState
	selfClosingStartTagState
	markupDeclarationOpenState
	markupDeclarationHyphenState
	doctypeKeywordState
	beforeDoctypeNameState
	doctypeNameState
	afterDoctypeNameState
	commentStartState
	commentStartDashState
	commentState
	commentEndDashState
	commentEndState
	commentEndBangState
	bogusCommentState
	processingInstructionOpenState
	processingInstructionTargetState
	afterProcessingInstructionTargetState
	processingInstructionDataState
	processingInstructionQuestionableState
)

type textMode uint8

const (
	rawTextMode textMode = iota
	rcdataMode
	scriptDataMode
)

const textTokenLimit = 32 * 1024

// Tokenizer incrementally converts an HTML code-point stream into tokens.
// Parse errors are recovered in the token stream; underlying reader failures
// are returned as Go errors.
type Tokenizer struct {
	input *inputStream
	state tokenizerState

	currentRune rune
	reconsume   bool
	done        bool

	currentToken   Token
	tokenData      strings.Builder
	attributeName  strings.Builder
	attributeValue strings.Builder
	attributeOpen  bool
	keywordIndex   int

	text strings.Builder

	textModeTag        string
	textModeReferences bool
	pending            []Token

	processingInstructionTarget strings.Builder
}

// NewTokenizer creates a tokenizer over reader.
func NewTokenizer(reader io.Reader) *Tokenizer {
	return &Tokenizer{
		input: newInputStream(reader),
		state: dataState,
	}
}

// Next returns the next token. It returns io.EOF after the complete input has
// been consumed.
func (tokenizer *Tokenizer) Next() (Token, error) {
	if len(tokenizer.pending) > 0 {
		token := tokenizer.pending[0]
		tokenizer.pending = tokenizer.pending[1:]
		return token, nil
	}
	if tokenizer.done {
		return Token{}, io.EOF
	}
	if tokenizer.textModeTag != "" {
		return tokenizer.nextTextModeToken()
	}

	for {
		character, err := tokenizer.nextRune()
		if err != nil {
			if err == io.EOF {
				return tokenizer.handleEOF()
			}
			return Token{}, err
		}

		token, emit, err := tokenizer.consume(character)
		if err != nil {
			return Token{}, err
		}
		if emit {
			return token, nil
		}
	}
}

func (tokenizer *Tokenizer) consume(character rune) (Token, bool, error) {
	switch tokenizer.state {
	case dataState:
		switch character {
		case '&':
			reference, err := tokenizer.consumeCharacterReference()
			if err != nil {
				return Token{}, false, err
			}
			tokenizer.text.WriteString(reference)
		case '<':
			tokenizer.state = tagOpenState
			if tokenizer.text.Len() > 0 {
				return tokenizer.emitText(), true, nil
			}
		case 0:
			tokenizer.text.WriteRune(utf8.RuneError)
		default:
			tokenizer.text.WriteRune(character)
		}
		if tokenizer.state == dataState && tokenizer.text.Len() >= textTokenLimit {
			return tokenizer.emitText(), true, nil
		}

	case tagOpenState:
		switch {
		case character == '!':
			tokenizer.state = markupDeclarationOpenState
		case character == '/':
			tokenizer.state = endTagOpenState
		case character == '?':
			tokenizer.processingInstructionTarget.Reset()
			tokenizer.state = processingInstructionOpenState
		case isASCIIAlpha(character):
			tokenizer.beginTag(StartTagToken)
			tokenizer.reconsumeIn(tagNameState, character)
		default:
			tokenizer.text.WriteRune('<')
			tokenizer.reconsumeIn(dataState, character)
		}

	case endTagOpenState:
		switch {
		case isASCIIAlpha(character):
			tokenizer.beginTag(EndTagToken)
			tokenizer.reconsumeIn(tagNameState, character)
		case character == '>':
			tokenizer.state = dataState
		default:
			tokenizer.text.WriteString("</")
			tokenizer.reconsumeIn(dataState, character)
		}

	case tagNameState:
		switch {
		case isHTMLSpace(character):
			tokenizer.state = beforeAttributeNameState
		case character == '/':
			tokenizer.state = selfClosingStartTagState
		case character == '>':
			return tokenizer.emitTag(), true, nil
		case character == 0:
			tokenizer.tokenData.WriteRune(utf8.RuneError)
		default:
			tokenizer.tokenData.WriteRune(toASCIILower(character))
		}

	case beforeAttributeNameState:
		switch {
		case isHTMLSpace(character):
		case character == '/':
			tokenizer.state = selfClosingStartTagState
		case character == '>':
			return tokenizer.emitTag(), true, nil
		default:
			tokenizer.startAttribute()
			tokenizer.reconsumeIn(attributeNameState, character)
		}

	case attributeNameState:
		switch {
		case isHTMLSpace(character):
			tokenizer.state = afterAttributeNameState
		case character == '/':
			tokenizer.finishAttribute()
			tokenizer.state = selfClosingStartTagState
		case character == '=':
			tokenizer.state = beforeAttributeValueState
		case character == '>':
			tokenizer.finishAttribute()
			return tokenizer.emitTag(), true, nil
		case character == 0:
			tokenizer.attributeName.WriteRune(utf8.RuneError)
		default:
			tokenizer.attributeName.WriteRune(toASCIILower(character))
		}

	case afterAttributeNameState:
		switch {
		case isHTMLSpace(character):
		case character == '/':
			tokenizer.finishAttribute()
			tokenizer.state = selfClosingStartTagState
		case character == '=':
			tokenizer.state = beforeAttributeValueState
		case character == '>':
			tokenizer.finishAttribute()
			return tokenizer.emitTag(), true, nil
		default:
			tokenizer.finishAttribute()
			tokenizer.startAttribute()
			tokenizer.reconsumeIn(attributeNameState, character)
		}

	case beforeAttributeValueState:
		switch {
		case isHTMLSpace(character):
		case character == '"':
			tokenizer.state = attributeValueDoubleQuotedState
		case character == '\'':
			tokenizer.state = attributeValueSingleQuotedState
		case character == '>':
			tokenizer.finishAttribute()
			return tokenizer.emitTag(), true, nil
		default:
			tokenizer.reconsumeIn(attributeValueUnquotedState, character)
		}

	case attributeValueDoubleQuotedState:
		switch character {
		case '"':
			tokenizer.finishAttribute()
			tokenizer.state = afterAttributeValueQuotedState
		case '&':
			if err := tokenizer.appendAttributeReference(); err != nil {
				return Token{}, false, err
			}
		case 0:
			tokenizer.attributeValue.WriteRune(utf8.RuneError)
		default:
			tokenizer.attributeValue.WriteRune(character)
		}

	case attributeValueSingleQuotedState:
		switch character {
		case '\'':
			tokenizer.finishAttribute()
			tokenizer.state = afterAttributeValueQuotedState
		case '&':
			if err := tokenizer.appendAttributeReference(); err != nil {
				return Token{}, false, err
			}
		case 0:
			tokenizer.attributeValue.WriteRune(utf8.RuneError)
		default:
			tokenizer.attributeValue.WriteRune(character)
		}

	case attributeValueUnquotedState:
		switch {
		case isHTMLSpace(character):
			tokenizer.finishAttribute()
			tokenizer.state = beforeAttributeNameState
		case character == '&':
			if err := tokenizer.appendAttributeReference(); err != nil {
				return Token{}, false, err
			}
		case character == '>':
			tokenizer.finishAttribute()
			return tokenizer.emitTag(), true, nil
		case character == 0:
			tokenizer.attributeValue.WriteRune(utf8.RuneError)
		default:
			tokenizer.attributeValue.WriteRune(character)
		}

	case afterAttributeValueQuotedState:
		switch {
		case isHTMLSpace(character):
			tokenizer.state = beforeAttributeNameState
		case character == '/':
			tokenizer.state = selfClosingStartTagState
		case character == '>':
			return tokenizer.emitTag(), true, nil
		default:
			tokenizer.reconsumeIn(beforeAttributeNameState, character)
		}

	case selfClosingStartTagState:
		if character == '>' {
			tokenizer.currentToken.SelfClosing = true
			return tokenizer.emitTag(), true, nil
		}
		tokenizer.reconsumeIn(beforeAttributeNameState, character)

	case markupDeclarationOpenState:
		switch {
		case character == '-':
			tokenizer.state = markupDeclarationHyphenState
		case character == 'd' || character == 'D':
			tokenizer.keywordIndex = 1
			tokenizer.state = doctypeKeywordState
		default:
			tokenizer.beginComment("")
			tokenizer.reconsumeIn(bogusCommentState, character)
		}

	case markupDeclarationHyphenState:
		if character == '-' {
			tokenizer.beginComment("")
			tokenizer.state = commentStartState
		} else {
			tokenizer.beginComment("-")
			tokenizer.reconsumeIn(bogusCommentState, character)
		}

	case doctypeKeywordState:
		const keyword = "doctype"
		if toASCIILower(character) == rune(keyword[tokenizer.keywordIndex]) {
			tokenizer.keywordIndex++
			if tokenizer.keywordIndex == len(keyword) {
				tokenizer.beginDoctype()
				tokenizer.state = beforeDoctypeNameState
			}
		} else {
			tokenizer.beginComment(keyword[:tokenizer.keywordIndex])
			tokenizer.reconsumeIn(bogusCommentState, character)
		}

	case beforeDoctypeNameState:
		switch {
		case isHTMLSpace(character):
		case character == '>':
			return tokenizer.emitDoctype(), true, nil
		default:
			tokenizer.reconsumeIn(doctypeNameState, character)
		}

	case doctypeNameState:
		switch {
		case isHTMLSpace(character):
			tokenizer.state = afterDoctypeNameState
		case character == '>':
			return tokenizer.emitDoctype(), true, nil
		case character == 0:
			tokenizer.tokenData.WriteRune(utf8.RuneError)
		default:
			tokenizer.tokenData.WriteRune(toASCIILower(character))
		}

	case afterDoctypeNameState:
		if character == '>' {
			return tokenizer.emitDoctype(), true, nil
		}

	case commentStartState:
		switch character {
		case '-':
			tokenizer.state = commentStartDashState
		case '>':
			return tokenizer.emitComment(), true, nil
		default:
			tokenizer.reconsumeIn(commentState, character)
		}

	case commentStartDashState:
		switch character {
		case '-':
			tokenizer.state = commentEndState
		case '>':
			return tokenizer.emitComment(), true, nil
		default:
			tokenizer.tokenData.WriteRune('-')
			tokenizer.reconsumeIn(commentState, character)
		}

	case commentState:
		if character == '-' {
			tokenizer.state = commentEndDashState
		} else if character == 0 {
			tokenizer.tokenData.WriteRune(utf8.RuneError)
		} else {
			tokenizer.tokenData.WriteRune(character)
		}

	case commentEndDashState:
		if character == '-' {
			tokenizer.state = commentEndState
		} else {
			tokenizer.tokenData.WriteRune('-')
			tokenizer.reconsumeIn(commentState, character)
		}

	case commentEndState:
		switch character {
		case '>':
			return tokenizer.emitComment(), true, nil
		case '!':
			tokenizer.state = commentEndBangState
		case '-':
			tokenizer.tokenData.WriteRune('-')
		default:
			tokenizer.tokenData.WriteString("--")
			tokenizer.reconsumeIn(commentState, character)
		}

	case commentEndBangState:
		if character == '>' {
			return tokenizer.emitComment(), true, nil
		}
		tokenizer.tokenData.WriteString("--!")
		tokenizer.reconsumeIn(commentState, character)

	case bogusCommentState:
		if character == '>' {
			return tokenizer.emitComment(), true, nil
		}
		if character == 0 {
			tokenizer.tokenData.WriteRune(utf8.RuneError)
		} else {
			tokenizer.tokenData.WriteRune(character)
		}

	case processingInstructionOpenState:
		if isASCIIAlpha(character) || character == '_' {
			tokenizer.reconsumeIn(processingInstructionTargetState, character)
		} else {
			tokenizer.beginComment("")
			tokenizer.reconsumeIn(bogusCommentState, character)
		}

	case processingInstructionTargetState:
		switch {
		case isHTMLSpace(character), character == '?', character == '>':
			target := tokenizer.processingInstructionTarget.String()
			if strings.EqualFold(target, "xml") || strings.EqualFold(target, "xml-stylesheet") {
				tokenizer.beginComment(target)
				tokenizer.reconsumeIn(bogusCommentState, character)
				break
			}
			tokenizer.currentToken = Token{Type: ProcessingInstructionToken, Target: target}
			tokenizer.tokenData.Reset()
			tokenizer.reconsumeIn(afterProcessingInstructionTargetState, character)
		case isASCIIAlphaNumeric(character), character == '-', character == '_':
			tokenizer.processingInstructionTarget.WriteRune(character)
		default:
			tokenizer.beginComment(tokenizer.processingInstructionTarget.String())
			tokenizer.reconsumeIn(bogusCommentState, character)
		}

	case afterProcessingInstructionTargetState:
		if !isHTMLSpace(character) {
			tokenizer.reconsumeIn(processingInstructionDataState, character)
		}

	case processingInstructionDataState:
		switch character {
		case '?':
			tokenizer.state = processingInstructionQuestionableState
		case '>':
			return tokenizer.emitProcessingInstruction(), true, nil
		default:
			tokenizer.tokenData.WriteRune(character)
		}

	case processingInstructionQuestionableState:
		if character == '>' {
			return tokenizer.emitProcessingInstruction(), true, nil
		}
		tokenizer.tokenData.WriteRune('?')
		tokenizer.reconsumeIn(processingInstructionDataState, character)
	}

	return Token{}, false, nil
}

func (tokenizer *Tokenizer) nextTextModeToken() (Token, error) {
	for {
		character, err := tokenizer.nextRune()
		if err != nil {
			if err == io.EOF {
				tokenizer.done = true
				tokenizer.textModeTag = ""
				if tokenizer.text.Len() > 0 {
					return tokenizer.emitText(), nil
				}
				return Token{}, io.EOF
			}
			return Token{}, err
		}

		if character == '&' && tokenizer.textModeReferences {
			reference, err := tokenizer.consumeCharacterReference()
			if err != nil {
				return Token{}, err
			}
			tokenizer.text.WriteString(reference)
		} else if character != '<' {
			if character == 0 {
				character = utf8.RuneError
			}
			tokenizer.text.WriteRune(character)
		} else {
			matched, consumed, err := tokenizer.consumeTextModeEndTag()
			if err != nil {
				if err == io.EOF {
					tokenizer.text.WriteString(consumed)
					tokenizer.done = true
					tokenizer.textModeTag = ""
					if tokenizer.text.Len() > 0 {
						return tokenizer.emitText(), nil
					}
					return Token{}, io.EOF
				}
				return Token{}, err
			}
			if !matched {
				tokenizer.text.WriteString(consumed)
			} else {
				endTag := Token{Type: EndTagToken, Data: tokenizer.textModeTag}
				tokenizer.textModeTag = ""
				tokenizer.textModeReferences = false
				if tokenizer.text.Len() > 0 {
					tokenizer.pending = append(tokenizer.pending, endTag)
					return tokenizer.emitText(), nil
				}
				return endTag, nil
			}
		}

		if tokenizer.text.Len() >= textTokenLimit {
			return tokenizer.emitText(), nil
		}
	}
}

func (tokenizer *Tokenizer) consumeTextModeEndTag() (bool, string, error) {
	var consumed strings.Builder
	consumed.WriteRune('<')

	character, err := tokenizer.nextRune()
	if err != nil {
		return false, consumed.String(), err
	}
	consumed.WriteRune(character)
	if character != '/' {
		return false, consumed.String(), nil
	}

	for _, expected := range tokenizer.textModeTag {
		character, err = tokenizer.nextRune()
		if err != nil {
			return false, consumed.String(), err
		}
		consumed.WriteRune(character)
		if toASCIILower(character) != expected {
			return false, consumed.String(), nil
		}
	}

	character, err = tokenizer.nextRune()
	if err != nil {
		return false, consumed.String(), err
	}
	consumed.WriteRune(character)
	if character == '>' {
		return true, consumed.String(), nil
	}
	if !isHTMLSpace(character) && character != '/' {
		return false, consumed.String(), nil
	}

	var quote rune
	for {
		character, err = tokenizer.nextRune()
		if err != nil {
			return false, consumed.String(), err
		}
		consumed.WriteRune(character)
		switch {
		case quote != 0 && character == quote:
			quote = 0
		case quote != 0:
		case character == '\'' || character == '"':
			quote = character
		case character == '>':
			return true, consumed.String(), nil
		}
	}
}

func (tokenizer *Tokenizer) enterTextMode(mode textMode, tagName string) {
	tokenizer.textModeTag = strings.ToLower(tagName)
	tokenizer.textModeReferences = mode == rcdataMode
}

func (tokenizer *Tokenizer) consumeCharacterReference() (string, error) {
	var candidate strings.Builder
	candidate.WriteRune('&')

	character, err := tokenizer.nextRune()
	if err != nil {
		if err == io.EOF {
			return candidate.String(), nil
		}
		return "", err
	}
	if character == '#' {
		candidate.WriteRune(character)
		return tokenizer.consumeNumericReference(&candidate)
	}
	if !isASCIIAlphaNumeric(character) {
		tokenizer.reconsumeRune(character)
		return candidate.String(), nil
	}

	for {
		candidate.WriteRune(character)
		character, err = tokenizer.nextRune()
		if err != nil {
			if err == io.EOF {
				return candidate.String(), nil
			}
			return "", err
		}
		if !isASCIIAlphaNumeric(character) {
			break
		}
	}

	if character != ';' {
		tokenizer.reconsumeRune(character)
		return candidate.String(), nil
	}
	candidate.WriteRune(';')
	return stdhtml.UnescapeString(candidate.String()), nil
}

func (tokenizer *Tokenizer) consumeNumericReference(candidate *strings.Builder) (string, error) {
	character, err := tokenizer.nextRune()
	if err != nil {
		if err == io.EOF {
			return candidate.String(), nil
		}
		return "", err
	}

	hexadecimal := character == 'x' || character == 'X'
	if hexadecimal {
		candidate.WriteRune(character)
		character, err = tokenizer.nextRune()
		if err != nil {
			if err == io.EOF {
				return candidate.String(), nil
			}
			return "", err
		}
	}

	digits := 0
	for isASCIIDigit(character) || hexadecimal && isASCIIHexDigit(character) {
		candidate.WriteRune(character)
		digits++
		character, err = tokenizer.nextRune()
		if err != nil {
			if err == io.EOF {
				if digits == 0 {
					return candidate.String(), nil
				}
				return stdhtml.UnescapeString(candidate.String()), nil
			}
			return "", err
		}
	}

	if digits == 0 {
		tokenizer.reconsumeRune(character)
		return candidate.String(), nil
	}
	if character == ';' {
		candidate.WriteRune(character)
	} else {
		tokenizer.reconsumeRune(character)
	}
	return stdhtml.UnescapeString(candidate.String()), nil
}

func (tokenizer *Tokenizer) appendAttributeReference() error {
	reference, err := tokenizer.consumeCharacterReference()
	if err != nil {
		return err
	}
	tokenizer.attributeValue.WriteString(reference)
	return nil
}

func (tokenizer *Tokenizer) handleEOF() (Token, error) {
	tokenizer.done = true

	switch tokenizer.state {
	case dataState:
		if tokenizer.text.Len() > 0 {
			return tokenizer.emitText(), nil
		}
	case tagOpenState:
		tokenizer.text.WriteRune('<')
		return tokenizer.emitText(), nil
	case endTagOpenState:
		tokenizer.text.WriteString("</")
		return tokenizer.emitText(), nil
	case markupDeclarationOpenState:
		tokenizer.beginComment("")
		return tokenizer.emitComment(), nil
	case markupDeclarationHyphenState:
		tokenizer.beginComment("-")
		return tokenizer.emitComment(), nil
	case doctypeKeywordState:
		const keyword = "doctype"
		tokenizer.beginComment(keyword[:tokenizer.keywordIndex])
		return tokenizer.emitComment(), nil
	case commentStartDashState, commentEndDashState:
		tokenizer.tokenData.WriteRune('-')
		return tokenizer.emitComment(), nil
	case commentEndState:
		tokenizer.tokenData.WriteString("--")
		return tokenizer.emitComment(), nil
	case commentEndBangState:
		tokenizer.tokenData.WriteString("--!")
		return tokenizer.emitComment(), nil
	case commentStartState, commentState, bogusCommentState:
		return tokenizer.emitComment(), nil
	case beforeDoctypeNameState, doctypeNameState, afterDoctypeNameState:
		return tokenizer.emitDoctype(), nil
	}

	return Token{}, io.EOF
}

func (tokenizer *Tokenizer) beginTag(tokenType TokenType) {
	tokenizer.currentToken = Token{Type: tokenType}
	tokenizer.tokenData.Reset()
	tokenizer.attributeName.Reset()
	tokenizer.attributeValue.Reset()
	tokenizer.attributeOpen = false
}

func (tokenizer *Tokenizer) emitTag() Token {
	tokenizer.finishAttribute()
	tokenizer.currentToken.Data = tokenizer.tokenData.String()
	token := tokenizer.currentToken
	tokenizer.currentToken = Token{}
	tokenizer.state = dataState
	return token
}

func (tokenizer *Tokenizer) startAttribute() {
	tokenizer.attributeName.Reset()
	tokenizer.attributeValue.Reset()
	tokenizer.attributeOpen = true
}

func (tokenizer *Tokenizer) finishAttribute() {
	if !tokenizer.attributeOpen {
		return
	}
	attribute := Attribute{Name: tokenizer.attributeName.String(), Value: tokenizer.attributeValue.String()}
	for _, existing := range tokenizer.currentToken.Attributes {
		if existing.Name == attribute.Name {
			tokenizer.attributeOpen = false
			return
		}
	}
	tokenizer.currentToken.Attributes = append(tokenizer.currentToken.Attributes, attribute)
	tokenizer.attributeOpen = false
}

func (tokenizer *Tokenizer) beginComment(initial string) {
	tokenizer.currentToken = Token{Type: CommentToken}
	tokenizer.tokenData.Reset()
	tokenizer.tokenData.WriteString(initial)
}

func (tokenizer *Tokenizer) emitComment() Token {
	token := Token{Type: CommentToken, Data: tokenizer.tokenData.String()}
	tokenizer.currentToken = Token{}
	tokenizer.state = dataState
	return token
}

func (tokenizer *Tokenizer) beginDoctype() {
	tokenizer.currentToken = Token{Type: DoctypeToken}
	tokenizer.tokenData.Reset()
}

func (tokenizer *Tokenizer) emitDoctype() Token {
	token := Token{Type: DoctypeToken, Data: tokenizer.tokenData.String()}
	tokenizer.currentToken = Token{}
	tokenizer.state = dataState
	return token
}

func (tokenizer *Tokenizer) emitProcessingInstruction() Token {
	tokenizer.currentToken.Data = tokenizer.tokenData.String()
	token := tokenizer.currentToken
	tokenizer.currentToken = Token{}
	tokenizer.state = dataState
	return token
}

func (tokenizer *Tokenizer) emitText() Token {
	token := Token{Type: CharacterToken, Data: tokenizer.text.String()}
	tokenizer.text.Reset()
	return token
}

func (tokenizer *Tokenizer) nextRune() (rune, error) {
	if tokenizer.reconsume {
		tokenizer.reconsume = false
		return tokenizer.currentRune, nil
	}

	character, err := tokenizer.input.NextRune()
	if err != nil {
		return 0, err
	}
	tokenizer.currentRune = character
	return character, nil
}

func (tokenizer *Tokenizer) reconsumeIn(state tokenizerState, character rune) {
	tokenizer.state = state
	tokenizer.reconsumeRune(character)
}

func (tokenizer *Tokenizer) reconsumeRune(character rune) {
	tokenizer.currentRune = character
	tokenizer.reconsume = true
}

type inputStream struct {
	reader  *bufio.Reader
	atStart bool
}

func newInputStream(reader io.Reader) *inputStream {
	return &inputStream{reader: bufio.NewReader(reader), atStart: true}
}

func (input *inputStream) NextRune() (rune, error) {
	for {
		character, _, err := input.reader.ReadRune()
		if err != nil {
			return 0, err
		}
		if input.atStart {
			input.atStart = false
			if character == '\ufeff' {
				continue
			}
		}
		if character != '\r' {
			return character, nil
		}

		next, err := input.reader.Peek(1)
		if err == nil && next[0] == '\n' {
			_, _ = input.reader.ReadByte()
		}
		return '\n', nil
	}
}

func isHTMLSpace(character rune) bool {
	return character == '\t' || character == '\n' || character == '\f' || character == '\r' || character == ' '
}

func isASCIIAlpha(character rune) bool {
	return character >= 'a' && character <= 'z' || character >= 'A' && character <= 'Z'
}

func isASCIIDigit(character rune) bool {
	return character >= '0' && character <= '9'
}

func isASCIIHexDigit(character rune) bool {
	return isASCIIDigit(character) || character >= 'a' && character <= 'f' || character >= 'A' && character <= 'F'
}

func isASCIIAlphaNumeric(character rune) bool {
	return isASCIIAlpha(character) || isASCIIDigit(character)
}

func toASCIILower(character rune) rune {
	if character >= 'A' && character <= 'Z' {
		return character + ('a' - 'A')
	}
	return character
}
