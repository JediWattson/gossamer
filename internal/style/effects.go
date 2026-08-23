package style

import (
	"image/color"
	"math"
	"strconv"
	"strings"

	"github.com/JediWattson/gossamer/internal/css"
)

const (
	maxBoxShadows       = 16
	maxBackgroundImages = 8
	maxGradientStops    = 16
	maxTransformOps     = 16
)

// BoxShadow is one computed shadow layer. Lengths remain typed until paint;
// percentages are rejected by the grammar, so every retained value has a
// geometry-independent computed representation.
type BoxShadow struct {
	offsetX      Length
	offsetY      Length
	blurRadius   Length
	spreadRadius Length
	color        color.NRGBA
	currentColor bool
	inset        bool
}

func (shadow BoxShadow) OffsetX() Length      { return shadow.offsetX }
func (shadow BoxShadow) OffsetY() Length      { return shadow.offsetY }
func (shadow BoxShadow) BlurRadius() Length   { return shadow.blurRadius }
func (shadow BoxShadow) SpreadRadius() Length { return shadow.spreadRadius }
func (shadow BoxShadow) Inset() bool          { return shadow.inset }
func (shadow BoxShadow) Color(current color.NRGBA) color.NRGBA {
	if shadow.currentColor {
		return current
	}
	return shadow.color
}

// BoxShadowList is a bounded, allocation-free computed shadow list.
type BoxShadowList struct {
	items [maxBoxShadows]BoxShadow
	count uint8
}

func (shadows BoxShadowList) Len() int { return int(shadows.count) }

func (shadows BoxShadowList) At(index int) (BoxShadow, bool) {
	if index < 0 || index >= int(shadows.count) {
		return BoxShadow{}, false
	}
	return shadows.items[index], true
}

// BackgroundImageKind identifies the currently retained CSS image functions.
type BackgroundImageKind uint8

const (
	BackgroundImageNone BackgroundImageKind = iota
	BackgroundImageURL
	BackgroundImageLinearGradient
	BackgroundImageRadialGradient
)

// GradientStop is one computed color stop. An omitted position remains
// omitted until paint distributes the stop between its neighbors.
type GradientStop struct {
	color        color.NRGBA
	currentColor bool
	position     Length
	hasPosition  bool
}

func (stop GradientStop) Color(current color.NRGBA) color.NRGBA {
	if stop.currentColor {
		return current
	}
	return stop.color
}
func (stop GradientStop) Position() (Length, bool) { return stop.position, stop.hasPosition }

// BackgroundImage is one URL or gradient layer. Linear angles use CSS
// degrees: zero points up and ninety points right.
type BackgroundImage struct {
	kind      BackgroundImageKind
	url       string
	angle     float64
	repeating bool
	stops     [maxGradientStops]GradientStop
	stopCount uint8
}

func (image BackgroundImage) Kind() BackgroundImageKind { return image.kind }
func (image BackgroundImage) URL() string               { return image.url }
func (image BackgroundImage) AngleDegrees() float64     { return image.angle }
func (image BackgroundImage) Repeating() bool           { return image.repeating }
func (image BackgroundImage) StopCount() int            { return int(image.stopCount) }
func (image BackgroundImage) Stop(index int) (GradientStop, bool) {
	if index < 0 || index >= int(image.stopCount) {
		return GradientStop{}, false
	}
	return image.stops[index], true
}

// BackgroundImageList is a bounded computed image-layer list.
type BackgroundImageList struct {
	items [maxBackgroundImages]BackgroundImage
	count uint8
}

func (images BackgroundImageList) Len() int { return int(images.count) }
func (images BackgroundImageList) At(index int) (BackgroundImage, bool) {
	if index < 0 || index >= int(images.count) {
		return BackgroundImage{}, false
	}
	return images.items[index], true
}

// TransformOperationKind identifies one supported two-dimensional transform.
type TransformOperationKind uint8

const (
	TransformMatrix TransformOperationKind = iota + 1
	TransformTranslate
	TransformScale
	TransformRotate
	TransformSkew
)

