package style

import (
	"math"
	"strings"

	"github.com/JediWattson/gossamer/internal/css"
)

// CSS math is intentionally bounded independently of the component-value
// parser. This keeps adversarial but syntactically shallow expressions from
// creating unbounded style and layout work.
const maxLengthMathNodes = 256

type lengthExpressionKind uint8

const (
	lengthExpressionLinear lengthExpressionKind = iota
	lengthExpressionSum
	lengthExpressionScale
	lengthExpressionMin
	lengthExpressionMax
	lengthExpressionClamp
)

type linearLength struct {
	px      float64
	percent float64
	vw      float64
	vh      float64
}

// lengthExpression is immutable after parsing. Slices are shared by copied
// ComputedStyle values and never exposed or mutated.
type lengthExpression struct {
	kind   lengthExpressionKind
	linear linearLength
	factor float64
	args   []lengthExpression
}

func (expression lengthExpression) resolve(percentBase, viewportWidth, viewportHeight float64) float64 {
	switch expression.kind {
	case lengthExpressionLinear:
		return expression.linear.px +
			percentBase*expression.linear.percent/100 +
			viewportWidth*expression.linear.vw/100 +
			viewportHeight*expression.linear.vh/100
	case lengthExpressionSum:
		result := 0.0
		for _, argument := range expression.args {
			result += argument.resolve(percentBase, viewportWidth, viewportHeight)
		}
		return result
	case lengthExpressionScale:
		return expression.factor * expression.args[0].resolve(percentBase, viewportWidth, viewportHeight)
	case lengthExpressionMin:
		result := math.Inf(1)
		for _, argument := range expression.args {
			result = math.Min(result, argument.resolve(percentBase, viewportWidth, viewportHeight))
		}
		return result
	case lengthExpressionMax:
		result := math.Inf(-1)
		for _, argument := range expression.args {
			result = math.Max(result, argument.resolve(percentBase, viewportWidth, viewportHeight))
		}
		return result
	case lengthExpressionClamp:
		minimum := expression.args[0].resolve(percentBase, viewportWidth, viewportHeight)
		preferred := expression.args[1].resolve(percentBase, viewportWidth, viewportHeight)
		maximum := expression.args[2].resolve(percentBase, viewportWidth, viewportHeight)
		return math.Max(minimum, math.Min(preferred, maximum))
	default:
		return math.NaN()
	}
}

func (expression lengthExpression) dependsOnPercent() bool {
	if expression.kind == lengthExpressionLinear && expression.linear.percent != 0 {
		return true
	}
	for _, argument := range expression.args {
		if argument.dependsOnPercent() {
			return true
		}
	}
	return false
}

func (expression lengthExpression) finite() bool {
	if expression.kind == lengthExpressionLinear &&
		(!isFinite(expression.linear.px) || !isFinite(expression.linear.percent) ||
			!isFinite(expression.linear.vw) || !isFinite(expression.linear.vh)) {
		return false
	}
	if expression.kind == lengthExpressionScale && !isFinite(expression.factor) {
		return false
	}
	for _, argument := range expression.args {
		if !argument.finite() {
			return false
		}
	}
	return true
}

func expressionFromLength(value length) (lengthExpression, bool) {
	linear := linearLength{}
	switch value.unit {
	case lengthPX:
		linear.px = value.value
	case lengthPercent:
		linear.percent = value.value
	case lengthVW:
		linear.vw = value.value
	case lengthVH:
		linear.vh = value.value
	case lengthCalc:
		if value.calculation == nil {
			return lengthExpression{}, false
		}
		return *value.calculation, true
	default:
		return lengthExpression{}, false
	}
	return lengthExpression{kind: lengthExpressionLinear, linear: linear}, true
}

func lengthFromExpression(expression lengthExpression) length {
	if expression.kind == lengthExpressionLinear {
		linear := expression.linear
		count := 0
		value := 0.0
		unit := lengthPX
		for _, candidate := range []struct {
			value float64
			unit  lengthUnit
		}{
			{linear.px, lengthPX},
			{linear.percent, lengthPercent},
			{linear.vw, lengthVW},
			{linear.vh, lengthVH},
		} {
			if candidate.value != 0 {
				count++
				value = candidate.value
				unit = candidate.unit
			}
		}
		if count == 0 {
			return px(0)
		}
		if count == 1 {
			return length{value: value, unit: unit}
		}
	}
	return length{unit: lengthCalc, calculation: &expression}
}

