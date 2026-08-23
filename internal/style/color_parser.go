package style

import (
	"image/color"
	"math"

	"github.com/JediWattson/gossamer/internal/css"
	"golang.org/x/image/colornames"
)

const maxColorMixDepth = 8

type colorMixSpace uint8

const (
	colorMixSRGB colorMixSpace = iota + 1
	colorMixLinearSRGB
	colorMixLab
	colorMixOKLab
)

type colorCoordinates struct {
	first  float64
	second float64
	third  float64
	alpha  float64
}

type computedColorMix struct {
	space           colorMixSpace
	left            computedColorValue
	right           computedColorValue
	leftWeight      float64
	rightWeight     float64
	alphaMultiplier float64
}

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
	case "oklab":
		return parseOKLabFunction(function.Values)
	case "oklch":
		return parseOKLCHFunction(function.Values)
	case "color-mix":
		parsed, ok := parseColorMixFunction(function.Values, 0, false)
		if !ok {
			return color.NRGBA{}, false
		}
		return parsed.resolve(color.NRGBA{}), true
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

func parseOKLabFunction(values []css.ComponentValue) (color.NRGBA, bool) {
	channels, alpha, legacy, ok := colorFunctionArguments(values)
	if !ok || legacy || len(channels) != 3 {
		return color.NRGBA{}, false
	}
	lightness, ok := parseOKLightness(channels[0])
	if !ok {
		return color.NRGBA{}, false
	}
	a, ok := parseOKAxis(channels[1])
	if !ok {
		return color.NRGBA{}, false
	}
	b, ok := parseOKAxis(channels[2])
	if !ok {
		return color.NRGBA{}, false
	}
	alphaValue, ok := parseAlphaComponent(alpha, true)
	if !ok {
		return color.NRGBA{}, false
	}
	red, green, blue := okLabToSRGB(lightness, a, b)
	return nrgbaFromUnit(red, green, blue, alphaValue), true
}

func parseOKLCHFunction(values []css.ComponentValue) (color.NRGBA, bool) {
	channels, alpha, legacy, ok := colorFunctionArguments(values)
	if !ok || legacy || len(channels) != 3 {
		return color.NRGBA{}, false
	}
	lightness, ok := parseOKLightness(channels[0])
	if !ok {
		return color.NRGBA{}, false
	}
	chroma, ok := parseOKChroma(channels[1])
	if !ok {
		return color.NRGBA{}, false
	}
	hue, ok := parseHueComponent(channels[2], true)
	if !ok {
		return color.NRGBA{}, false
	}
	alphaValue, ok := parseAlphaComponent(alpha, true)
	if !ok {
		return color.NRGBA{}, false
	}
	radians := hue * math.Pi / 180
	red, green, blue := okLabToSRGB(lightness, chroma*math.Cos(radians), chroma*math.Sin(radians))
	return nrgbaFromUnit(red, green, blue, alphaValue), true
}

