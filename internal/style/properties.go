package style

import (
	"sort"
	"strconv"
	"strings"

	"github.com/JediWattson/gossamer/internal/css"
)

// propertyInvalidationClass records which downstream stages can observe a
// computed-value change. The current renderer still rebuilds the whole style
// and layout tree, but keeping this metadata beside the property definition
// prevents a later incremental invalidator from growing a second property
// registry.
type propertyInvalidationClass uint8

const (
	propertyInvalidatesLayout propertyInvalidationClass = 1 << iota
	propertyInvalidatesPaint
)

type propertyKind uint8

const (
	propertyBackgroundColor propertyKind = iota
	propertyBorderColor
	propertyBorderStyle
	propertyBorderWidth
	propertyColor
	propertyDisplay
	propertyFontSize
	propertyFontWeight
	propertyHeight
	propertyLineHeight
	propertyListStyleType
	propertyMargin
	propertyMaxWidth
	propertyMinWidth
	propertyOpacity
	propertyPadding
	propertyTextAlign
	propertyTextDecorationLine
	propertyWidth
)

type propertyEdge uint8

const (
	propertyNoEdge propertyEdge = iota
	propertyTop
	propertyRight
	propertyBottom
	propertyLeft
)

// propertyDefinition is the single source of truth for every ordinary
// longhand represented by ComputedStyle. Its kind and edge select the grammar,
// computer, copier, and serializer below; inherited and invalidation are
// declarative metadata used directly by cascade and CSSOM.
type propertyDefinition struct {
	name      string
	kind      propertyKind
	edge      propertyEdge
	inherited bool
	// Font-size computes before other em-dependent longhands.
	computeEarly bool
	// CSS excludes direction and unicode-bidi from all. They are not supported
	// yet, but this flag keeps that exception in the registry when they arrive.
	excludedFromAll bool
	invalidation    propertyInvalidationClass
}

// propertyDefinitions is kept in canonical byte order so CSSStyleDeclaration
// enumeration, all expansion, and deterministic test/debug output agree.
var propertyDefinitions = [...]propertyDefinition{
	{name: "background-color", kind: propertyBackgroundColor, invalidation: propertyInvalidatesPaint},
	{name: "border-bottom-color", kind: propertyBorderColor, edge: propertyBottom, invalidation: propertyInvalidatesPaint},
	{name: "border-bottom-style", kind: propertyBorderStyle, edge: propertyBottom, invalidation: propertyInvalidatesLayout | propertyInvalidatesPaint},
	{name: "border-bottom-width", kind: propertyBorderWidth, edge: propertyBottom, invalidation: propertyInvalidatesLayout | propertyInvalidatesPaint},
	{name: "border-left-color", kind: propertyBorderColor, edge: propertyLeft, invalidation: propertyInvalidatesPaint},
	{name: "border-left-style", kind: propertyBorderStyle, edge: propertyLeft, invalidation: propertyInvalidatesLayout | propertyInvalidatesPaint},
	{name: "border-left-width", kind: propertyBorderWidth, edge: propertyLeft, invalidation: propertyInvalidatesLayout | propertyInvalidatesPaint},
	{name: "border-right-color", kind: propertyBorderColor, edge: propertyRight, invalidation: propertyInvalidatesPaint},
	{name: "border-right-style", kind: propertyBorderStyle, edge: propertyRight, invalidation: propertyInvalidatesLayout | propertyInvalidatesPaint},
	{name: "border-right-width", kind: propertyBorderWidth, edge: propertyRight, invalidation: propertyInvalidatesLayout | propertyInvalidatesPaint},
	{name: "border-top-color", kind: propertyBorderColor, edge: propertyTop, invalidation: propertyInvalidatesPaint},
	{name: "border-top-style", kind: propertyBorderStyle, edge: propertyTop, invalidation: propertyInvalidatesLayout | propertyInvalidatesPaint},
	{name: "border-top-width", kind: propertyBorderWidth, edge: propertyTop, invalidation: propertyInvalidatesLayout | propertyInvalidatesPaint},
	{name: "color", kind: propertyColor, inherited: true, invalidation: propertyInvalidatesPaint},
	{name: "display", kind: propertyDisplay, invalidation: propertyInvalidatesLayout | propertyInvalidatesPaint},
	{name: "font-size", kind: propertyFontSize, inherited: true, computeEarly: true, invalidation: propertyInvalidatesLayout | propertyInvalidatesPaint},
	{name: "font-weight", kind: propertyFontWeight, inherited: true, invalidation: propertyInvalidatesLayout | propertyInvalidatesPaint},
	{name: "height", kind: propertyHeight, invalidation: propertyInvalidatesLayout},
	{name: "line-height", kind: propertyLineHeight, inherited: true, invalidation: propertyInvalidatesLayout | propertyInvalidatesPaint},
	{name: "list-style-type", kind: propertyListStyleType, inherited: true, invalidation: propertyInvalidatesLayout | propertyInvalidatesPaint},
	{name: "margin-bottom", kind: propertyMargin, edge: propertyBottom, invalidation: propertyInvalidatesLayout},
	{name: "margin-left", kind: propertyMargin, edge: propertyLeft, invalidation: propertyInvalidatesLayout},
	{name: "margin-right", kind: propertyMargin, edge: propertyRight, invalidation: propertyInvalidatesLayout},
	{name: "margin-top", kind: propertyMargin, edge: propertyTop, invalidation: propertyInvalidatesLayout},
	{name: "max-width", kind: propertyMaxWidth, invalidation: propertyInvalidatesLayout},
	{name: "min-width", kind: propertyMinWidth, invalidation: propertyInvalidatesLayout},
	{name: "opacity", kind: propertyOpacity, invalidation: propertyInvalidatesPaint},
	{name: "padding-bottom", kind: propertyPadding, edge: propertyBottom, invalidation: propertyInvalidatesLayout},
	{name: "padding-left", kind: propertyPadding, edge: propertyLeft, invalidation: propertyInvalidatesLayout},
	{name: "padding-right", kind: propertyPadding, edge: propertyRight, invalidation: propertyInvalidatesLayout},
	{name: "padding-top", kind: propertyPadding, edge: propertyTop, invalidation: propertyInvalidatesLayout},
	{name: "text-align", kind: propertyTextAlign, inherited: true, invalidation: propertyInvalidatesLayout | propertyInvalidatesPaint},
	{name: "text-decoration-line", kind: propertyTextDecorationLine, invalidation: propertyInvalidatesPaint},
	{name: "width", kind: propertyWidth, invalidation: propertyInvalidatesLayout},
}