type lengthMathValue struct {
	number     float64
	expression lengthExpression
	isNumber   bool
}

type lengthMathParser struct {
	source string
	values []css.ComponentValue
	pos    int
	nodes  *int
}

func parseLengthMath(component css.ComponentValue, source string, emBase float64, viewport Viewport) (length, bool) {
	if component.Kind != css.ComponentFunction {
		return length{}, false
	}
	nodes := 0
	value, ok := parseLengthMathFunction(component, source, emBase, viewport, &nodes)
	if !ok || value.isNumber || !value.expression.finite() {
		return length{}, false
	}
	return lengthFromExpression(value.expression), true
}

func parseLengthMathFunction(component css.ComponentValue, source string, emBase float64, viewport Viewport, nodes *int) (lengthMathValue, bool) {
	if !consumeLengthMathNode(nodes) {
		return lengthMathValue{}, false
	}
	name := lowerASCIIValue(component.Token.Value)
	parser := lengthMathParser{source: source, values: component.Values, nodes: nodes}
	switch name {
	case "calc":
		value, ok := parser.parseSum(emBase, viewport)
		parser.skipWhitespace()
		return value, ok && parser.pos == len(parser.values)
	case "min", "max", "clamp":
		arguments := make([]lengthExpression, 0, 3)
		for {
			value, ok := parser.parseSum(emBase, viewport)
			if !ok || value.isNumber {
				return lengthMathValue{}, false
			}
			arguments = append(arguments, value.expression)
			parser.skipWhitespace()
			if parser.pos == len(parser.values) {
				break
			}
			if !parser.consumeToken(css.TokenComma, "") {
				return lengthMathValue{}, false
			}
			parser.skipWhitespace()
			if parser.pos == len(parser.values) {
				return lengthMathValue{}, false
			}
		}
		kind := lengthExpressionMin
		if name == "max" {
			kind = lengthExpressionMax
		}
		if name == "clamp" {
			kind = lengthExpressionClamp
			if len(arguments) != 3 {
				return lengthMathValue{}, false
			}
		} else if len(arguments) == 0 {
			return lengthMathValue{}, false
		}
		return lengthMathValue{expression: lengthExpression{kind: kind, args: arguments}}, true
	default:
		return lengthMathValue{}, false
	}
}

func (parser *lengthMathParser) parseSum(emBase float64, viewport Viewport) (lengthMathValue, bool) {
	left, ok := parser.parseProduct(emBase, viewport)
	if !ok {
		return lengthMathValue{}, false
	}
	for {
		parser.skipWhitespace()
		if parser.pos >= len(parser.values) {
			return left, true
		}
		operator := parser.values[parser.pos]
		if !componentIsDelimiter(operator, "+") && !componentIsDelimiter(operator, "-") {
			return left, true
		}
		if !binaryPlusMinusHasWhitespace(parser.source, operator.Span) || !consumeLengthMathNode(parser.nodes) {
			return lengthMathValue{}, false
		}
		parser.pos++
		right, valid := parser.parseProduct(emBase, viewport)
		if !valid || left.isNumber != right.isNumber {
			return lengthMathValue{}, false
		}
		sign := 1.0
		if componentIsDelimiter(operator, "-") {
			sign = -1
		}
		if left.isNumber {
			left.number += sign * right.number
			if !isFinite(left.number) {
				return lengthMathValue{}, false
			}
		} else {
			left.expression = addLengthExpressions(left.expression, scaleLengthExpression(right.expression, sign))
		}
	}
}

