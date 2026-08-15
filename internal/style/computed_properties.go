package style

import (
	"image/color"
	"strconv"
)

// computedPropertyNames is the canonical, deterministic enumeration order for
// the layout-independent longhands currently represented by ComputedStyle.
// Shorthands are deliberately excluded: callers observe the values the style
// engine actually owns rather than reconstructed declarations.
var computedPropertyNames = [...]string{
	"background-color",
	"border-bottom-color",
	"border-bottom-style",
	"border-bottom-width",
	"border-left-color",
	"border-left-style",
	"border-left-width",
	"border-right-color",
	"border-right-style",
	"border-right-width",
	"border-top-color",
	"border-top-style",
	"border-top-width",
	"color",
	"display",
	"font-size",
	"font-weight",
	"height",
	"line-height",
	"list-style-type",
	"margin-bottom",
	"margin-left",
	"margin-right",
	"margin-top",
	"max-width",
	"min-width",
	"opacity",
	"padding-bottom",
	"padding-left",
	"padding-right",
	"padding-top",
	"text-align",
	"text-decoration-line",
	"width",
}

// ComputedPropertyNames returns the canonical names of the layout-independent
// longhands supported by ComputedStyle, followed by the effective custom
// properties in ascending byte order. The returned slice is owned by the
// caller.
func ComputedPropertyNames(computed ComputedStyle) []string {
	custom := computed.customProperties.Names()
	names := make([]string, 0, len(computedPropertyNames)+len(custom))
	names = append(names, computedPropertyNames[:]...)
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

	switch asciiLower(property) {
	case "background-color":
		background, _ := computed.Background()
		return serializeComputedColor(background), true
	case "border-bottom-color":
		return serializeComputedBorderColor(computed.borderBottom, computed.color), true
	case "border-bottom-style":
		return serializeComputedBorderStyle(computed.borderBottom.style), true
	case "border-bottom-width":
		return serializeComputedBorderWidth(computed.borderBottom), true
	case "border-left-color":
		return serializeComputedBorderColor(computed.borderLeft, computed.color), true
	case "border-left-style":
		return serializeComputedBorderStyle(computed.borderLeft.style), true
	case "border-left-width":
		return serializeComputedBorderWidth(computed.borderLeft), true
	case "border-right-color":
		return serializeComputedBorderColor(computed.borderRight, computed.color), true
	case "border-right-style":
		return serializeComputedBorderStyle(computed.borderRight.style), true
	case "border-right-width":
		return serializeComputedBorderWidth(computed.borderRight), true
	case "border-top-color":
		return serializeComputedBorderColor(computed.borderTop, computed.color), true
	case "border-top-style":
		return serializeComputedBorderStyle(computed.borderTop.style), true
	case "border-top-width":
		return serializeComputedBorderWidth(computed.borderTop), true
	case "color":
		return serializeComputedColor(computed.color), true
	case "display":
		return serializeComputedDisplay(computed.display), true
	case "font-size":
		return serializeComputedNumber(computed.fontSize) + "px", true
	case "font-weight":
		return strconv.Itoa(computed.fontWeightValue), true
	case "height":
		return serializeComputedLength(computed.height), true
	case "line-height":
		if computed.lineHeight.normal {
			return "normal", true
		}
		value := serializeComputedNumber(computed.lineHeight.value)
		if computed.lineHeight.absolute {
			value += "px"
		}
		return value, true
	case "list-style-type":
		return serializeComputedListStyle(computed.listStyleType), true
	case "margin-bottom":
		return serializeComputedLength(computed.marginBottom), true
	case "margin-left":
		return serializeComputedLength(computed.marginLeft), true
	case "margin-right":
		return serializeComputedLength(computed.marginRight), true
	case "margin-top":
		return serializeComputedLength(computed.marginTop), true
	case "max-width":
		if computed.maxWidth.unit == LengthAuto {
			return "none", true
		}
		return serializeComputedLength(computed.maxWidth), true
	case "min-width":
		return serializeComputedLength(computed.minWidth), true
	case "opacity":
		return serializeComputedNumber(computed.opacity), true
	case "padding-bottom":
		return serializeComputedLength(computed.paddingBottom), true
	case "padding-left":
		return serializeComputedLength(computed.paddingLeft), true
	case "padding-right":
		return serializeComputedLength(computed.paddingRight), true
	case "padding-top":
		return serializeComputedLength(computed.paddingTop), true
	case "text-align":
		return serializeComputedTextAlignment(computed.textAlign), true
	case "text-decoration-line":
		if computed.textDecoration == TextDecorationUnderline {
			return "underline", true
		}
		return "none", true
	case "width":
		return serializeComputedLength(computed.width), true
	default:
		return "", false
	}
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
	case DisplayNone:
		return "none"
	default:
		return "inline"
	}
}

func serializeComputedBorderStyle(value BorderStyle) string {
	switch value {
	case BorderStyleSolid:
		return "solid"
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