// TransformOperation retains one computed transform function. A-F carry a
// matrix for TransformMatrix; X/Y carry translate lengths; A/B carry scale;
// and A/B carry radians for rotate/skew.
type TransformOperation struct {
	kind TransformOperationKind
	x    Length
	y    Length
	a    float64
	b    float64
	c    float64
	d    float64
	e    float64
	f    float64
}

func (operation TransformOperation) Kind() TransformOperationKind { return operation.kind }
func (operation TransformOperation) X() Length                    { return operation.x }
func (operation TransformOperation) Y() Length                    { return operation.y }
func (operation TransformOperation) Values() (a, b, c, d, e, f float64) {
	return operation.a, operation.b, operation.c, operation.d, operation.e, operation.f
}

// TransformList is a bounded, allocation-free transform function list.
type TransformList struct {
	items [maxTransformOps]TransformOperation
	count uint8
}

func (transforms TransformList) Len() int { return int(transforms.count) }
func (transforms TransformList) At(index int) (TransformOperation, bool) {
	if index < 0 || index >= int(transforms.count) {
		return TransformOperation{}, false
	}
	return transforms.items[index], true
}

// TransformOrigin retains the two-dimensional origin used by transform.
type TransformOrigin struct {
	x Length
	y Length
}

func (origin TransformOrigin) X() Length { return origin.x }
func (origin TransformOrigin) Y() Length { return origin.y }

func initialTransformOrigin() TransformOrigin {
	return TransformOrigin{x: length{value: 50, unit: lengthPercent}, y: length{value: 50, unit: lengthPercent}}
}

// TranslateValue, ScaleValue, and RotateValue retain the CSS Transforms Level
// 2 individual transform properties. The explicit bit distinguishes their
// initial none value from a specified identity transform for CSSOM reads.
type TranslateValue struct {
	x        Length
	y        Length
	explicit bool
}

func (value TranslateValue) IsNone() bool { return !value.explicit }
func (value TranslateValue) X() Length    { return value.x }
func (value TranslateValue) Y() Length    { return value.y }

type ScaleValue struct {
	x        float64
	y        float64
	explicit bool
}

func (value ScaleValue) IsNone() bool { return !value.explicit }
func (value ScaleValue) X() float64   { return value.x }
func (value ScaleValue) Y() float64   { return value.y }

type RotateValue struct {
	radians  float64
	explicit bool
}

func (value RotateValue) IsNone() bool     { return !value.explicit }
func (value RotateValue) Radians() float64 { return value.radians }

func parseBoxShadowList(source string, emBase float64, viewport Viewport) (BoxShadowList, bool) {
	value, ok := parsePropertyValue(source)
	if !ok || len(value.terms) == 0 {
		return BoxShadowList{}, false
	}
	if keyword, single := value.single(); single {
		if parsed, keywordOK := componentKeyword(keyword); keywordOK && parsed == "none" {
			return BoxShadowList{}, true
		}
	}
	layers, ok := splitComponentGroups(value.terms, maxBoxShadows)
	if !ok {
		return BoxShadowList{}, false
	}
	result := BoxShadowList{}
	for _, layer := range layers {
		shadow, valid := parseBoxShadowLayer(layer, source, emBase, viewport)
		if !valid {
			return BoxShadowList{}, false
		}
		result.items[result.count] = shadow
		result.count++
	}
	return result, true
}

func parseBoxShadowLayer(terms []css.ComponentValue, source string, emBase float64, viewport Viewport) (BoxShadow, bool) {
	shadow := BoxShadow{blurRadius: px(0), spreadRadius: px(0), currentColor: true}
	lengths := make([]Length, 0, 4)
	colorSeen := false
	for _, term := range terms {
		if keyword, ok := componentKeyword(term); ok && keyword == "inset" {
			if shadow.inset {
				return BoxShadow{}, false
			}
			shadow.inset = true
			continue
		}
		if parsed, ok := parseComputedColorComponent(term); ok {
			if colorSeen {
				return BoxShadow{}, false
			}
			shadow.color, shadow.currentColor, colorSeen = parsed.value, parsed.currentColor, true
			continue
		}
		parsed, ok := parseLengthComponent(term, source, emBase, viewport)
		if !ok || parsed.unit == lengthAuto || parsed.DependsOnPercent() || len(lengths) == 4 {
			return BoxShadow{}, false
		}
		lengths = append(lengths, parsed)
	}
	if len(lengths) < 2 {
		return BoxShadow{}, false
	}
	shadow.offsetX, shadow.offsetY = lengths[0], lengths[1]
	if len(lengths) >= 3 {
		blur, resolved := lengths[2].Resolve(0, float64(viewport.Width), float64(viewport.Height))
		if !resolved || blur < 0 {
			return BoxShadow{}, false
		}
		shadow.blurRadius = lengths[2]
	}
	if len(lengths) == 4 {
		shadow.spreadRadius = lengths[3]
	}
	return shadow, true
}