func (parser *lengthMathParser) parseProduct(emBase float64, viewport Viewport) (lengthMathValue, bool) {
	left, ok := parser.parsePrimary(emBase, viewport)
	if !ok {
		return lengthMathValue{}, false
	}
	for {
		parser.skipWhitespace()
		if parser.pos >= len(parser.values) {
			return left, true
		}
		operator := parser.values[parser.pos]
		if !componentIsDelimiter(operator, "*") && !componentIsDelimiter(operator, "/") {
			return left, true
		}
		if !consumeLengthMathNode(parser.nodes) {
			return lengthMathValue{}, false
		}
		parser.pos++
		right, valid := parser.parsePrimary(emBase, viewport)
		if !valid {
			return lengthMathValue{}, false
		}
		division := componentIsDelimiter(operator, "/")
		switch {
		case left.isNumber && right.isNumber:
			if division {
				if right.number == 0 {
					return lengthMathValue{}, false
				}
				left.number /= right.number
			} else {
				left.number *= right.number
			}
			if !isFinite(left.number) {
				return lengthMathValue{}, false
			}
		case !left.isNumber && right.isNumber:
			factor := right.number
			if division {
				if factor == 0 {
					return lengthMathValue{}, false
				}
				factor = 1 / factor
			}
			left.expression = scaleLengthExpression(left.expression, factor)
		case left.isNumber && !right.isNumber && !division:
			left = lengthMathValue{expression: scaleLengthExpression(right.expression, left.number)}
		default:
			return lengthMathValue{}, false
		}
	}
}

func (parser *lengthMathParser) parsePrimary(emBase float64, viewport Viewport) (lengthMathValue, bool) {
	parser.skipWhitespace()
	if parser.pos >= len(parser.values) {
		return lengthMathValue{}, false
	}
	component := parser.values[parser.pos]
	parser.pos++
	if !consumeLengthMathNode(parser.nodes) {
		return lengthMathValue{}, false
	}
	if token, ok := componentToken(component); ok {
		if token.Kind == css.TokenNumber && isFinite(token.Number) {
			return lengthMathValue{number: token.Number, isNumber: true}, true
		}
		parsed, valid := parseSimpleLengthToken(token, emBase, viewport, false)
		if !valid {
			return lengthMathValue{}, false
		}
		expression, _ := expressionFromLength(parsed)
		return lengthMathValue{expression: expression}, true
	}
	if component.Kind == css.ComponentBlock && component.Token.Kind == css.TokenOpenParen {
		nested := lengthMathParser{source: parser.source, values: component.Values, nodes: parser.nodes}
		value, valid := nested.parseSum(emBase, viewport)
		nested.skipWhitespace()
		return value, valid && nested.pos == len(nested.values)
	}
	if component.Kind == css.ComponentFunction {
		return parseLengthMathFunction(component, parser.source, emBase, viewport, parser.nodes)
	}
	return lengthMathValue{}, false
}

func (parser *lengthMathParser) skipWhitespace() {
	for parser.pos < len(parser.values) && valueWhitespace(parser.values[parser.pos]) {
		parser.pos++
	}
}

func (parser *lengthMathParser) consumeToken(kind css.TokenKind, value string) bool {
	if parser.pos >= len(parser.values) {
		return false
	}
	token, ok := componentToken(parser.values[parser.pos])
	if !ok || token.Kind != kind || value != "" && token.Value != value {
		return false
	}
	parser.pos++
	return true
}

func componentIsDelimiter(component css.ComponentValue, delimiter string) bool {
	token, ok := componentToken(component)
	return ok && token.Kind == css.TokenDelim && token.Value == delimiter
}

func binaryPlusMinusHasWhitespace(source string, span css.Span) bool {
	return span.Start > 0 && span.End < len(source) &&
		cssMathWhitespace(source[span.Start-1]) && cssMathWhitespace(source[span.End])
}

func cssMathWhitespace(value byte) bool {
	return value == ' ' || value == '\t' || value == '\n' || value == '\r' || value == '\f'
}

func consumeLengthMathNode(nodes *int) bool {
	(*nodes)++
	return *nodes <= maxLengthMathNodes
}

func addLengthExpressions(left, right lengthExpression) lengthExpression {
	if left.kind == lengthExpressionLinear && right.kind == lengthExpressionLinear {
		return lengthExpression{kind: lengthExpressionLinear, linear: linearLength{
			px:      left.linear.px + right.linear.px,
			percent: left.linear.percent + right.linear.percent,
			vw:      left.linear.vw + right.linear.vw,
			vh:      left.linear.vh + right.linear.vh,
		}}
	}
	arguments := make([]lengthExpression, 0, 4)
	if left.kind == lengthExpressionSum {
		arguments = append(arguments, left.args...)
	} else {
		arguments = append(arguments, left)
	}
	if right.kind == lengthExpressionSum {
		arguments = append(arguments, right.args...)
	} else {
		arguments = append(arguments, right)
	}
	return lengthExpression{kind: lengthExpressionSum, args: arguments}
}