var computedPropertyNames = func() []string {
	names := make([]string, len(propertyDefinitions))
	for index := range propertyDefinitions {
		names[index] = propertyDefinitions[index].name
	}
	return names
}()

var allPropertyNames = func() []string {
	names := make([]string, 0, len(propertyDefinitions))
	for index := range propertyDefinitions {
		definition := propertyDefinitions[index]
		if !definition.excludedFromAll {
			names = append(names, definition.name)
		}
	}
	return names
}()

var shorthandTargets = map[string][]string{
	"background":      {"background-color"},
	"border":          {"border-top-width", "border-right-width", "border-bottom-width", "border-left-width", "border-top-style", "border-right-style", "border-bottom-style", "border-left-style", "border-top-color", "border-right-color", "border-bottom-color", "border-left-color"},
	"border-bottom":   {"border-bottom-width", "border-bottom-style", "border-bottom-color"},
	"border-color":    {"border-top-color", "border-right-color", "border-bottom-color", "border-left-color"},
	"border-left":     {"border-left-width", "border-left-style", "border-left-color"},
	"border-right":    {"border-right-width", "border-right-style", "border-right-color"},
	"border-style":    {"border-top-style", "border-right-style", "border-bottom-style", "border-left-style"},
	"border-top":      {"border-top-width", "border-top-style", "border-top-color"},
	"border-width":    {"border-top-width", "border-right-width", "border-bottom-width", "border-left-width"},
	"font":            {"font-size", "font-weight", "line-height"},
	"list-style":      {"list-style-type"},
	"margin":          {"margin-top", "margin-right", "margin-bottom", "margin-left"},
	"padding":         {"padding-top", "padding-right", "padding-bottom", "padding-left"},
	"text-decoration": {"text-decoration-line"},
}

type propertyApplyContext struct {
	parentFontSize   float64
	parentFontWeight int
	viewport         Viewport
}

func lookupPropertyDefinition(name string) (*propertyDefinition, bool) {
	index := sort.Search(len(propertyDefinitions), func(index int) bool {
		return propertyDefinitions[index].name >= name
	})
	if index >= len(propertyDefinitions) || propertyDefinitions[index].name != name {
		return nil, false
	}
	return &propertyDefinitions[index], true
}