func serializeBoxShadowList(shadows BoxShadowList) string {
	if shadows.count == 0 {
		return "none"
	}
	layers := make([]string, 0, shadows.count)
	for index := 0; index < int(shadows.count); index++ {
		shadow := shadows.items[index]
		parts := []string{serializeComputedLength(shadow.offsetX), serializeComputedLength(shadow.offsetY)}
		if shadow.blurRadius != px(0) || shadow.spreadRadius != px(0) {
			parts = append(parts, serializeComputedLength(shadow.blurRadius))
		}
		if shadow.spreadRadius != px(0) {
			parts = append(parts, serializeComputedLength(shadow.spreadRadius))
		}
		if shadow.currentColor {
			parts = append(parts, "currentcolor")
		} else {
			parts = append(parts, serializeComputedColor(shadow.color))
		}
		if shadow.inset {
			parts = append(parts, "inset")
		}
		layers = append(layers, strings.Join(parts, " "))
	}
	return strings.Join(layers, ", ")
}

func parseBackgroundImageList(source string, emBase float64, viewport Viewport) (BackgroundImageList, bool) {
	value, ok := parsePropertyValue(source)
	if !ok || len(value.terms) == 0 {
		return BackgroundImageList{}, false
	}
	if component, single := value.single(); single {
		if keyword, keywordOK := componentKeyword(component); keywordOK && keyword == "none" {
			return BackgroundImageList{}, true
		}
	}
	layers, ok := splitComponentGroups(value.terms, maxBackgroundImages)
	if !ok {
		return BackgroundImageList{}, false
	}
	result := BackgroundImageList{}
	for _, terms := range layers {
		if len(terms) != 1 {
			return BackgroundImageList{}, false
		}
		parsed, valid := parseBackgroundImage(terms[0], source, emBase, viewport)
		if !valid {
			return BackgroundImageList{}, false
		}
		result.items[result.count] = parsed
		result.count++
	}
	return result, true
}

func parseBackgroundImage(component css.ComponentValue, source string, emBase float64, viewport Viewport) (BackgroundImage, bool) {
	if token, ok := componentToken(component); ok && token.Kind == css.TokenURL {
		return BackgroundImage{kind: BackgroundImageURL, url: token.Value}, token.Value != ""
	}
	if component.Kind != css.ComponentFunction {
		return BackgroundImage{}, false
	}
	name := lowerASCIIValue(component.Token.Value)
	if name == "url" {
		values := effectNonWhitespaceComponents(component.Values)
		if len(values) != 1 {
			return BackgroundImage{}, false
		}
		token, ok := componentToken(values[0])
		if !ok || (token.Kind != css.TokenString && token.Kind != css.TokenURL) || token.Value == "" {
			return BackgroundImage{}, false
		}
		return BackgroundImage{kind: BackgroundImageURL, url: token.Value}, true
	}
	kind := BackgroundImageNone
	repeating := false
	switch name {
	case "linear-gradient":
		kind = BackgroundImageLinearGradient
	case "repeating-linear-gradient":
		kind, repeating = BackgroundImageLinearGradient, true
	case "radial-gradient":
		kind = BackgroundImageRadialGradient
	case "repeating-radial-gradient":
		kind, repeating = BackgroundImageRadialGradient, true
	default:
		return BackgroundImage{}, false
	}
	groups, ok := splitComponentGroups(effectNonWhitespaceComponents(component.Values), maxGradientStops+1)
	if !ok || len(groups) < 2 {
		return BackgroundImage{}, false
	}
	image := BackgroundImage{kind: kind, angle: 180, repeating: repeating}
	start := 0
	if kind == BackgroundImageLinearGradient {
		if angle, ok := parseLinearGradientDirection(groups[0]); ok {
			image.angle = angle
			start = 1
		}
	} else if _, ok := parseGradientStop(groups[0], source, emBase, viewport); !ok {
		// The first radial group contains shape/size/position. The retained
		// foundation paints it centered with farthest-corner sizing.
		start = 1
	}
	if len(groups)-start < 2 || len(groups)-start > maxGradientStops {
		return BackgroundImage{}, false
	}
	for _, group := range groups[start:] {
		stop, valid := parseGradientStop(group, source, emBase, viewport)
		if !valid {
			return BackgroundImage{}, false
		}
		image.stops[image.stopCount] = stop
		image.stopCount++
	}
	return image, true
}

