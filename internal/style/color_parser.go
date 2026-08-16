package style

import (
	"image/color"
	"math"

	"github.com/JediWattson/gossamer/internal/css"
	"golang.org/x/image/colornames"
)

func parseFunctionalColor(function css.ComponentValue) (color.NRGBA, bool) {
	if function.Kind != css.ComponentFunction {
		return color.NRGBA{}, false
	}
	name := lowerASCIIValue(function.Token.Value)
	switch name {
	case "rgb", "rgba":
		return parseRGBFunction(function.Values)
	case "hsl", "hsla":
		return parseHSLFunction(function.Values)
	case "hwb":
		return parseHWBFunction(function.Values)
	default:
		return color.NRGBA{}, false
	}
}

func parseNamedColor(name string) (color.NRGBA, bool) {
	switch name {
	case "transparent":
		return color.NRGBA{}, true
	case "rebeccapurple":
		return color.NRGBA{R: 0x66, G: 0x33, B: 0x99, A: 0xff}, true
	}
	value, ok := colornames.Map[name]
	if !ok {
		return color.NRGBA{}, false
	}
	return color.NRGBA{R: value.R, G: value.G, B: value.B, A: value.A}, true
}

func parseRGBFunction(values []css.ComponentValue) (color.NRGBA, bool) {
	channels, alpha, legacy, ok := colorFunctionArguments(values)
	if !ok || len(channels) != 3 {
		return color.NRGBA{}, false
	}
	parsed := [3]float64{}
	legacyPercentage := false
	for index, component := range channels {
		value, percentage, valid := parseColorChannel(component, !legacy)
		if !valid {
			return color.NRGBA{}, false
		}
		if legacy && index == 0 {
			legacyPercentage = percentage
		} else if legacy && percentage != legacyPercentage {
			return color.NRGBA{}, false
		}
		if percentage {
			parsed[index] = value / 100
		} else {
			parsed[index] = value / 255
		}
	}
	alphaValue, valid := parseAlphaComponent(alpha, !legacy)
	if !valid {
		return color.NRGBA{}, false
	}
	return nrgbaFromUnit(parsed[0], parsed[1], parsed[2], alphaValue), true
}

func parseHSLFunction(values []css.ComponentValue) (color.NRGBA, bool) {
	channels, alpha, legacy, ok := colorFunctionArguments(values)
	if !ok || len(channels) != 3 {
		return color.NRGBA{}, false
	}
	hue, valid := parseHueComponent(channels[0], !legacy)
	if !valid {
		return color.NRGBA{}, false
	}
	saturation, valid := parsePercentageComponent(channels[1], !legacy)
	if !valid {
		return color.NRGBA{}, false
	}
	lightness, valid := parsePercentageComponent(channels[2], !legacy)
	if !valid {
		return color.NRGBA{}, false
	}
	alphaValue, valid := parseAlphaComponent(alpha, !legacy)
	if !valid {
		return color.NRGBA{}, false
	}
	red, green, blue := hslToUnitRGB(hue, clamp(saturation/100, 0, 1), clamp(lightness/100, 0, 1))
	return nrgbaFromUnit(red, green, blue, alphaValue), true
}

func parseHWBFunction(values []css.ComponentValue) (color.NRGBA, bool) {
	channels, alpha, legacy, ok := colorFunctionArguments(values)
	if !ok || legacy || len(channels) != 3 {
		return color.NRGBA{}, false
	}
	hue, valid := parseHueComponent(channels[0], true)
	if !valid {
		return color.NRGBA{}, false
	}
	whiteness, valid := parsePercentageComponent(channels[1], true)
	if !valid {
		return color.NRGBA{}, false
	}
	blackness, valid := parsePercentageComponent(channels[2], true)
	if !valid {
		return color.NRGBA{}, false
	}
	alphaValue, valid := parseAlphaComponent(alpha, true)
	if !valid {
		return color.NRGBA{}, false
	}
	whiteness = clamp(whiteness/100, 0, 1)
	blackness = clamp(blackness/100, 0, 1)
	if whiteness+blackness >= 1 {
		gray := whiteness / (whiteness + blackness)
		return nrgbaFromUnit(gray, gray, gray, alphaValue), true
	}
	red, green, blue := hslToUnitRGB(hue, 1, .5)
	factor := 1 - whiteness - blackness
	return nrgbaFromUnit(red*factor+whiteness, green*factor+whiteness, blue*factor+whiteness, alphaValue), true
}

// colorFunctionArguments accepts the comma-separated legacy grammar or the
// whitespace-separated modern grammar with an optional slash alpha. The
// returned alpha is zero-valued when omitted, which parseAlphaComponent treats
// as opaque.
func colorFunctionArguments(values []css.ComponentValue) (channels []css.ComponentValue, alpha css.ComponentValue, legacy bool, ok bool) {
	values = trimValueWhitespace(values)
	if len(values) == 0 {
		return nil, css.ComponentValue{}, false, false
	}
	hasComma := false
	for _, value := range values {
		if token, tokenOK := componentToken(value); tokenOK && token.Kind == css.TokenComma {
			hasComma = true
			break
		}
	}
	if hasComma {
		groups := splitColorComponents(values, css.TokenComma, "")
		if len(groups) != 3 && len(groups) != 4 {
			return nil, css.ComponentValue{}, true, false
		}
		for _, group := range groups {
			if len(group) != 1 {
				return nil, css.ComponentValue{}, true, false
			}
		}
		channels = []css.ComponentValue{groups[0][0], groups[1][0], groups[2][0]}
		if len(groups) == 4 {
			alpha = groups[3][0]
		}
		return channels, alpha, true, true
	}

	channels, alpha, ok = modernColorFunctionArguments(values)
	if !ok {
		return nil, css.ComponentValue{}, false, false
	}
	return channels, alpha, false, true
}