func declarationTargets(property string) []string {
	if strings.HasPrefix(property, "--") {
		return []string{property}
	}
	if property == "all" {
		return allPropertyNames
	}
	if definition, ok := lookupPropertyDefinition(property); ok {
		return []string{definition.name}
	}
	return shorthandTargets[property]
}

func copyComputedProperty(destination *computedStyle, source computedStyle, property string) {
	if definition, ok := lookupPropertyDefinition(property); ok {
		definition.copy(destination, source)
	}
}

func (definition propertyDefinition) copy(destination *computedStyle, source computedStyle) {
	switch definition.kind {
	case propertyBackgroundColor:
		destination.background = source.background
		destination.hasBackground = source.hasBackground
	case propertyBorderColor:
		destinationSide := definition.borderSide(destination)
		sourceSide := definition.borderSide(&source)
		destinationSide.color = sourceSide.color
		destinationSide.hasColor = sourceSide.hasColor
	case propertyBorderStyle:
		definition.borderSide(destination).style = definition.borderSide(&source).style
	case propertyBorderWidth:
		definition.borderSide(destination).width = definition.borderSide(&source).width
	case propertyColor:
		destination.color = source.color
	case propertyDisplay:
		destination.display = source.display
	case propertyFontSize:
		destination.fontSize = source.fontSize
	case propertyFontWeight:
		destination.fontWeightValue = source.fontWeightValue
	case propertyHeight:
		destination.height = source.height
	case propertyLineHeight:
		destination.lineHeight = source.lineHeight
	case propertyListStyleType:
		destination.listStyleType = source.listStyleType
	case propertyMargin:
		*definition.boxLength(destination) = *definition.boxLength(&source)
	case propertyMaxWidth:
		destination.maxWidth = source.maxWidth
	case propertyMinWidth:
		destination.minWidth = source.minWidth
	case propertyOpacity:
		destination.opacity = source.opacity
	case propertyPadding:
		*definition.boxLength(destination) = *definition.boxLength(&source)
	case propertyTextAlign:
		destination.textAlign = source.textAlign
	case propertyTextDecorationLine:
		destination.textDecoration = source.textDecoration
		destination.underline = destination.ancestorUnderline || source.textDecoration == TextDecorationUnderline
	case propertyWidth:
		destination.width = source.width
	}
}

func (definition propertyDefinition) resetToInitial(destination *computedStyle, viewport Viewport) {
	definition.copy(destination, cssInitialStyle(viewport))
}

func (definition propertyDefinition) valid(source string, viewport Viewport) bool {
	value := strings.TrimSpace(strings.ToLower(source))
	switch definition.kind {
	case propertyBackgroundColor:
		_, ok := parseColor(firstCSSValue(value))
		return ok
	case propertyBorderColor:
		_, ok := parseBorderColor(value)
		return ok
	case propertyBorderStyle:
		_, ok := parseBorderStyle(value)
		return ok
	case propertyBorderWidth:
		_, ok := parseBorderWidth(value, 1, viewport)
		return ok
	case propertyColor:
		_, ok := parseColor(value)
		return ok
	case propertyDisplay:
		switch value {
		case "none", "block", "list-item", "inline", "inline-block":
			return true
		default:
			return false
		}
	case propertyFontSize:
		parsed, ok := parseLength(value, 1, 1, viewport)
		if !ok || parsed.unit == lengthAuto {
			return false
		}
		resolved := resolveLength(parsed, 1, viewport, 1)
		return resolved > 0 && isFinite(resolved)
	case propertyFontWeight:
		if value == "bold" || value == "bolder" || value == "normal" || value == "lighter" {
			return true
		}
		numeric, err := strconv.Atoi(value)
		return err == nil && numeric >= 1 && numeric <= 1000
	case propertyHeight, propertyMinWidth, propertyWidth:
		parsed, ok := parseLength(value, 1, 1, viewport)
		return ok && nonNegativeLength(parsed)
	case propertyLineHeight:
		if value == "normal" {
			return true
		}
		if numeric, err := strconv.ParseFloat(value, 64); err == nil {
			return numeric > 0 && isFinite(numeric)
		}
		parsed, ok := parseLength(value, 1, 1, viewport)
		if !ok || parsed.unit == lengthAuto {
			return false
		}
		resolved := resolveLength(parsed, 1, viewport, 1)
		return resolved > 0 && isFinite(resolved)
	case propertyListStyleType:
		_, ok := parseListStyleType(value)
		return ok
	case propertyMargin:
		_, ok := parseLength(value, 1, 1, viewport)
		return ok
	case propertyMaxWidth:
		if value == "none" {
			return true
		}
		parsed, ok := parseLength(value, 1, 1, viewport)
		return ok && nonNegativeLength(parsed)
	case propertyOpacity:
		numeric, err := strconv.ParseFloat(value, 64)
		return err == nil && isFinite(numeric)
	case propertyPadding:
		parsed, ok := parseLength(value, 1, 1, viewport)
		return ok && parsed.unit != lengthAuto && nonNegativeLength(parsed)
	case propertyTextAlign:
		switch value {
		case "center", "right", "end", "left", "start", "justify":
			return true
		default:
			return false
		}
	case propertyTextDecorationLine:
		if value == "none" {
			return true
		}
		for _, token := range strings.Fields(value) {
			if token == "underline" {
				return true
			}
		}
		return false
	default:
		return false
	}
}