func parseGradientStop(terms []css.ComponentValue, source string, emBase float64, viewport Viewport) (GradientStop, bool) {
	if len(terms) < 1 || len(terms) > 2 {
		return GradientStop{}, false
	}
	parsed, ok := parseComputedColorComponent(terms[0])
	if !ok {
		return GradientStop{}, false
	}
	stop := GradientStop{color: parsed.value, currentColor: parsed.currentColor}
	if len(terms) == 2 {
		position, ok := parseLengthComponent(terms[1], source, emBase, viewport)
		if !ok || position.unit == lengthAuto {
			return GradientStop{}, false
		}
		stop.position, stop.hasPosition = position, true
	}
	return stop, true
}

func parseLinearGradientDirection(terms []css.ComponentValue) (float64, bool) {
	if len(terms) == 1 {
		if angle, ok := parseAngleComponent(terms[0]); ok {
			return normalizeDegrees(angle * 180 / math.Pi), true
		}
	}
	if len(terms) < 2 || len(terms) > 3 {
		return 0, false
	}
	first, ok := componentKeyword(terms[0])
	if !ok || first != "to" {
		return 0, false
	}
	x, y := 0, 0
	horizontal, vertical := false, false
	for _, term := range terms[1:] {
		keyword, ok := componentKeyword(term)
		if !ok {
			return 0, false
		}
		switch keyword {
		case "left":
			if horizontal {
				return 0, false
			}
			horizontal = true
			x--
		case "right":
			if horizontal {
				return 0, false
			}
			horizontal = true
			x++
		case "top":
			if vertical {
				return 0, false
			}
			vertical = true
			y--
		case "bottom":
			if vertical {
				return 0, false
			}
			vertical = true
			y++
		default:
			return 0, false
		}
	}
	if x == 0 && y == 0 {
		return 0, false
	}
	return normalizeDegrees(math.Atan2(float64(x), float64(-y)) * 180 / math.Pi), true
}

func serializeBackgroundImageList(images BackgroundImageList) string {
	if images.count == 0 {
		return "none"
	}
	layers := make([]string, 0, images.count)
	for index := 0; index < int(images.count); index++ {
		image := images.items[index]
		if image.kind == BackgroundImageURL {
			layers = append(layers, "url("+strconv.Quote(image.url)+")")
			continue
		}
		name := "linear-gradient"
		prefix := serializeComputedNumber(image.angle) + "deg"
		if image.kind == BackgroundImageRadialGradient {
			name, prefix = "radial-gradient", "circle at center"
		}
		if image.repeating {
			name = "repeating-" + name
		}
		parts := []string{prefix}
		for stopIndex := 0; stopIndex < int(image.stopCount); stopIndex++ {
			stop := image.stops[stopIndex]
			part := "currentcolor"
			if !stop.currentColor {
				part = serializeComputedColor(stop.color)
			}
			if stop.hasPosition {
				part += " " + serializeComputedLength(stop.position)
			}
			parts = append(parts, part)
		}
		layers = append(layers, name+"("+strings.Join(parts, ", ")+")")
	}
	return strings.Join(layers, ", ")
}