func parseOKLightness(component css.ComponentValue) (float64, bool) {
	if keyword, ok := componentKeyword(component); ok && keyword == "none" {
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

func parseOKAxis(component css.ComponentValue) (float64, bool) {
	if keyword, ok := componentKeyword(component); ok && keyword == "none" {
		return 0, true
	}
	token, ok := componentToken(component)
	if !ok || !isFinite(token.Number) {
		return 0, false
	}
	switch token.Kind {
	case css.TokenNumber:
		return token.Number, true
	case css.TokenPercentage:
		return token.Number * .004, true
	default:
		return 0, false
	}
}

func parseOKChroma(component css.ComponentValue) (float64, bool) {
	value, ok := parseOKAxis(component)
	if !ok {
		return 0, false
	}
	return math.Max(0, value), true
}

func parseColorMixFunction(values []css.ComponentValue, depth int, allowCurrentColor bool) (computedColorValue, bool) {
	if depth >= maxColorMixDepth {
		return computedColorValue{}, false
	}
	groups := splitColorComponents(trimValueWhitespace(values), css.TokenComma, "")
	space := colorMixOKLab
	operandStart := 0
	if len(groups) == 3 {
		if len(groups[0]) != 2 {
			return computedColorValue{}, false
		}
		intro, introOK := componentKeyword(groups[0][0])
		spaceName, spaceOK := componentKeyword(groups[0][1])
		if !introOK || intro != "in" || !spaceOK {
			return computedColorValue{}, false
		}
		var ok bool
		space, ok = parseColorMixSpace(spaceName)
		if !ok {
			return computedColorValue{}, false
		}
		operandStart = 1
	} else if len(groups) != 2 {
		return computedColorValue{}, false
	}
	left, leftPercentage, leftHasPercentage, ok := parseColorMixOperand(groups[operandStart], depth+1, allowCurrentColor)
	if !ok {
		return computedColorValue{}, false
	}
	right, rightPercentage, rightHasPercentage, ok := parseColorMixOperand(groups[operandStart+1], depth+1, allowCurrentColor)
	if !ok {
		return computedColorValue{}, false
	}
	leftWeight, rightWeight, alphaMultiplier, ok := normalizeColorMixPercentages(
		leftPercentage, leftHasPercentage, rightPercentage, rightHasPercentage,
	)
	if !ok {
		return computedColorValue{}, false
	}
	result := computedColorValue{mix: &computedColorMix{
		space:           space,
		left:            left,
		right:           right,
		leftWeight:      leftWeight,
		rightWeight:     rightWeight,
		alphaMultiplier: alphaMultiplier,
	}}
	if !result.usesCurrentColor() {
		return computedColorValue{value: result.resolve(color.NRGBA{})}, true
	}
	return result, true
}

func parseColorMixSpace(name string) (colorMixSpace, bool) {
	switch name {
	case "srgb":
		return colorMixSRGB, true
	case "srgb-linear":
		return colorMixLinearSRGB, true
	case "lab":
		return colorMixLab, true
	case "oklab":
		return colorMixOKLab, true
	default:
		return 0, false
	}
}

func parseColorMixOperand(values []css.ComponentValue, depth int, allowCurrentColor bool) (computedColorValue, float64, bool, bool) {
	if len(values) < 1 || len(values) > 2 {
		return computedColorValue{}, 0, false, false
	}
	colorIndex := 0
	percentageIndex := -1
	if len(values) == 2 {
		_, firstOK := colorMixPercentage(values[0])
		_, secondOK := colorMixPercentage(values[1])
		switch {
		case firstOK && !secondOK:
			percentageIndex, colorIndex = 0, 1
		case secondOK && !firstOK:
			percentageIndex = 1
		default:
			return computedColorValue{}, 0, false, false
		}
	}
	parsed, ok := parseComputedColorTerm(values[colorIndex], depth, allowCurrentColor)
	if !ok {
		return computedColorValue{}, 0, false, false
	}
	if percentageIndex < 0 {
		return parsed, 0, false, true
	}
	percentage, ok := colorMixPercentage(values[percentageIndex])
	if !ok {
		return computedColorValue{}, 0, false, false
	}
	return parsed, percentage, true, true
}

func colorMixPercentage(component css.ComponentValue) (float64, bool) {
	token, ok := componentToken(component)
	if !ok || token.Kind != css.TokenPercentage || !isFinite(token.Number) || token.Number < 0 || token.Number > 100 {
		return 0, false
	}
	return token.Number / 100, true
}

func normalizeColorMixPercentages(left float64, leftSet bool, right float64, rightSet bool) (float64, float64, float64, bool) {
	switch {
	case !leftSet && !rightSet:
		left, right = .5, .5
	case !leftSet:
		left = 1 - right
	case !rightSet:
		right = 1 - left
	}
	total := left + right
	if total <= 0 || !isFinite(total) {
		return 0, 0, 0, false
	}
	alphaMultiplier := math.Min(total, 1)
	return left / total, right / total, alphaMultiplier, true
}

func parseComputedColorTerm(component css.ComponentValue, depth int, allowCurrentColor bool) (computedColorValue, bool) {
	if keyword, ok := componentKeyword(component); ok && keyword == "currentcolor" {
		if !allowCurrentColor {
			return computedColorValue{}, false
		}
		return computedColorValue{currentColor: true}, true
	}
	if component.Kind == css.ComponentFunction && lowerASCIIValue(component.Token.Value) == "color-mix" {
		return parseColorMixFunction(component.Values, depth, allowCurrentColor)
	}
	parsed, ok := parseColorComponent(component)
	return computedColorValue{value: parsed}, ok
}

func (value computedColorValue) resolve(current color.NRGBA) color.NRGBA {
	if value.currentColor {
		return current
	}
	if value.mix == nil {
		return value.value
	}
	left := colorToCoordinates(value.mix.left.resolve(current), value.mix.space)
	right := colorToCoordinates(value.mix.right.resolve(current), value.mix.space)
	alpha := left.alpha*value.mix.leftWeight + right.alpha*value.mix.rightWeight
	coordinates := colorCoordinates{alpha: alpha * value.mix.alphaMultiplier}
	if alpha > 0 {
		coordinates.first = (left.first*left.alpha*value.mix.leftWeight + right.first*right.alpha*value.mix.rightWeight) / alpha
		coordinates.second = (left.second*left.alpha*value.mix.leftWeight + right.second*right.alpha*value.mix.rightWeight) / alpha
		coordinates.third = (left.third*left.alpha*value.mix.leftWeight + right.third*right.alpha*value.mix.rightWeight) / alpha
	}
	return coordinatesToColor(coordinates, value.mix.space)
}

func (value computedColorValue) usesCurrentColor() bool {
	if value.currentColor {
		return true
	}
	return value.mix != nil && (value.mix.left.usesCurrentColor() || value.mix.right.usesCurrentColor())
}

func colorToCoordinates(value color.NRGBA, space colorMixSpace) colorCoordinates {
	red := float64(value.R) / 255
	green := float64(value.G) / 255
	blue := float64(value.B) / 255
	result := colorCoordinates{alpha: float64(value.A) / 255}
	switch space {
	case colorMixSRGB:
		result.first, result.second, result.third = red, green, blue
	case colorMixLinearSRGB:
		result.first, result.second, result.third = srgbToLinear(red), srgbToLinear(green), srgbToLinear(blue)
	case colorMixLab:
		result.first, result.second, result.third = srgbToLab(red, green, blue)
	case colorMixOKLab:
		result.first, result.second, result.third = srgbToOKLab(red, green, blue)
	}
	return result
}

func coordinatesToColor(value colorCoordinates, space colorMixSpace) color.NRGBA {
	red, green, blue := value.first, value.second, value.third
	switch space {
	case colorMixLinearSRGB:
		red, green, blue = linearToSRGB(red), linearToSRGB(green), linearToSRGB(blue)
	case colorMixLab:
		red, green, blue = labToSRGB(red, green, blue)
	case colorMixOKLab:
		red, green, blue = okLabToSRGB(red, green, blue)
	}
	return nrgbaFromUnit(red, green, blue, value.alpha)
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

func srgbToLinear(value float64) float64 {
	absolute := math.Abs(value)
	if absolute <= .04045 {
		return value / 12.92
	}
	converted := math.Pow((absolute+.055)/1.055, 2.4)
	if value < 0 {
		return -converted
	}
	return converted
}

func linearToSRGB(value float64) float64 {
	absolute := math.Abs(value)
	if absolute <= .0031308 {
		return 12.92 * value
	}
	converted := 1.055*math.Pow(absolute, 1/2.4) - .055
	if value < 0 {
		return -converted
	}
	return converted
}

func srgbToOKLab(red, green, blue float64) (float64, float64, float64) {
	red, green, blue = srgbToLinear(red), srgbToLinear(green), srgbToLinear(blue)
	l := math.Cbrt(.4122214708*red + .5363325363*green + .0514459929*blue)
	m := math.Cbrt(.2119034982*red + .6806995451*green + .1073969566*blue)
	s := math.Cbrt(.0883024619*red + .2817188376*green + .6299787005*blue)
	return .2104542553*l + .793617785*m - .0040720468*s,
		1.9779984951*l - 2.428592205*m + .4505937099*s,
		.0259040371*l + .7827717662*m - .808675766*s
}

func okLabToSRGB(lightness, a, b float64) (float64, float64, float64) {
	return gamutMapOKLabToSRGB(lightness, a, b)
}

func rawOKLabToSRGB(lightness, a, b float64) (float64, float64, float64) {
	l := lightness + .3963377774*a + .2158037573*b
	m := lightness - .1055613458*a - .0638541728*b
	s := lightness - .0894841775*a - 1.291485548*b
	l, m, s = l*l*l, m*m*m, s*s*s
	return linearToSRGB(4.0767416621*l - 3.3077115913*m + .2309699292*s),
		linearToSRGB(-1.2684380046*l + 2.6097574011*m - .3413193965*s),
		linearToSRGB(-.0041960863*l - .7034186147*m + 1.707614701*s)
}

func gamutMapOKLabToSRGB(lightness, a, b float64) (float64, float64, float64) {
	if lightness >= 1 {
		return 1, 1, 1
	}
	if lightness <= 0 {
		return 0, 0, 0
	}
	red, green, blue := rawOKLabToSRGB(lightness, a, b)
	if inSRGBGamut(red, green, blue) {
		return red, green, blue
	}
	hue := math.Atan2(b, a)
	minimum := 0.0
	maximum := math.Hypot(a, b)
	minimumInGamut := true
	clippedRed, clippedGreen, clippedBlue := clamp(red, 0, 1), clamp(green, 0, 1), clamp(blue, 0, 1)
	if deltaEOKFromClipped(lightness, a, b, clippedRed, clippedGreen, clippedBlue) < .02 {
		return clippedRed, clippedGreen, clippedBlue
	}
	for maximum-minimum > .0001 {
		chroma := (minimum + maximum) / 2
		currentA, currentB := chroma*math.Cos(hue), chroma*math.Sin(hue)
		red, green, blue = rawOKLabToSRGB(lightness, currentA, currentB)
		if minimumInGamut && inSRGBGamut(red, green, blue) {
			minimum = chroma
			continue
		}
		clippedRed, clippedGreen, clippedBlue = clamp(red, 0, 1), clamp(green, 0, 1), clamp(blue, 0, 1)
		difference := deltaEOKFromClipped(lightness, currentA, currentB, clippedRed, clippedGreen, clippedBlue)
		if difference < .02 {
			if .02-difference < .0001 {
				return clippedRed, clippedGreen, clippedBlue
			}
			minimumInGamut = false
			minimum = chroma
			continue
		}
		maximum = chroma
	}
	return clippedRed, clippedGreen, clippedBlue
}

func inSRGBGamut(red, green, blue float64) bool {
	const epsilon = 1e-7
	return red >= -epsilon && red <= 1+epsilon && green >= -epsilon && green <= 1+epsilon && blue >= -epsilon && blue <= 1+epsilon
}

func deltaEOKFromClipped(lightness, a, b, red, green, blue float64) float64 {
	clippedLightness, clippedA, clippedB := srgbToOKLab(red, green, blue)
	return math.Sqrt(
		(lightness-clippedLightness)*(lightness-clippedLightness) +
			(a-clippedA)*(a-clippedA) +
			(b-clippedB)*(b-clippedB),
	)
}

func srgbToLab(red, green, blue float64) (float64, float64, float64) {
	red, green, blue = srgbToLinear(red), srgbToLinear(green), srgbToLinear(blue)
	x65 := .4123907992659595*red + .357584339383878*green + .1804807884018343*blue
	y65 := .2126390058715104*red + .715168678767756*green + .0721923153607337*blue
	z65 := .0193308187155918*red + .1191947797946259*green + .9505321522496607*blue
	x50 := 1.0479298208405488*x65 + .0229467933410191*y65 - .0501922295431356*z65
	y50 := .0296278156881593*x65 + .990434484573249*y65 - .0170738250293851*z65
	z50 := -.0092430581525912*x65 + .0150551448965779*y65 + .7518742814281371*z65
	fx := labForward(x50 / .96422)
	fy := labForward(y50)
	fz := labForward(z50 / .82521)
	return 116*fy - 16, 500 * (fx - fy), 200 * (fy - fz)
}

func labToSRGB(lightness, a, b float64) (float64, float64, float64) {
	fy := (lightness + 16) / 116
	fx := fy + a/500
	fz := fy - b/200
	x50 := .96422 * labInverse(fx)
	y50 := labInverse(fy)
	z50 := .82521 * labInverse(fz)
	x65 := .955473421488075*x50 - .0230984549487647*y50 + .0632592432005707*z50
	y65 := -.0283697093338637*x50 + 1.0099953980813041*y50 + .0210414411919173*z50
	z65 := .0123140148644819*x50 - .0205076492988989*y50 + 1.330365926242124*z50
	red := 3.2409699419045226*x65 - 1.537383177570094*y65 - .4986107602930034*z65
	green := -.9692436362808796*x65 + 1.8759675015077202*y65 + .0415550574071756*z65
	blue := .0556300796969937*x65 - .2039769588889765*y65 + 1.0569715142428786*z65
	red, green, blue = linearToSRGB(red), linearToSRGB(green), linearToSRGB(blue)
	if inSRGBGamut(red, green, blue) {
		return red, green, blue
	}
	okLightness, okA, okB := srgbToOKLab(red, green, blue)
	return gamutMapOKLabToSRGB(okLightness, okA, okB)
}

func labForward(value float64) float64 {
	const epsilon = 216.0 / 24389
	const kappa = 24389.0 / 27
	if value > epsilon {
		return math.Cbrt(value)
	}
	return (kappa*value + 16) / 116
}

func labInverse(value float64) float64 {
	const epsilon = 216.0 / 24389
	const kappa = 24389.0 / 27
	cubed := value * value * value
	if cubed > epsilon {
		return cubed
	}
	return (116*value - 16) / kappa
}

func nrgbaFromUnit(red, green, blue, alpha float64) color.NRGBA {
	component := func(value float64) uint8 {
		return uint8(math.Round(clamp(value, 0, 1) * 255))
	}
	return color.NRGBA{R: component(red), G: component(green), B: component(blue), A: component(alpha)}
}