func (definition propertyDefinition) apply(style *computedStyle, source string, context propertyApplyContext) {
	value := strings.TrimSpace(strings.ToLower(source))
	switch definition.kind {
	case propertyBackgroundColor:
		if parsed, ok := parseColor(firstCSSValue(value)); ok {
			style.background = parsed
			style.hasBackground = parsed.A != 0
		}
	case propertyBorderColor:
		if parsed, ok := parseBorderColor(value); ok {
			applyBorderColor(definition.borderSide(style), parsed)
		}
	case propertyBorderStyle:
		if parsed, ok := parseBorderStyle(value); ok {
			definition.borderSide(style).style = parsed
		}
	case propertyBorderWidth:
		if parsed, ok := parseBorderWidth(value, style.fontSize, context.viewport); ok {
			definition.borderSide(style).width = parsed
		}
	case propertyColor:
		if parsed, ok := parseColor(value); ok {
			style.color = parsed
		}
	case propertyDisplay:
		switch value {
		case "none":
			style.display = displayNone
		case "block":
			style.display = displayBlock
		case "list-item":
			style.display = displayListItem
		case "inline":
			style.display = displayInline
		case "inline-block":
			style.display = displayInlineBlock
		}
	case propertyFontSize:
		if parsed, ok := parseLength(value, context.parentFontSize, context.parentFontSize, context.viewport); ok && parsed.unit != lengthAuto {
			resolved := resolveLength(parsed, context.parentFontSize, context.viewport, context.parentFontSize)
			if resolved > 0 && isFinite(resolved) {
				style.fontSize = resolved
			}
		}
	case propertyFontWeight:
		switch value {
		case "bold":
			style.fontWeightValue = 700
		case "bolder":
			style.fontWeightValue = relativeFontWeight(context.parentFontWeight, true)
		case "normal":
			style.fontWeightValue = 400
		case "lighter":
			style.fontWeightValue = relativeFontWeight(context.parentFontWeight, false)
		default:
			if numeric, err := strconv.Atoi(value); err == nil && numeric >= 1 && numeric <= 1000 {
				style.fontWeightValue = numeric
			}
		}
	case propertyHeight:
		if parsed, ok := parseLength(value, style.fontSize, style.fontSize, context.viewport); ok && nonNegativeLength(parsed) {
			style.height = parsed
		}
	case propertyLineHeight:
		if value == "normal" {
			style.lineHeight = computedLineHeight{value: 1.2, normal: true}
		} else if numeric, err := strconv.ParseFloat(value, 64); err == nil && numeric > 0 && isFinite(numeric) {
			style.lineHeight = computedLineHeight{value: numeric}
		} else if parsed, ok := parseLength(value, style.fontSize, style.fontSize, context.viewport); ok && parsed.unit != lengthAuto {
			resolved := resolveLength(parsed, style.fontSize, context.viewport, style.lineHeight.pixels(style.fontSize))
			if resolved > 0 && isFinite(resolved) {
				style.lineHeight = computedLineHeight{value: resolved, absolute: true}
			}
		}
	case propertyListStyleType:
		if parsed, ok := parseListStyleType(value); ok {
			style.listStyleType = parsed
		}
	case propertyMargin:
		if parsed, ok := parseLength(value, style.fontSize, style.fontSize, context.viewport); ok {
			*definition.boxLength(style) = parsed
		}
	case propertyMaxWidth:
		if value == "none" {
			style.maxWidth = length{unit: lengthAuto}
		} else if parsed, ok := parseLength(value, style.fontSize, style.fontSize, context.viewport); ok && nonNegativeLength(parsed) {
			style.maxWidth = parsed
		}
	case propertyMinWidth:
		if value == "auto" {
			style.minWidth = px(0)
		} else if parsed, ok := parseLength(value, style.fontSize, style.fontSize, context.viewport); ok && nonNegativeLength(parsed) {
			style.minWidth = parsed
		}
	case propertyOpacity:
		if numeric, err := strconv.ParseFloat(value, 64); err == nil && isFinite(numeric) {
			style.opacity = clamp(numeric, 0, 1)
		}
	case propertyPadding:
		if parsed, ok := parseLength(value, style.fontSize, style.fontSize, context.viewport); ok && parsed.unit != lengthAuto && nonNegativeLength(parsed) {
			*definition.boxLength(style) = parsed
		}
	case propertyTextAlign:
		switch value {
		case "center":
			style.textAlign = alignCenter
		case "right":
			style.textAlign = alignRight
		case "end":
			style.textAlign = alignEnd
		case "left":
			style.textAlign = alignLeft
		case "start":
			style.textAlign = alignStart
		case "justify":
			style.textAlign = alignJustify
		}
	case propertyTextDecorationLine:
		if strings.Contains(value, "underline") {
			style.textDecoration = TextDecorationUnderline
			style.underline = true
		} else if value == "none" {
			style.textDecoration = TextDecorationNone
			style.underline = style.ancestorUnderline
		}
	case propertyWidth:
		if parsed, ok := parseLength(value, style.fontSize, style.fontSize, context.viewport); ok && nonNegativeLength(parsed) {
			style.width = parsed
		}
	}
}