func parseTransformList(source string, emBase float64, viewport Viewport) (TransformList, bool) {
	value, ok := parsePropertyValue(source)
	if !ok || len(value.terms) == 0 {
		return TransformList{}, false
	}
	if component, single := value.single(); single {
		if keyword, keywordOK := componentKeyword(component); keywordOK && keyword == "none" {
			return TransformList{}, true
		}
	}
	if len(value.terms) > maxTransformOps {
		return TransformList{}, false
	}
	result := TransformList{}
	for _, term := range value.terms {
		operation, valid := parseTransformOperation(term, source, emBase, viewport)
		if !valid {
			return TransformList{}, false
		}
		result.items[result.count] = operation
		result.count++
	}
	return result, true
}

func parseTransformOperation(function css.ComponentValue, source string, emBase float64, viewport Viewport) (TransformOperation, bool) {
	if function.Kind != css.ComponentFunction {
		return TransformOperation{}, false
	}
	name := lowerASCIIValue(function.Token.Value)
	terms, argumentsOK := effectFunctionArguments(function.Values)
	if !argumentsOK {
		return TransformOperation{}, false
	}
	lengthAt := func(index int) (Length, bool) {
		if index < 0 || index >= len(terms) {
			return Length{}, false
		}
		parsed, ok := parseLengthComponent(terms[index], source, emBase, viewport)
		return parsed, ok && parsed.unit != lengthAuto
	}
	numberAt := func(index int) (float64, bool) {
		if index < 0 || index >= len(terms) {
			return 0, false
		}
		token, ok := componentToken(terms[index])
		return token.Number, ok && token.Kind == css.TokenNumber && isFinite(token.Number)
	}
	switch name {
	case "matrix":
		if len(terms) != 6 {
			return TransformOperation{}, false
		}
		values := [6]float64{}
		for index := range values {
			parsed, ok := numberAt(index)
			if !ok {
				return TransformOperation{}, false
			}
			values[index] = parsed
		}
		return TransformOperation{kind: TransformMatrix, a: values[0], b: values[1], c: values[2], d: values[3], e: values[4], f: values[5]}, true
	case "translate", "translatex", "translatey":
		if len(terms) < 1 || len(terms) > 2 || name != "translate" && len(terms) != 1 {
			return TransformOperation{}, false
		}
		zero := px(0)
		x, y := zero, zero
		var ok bool
		if name == "translatey" {
			y, ok = lengthAt(0)
		} else {
			x, ok = lengthAt(0)
			if ok && len(terms) == 2 {
				y, ok = lengthAt(1)
			}
		}
		return TransformOperation{kind: TransformTranslate, x: x, y: y}, ok
	case "scale", "scalex", "scaley":
		if len(terms) < 1 || len(terms) > 2 || name != "scale" && len(terms) != 1 {
			return TransformOperation{}, false
		}
		x, ok := numberAt(0)
		if !ok {
			return TransformOperation{}, false
		}
		y := x
		if name == "scalex" {
			y = 1
		} else if name == "scaley" {
			y, x = x, 1
		} else if len(terms) == 2 {
			y, ok = numberAt(1)
		}
		return TransformOperation{kind: TransformScale, a: x, b: y}, ok
	case "rotate":
		if len(terms) != 1 {
			return TransformOperation{}, false
		}
		angle, ok := parseAngleComponent(terms[0])
		return TransformOperation{kind: TransformRotate, a: angle}, ok
	case "skew", "skewx", "skewy":
		if len(terms) < 1 || len(terms) > 2 || name != "skew" && len(terms) != 1 {
			return TransformOperation{}, false
		}
		x, y := 0.0, 0.0
		var ok bool
		if name == "skewy" {
			y, ok = parseAngleComponent(terms[0])
		} else {
			x, ok = parseAngleComponent(terms[0])
			if ok && len(terms) == 2 {
				y, ok = parseAngleComponent(terms[1])
			}
		}
		return TransformOperation{kind: TransformSkew, a: x, b: y}, ok
	default:
		return TransformOperation{}, false
	}
}

