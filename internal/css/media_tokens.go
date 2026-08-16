package css

import (
	"math"
	"strings"
)

func mediaQueryListMatchesTokens(source string, environment MediaEnvironment) bool {
	values, err := ParseComponentValues(source)
	if err != nil || !mediaComponentsExplicitlyClosed(source, values) {
		return false
	}
	values = trimComponentWhitespace(values)
	if len(values) == 0 {
		return true
	}
	for _, candidate := range splitMediaQueryGroups(values) {
		matched, valid := matchMediaQueryTokens(candidate, environment)
		if valid && matched == mediaTrue {
			return true
		}
	}
	return false
}

func mediaComponentsExplicitlyClosed(source string, values []ComponentValue) bool {
	for _, value := range values {
		if value.Kind == ComponentFunction || value.Kind == ComponentBlock {
			closing := byte(')')
			switch value.Token.Kind {
			case TokenOpenSquare:
				closing = ']'
			case TokenOpenCurly:
				closing = '}'
			}
			if value.Span.End <= value.Token.Span.End || value.Span.End > len(source) || source[value.Span.End-1] != closing {
				return false
			}
		}
		if !mediaComponentsExplicitlyClosed(source, value.Values) {
			return false
		}
	}
	return true
}

func splitMediaQueryGroups(values []ComponentValue) [][]ComponentValue {
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

func matchMediaQueryTokens(values []ComponentValue, environment MediaEnvironment) (mediaTruth, bool) {
	parser := mediaTokenParser{values: trimComponentWhitespace(values)}
	if parser.done() {
		return mediaFalse, false
	}

	negated := false
	modifier := ""
	if token, ok := parser.peekIdent(); ok {
		keyword := lowerASCII(token.Value)
		if keyword == "not" || keyword == "only" {
			modifier = keyword
			negated = keyword == "not"
			parser.pos++
			parser.skipWhitespace()
			if parser.done() {
				return mediaFalse, false
			}
		}
	}

	matched := mediaTrue
	hasType := !parser.peekParenthesizedBlock()
	if modifier == "only" && !hasType {
		return mediaFalse, false
	}
	if hasType {
		mediaType, ok := parser.peekIdent()
		if !ok {
			return mediaFalse, false
		}
		name := lowerASCII(mediaType.Value)
		if reservedMediaType(name) {
			return mediaFalse, false
		}
		matched = mediaTypeMatches(name, environment)
		parser.pos++
		parser.skipWhitespace()
	}

	hasCondition := false
	needsAnd := hasType
	for !parser.done() {
		if needsAnd {
			keyword, ok := parser.peekIdent()
			if !ok || !equalASCIIFold(keyword.Value, "and") {
				return mediaFalse, false
			}
			parser.pos++
			parser.skipWhitespace()
		}
		if parser.done() || !parser.peekParenthesizedBlock() {
			return mediaFalse, false
		}
		condition := parser.values[parser.pos]
		parser.pos++
		conditionMatched, conditionValid := mediaFeatureMatchesTokens(condition.Values, environment)
		if !conditionValid {
			return mediaFalse, false
		}
		matched = andMediaTruth(matched, conditionMatched)
		hasCondition = true
		needsAnd = true
		parser.skipWhitespace()
	}
	if !hasType && !hasCondition {
		return mediaFalse, false
	}
	if negated {
		matched = notMediaTruth(matched)
	}
	return matched, true
}

func mediaFeatureMatchesTokens(values []ComponentValue, environment MediaEnvironment) (mediaTruth, bool) {
	values = trimComponentWhitespace(values)
	if len(values) == 0 {
		return mediaFalse, false
	}
	colon := -1
	for index, value := range values {
		if value.Kind == ComponentToken && value.Token.Kind == TokenColon {
			if colon >= 0 {
				return mediaFalse, false
			}
			colon = index
		}
	}
	nameValues := values
	var valueValues []ComponentValue
	if colon >= 0 {
		nameValues = trimComponentWhitespace(values[:colon])
		valueValues = trimComponentWhitespace(values[colon+1:])
	}
	if len(nameValues) != 1 || nameValues[0].Kind != ComponentToken || nameValues[0].Token.Kind != TokenIdent {
		return mediaFalse, false
	}
	name := lowerASCII(nameValues[0].Token.Value)
	if colon < 0 {
		switch name {
		case "width":
			return mediaTruthFor(environment.Width > 0), true
		case "height":
			return mediaTruthFor(environment.Height > 0), true
		case "orientation":
			return mediaTruthFor(environment.Width > 0 && environment.Height > 0), true
		case "hover", "any-hover", "pointer", "any-pointer":
			return mediaFalse, true
		default:
			return mediaUnknown, true
		}
	}
	if len(valueValues) != 1 || valueValues[0].Kind != ComponentToken {
		return mediaUnknown, true
	}
	value := valueValues[0].Token

	switch name {
	case "width", "min-width", "max-width":
		length, ok := parseMediaLengthToken(value, environment)
		if !ok {
			return mediaUnknown, true
		}
		return mediaTruthFor(compareMediaDimension(environment.Width, length, name)), true
	case "height", "min-height", "max-height":
		length, ok := parseMediaLengthToken(value, environment)
		if !ok {
			return mediaUnknown, true
		}
		return mediaTruthFor(compareMediaDimension(environment.Height, length, name)), true
	}
	if value.Kind != TokenIdent {
		return mediaUnknown, true
	}
	keyword := lowerASCII(value.Value)
	switch name {
	case "orientation":
		switch keyword {
		case "landscape":
			return mediaTruthFor(environment.Width > environment.Height), true
		case "portrait":
			return mediaTruthFor(environment.Height >= environment.Width), true
		default:
			return mediaUnknown, true
		}
	case "hover", "any-hover":
		if keyword != "none" && keyword != "hover" {
			return mediaUnknown, true
		}
		return mediaTruthFor(keyword == "none"), true
	case "pointer", "any-pointer":
		if keyword != "none" && keyword != "coarse" && keyword != "fine" {
			return mediaUnknown, true
		}
		return mediaTruthFor(keyword == "none"), true
	case "prefers-color-scheme":
		if keyword != "light" && keyword != "dark" {
			return mediaUnknown, true
		}
		return mediaTruthFor(keyword == "light"), true
	case "prefers-reduced-motion":
		if keyword != "no-preference" && keyword != "reduce" {
			return mediaUnknown, true
		}
		return mediaTruthFor(keyword == "no-preference"), true
	case "display-mode":
		if keyword != "browser" && keyword != "fullscreen" && keyword != "standalone" && keyword != "minimal-ui" {
			return mediaUnknown, true
		}
		return mediaTruthFor(keyword == "browser"), true
	case "forced-colors":
		if keyword != "none" && keyword != "active" {
			return mediaUnknown, true
		}
		return mediaTruthFor(keyword == "none"), true
	default:
		return mediaUnknown, true
	}
}

func parseMediaLengthToken(token Token, environment MediaEnvironment) (float64, bool) {
	if token.Kind == TokenNumber && token.Number == 0 {
		return 0, true
	}
	if token.Kind != TokenDimension || token.Number < 0 || math.IsNaN(token.Number) || math.IsInf(token.Number, 0) {
		return 0, false
	}
	initialFontSize := environment.InitialFontSize
	if initialFontSize <= 0 || math.IsNaN(initialFontSize) || math.IsInf(initialFontSize, 0) {
		initialFontSize = 16
	}
	var scale float64
	switch lowerASCII(token.Value) {
	case "rem", "em":
		scale = initialFontSize
	case "px":
		scale = 1
	case "vw":
		scale = environment.Width / 100
	case "vh":
		scale = environment.Height / 100
	default:
		return 0, false
	}
	resolved := token.Number * scale
	return resolved, !math.IsNaN(resolved) && !math.IsInf(resolved, 0)
}

type mediaTokenParser struct {
	values []ComponentValue
	pos    int
}

func (parser *mediaTokenParser) skipWhitespace() {
	for !parser.done() && isWhitespaceComponent(parser.values[parser.pos]) {
		parser.pos++
	}
}

func (parser *mediaTokenParser) peekIdent() (Token, bool) {
	if parser.done() {
		return Token{}, false
	}
	value := parser.values[parser.pos]
	return value.Token, value.Kind == ComponentToken && value.Token.Kind == TokenIdent
}

func (parser *mediaTokenParser) peekParenthesizedBlock() bool {
	return !parser.done() && parser.values[parser.pos].Kind == ComponentBlock && parser.values[parser.pos].Token.Kind == TokenOpenParen
}

func (parser *mediaTokenParser) done() bool {
	return parser.pos >= len(parser.values)
}

func normalizedMediaType(mediaType string) string {
	mediaType = strings.TrimSpace(mediaType)
	if mediaType == "" {
		return "screen"
	}
	return lowerASCII(mediaType)
}