func (definition propertyDefinition) serialize(computed ComputedStyle) string {
	switch definition.kind {
	case propertyBackgroundColor:
		return serializeComputedColor(computed.background)
	case propertyBorderColor:
		return serializeComputedBorderColor(*definition.borderSide(&computed), computed.color)
	case propertyBorderStyle:
		return serializeComputedBorderStyle(definition.borderSide(&computed).style)
	case propertyBorderWidth:
		return serializeComputedBorderWidth(*definition.borderSide(&computed))
	case propertyColor:
		return serializeComputedColor(computed.color)
	case propertyDisplay:
		return serializeComputedDisplay(computed.display)
	case propertyFontSize:
		return serializeComputedNumber(computed.fontSize) + "px"
	case propertyFontWeight:
		return strconv.Itoa(computed.fontWeightValue)
	case propertyHeight:
		return serializeComputedLength(computed.height)
	case propertyLineHeight:
		if computed.lineHeight.normal {
			return "normal"
		}
		value := serializeComputedNumber(computed.lineHeight.value)
		if computed.lineHeight.absolute {
			value += "px"
		}
		return value
	case propertyListStyleType:
		return serializeComputedListStyle(computed.listStyleType)
	case propertyMargin, propertyPadding:
		return serializeComputedLength(*definition.boxLength(&computed))
	case propertyMaxWidth:
		if computed.maxWidth.unit == LengthAuto {
			return "none"
		}
		return serializeComputedLength(computed.maxWidth)
	case propertyMinWidth:
		return serializeComputedLength(computed.minWidth)
	case propertyOpacity:
		return serializeComputedNumber(computed.opacity)
	case propertyTextAlign:
		return serializeComputedTextAlignment(computed.textAlign)
	case propertyTextDecorationLine:
		if computed.textDecoration == TextDecorationUnderline {
			return "underline"
		}
		return "none"
	case propertyWidth:
		return serializeComputedLength(computed.width)
	default:
		return ""
	}
}

func (definition propertyDefinition) borderSide(style *computedStyle) *borderSide {
	switch definition.edge {
	case propertyTop:
		return &style.borderTop
	case propertyRight:
		return &style.borderRight
	case propertyBottom:
		return &style.borderBottom
	case propertyLeft:
		return &style.borderLeft
	default:
		panic("style: border property has no edge")
	}
}

func (definition propertyDefinition) boxLength(style *computedStyle) *length {
	switch definition.kind {
	case propertyMargin:
		switch definition.edge {
		case propertyTop:
			return &style.marginTop
		case propertyRight:
			return &style.marginRight
		case propertyBottom:
			return &style.marginBottom
		case propertyLeft:
			return &style.marginLeft
		}
	case propertyPadding:
		switch definition.edge {
		case propertyTop:
			return &style.paddingTop
		case propertyRight:
			return &style.paddingRight
		case propertyBottom:
			return &style.paddingBottom
		case propertyLeft:
			return &style.paddingLeft
		}
	}
	panic("style: box property has no edge")
}