func parseTransformOrigin(source string, emBase float64, viewport Viewport) (TransformOrigin, bool) {
	value, ok := parsePropertyValue(source)
	if !ok || len(value.terms) < 1 || len(value.terms) > 2 {
		return TransformOrigin{}, false
	}
	center := length{value: 50, unit: lengthPercent}
	parseAxis := func(component css.ComponentValue, horizontal bool) (Length, bool) {
		if keyword, ok := componentKeyword(component); ok {
			switch keyword {
			case "center":
				return center, true
			case "left":
				if horizontal {
					return px(0), true
				}
			case "right":
				if horizontal {
					return length{value: 100, unit: lengthPercent}, true
				}
			case "top":
				if !horizontal {
					return px(0), true
				}
			case "bottom":
				if !horizontal {
					return length{value: 100, unit: lengthPercent}, true
				}
			}
			return Length{}, false
		}
		parsed, ok := parseLengthComponent(component, source, emBase, viewport)
		return parsed, ok && parsed.unit != lengthAuto
	}
	if len(value.terms) == 1 {
		if keyword, keywordOK := componentKeyword(value.terms[0]); keywordOK && (keyword == "top" || keyword == "bottom") {
			y, ok := parseAxis(value.terms[0], false)
			return TransformOrigin{x: center, y: y}, ok
		}
		x, ok := parseAxis(value.terms[0], true)
		return TransformOrigin{x: x, y: center}, ok
	}
	x, xOK := parseAxis(value.terms[0], true)
	y, yOK := parseAxis(value.terms[1], false)
	if !xOK || !yOK {
		// Permit the vertical keyword first, as CSS does.
		y, yOK = parseAxis(value.terms[0], false)
		x, xOK = parseAxis(value.terms[1], true)
	}
	return TransformOrigin{x: x, y: y}, xOK && yOK
}

func parseTranslateValue(source string, emBase float64, viewport Viewport) (TranslateValue, bool) {
	value, ok := parsePropertyValue(source)
	if !ok || len(value.terms) < 1 || len(value.terms) > 2 {
		return TranslateValue{}, false
	}
	if component, single := value.single(); single {
		if keyword, keywordOK := componentKeyword(component); keywordOK && keyword == "none" {
			return TranslateValue{}, true
		}
	}
	parsed := TranslateValue{x: px(0), y: px(0), explicit: true}
	parsed.x, ok = parseLengthComponent(value.terms[0], source, emBase, viewport)
	if !ok || parsed.x.unit == lengthAuto {
		return TranslateValue{}, false
	}
	if len(value.terms) == 2 {
		parsed.y, ok = parseLengthComponent(value.terms[1], source, emBase, viewport)
		if !ok || parsed.y.unit == lengthAuto {
			return TranslateValue{}, false
		}
	}
	return parsed, true
}

func parseScaleValue(source string) (ScaleValue, bool) {
	value, ok := parsePropertyValue(source)
	if !ok || len(value.terms) < 1 || len(value.terms) > 2 {
		return ScaleValue{}, false
	}
	if component, single := value.single(); single {
		if keyword, keywordOK := componentKeyword(component); keywordOK && keyword == "none" {
			return ScaleValue{}, true
		}
	}
	parse := func(component css.ComponentValue) (float64, bool) {
		token, tokenOK := componentToken(component)
		if !tokenOK || !isFinite(token.Number) {
			return 0, false
		}
		switch token.Kind {
		case css.TokenNumber:
			return token.Number, true
		case css.TokenPercentage:
			return token.Number / 100, true
		default:
			return 0, false
		}
	}
	x, ok := parse(value.terms[0])
	if !ok {
		return ScaleValue{}, false
	}
	y := x
	if len(value.terms) == 2 {
		y, ok = parse(value.terms[1])
		if !ok {
			return ScaleValue{}, false
		}
	}
	return ScaleValue{x: x, y: y, explicit: true}, true
}

func parseRotateValue(source string) (RotateValue, bool) {
	value, ok := parsePropertyValue(source)
	if !ok {
		return RotateValue{}, false
	}
	component, single := value.single()
	if !single {
		return RotateValue{}, false
	}
	if keyword, keywordOK := componentKeyword(component); keywordOK && keyword == "none" {
		return RotateValue{}, true
	}
	radians, ok := parseAngleComponent(component)
	return RotateValue{radians: radians, explicit: true}, ok
}