func modernColorFunctionArguments(values []css.ComponentValue) ([]css.ComponentValue, css.ComponentValue, bool) {
	channels := make([]css.ComponentValue, 0, 3)
	alpha := css.ComponentValue{}
	seenSlash := false
	whitespaceSinceTerm := false
	for _, value := range values {
		if valueWhitespace(value) {
			whitespaceSinceTerm = true
			continue
		}
		if componentIsDelimiter(value, "/") {
			if seenSlash || len(channels) != 3 {
				return nil, css.ComponentValue{}, false
			}
			seenSlash = true
			whitespaceSinceTerm = false
			continue
		}
		if seenSlash {
			if alpha.Kind != 0 {
				return nil, css.ComponentValue{}, false
			}
			alpha = value
			whitespaceSinceTerm = false
			continue
		}
		if len(channels) > 0 && !whitespaceSinceTerm {
			return nil, css.ComponentValue{}, false
		}
		channels = append(channels, value)
		whitespaceSinceTerm = false
	}
	if len(channels) != 3 || seenSlash && alpha.Kind == 0 {
		return nil, css.ComponentValue{}, false
	}
	return channels, alpha, true
}

func splitColorComponents(values []css.ComponentValue, separator css.TokenKind, delimiter string) [][]css.ComponentValue {
	groups := make([][]css.ComponentValue, 1)
	for _, value := range values {
		token, tokenOK := componentToken(value)
		if tokenOK && token.Kind == separator && (delimiter == "" || token.Value == delimiter) {
			groups = append(groups, nil)
			continue
		}
		if !valueWhitespace(value) {
			groups[len(groups)-1] = append(groups[len(groups)-1], value)
		}
	}
	return groups
}

func parseColorChannel(component css.ComponentValue, allowNone bool) (value float64, percentage bool, ok bool) {
	if allowNone {
		if keyword, keywordOK := componentKeyword(component); keywordOK && keyword == "none" {
			return 0, false, true
		}
	}
	token, tokenOK := componentToken(component)
	if !tokenOK || !isFinite(token.Number) {
		return 0, false, false
	}
	switch token.Kind {
	case css.TokenNumber:
		return token.Number, false, true
	case css.TokenPercentage:
		return token.Number, true, true
	default:
		return 0, false, false
	}
}

func parseAlphaComponent(component css.ComponentValue, allowNone bool) (float64, bool) {
	if component.Kind == 0 {
		return 1, true
	}
	if keyword, ok := componentKeyword(component); allowNone && ok && keyword == "none" {
		return 0, true
	}
	token, ok := componentToken(component)
	if !ok || !isFinite(token.Number) {
		return 0, false
	}
	switch token.Kind {
	case css.TokenNumber:
		return clamp(token.Number, 0, 1), true
	case css.TokenPercentage:
		return clamp(token.Number/100, 0, 1), true
	default:
		return 0, false
	}
}

func parsePercentageComponent(component css.ComponentValue, allowNone bool) (float64, bool) {
	if allowNone {
		if keyword, ok := componentKeyword(component); ok && keyword == "none" {
			return 0, true
		}
	}
	token, ok := componentToken(component)
	return token.Number, ok && token.Kind == css.TokenPercentage && isFinite(token.Number)
}

func parseHueComponent(component css.ComponentValue, allowNone bool) (float64, bool) {
	if keyword, ok := componentKeyword(component); allowNone && ok && keyword == "none" {
		return 0, true
	}
	token, ok := componentToken(component)
	if !ok || !isFinite(token.Number) {
		return 0, false
	}
	hue := token.Number
	if token.Kind == css.TokenDimension {
		switch lowerASCIIValue(token.Value) {
		case "deg":
		case "grad":
			hue *= .9
		case "rad":
			hue *= 180 / math.Pi
		case "turn":
			hue *= 360
		default:
			return 0, false
		}
	} else if token.Kind != css.TokenNumber {
		return 0, false
	}
	hue = math.Mod(hue, 360)
	if hue < 0 {
		hue += 360
	}
	return hue, true
}

func hslToUnitRGB(hue, saturation, lightness float64) (float64, float64, float64) {
	chroma := (1 - math.Abs(2*lightness-1)) * saturation
	sector := hue / 60
	x := chroma * (1 - math.Abs(math.Mod(sector, 2)-1))
	red, green, blue := 0.0, 0.0, 0.0
	switch int(math.Floor(sector)) % 6 {
	case 0:
		red, green = chroma, x
	case 1:
		red, green = x, chroma
	case 2:
		green, blue = chroma, x
	case 3:
		green, blue = x, chroma
	case 4:
		red, blue = x, chroma
	case 5:
		red, blue = chroma, x
	}
	match := lightness - chroma/2
	return red + match, green + match, blue + match
}

func nrgbaFromUnit(red, green, blue, alpha float64) color.NRGBA {
	component := func(value float64) uint8 {
		return uint8(math.Round(clamp(value, 0, 1) * 255))
	}
	return color.NRGBA{R: component(red), G: component(green), B: component(blue), A: component(alpha)}
}