func validCascadedDeclaration(declaration css.Declaration, viewport Viewport) bool {
	return validComputedDeclaration(declaration, viewport)
}

func validComputedDeclaration(declaration css.Declaration, viewport Viewport) bool {
	if definition, ok := lookupPropertyDefinition(declaration.Property); ok {
		return definition.valid(declaration.Value, viewport)
	}

	value := strings.TrimSpace(strings.ToLower(declaration.Value))
	switch declaration.Property {
	case "all":
		return cssWideKeyword(value) != ""
	case "font":
		_, _, _, _, ok := parseFontShorthand(value, viewport)
		return ok
	case "background":
		_, ok := parseColor(firstCSSValue(value))
		return ok
	case "padding":
		_, ok := parsePaddingLengths(value, 1, viewport)
		return ok
	case "border", "border-top", "border-right", "border-bottom", "border-left":
		_, ok := parseBorderShorthand(value, 1, viewport)
		return ok
	case "border-width":
		_, ok := parseBorderWidths(value, 1, viewport)
		return ok
	case "border-style":
		_, ok := parseBorderStyles(value)
		return ok
	case "border-color":
		_, ok := parseBorderColors(value)
		return ok
	case "text-decoration":
		if value == "none" {
			return true
		}
		for _, token := range strings.Fields(value) {
			if token == "underline" {
				return true
			}
		}
		return false
	case "list-style":
		_, ok := parseListStyleType(value)
		return ok
	case "margin":
		_, ok := parseBoxLengths(value, 1, viewport)
		return ok
	default:
		return false
	}
}

func applyDeclaration(style *computedStyle, property, source string, context propertyApplyContext) {
	if definition, ok := lookupPropertyDefinition(property); ok {
		definition.apply(style, source, context)
		return
	}

	value := strings.TrimSpace(strings.ToLower(source))
	switch property {
	case "background":
		if parsed, ok := parseColor(firstCSSValue(value)); ok {
			style.background = parsed
			style.hasBackground = parsed.A != 0
		}
	case "padding":
		if values, ok := parsePaddingLengths(value, style.fontSize, context.viewport); ok {
			style.paddingTop, style.paddingRight, style.paddingBottom, style.paddingLeft = values[0], values[1], values[2], values[3]
		}
	case "border":
		if parsed, ok := parseBorderShorthand(value, style.fontSize, context.viewport); ok {
			style.borderTop, style.borderRight, style.borderBottom, style.borderLeft = parsed, parsed, parsed, parsed
		}
	case "border-top", "border-right", "border-bottom", "border-left":
		if parsed, ok := parseBorderShorthand(value, style.fontSize, context.viewport); ok {
			switch property {
			case "border-top":
				style.borderTop = parsed
			case "border-right":
				style.borderRight = parsed
			case "border-bottom":
				style.borderBottom = parsed
			case "border-left":
				style.borderLeft = parsed
			}
		}
	case "border-width":
		if parsed, ok := parseBorderWidths(value, style.fontSize, context.viewport); ok {
			style.borderTop.width, style.borderRight.width, style.borderBottom.width, style.borderLeft.width = parsed[0], parsed[1], parsed[2], parsed[3]
		}
	case "border-style":
		if parsed, ok := parseBorderStyles(value); ok {
			style.borderTop.style, style.borderRight.style, style.borderBottom.style, style.borderLeft.style = parsed[0], parsed[1], parsed[2], parsed[3]
		}
	case "border-color":
		if parsed, ok := parseBorderColors(value); ok {
			applyBorderColor(&style.borderTop, parsed[0])
			applyBorderColor(&style.borderRight, parsed[1])
			applyBorderColor(&style.borderBottom, parsed[2])
			applyBorderColor(&style.borderLeft, parsed[3])
		}
	case "text-decoration":
		if strings.Contains(value, "underline") {
			style.textDecoration = TextDecorationUnderline
			style.underline = true
		} else if value == "none" {
			style.textDecoration = TextDecorationNone
			style.underline = style.ancestorUnderline
		}
	case "list-style":
		if parsed, ok := parseListStyleType(value); ok {
			style.listStyleType = parsed
		}
	case "margin":
		if values, ok := parseBoxLengths(value, style.fontSize, context.viewport); ok {
			style.marginTop, style.marginRight, style.marginBottom, style.marginLeft = values[0], values[1], values[2], values[3]
		}
	}
}