func scaleLengthExpression(expression lengthExpression, factor float64) lengthExpression {
	if factor == 1 {
		return expression
	}
	if expression.kind == lengthExpressionLinear {
		expression.linear.px *= factor
		expression.linear.percent *= factor
		expression.linear.vw *= factor
		expression.linear.vh *= factor
		return expression
	}
	if factor == 0 {
		return lengthExpression{kind: lengthExpressionLinear}
	}
	return lengthExpression{kind: lengthExpressionScale, factor: factor, args: []lengthExpression{expression}}
}

func serializeLengthExpression(expression lengthExpression, nested bool) string {
	switch expression.kind {
	case lengthExpressionLinear:
		value := serializeLinearLength(expression.linear)
		if nested {
			return value
		}
		return "calc(" + value + ")"
	case lengthExpressionMin, lengthExpressionMax, lengthExpressionClamp:
		name := "min"
		if expression.kind == lengthExpressionMax {
			name = "max"
		} else if expression.kind == lengthExpressionClamp {
			name = "clamp"
		}
		arguments := make([]string, len(expression.args))
		for index, argument := range expression.args {
			arguments[index] = serializeLengthExpression(argument, true)
		}
		return name + "(" + strings.Join(arguments, ", ") + ")"
	case lengthExpressionScale:
		argument := serializeLengthExpression(expression.args[0], true)
		if expression.args[0].kind == lengthExpressionSum {
			argument = "(" + argument + ")"
		}
		value := serializeComputedNumber(expression.factor) + " * " + argument
		if nested {
			return value
		}
		return "calc(" + value + ")"
	case lengthExpressionSum:
		var builder strings.Builder
		for index, argument := range expression.args {
			if index == 0 {
				builder.WriteString(serializeLengthExpression(argument, true))
				continue
			}
			if positive, negative := positiveLengthExpression(argument); negative {
				builder.WriteString(" - ")
				builder.WriteString(serializeLengthExpression(positive, true))
			} else {
				builder.WriteString(" + ")
				builder.WriteString(serializeLengthExpression(argument, true))
			}
		}
		value := builder.String()
		if nested {
			return value
		}
		return "calc(" + value + ")"
	default:
		return "0px"
	}
}

func positiveLengthExpression(expression lengthExpression) (lengthExpression, bool) {
	if expression.kind == lengthExpressionScale && expression.factor < 0 {
		expression.factor = -expression.factor
		return expression, true
	}
	if expression.kind != lengthExpressionLinear {
		return lengthExpression{}, false
	}
	linear := expression.linear
	if linear.px > 0 || linear.percent > 0 || linear.vw > 0 || linear.vh > 0 {
		return lengthExpression{}, false
	}
	if linear.px == 0 && linear.percent == 0 && linear.vw == 0 && linear.vh == 0 {
		return lengthExpression{}, false
	}
	linear.px = -linear.px
	linear.percent = -linear.percent
	linear.vw = -linear.vw
	linear.vh = -linear.vh
	return lengthExpression{kind: lengthExpressionLinear, linear: linear}, true
}

func serializeLinearLength(linear linearLength) string {
	parts := make([]string, 0, 4)
	appendPart := func(value float64, suffix string) {
		if value == 0 {
			return
		}
		magnitude := value
		if magnitude < 0 {
			magnitude = -magnitude
		}
		part := serializeComputedNumber(magnitude) + suffix
		if len(parts) == 0 {
			if value < 0 {
				part = "-" + part
			}
			parts = append(parts, part)
			return
		}
		if value < 0 {
			parts = append(parts, "- "+part)
		} else {
			parts = append(parts, "+ "+part)
		}
	}
	// Percentage first keeps common expressions recognizable (100% - 2px).
	appendPart(linear.percent, "%")
	appendPart(linear.vw, "vw")
	appendPart(linear.vh, "vh")
	appendPart(linear.px, "px")
	if len(parts) == 0 {
		return "0px"
	}
	return strings.Join(parts, " ")
}