func serializeTransformList(transforms TransformList) string {
	if transforms.count == 0 {
		return "none"
	}
	parts := make([]string, 0, transforms.count)
	for index := 0; index < int(transforms.count); index++ {
		operation := transforms.items[index]
		switch operation.kind {
		case TransformMatrix:
			parts = append(parts, "matrix("+strings.Join([]string{
				serializeComputedNumber(operation.a), serializeComputedNumber(operation.b),
				serializeComputedNumber(operation.c), serializeComputedNumber(operation.d),
				serializeComputedNumber(operation.e), serializeComputedNumber(operation.f),
			}, ", ")+")")
		case TransformTranslate:
			parts = append(parts, "translate("+serializeComputedLength(operation.x)+", "+serializeComputedLength(operation.y)+")")
		case TransformScale:
			parts = append(parts, "scale("+serializeComputedNumber(operation.a)+", "+serializeComputedNumber(operation.b)+")")
		case TransformRotate:
			parts = append(parts, "rotate("+serializeComputedNumber(operation.a*180/math.Pi)+"deg)")
		case TransformSkew:
			parts = append(parts, "skew("+serializeComputedNumber(operation.a*180/math.Pi)+"deg, "+serializeComputedNumber(operation.b*180/math.Pi)+"deg)")
		}
	}
	return strings.Join(parts, " ")
}

func serializeTransformOrigin(origin TransformOrigin) string {
	return serializeComputedLength(origin.x) + " " + serializeComputedLength(origin.y)
}

func serializeTranslateValue(value TranslateValue) string {
	if value.IsNone() {
		return "none"
	}
	return serializeComputedLength(value.x) + " " + serializeComputedLength(value.y)
}

func serializeScaleValue(value ScaleValue) string {
	if value.IsNone() {
		return "none"
	}
	return serializeComputedNumber(value.x) + " " + serializeComputedNumber(value.y)
}

func serializeRotateValue(value RotateValue) string {
	if value.IsNone() {
		return "none"
	}
	return serializeComputedNumber(value.radians*180/math.Pi) + "deg"
}

func parseAngleComponent(component css.ComponentValue) (float64, bool) {
	token, ok := componentToken(component)
	if !ok {
		return 0, false
	}
	if token.Kind == css.TokenNumber && token.Number == 0 {
		return 0, true
	}
	if token.Kind != css.TokenDimension || !isFinite(token.Number) {
		return 0, false
	}
	switch lowerASCIIValue(token.Value) {
	case "deg":
		return token.Number * math.Pi / 180, true
	case "grad":
		return token.Number * math.Pi / 200, true
	case "rad":
		return token.Number, true
	case "turn":
		return token.Number * 2 * math.Pi, true
	default:
		return 0, false
	}
}

func splitComponentGroups(values []css.ComponentValue, limit int) ([][]css.ComponentValue, bool) {
	groups := make([][]css.ComponentValue, 1, min(limit, 4))
	for _, value := range values {
		if token, ok := componentToken(value); ok && token.Kind == css.TokenComma {
			if len(groups[len(groups)-1]) == 0 || len(groups) == limit {
				return nil, false
			}
			groups = append(groups, nil)
			continue
		}
		if valueWhitespace(value) {
			continue
		}
		groups[len(groups)-1] = append(groups[len(groups)-1], value)
	}
	if len(groups[len(groups)-1]) == 0 {
		return nil, false
	}
	return groups, true
}

func effectNonWhitespaceComponents(values []css.ComponentValue) []css.ComponentValue {
	result := make([]css.ComponentValue, 0, len(values))
	for _, value := range values {
		if !valueWhitespace(value) {
			result = append(result, value)
		}
	}
	return result
}

func effectFunctionArguments(values []css.ComponentValue) ([]css.ComponentValue, bool) {
	result := make([]css.ComponentValue, 0, len(values))
	lastWasComma := false
	for _, value := range values {
		if valueWhitespace(value) {
			continue
		}
		if token, ok := componentToken(value); ok && token.Kind == css.TokenComma {
			if len(result) == 0 || lastWasComma {
				return nil, false
			}
			lastWasComma = true
			continue
		}
		result = append(result, value)
		lastWasComma = false
	}
	return result, len(result) > 0 && !lastWasComma
}

func normalizeDegrees(value float64) float64 {
	value = math.Mod(value, 360)
	if value < 0 {
		value += 360
	}
	return value
}
