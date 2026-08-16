package style

import (
	"image/color"
	"strconv"
)

// ComputedPropertyNames returns the canonical names of the layout-independent
// longhands supported by ComputedStyle, followed by the effective custom
// properties in ascending byte order. The returned slice is owned by the
// caller.
func ComputedPropertyNames(computed ComputedStyle) []string {
	custom := computed.customProperties.Names()
	names := make([]string, 0, len(computedPropertyNames)+len(custom))
	names = append(names, computedPropertyNames...)
	names = append(names, custom...)
	return names
}

// ComputedPropertyValue serializes one layout-independent computed value.
// Supported ordinary property names are matched ASCII case-insensitively.
// Custom-property names remain case-sensitive and their resolved value is
// returned verbatim, including an empty value when it is present.
//
// Percentages, viewport units, and auto remain in computed-value form. Values
// that depend on a box's used geometry are outside this API's current scope.
func ComputedPropertyValue(computed ComputedStyle, property string) (string, bool) {
	if len(property) >= 2 && property[0] == '-' && property[1] == '-' {
		return computed.customProperties.Value(property)
	}

	definition, ok := lookupPropertyDefinition(asciiLower(property))
	if !ok {
		return "", false
	}
	return definition.serialize(computed), true
}

func asciiLower(source string) string {
	result := []byte(source)
	for index, value := range result {
		if value >= 'A' && value <= 'Z' {
			result[index] = value + ('a' - 'A')
		}
	}
	return string(result)
}

func serializeComputedNumber(value float64) string {
	if value == 0 {
		return "0"
	}
	return strconv.FormatFloat(value, 'f', -1, 64)
}

func serializeComputedLength(value Length) string {
	if value.unit == LengthCalculated && value.calculation != nil {
		return serializeLengthExpression(*value.calculation, false)
	}
	suffix := ""
	switch value.unit {
	case LengthAuto:
		return "auto"
	case LengthPX:
		suffix = "px"
	case LengthPercent:
		suffix = "%"
	case LengthVW:
		suffix = "vw"
	case LengthVH:
		suffix = "vh"
	case LengthVMin:
		suffix = "vmin"
	case LengthVMax:
		suffix = "vmax"
	}
	return serializeComputedNumber(value.value) + suffix
}

func serializeComputedColor(value color.NRGBA) string {
	red := strconv.Itoa(int(value.R))
	green := strconv.Itoa(int(value.G))
	blue := strconv.Itoa(int(value.B))
	if value.A == 0xff {
		return "rgb(" + red + ", " + green + ", " + blue + ")"
	}
	alpha := float64(value.A) / 255
	for percentage := 0; percentage <= 100; percentage++ {
		// Prefer an exact integer percentage when converting it back to an
		// eight-bit component produces this alpha. This keeps common values such
		// as 128 compact (0.5) without losing the stored color.
		if (percentage*255+50)/100 == int(value.A) {
			alpha = float64(percentage) / 100
			break
		}
	}
	return "rgba(" + red + ", " + green + ", " + blue + ", " + serializeComputedNumber(alpha) + ")"
}

func serializeComputedBorderColor(side BorderSide, currentColor color.NRGBA) string {
	value, explicit := side.Color()
	if !explicit {
		value = currentColor
	}
	return serializeComputedColor(value)
}

func serializeComputedBorderWidth(side BorderSide) string {
	if side.style == BorderStyleNone || side.style == BorderStyleHidden {
		return "0px"
	}
	return serializeComputedLength(side.width)
}

func serializeComputedDisplay(value DisplayMode) string {
	switch value {
	case DisplayInlineBlock:
		return "inline-block"
	case DisplayBlock:
		return "block"
	case DisplayListItem:
		return "list-item"
	case DisplayFlex:
		return "flex"
	case DisplayInlineFlex:
		return "inline-flex"
	case DisplayGrid:
		return "grid"
	case DisplayInlineGrid:
		return "inline-grid"
	case DisplayTable:
		return "table"
	case DisplayInlineTable:
		return "inline-table"
	case DisplayTableRowGroup:
		return "table-row-group"
	case DisplayTableHeaderGroup:
		return "table-header-group"
	case DisplayTableFooterGroup:
		return "table-footer-group"
	case DisplayTableRow:
		return "table-row"
	case DisplayTableCell:
		return "table-cell"
	case DisplayTableColumnGroup:
		return "table-column-group"
	case DisplayTableColumn:
		return "table-column"
	case DisplayTableCaption:
		return "table-caption"
	case DisplayNone:
		return "none"
	default:
		return "inline"
	}
}

func serializeComputedBorderStyle(value BorderStyle) string {
	switch value {
	case BorderStyleDotted:
		return "dotted"
	case BorderStyleDashed:
		return "dashed"
	case BorderStyleSolid:
		return "solid"
	case BorderStyleDouble:
		return "double"
	case BorderStyleGroove:
		return "groove"
	case BorderStyleRidge:
		return "ridge"
	case BorderStyleInset:
		return "inset"
	case BorderStyleOutset:
		return "outset"
	case BorderStyleHidden:
		return "hidden"
	default:
		return "none"
	}
}

func serializeComputedTextAlignment(value TextAlignment) string {
	switch value {
	case AlignCenter:
		return "center"
	case AlignRight:
		return "right"
	case AlignStart:
		return "start"
	case AlignEnd:
		return "end"
	case AlignJustify:
		return "justify"
	default:
		return "left"
	}
}

func serializeComputedListStyle(value ListStyleType) string {
	switch value {
	case ListStyleCircle:
		return "circle"
	case ListStyleSquare:
		return "square"
	case ListStyleDecimal:
		return "decimal"
	case ListStyleNone:
		return "none"
	default:
		return "disc"
	}
}
