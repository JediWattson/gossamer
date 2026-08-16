package style

import (
	"image/color"
	"strconv"
	"strings"

	"github.com/JediWattson/gossamer/internal/css"
)

// propertyValue is the style layer's bounded view over CSS component values.
// It deliberately preserves the tokenizer's decoded tokens rather than
// reconstructing values from whitespace-split source text.
type propertyValue struct {
	source string
	terms  []css.ComponentValue
}

func parsePropertyValue(source string) (propertyValue, bool) {
	values, err := css.ParseComponentValues(source)
	if err != nil {
		return propertyValue{}, false
	}
	values = trimValueWhitespace(values)
	terms := make([]css.ComponentValue, 0, len(values))
	for _, value := range values {
		if !valueWhitespace(value) {
			terms = append(terms, value)
		}
	}
	return propertyValue{source: source, terms: terms}, true
}

func trimValueWhitespace(values []css.ComponentValue) []css.ComponentValue {
	for len(values) > 0 && valueWhitespace(values[0]) {
		values = values[1:]
	}
	for len(values) > 0 && valueWhitespace(values[len(values)-1]) {
		values = values[:len(values)-1]
	}
	return values
}

func valueWhitespace(value css.ComponentValue) bool {
	return value.Kind == css.ComponentToken && value.Token.Kind == css.TokenWhitespace
}

func (value propertyValue) single() (css.ComponentValue, bool) {
	if len(value.terms) != 1 {
		return css.ComponentValue{}, false
	}
	return value.terms[0], true
}

func (value propertyValue) raw(component css.ComponentValue) string {
	return component.Span.Slice(value.source)
}

func componentToken(value css.ComponentValue) (css.Token, bool) {
	if value.Kind != css.ComponentToken {
		return css.Token{}, false
	}
	return value.Token, true
}

func componentKeyword(value css.ComponentValue) (string, bool) {
	token, ok := componentToken(value)
	if !ok || token.Kind != css.TokenIdent {
		return "", false
	}
	return lowerASCIIValue(token.Value), true
}

func singleCSSKeyword(source string) (string, bool) {
	value, ok := parsePropertyValue(source)
	if !ok {
		return "", false
	}
	component, ok := value.single()
	if !ok {
		return "", false
	}
	return componentKeyword(component)
}

func singleCSSNumber(source string) (css.Token, bool) {
	value, ok := parsePropertyValue(source)
	if !ok {
		return css.Token{}, false
	}
	component, ok := value.single()
	if !ok {
		return css.Token{}, false
	}
	token, ok := componentToken(component)
	return token, ok && token.Kind == css.TokenNumber && isFinite(token.Number)
}

func valueContainsKeyword(source, expected string) bool {
	value, ok := parsePropertyValue(source)
	if !ok {
		return false
	}
	for _, term := range value.terms {
		keyword, ok := componentKeyword(term)
		if ok && keyword == expected {
			return true
		}
	}
	return false
}

func parseLengthComponent(component css.ComponentValue, source string, emBase float64, viewport Viewport) (length, bool) {
	if component.Kind == css.ComponentFunction {
		return parseLengthMath(component, source, emBase, viewport)
	}
	token, ok := componentToken(component)
	if !ok {
		return length{}, false
	}
	return parseSimpleLengthToken(token, emBase, viewport, true)
}

func parseSimpleLengthToken(token css.Token, emBase float64, viewport Viewport, allowAuto bool) (length, bool) {
	switch token.Kind {
	case css.TokenIdent:
		if allowAuto && lowerASCIIValue(token.Value) == "auto" {
			return length{unit: lengthAuto}, true
		}
	case css.TokenNumber:
		if token.Number == 0 && isFinite(token.Number) {
			return px(0), true
		}
	case css.TokenPercentage:
		if isFinite(token.Number) {
			return length{value: token.Number, unit: lengthPercent}, true
		}
	case css.TokenDimension:
		if !isFinite(token.Number) {
			return length{}, false
		}
		unit := lowerASCIIValue(token.Value)
		switch unit {
		case "rem":
			return finiteScaledLength(token.Number, environmentInitialFontSize(viewport), lengthPX)
		case "px":
			return finiteScaledLength(token.Number, 1, lengthPX)
		case "em":
			return finiteScaledLength(token.Number, emBase, lengthPX)
		case "vw":
			return length{value: token.Number, unit: lengthVW}, true
		case "vh":
			return length{value: token.Number, unit: lengthVH}, true
		}
	}
	return length{}, false
}

func finiteScaledLength(number, scale float64, unit lengthUnit) (length, bool) {
	value := number * scale
	if !isFinite(value) {
		return length{}, false
	}
	return length{value: value, unit: unit}, true
}

func parseColorComponent(component css.ComponentValue) (color.NRGBA, bool) {
	token, ok := componentToken(component)
	if !ok {
		return color.NRGBA{}, false
	}
	if token.Kind == css.TokenHash {
		hex := token.Value
		if len(hex) == 3 || len(hex) == 4 {
			expanded := make([]byte, 0, len(hex)*2)
			for index := range hex {
				expanded = append(expanded, hex[index], hex[index])
			}
			hex = string(expanded)
		}
		if len(hex) == 6 || len(hex) == 8 {
			encoded, err := strconv.ParseUint(hex, 16, 32)
			if err == nil {
				if len(hex) == 6 {
					return color.NRGBA{R: uint8(encoded >> 16), G: uint8(encoded >> 8), B: uint8(encoded), A: 0xff}, true
				}
				return color.NRGBA{R: uint8(encoded >> 24), G: uint8(encoded >> 16), B: uint8(encoded >> 8), A: uint8(encoded)}, true
			}
		}
		return color.NRGBA{}, false
	}
	keyword, ok := componentKeyword(component)
	if !ok {
		return color.NRGBA{}, false
	}
	switch keyword {
	case "black":
		return color.NRGBA{A: 0xff}, true
	case "white":
		return color.NRGBA{R: 0xff, G: 0xff, B: 0xff, A: 0xff}, true
	case "red":
		return color.NRGBA{R: 0xff, A: 0xff}, true
	case "green":
		return color.NRGBA{G: 0x80, A: 0xff}, true
	case "blue":
		return color.NRGBA{B: 0xff, A: 0xff}, true
	case "gray", "grey":
		return color.NRGBA{R: 0x80, G: 0x80, B: 0x80, A: 0xff}, true
	case "transparent":
		return color.NRGBA{}, true
	default:
		return color.NRGBA{}, false
	}
}

func lowerASCIIValue(value string) string {
	var builder strings.Builder
	builder.Grow(len(value))
	changed := false
	for index := 0; index < len(value); index++ {
		character := value[index]
		if character >= 'A' && character <= 'Z' {
			character += 'a' - 'A'
			changed = true
		}
		builder.WriteByte(character)
	}
	if !changed {
		return value
	}
	return builder.String()
}
