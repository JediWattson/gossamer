package style

import (
	"image/color"
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
	propertyAlignItems propertyKind = iota
	propertyBackgroundColor
	propertyBorderColor
	propertyBorderStyle
	propertyBorderWidth
	propertyBoxSizing
	propertyColor
	propertyContent
	propertyDisplay
	propertyFlexBasis
	propertyFlexDirection
	propertyFlexGrow
	propertyFlexShrink
	propertyFontFamily
	propertyFontSize
	propertyFontStyle
	propertyFontWeight
	propertyHeight
	propertyInset
	propertyJustifyContent
	propertyLineHeight
	propertyListStyleType
	propertyMargin
	propertyMaxHeight
	propertyMaxWidth
	propertyMinHeight
	propertyMinWidth
	propertyOpacity
	propertyOrder
	propertyOverflowX
	propertyOverflowY
	propertyPadding
	propertyPosition
	propertyGap
	propertyTextAlign
	propertyTextDecorationLine
	propertyVerticalAlign
	propertyVisibility
	propertyWhiteSpace
	propertyWidth
	propertyZIndex
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
	{name: "align-items", kind: propertyAlignItems, invalidation: propertyInvalidatesLayout},
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
	{name: "bottom", kind: propertyInset, edge: propertyBottom, invalidation: propertyInvalidatesLayout | propertyInvalidatesPaint},
	{name: "box-sizing", kind: propertyBoxSizing, invalidation: propertyInvalidatesLayout},
	{name: "color", kind: propertyColor, inherited: true, invalidation: propertyInvalidatesPaint},
	{name: "column-gap", kind: propertyGap, edge: propertyRight, invalidation: propertyInvalidatesLayout},
	{name: "content", kind: propertyContent, invalidation: propertyInvalidatesLayout | propertyInvalidatesPaint},
	{name: "display", kind: propertyDisplay, invalidation: propertyInvalidatesLayout | propertyInvalidatesPaint},
	{name: "flex-basis", kind: propertyFlexBasis, invalidation: propertyInvalidatesLayout},
	{name: "flex-direction", kind: propertyFlexDirection, invalidation: propertyInvalidatesLayout},
	{name: "flex-grow", kind: propertyFlexGrow, invalidation: propertyInvalidatesLayout},
	{name: "flex-shrink", kind: propertyFlexShrink, invalidation: propertyInvalidatesLayout},
	{name: "font-family", kind: propertyFontFamily, inherited: true, invalidation: propertyInvalidatesLayout | propertyInvalidatesPaint},
	{name: "font-size", kind: propertyFontSize, inherited: true, computeEarly: true, invalidation: propertyInvalidatesLayout | propertyInvalidatesPaint},
	{name: "font-style", kind: propertyFontStyle, inherited: true, invalidation: propertyInvalidatesLayout | propertyInvalidatesPaint},
	{name: "font-weight", kind: propertyFontWeight, inherited: true, invalidation: propertyInvalidatesLayout | propertyInvalidatesPaint},
	{name: "height", kind: propertyHeight, invalidation: propertyInvalidatesLayout},
	{name: "justify-content", kind: propertyJustifyContent, invalidation: propertyInvalidatesLayout},
	{name: "left", kind: propertyInset, edge: propertyLeft, invalidation: propertyInvalidatesLayout | propertyInvalidatesPaint},
	{name: "line-height", kind: propertyLineHeight, inherited: true, invalidation: propertyInvalidatesLayout | propertyInvalidatesPaint},
	{name: "list-style-type", kind: propertyListStyleType, inherited: true, invalidation: propertyInvalidatesLayout | propertyInvalidatesPaint},
	{name: "margin-bottom", kind: propertyMargin, edge: propertyBottom, invalidation: propertyInvalidatesLayout},
	{name: "margin-left", kind: propertyMargin, edge: propertyLeft, invalidation: propertyInvalidatesLayout},
	{name: "margin-right", kind: propertyMargin, edge: propertyRight, invalidation: propertyInvalidatesLayout},
	{name: "margin-top", kind: propertyMargin, edge: propertyTop, invalidation: propertyInvalidatesLayout},
	{name: "max-height", kind: propertyMaxHeight, invalidation: propertyInvalidatesLayout},
	{name: "max-width", kind: propertyMaxWidth, invalidation: propertyInvalidatesLayout},
	{name: "min-height", kind: propertyMinHeight, invalidation: propertyInvalidatesLayout},
	{name: "min-width", kind: propertyMinWidth, invalidation: propertyInvalidatesLayout},
	{name: "opacity", kind: propertyOpacity, invalidation: propertyInvalidatesPaint},
	{name: "order", kind: propertyOrder, invalidation: propertyInvalidatesLayout | propertyInvalidatesPaint},
	{name: "overflow-x", kind: propertyOverflowX, invalidation: propertyInvalidatesPaint},
	{name: "overflow-y", kind: propertyOverflowY, invalidation: propertyInvalidatesPaint},
	{name: "padding-bottom", kind: propertyPadding, edge: propertyBottom, invalidation: propertyInvalidatesLayout},
	{name: "padding-left", kind: propertyPadding, edge: propertyLeft, invalidation: propertyInvalidatesLayout},
	{name: "padding-right", kind: propertyPadding, edge: propertyRight, invalidation: propertyInvalidatesLayout},
	{name: "padding-top", kind: propertyPadding, edge: propertyTop, invalidation: propertyInvalidatesLayout},
	{name: "position", kind: propertyPosition, invalidation: propertyInvalidatesLayout | propertyInvalidatesPaint},
	{name: "right", kind: propertyInset, edge: propertyRight, invalidation: propertyInvalidatesLayout | propertyInvalidatesPaint},
	{name: "row-gap", kind: propertyGap, edge: propertyBottom, invalidation: propertyInvalidatesLayout},
	{name: "text-align", kind: propertyTextAlign, inherited: true, invalidation: propertyInvalidatesLayout | propertyInvalidatesPaint},
	{name: "text-decoration-line", kind: propertyTextDecorationLine, invalidation: propertyInvalidatesPaint},
	{name: "top", kind: propertyInset, edge: propertyTop, invalidation: propertyInvalidatesLayout | propertyInvalidatesPaint},
	{name: "vertical-align", kind: propertyVerticalAlign, invalidation: propertyInvalidatesLayout | propertyInvalidatesPaint},
	{name: "visibility", kind: propertyVisibility, inherited: true, invalidation: propertyInvalidatesPaint},
	{name: "white-space", kind: propertyWhiteSpace, inherited: true, invalidation: propertyInvalidatesLayout | propertyInvalidatesPaint},
	{name: "width", kind: propertyWidth, invalidation: propertyInvalidatesLayout},
	{name: "z-index", kind: propertyZIndex, invalidation: propertyInvalidatesPaint},
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
	"flex":            {"flex-grow", "flex-shrink", "flex-basis"},
	"font":            {"font-family", "font-size", "font-style", "font-weight", "line-height"},
	"gap":             {"row-gap", "column-gap"},
	"list-style":      {"list-style-type"},
	"margin":          {"margin-top", "margin-right", "margin-bottom", "margin-left"},
	"overflow":        {"overflow-x", "overflow-y"},
	"padding":         {"padding-top", "padding-right", "padding-bottom", "padding-left"},
	"text-decoration": {"text-decoration-line"},
}

type propertyApplyContext struct {
	parentFontSize   float64
	parentFontWeight int
	parentColor      color.NRGBA
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
	case propertyAlignItems:
		destination.alignItems = source.alignItems
	case propertyBackgroundColor:
		destination.background = source.background
		destination.hasBackground = source.hasBackground
		destination.backgroundCurrent = source.backgroundCurrent
	case propertyBorderColor:
		destinationSide := definition.borderSide(destination)
		sourceSide := definition.borderSide(&source)
		destinationSide.color = sourceSide.color
		destinationSide.hasColor = sourceSide.hasColor
	case propertyBorderStyle:
		definition.borderSide(destination).style = definition.borderSide(&source).style
	case propertyBorderWidth:
		definition.borderSide(destination).width = definition.borderSide(&source).width
	case propertyBoxSizing:
		destination.boxSizing = source.boxSizing
	case propertyColor:
		destination.color = source.color
	case propertyContent:
		destination.content = source.content
	case propertyDisplay:
		destination.display = source.display
	case propertyFlexBasis:
		destination.flexBasis = source.flexBasis
	case propertyFlexDirection:
		destination.flexDirection = source.flexDirection
	case propertyFlexGrow:
		destination.flexGrow = source.flexGrow
	case propertyFlexShrink:
		destination.flexShrink = source.flexShrink
	case propertyFontFamily:
		destination.fontFamily = source.fontFamily
		destination.fontFamilyValue = source.fontFamilyValue
	case propertyFontSize:
		destination.fontSize = source.fontSize
	case propertyFontStyle:
		destination.fontStyle = source.fontStyle
	case propertyFontWeight:
		destination.fontWeightValue = source.fontWeightValue
	case propertyHeight:
		destination.height = source.height
	case propertyInset:
		*definition.boxLength(destination) = *definition.boxLength(&source)
	case propertyJustifyContent:
		destination.justifyContent = source.justifyContent
	case propertyLineHeight:
		destination.lineHeight = source.lineHeight
	case propertyListStyleType:
		destination.listStyleType = source.listStyleType
	case propertyMargin:
		*definition.boxLength(destination) = *definition.boxLength(&source)
	case propertyMaxHeight:
		destination.maxHeight = source.maxHeight
	case propertyMaxWidth:
		destination.maxWidth = source.maxWidth
	case propertyMinHeight:
		destination.minHeight = source.minHeight
	case propertyMinWidth:
		destination.minWidth = source.minWidth
	case propertyOpacity:
		destination.opacity = source.opacity
	case propertyOrder:
		destination.order = source.order
	case propertyOverflowX:
		destination.overflowX = source.overflowX
	case propertyOverflowY:
		destination.overflowY = source.overflowY
	case propertyPadding:
		*definition.boxLength(destination) = *definition.boxLength(&source)
	case propertyPosition:
		destination.position = source.position
	case propertyGap:
		*definition.boxLength(destination) = *definition.boxLength(&source)
	case propertyTextAlign:
		destination.textAlign = source.textAlign
	case propertyTextDecorationLine:
		destination.textDecoration = source.textDecoration
		destination.underline = destination.ancestorUnderline || source.textDecoration == TextDecorationUnderline
	case propertyVerticalAlign:
		destination.verticalAlign = source.verticalAlign
	case propertyVisibility:
		destination.visibility = source.visibility
	case propertyWhiteSpace:
		destination.whiteSpace = source.whiteSpace
	case propertyWidth:
		destination.width = source.width
	case propertyZIndex:
		destination.zIndex = source.zIndex
	}
}

func (definition propertyDefinition) resetToInitial(destination *computedStyle, viewport Viewport) {
	definition.copy(destination, cssInitialStyle(viewport))
}

func (definition propertyDefinition) valid(source string, viewport Viewport) bool {
	switch definition.kind {
	case propertyAlignItems:
		keyword, ok := singleCSSKeyword(source)
		return ok && (keyword == "stretch" || keyword == "flex-start" || keyword == "flex-end" || keyword == "center")
	case propertyBackgroundColor:
		_, ok := parseComputedColor(source)
		return ok
	case propertyBorderColor:
		_, ok := parseBorderColor(source)
		return ok
	case propertyBorderStyle:
		_, ok := parseBorderStyle(source)
		return ok
	case propertyBorderWidth:
		_, ok := parseBorderWidth(source, 1, viewport)
		return ok
	case propertyBoxSizing:
		keyword, ok := singleCSSKeyword(source)
		return ok && (keyword == "content-box" || keyword == "border-box")
	case propertyColor:
		_, ok := parseComputedColor(source)
		return ok
	case propertyContent:
		_, ok := parseContentValue(source)
		return ok
	case propertyDisplay:
		keyword, ok := singleCSSKeyword(source)
		if !ok {
			return false
		}
		switch keyword {
		case "none", "block", "list-item", "inline", "inline-block", "flex", "inline-flex",
			"table", "inline-table", "table-row-group", "table-header-group", "table-footer-group",
			"table-row", "table-cell", "table-column-group", "table-column", "table-caption":
			return true
		default:
			return false
		}
	case propertyFlexBasis:
		parsed, ok := parseLength(source, 1, 1, viewport)
		return ok && nonNegativeLength(parsed)
	case propertyFlexDirection:
		keyword, ok := singleCSSKeyword(source)
		return ok && (keyword == "row" || keyword == "row-reverse" || keyword == "column" || keyword == "column-reverse")
	case propertyFlexGrow, propertyFlexShrink:
		token, ok := singleCSSNumber(source)
		return ok && token.Number >= 0
	case propertyFontFamily:
		_, _, ok := parseFontFamily(source)
		return ok
	case propertyFontSize:
		parsed, ok := parseLength(source, 1, 1, viewport)
		if !ok || parsed.unit == lengthAuto {
			return false
		}
		resolved := resolveLength(parsed, 1, viewport, 1)
		return resolved > 0 && isFinite(resolved)
	case propertyFontStyle:
		keyword, ok := singleCSSKeyword(source)
		return ok && (keyword == "normal" || keyword == "italic" || keyword == "oblique")
	case propertyFontWeight:
		if keyword, ok := singleCSSKeyword(source); ok && (keyword == "bold" || keyword == "bolder" || keyword == "normal" || keyword == "lighter") {
			return true
		}
		token, ok := singleCSSNumber(source)
		return ok && token.Integer && token.Number >= 1 && token.Number <= 1000
	case propertyHeight, propertyMinHeight, propertyMinWidth, propertyWidth:
		parsed, ok := parseLength(source, 1, 1, viewport)
		return ok && nonNegativeLength(parsed)
	case propertyInset:
		_, ok := parseLength(source, 1, 1, viewport)
		return ok
	case propertyJustifyContent:
		keyword, ok := singleCSSKeyword(source)
		return ok && (keyword == "flex-start" || keyword == "flex-end" || keyword == "center" || keyword == "space-between" || keyword == "space-around" || keyword == "space-evenly")
	case propertyLineHeight:
		if keyword, ok := singleCSSKeyword(source); ok && keyword == "normal" {
			return true
		}
		if token, ok := singleCSSNumber(source); ok {
			return token.Number > 0
		}
		parsed, ok := parseLength(source, 1, 1, viewport)
		if !ok || parsed.unit == lengthAuto {
			return false
		}
		resolved := resolveLength(parsed, 1, viewport, 1)
		return resolved > 0 && isFinite(resolved)
	case propertyListStyleType:
		_, ok := parseListStyleType(source)
		return ok
	case propertyMargin:
		_, ok := parseLength(source, 1, 1, viewport)
		return ok
	case propertyMaxHeight, propertyMaxWidth:
		if keyword, ok := singleCSSKeyword(source); ok && keyword == "none" {
			return true
		}
		parsed, ok := parseLength(source, 1, 1, viewport)
		return ok && parsed.unit != lengthAuto && nonNegativeLength(parsed)
	case propertyOpacity:
		_, ok := singleCSSNumber(source)
		return ok
	case propertyOrder:
		token, ok := singleCSSNumber(source)
		return ok && token.Integer
	case propertyOverflowX, propertyOverflowY:
		keyword, ok := singleCSSKeyword(source)
		return ok && validOverflowKeyword(keyword)
	case propertyPadding:
		parsed, ok := parseLength(source, 1, 1, viewport)
		return ok && parsed.unit != lengthAuto && nonNegativeLength(parsed)
	case propertyPosition:
		keyword, ok := singleCSSKeyword(source)
		return ok && (keyword == "static" || keyword == "relative" || keyword == "absolute" || keyword == "fixed")
	case propertyGap:
		parsed, ok := parseLength(source, 1, 1, viewport)
		return ok && parsed.unit != lengthAuto && nonNegativeLength(parsed)
	case propertyTextAlign:
		keyword, ok := singleCSSKeyword(source)
		if !ok {
			return false
		}
		switch keyword {
		case "center", "right", "end", "left", "start", "justify":
			return true
		default:
			return false
		}
	case propertyTextDecorationLine:
		if keyword, ok := singleCSSKeyword(source); ok && keyword == "none" {
			return true
		}
		return valueContainsKeyword(source, "underline")
	case propertyVerticalAlign:
		if keyword, ok := singleCSSKeyword(source); ok {
			switch keyword {
			case "baseline", "sub", "super", "text-top", "text-bottom", "middle", "top", "bottom":
				return true
			}
		}
		parsed, ok := parseLength(source, 1, 1, viewport)
		return ok && parsed.unit != lengthAuto
	case propertyZIndex:
		if keyword, ok := singleCSSKeyword(source); ok && keyword == "auto" {
			return true
		}
		token, ok := singleCSSNumber(source)
		return ok && token.Integer
	case propertyVisibility:
		keyword, ok := singleCSSKeyword(source)
		return ok && (keyword == "visible" || keyword == "hidden" || keyword == "collapse")
	case propertyWhiteSpace:
		keyword, ok := singleCSSKeyword(source)
		return ok && (keyword == "normal" || keyword == "nowrap" || keyword == "pre" || keyword == "pre-wrap" || keyword == "pre-line" || keyword == "break-spaces")
	default:
		return false
	}
}

func (definition propertyDefinition) apply(style *computedStyle, source string, context propertyApplyContext) {
	switch definition.kind {
	case propertyAlignItems:
		keyword, _ := singleCSSKeyword(source)
		switch keyword {
		case "flex-start":
			style.alignItems = AlignFlexStart
		case "flex-end":
			style.alignItems = AlignFlexEnd
		case "center":
			style.alignItems = AlignCenterItems
		default:
			style.alignItems = AlignStretch
		}
	case propertyBackgroundColor:
		if parsed, ok := parseComputedColor(source); ok {
			style.background = parsed.value
			style.backgroundCurrent = parsed.currentColor
			style.hasBackground = !parsed.currentColor && parsed.value.A != 0
		}
	case propertyBorderColor:
		if parsed, ok := parseBorderColor(source); ok {
			applyBorderColor(definition.borderSide(style), parsed)
		}
	case propertyBorderStyle:
		if parsed, ok := parseBorderStyle(source); ok {
			definition.borderSide(style).style = parsed
		}
	case propertyBorderWidth:
		if parsed, ok := parseBorderWidth(source, style.fontSize, context.viewport); ok {
			definition.borderSide(style).width = parsed
		}
	case propertyBoxSizing:
		if keyword, ok := singleCSSKeyword(source); ok && keyword == "border-box" {
			style.boxSizing = BoxSizingBorderBox
		} else {
			style.boxSizing = BoxSizingContentBox
		}
	case propertyColor:
		if parsed, ok := parseComputedColor(source); ok {
			if parsed.currentColor {
				style.color = context.parentColor
			} else {
				style.color = parsed.value
			}
		}
	case propertyContent:
		if parsed, ok := parseContentValue(source); ok {
			style.content = parsed
		}
	case propertyDisplay:
		keyword, _ := singleCSSKeyword(source)
		switch keyword {
		case "none":
			style.display = displayNone
		case "block":
			style.display = displayBlock
		case "list-item":
			style.display = displayListItem
		case "flex":
			style.display = displayFlex
		case "inline-flex":
			style.display = displayInlineFlex
		case "inline":
			style.display = displayInline
		case "inline-block":
			style.display = displayInlineBlock
		case "table":
			style.display = DisplayTable
		case "inline-table":
			style.display = DisplayInlineTable
		case "table-row-group":
			style.display = DisplayTableRowGroup
		case "table-header-group":
			style.display = DisplayTableHeaderGroup
		case "table-footer-group":
			style.display = DisplayTableFooterGroup
		case "table-row":
			style.display = DisplayTableRow
		case "table-cell":
			style.display = DisplayTableCell
		case "table-column-group":
			style.display = DisplayTableColumnGroup
		case "table-column":
			style.display = DisplayTableColumn
		case "table-caption":
			style.display = DisplayTableCaption
		}
	case propertyFlexBasis:
		if parsed, ok := parseLength(source, style.fontSize, style.fontSize, context.viewport); ok && nonNegativeLength(parsed) {
			style.flexBasis = parsed
		}
	case propertyFlexDirection:
		keyword, _ := singleCSSKeyword(source)
		switch keyword {
		case "row-reverse":
			style.flexDirection = FlexDirectionRowReverse
		case "column":
			style.flexDirection = FlexDirectionColumn
		case "column-reverse":
			style.flexDirection = FlexDirectionColumnReverse
		default:
			style.flexDirection = FlexDirectionRow
		}
	case propertyFlexGrow:
		if token, ok := singleCSSNumber(source); ok && token.Number >= 0 {
			style.flexGrow = token.Number
		}
	case propertyFlexShrink:
		if token, ok := singleCSSNumber(source); ok && token.Number >= 0 {
			style.flexShrink = token.Number
		}
	case propertyFontFamily:
		if serialized, selected, ok := parseFontFamily(source); ok {
			style.fontFamilyValue = serialized
			style.fontFamily = selected
		}
	case propertyFontSize:
		if parsed, ok := parseLength(source, context.parentFontSize, context.parentFontSize, context.viewport); ok && parsed.unit != lengthAuto {
			resolved := resolveLength(parsed, context.parentFontSize, context.viewport, context.parentFontSize)
			if resolved > 0 && isFinite(resolved) {
				style.fontSize = resolved
			}
		}
	case propertyFontStyle:
		keyword, _ := singleCSSKeyword(source)
		if keyword == "italic" {
			style.fontStyle = FontStyleItalic
		} else if keyword == "oblique" {
			style.fontStyle = FontStyleOblique
		} else {
			style.fontStyle = FontStyleNormal
		}
	case propertyFontWeight:
		keyword, _ := singleCSSKeyword(source)
		switch keyword {
		case "bold":
			style.fontWeightValue = 700
		case "bolder":
			style.fontWeightValue = relativeFontWeight(context.parentFontWeight, true)
		case "normal":
			style.fontWeightValue = 400
		case "lighter":
			style.fontWeightValue = relativeFontWeight(context.parentFontWeight, false)
		default:
			if token, ok := singleCSSNumber(source); ok && token.Integer && token.Number >= 1 && token.Number <= 1000 {
				style.fontWeightValue = int(token.Number)
			}
		}
	case propertyHeight:
		if parsed, ok := parseLength(source, style.fontSize, style.fontSize, context.viewport); ok && nonNegativeLength(parsed) {
			style.height = parsed
		}
	case propertyInset:
		if parsed, ok := parseLength(source, style.fontSize, style.fontSize, context.viewport); ok {
			*definition.boxLength(style) = parsed
		}
	case propertyJustifyContent:
		keyword, _ := singleCSSKeyword(source)
		switch keyword {
		case "flex-end":
			style.justifyContent = JustifyFlexEnd
		case "center":
			style.justifyContent = JustifyCenter
		case "space-between":
			style.justifyContent = JustifySpaceBetween
		case "space-around":
			style.justifyContent = JustifySpaceAround
		case "space-evenly":
			style.justifyContent = JustifySpaceEvenly
		default:
			style.justifyContent = JustifyFlexStart
		}
	case propertyLineHeight:
		if keyword, ok := singleCSSKeyword(source); ok && keyword == "normal" {
			style.lineHeight = computedLineHeight{value: 1.2, normal: true}
		} else if token, ok := singleCSSNumber(source); ok && token.Number > 0 {
			style.lineHeight = computedLineHeight{value: token.Number}
		} else if parsed, ok := parseLength(source, style.fontSize, style.fontSize, context.viewport); ok && parsed.unit != lengthAuto {
			resolved := resolveLength(parsed, style.fontSize, context.viewport, style.lineHeight.pixels(style.fontSize))
			if resolved > 0 && isFinite(resolved) {
				style.lineHeight = computedLineHeight{value: resolved, absolute: true}
			}
		}
	case propertyListStyleType:
		if parsed, ok := parseListStyleType(source); ok {
			style.listStyleType = parsed
		}
	case propertyMargin:
		if parsed, ok := parseLength(source, style.fontSize, style.fontSize, context.viewport); ok {
			*definition.boxLength(style) = parsed
		}
	case propertyMaxHeight:
		if keyword, ok := singleCSSKeyword(source); ok && keyword == "none" {
			style.maxHeight = length{unit: lengthAuto}
		} else if parsed, ok := parseLength(source, style.fontSize, style.fontSize, context.viewport); ok && parsed.unit != lengthAuto && nonNegativeLength(parsed) {
			style.maxHeight = parsed
		}
	case propertyMaxWidth:
		if keyword, ok := singleCSSKeyword(source); ok && keyword == "none" {
			style.maxWidth = length{unit: lengthAuto}
		} else if parsed, ok := parseLength(source, style.fontSize, style.fontSize, context.viewport); ok && parsed.unit != lengthAuto && nonNegativeLength(parsed) {
			style.maxWidth = parsed
		}
	case propertyMinWidth:
		if keyword, ok := singleCSSKeyword(source); ok && keyword == "auto" {
			style.minWidth = px(0)
		} else if parsed, ok := parseLength(source, style.fontSize, style.fontSize, context.viewport); ok && nonNegativeLength(parsed) {
			style.minWidth = parsed
		}
	case propertyMinHeight:
		if keyword, ok := singleCSSKeyword(source); ok && keyword == "auto" {
			style.minHeight = px(0)
		} else if parsed, ok := parseLength(source, style.fontSize, style.fontSize, context.viewport); ok && nonNegativeLength(parsed) {
			style.minHeight = parsed
		}
	case propertyOpacity:
		if token, ok := singleCSSNumber(source); ok {
			style.opacity = clamp(token.Number, 0, 1)
		}
	case propertyOrder:
		if token, ok := singleCSSNumber(source); ok && token.Integer {
			style.order = int(token.Number)
		}
	case propertyOverflowX:
		if keyword, ok := singleCSSKeyword(source); ok {
			style.overflowX = parseOverflowMode(keyword)
		}
	case propertyOverflowY:
		if keyword, ok := singleCSSKeyword(source); ok {
			style.overflowY = parseOverflowMode(keyword)
		}
	case propertyPadding:
		if parsed, ok := parseLength(source, style.fontSize, style.fontSize, context.viewport); ok && parsed.unit != lengthAuto && nonNegativeLength(parsed) {
			*definition.boxLength(style) = parsed
		}
	case propertyPosition:
		keyword, _ := singleCSSKeyword(source)
		switch keyword {
		case "relative":
			style.position = PositionRelative
		case "absolute":
			style.position = PositionAbsolute
		case "fixed":
			style.position = PositionFixed
		default:
			style.position = PositionStatic
		}
	case propertyGap:
		if parsed, ok := parseLength(source, style.fontSize, style.fontSize, context.viewport); ok && parsed.unit != lengthAuto && nonNegativeLength(parsed) {
			*definition.boxLength(style) = parsed
		}
	case propertyTextAlign:
		keyword, _ := singleCSSKeyword(source)
		switch keyword {
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
		if valueContainsKeyword(source, "underline") {
			style.textDecoration = TextDecorationUnderline
			style.underline = true
		} else if keyword, ok := singleCSSKeyword(source); ok && keyword == "none" {
			style.textDecoration = TextDecorationNone
			style.underline = style.ancestorUnderline
		}
	case propertyVerticalAlign:
		if keyword, ok := singleCSSKeyword(source); ok {
			mode := VerticalAlignBaseline
			switch keyword {
			case "sub":
				mode = VerticalAlignSub
			case "super":
				mode = VerticalAlignSuper
			case "text-top":
				mode = VerticalAlignTextTop
			case "text-bottom":
				mode = VerticalAlignTextBottom
			case "middle":
				mode = VerticalAlignMiddle
			case "top":
				mode = VerticalAlignTop
			case "bottom":
				mode = VerticalAlignBottom
			}
			style.verticalAlign = VerticalAlignment{mode: mode}
		} else if parsed, ok := parseLength(source, style.fontSize, style.fontSize, context.viewport); ok && parsed.unit != lengthAuto {
			style.verticalAlign = VerticalAlignment{mode: VerticalAlignLength, offset: parsed}
		}
	case propertyVisibility:
		keyword, _ := singleCSSKeyword(source)
		if keyword == "hidden" {
			style.visibility = VisibilityHidden
		} else if keyword == "collapse" {
			style.visibility = VisibilityCollapse
		} else {
			style.visibility = VisibilityVisible
		}
	case propertyWhiteSpace:
		keyword, _ := singleCSSKeyword(source)
		switch keyword {
		case "nowrap":
			style.whiteSpace = WhiteSpaceNoWrap
		case "pre":
			style.whiteSpace = WhiteSpacePre
		case "pre-wrap":
			style.whiteSpace = WhiteSpacePreWrap
		case "pre-line":
			style.whiteSpace = WhiteSpacePreLine
		case "break-spaces":
			style.whiteSpace = WhiteSpaceBreakSpaces
		default:
			style.whiteSpace = WhiteSpaceNormal
		}
	case propertyWidth:
		if parsed, ok := parseLength(source, style.fontSize, style.fontSize, context.viewport); ok && nonNegativeLength(parsed) {
			style.width = parsed
		}
	case propertyZIndex:
		if keyword, ok := singleCSSKeyword(source); ok && keyword == "auto" {
			style.zIndex = ZIndex{auto: true}
		} else if token, ok := singleCSSNumber(source); ok && token.Integer {
			style.zIndex = ZIndex{value: int(token.Number)}
		}
	}
}

func (definition propertyDefinition) serialize(computed ComputedStyle) string {
	switch definition.kind {
	case propertyAlignItems:
		switch computed.alignItems {
		case AlignFlexStart:
			return "flex-start"
		case AlignFlexEnd:
			return "flex-end"
		case AlignCenterItems:
			return "center"
		default:
			return "stretch"
		}
	case propertyBackgroundColor:
		background, _ := computed.Background()
		return serializeComputedColor(background)
	case propertyBorderColor:
		return serializeComputedBorderColor(*definition.borderSide(&computed), computed.color)
	case propertyBorderStyle:
		return serializeComputedBorderStyle(definition.borderSide(&computed).style)
	case propertyBorderWidth:
		return serializeComputedBorderWidth(*definition.borderSide(&computed))
	case propertyBoxSizing:
		if computed.boxSizing == BoxSizingBorderBox {
			return "border-box"
		}
		return "content-box"
	case propertyColor:
		return serializeComputedColor(computed.color)
	case propertyContent:
		return serializeContentValue(computed.content)
	case propertyDisplay:
		return serializeComputedDisplay(computed.display)
	case propertyFlexBasis:
		return serializeComputedLength(computed.flexBasis)
	case propertyFlexDirection:
		switch computed.flexDirection {
		case FlexDirectionRowReverse:
			return "row-reverse"
		case FlexDirectionColumn:
			return "column"
		case FlexDirectionColumnReverse:
			return "column-reverse"
		default:
			return "row"
		}
	case propertyFlexGrow:
		return serializeComputedNumber(computed.flexGrow)
	case propertyFlexShrink:
		return serializeComputedNumber(computed.flexShrink)
	case propertyFontFamily:
		return computed.fontFamilyValue
	case propertyFontSize:
		return serializeComputedNumber(computed.fontSize) + "px"
	case propertyFontStyle:
		if computed.fontStyle == FontStyleItalic {
			return "italic"
		}
		if computed.fontStyle == FontStyleOblique {
			return "oblique"
		}
		return "normal"
	case propertyFontWeight:
		return strconv.Itoa(computed.fontWeightValue)
	case propertyHeight:
		return serializeComputedLength(computed.height)
	case propertyInset:
		return serializeComputedLength(*definition.boxLength(&computed))
	case propertyJustifyContent:
		switch computed.justifyContent {
		case JustifyFlexEnd:
			return "flex-end"
		case JustifyCenter:
			return "center"
		case JustifySpaceBetween:
			return "space-between"
		case JustifySpaceAround:
			return "space-around"
		case JustifySpaceEvenly:
			return "space-evenly"
		default:
			return "flex-start"
		}
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
	case propertyMaxHeight:
		if computed.maxHeight.unit == LengthAuto {
			return "none"
		}
		return serializeComputedLength(computed.maxHeight)
	case propertyMaxWidth:
		if computed.maxWidth.unit == LengthAuto {
			return "none"
		}
		return serializeComputedLength(computed.maxWidth)
	case propertyMinWidth:
		return serializeComputedLength(computed.minWidth)
	case propertyMinHeight:
		return serializeComputedLength(computed.minHeight)
	case propertyOpacity:
		return serializeComputedNumber(computed.opacity)
	case propertyOrder:
		return strconv.Itoa(computed.order)
	case propertyOverflowX:
		return serializeOverflowMode(computed.overflowX)
	case propertyOverflowY:
		return serializeOverflowMode(computed.overflowY)
	case propertyPosition:
		switch computed.position {
		case PositionRelative:
			return "relative"
		case PositionAbsolute:
			return "absolute"
		case PositionFixed:
			return "fixed"
		default:
			return "static"
		}
	case propertyGap:
		return serializeComputedLength(*definition.boxLength(&computed))
	case propertyTextAlign:
		return serializeComputedTextAlignment(computed.textAlign)
	case propertyTextDecorationLine:
		if computed.textDecoration == TextDecorationUnderline {
			return "underline"
		}
		return "none"
	case propertyVerticalAlign:
		switch computed.verticalAlign.mode {
		case VerticalAlignSub:
			return "sub"
		case VerticalAlignSuper:
			return "super"
		case VerticalAlignTextTop:
			return "text-top"
		case VerticalAlignTextBottom:
			return "text-bottom"
		case VerticalAlignMiddle:
			return "middle"
		case VerticalAlignTop:
			return "top"
		case VerticalAlignBottom:
			return "bottom"
		case VerticalAlignLength:
			return serializeComputedLength(computed.verticalAlign.offset)
		default:
			return "baseline"
		}
	case propertyVisibility:
		switch computed.visibility {
		case VisibilityHidden:
			return "hidden"
		case VisibilityCollapse:
			return "collapse"
		default:
			return "visible"
		}
	case propertyWhiteSpace:
		switch computed.whiteSpace {
		case WhiteSpaceNoWrap:
			return "nowrap"
		case WhiteSpacePre:
			return "pre"
		case WhiteSpacePreWrap:
			return "pre-wrap"
		case WhiteSpacePreLine:
			return "pre-line"
		case WhiteSpaceBreakSpaces:
			return "break-spaces"
		default:
			return "normal"
		}
	case propertyWidth:
		return serializeComputedLength(computed.width)
	case propertyZIndex:
		if computed.zIndex.auto {
			return "auto"
		}
		return strconv.Itoa(computed.zIndex.value)
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
	case propertyGap:
		if definition.edge == propertyBottom {
			return &style.rowGap
		}
		if definition.edge == propertyRight {
			return &style.columnGap
		}
	case propertyInset:
		switch definition.edge {
		case propertyTop:
			return &style.top
		case propertyRight:
			return &style.right
		case propertyBottom:
			return &style.bottom
		case propertyLeft:
			return &style.left
		}
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

// SupportsDeclaration reports whether the current style engine recognizes a
// parsed declaration's property and value grammar. It is the browser-owned
// stylesheet graph's capability boundary for @supports import conditions;
// it performs no cascade or DOM-dependent computation.
func SupportsDeclaration(declaration css.Declaration) bool {
	if strings.HasPrefix(declaration.Property, "--") {
		return css.ValidCustomPropertyValue(declaration.Value)
	}
	if len(declarationTargets(declaration.Property)) == 0 {
		return false
	}
	if css.ContainsVarFunction(declaration.Value) {
		return css.ValidVariableFunctions(declaration.Value)
	}
	return validComputedDeclaration(declaration, Viewport{Width: 800, Height: 600, InitialFontSize: 16})
}

func validComputedDeclaration(declaration css.Declaration, viewport Viewport) bool {
	if definition, ok := lookupPropertyDefinition(declaration.Property); ok {
		return definition.valid(declaration.Value, viewport)
	}

	switch declaration.Property {
	case "all":
		return cssWideKeyword(declaration.Value) != ""
	case "font":
		_, _, _, _, _, ok := parseFontShorthand(declaration.Value, viewport)
		return ok
	case "flex":
		_, _, _, ok := parseFlexShorthand(declaration.Value, 1, viewport)
		return ok
	case "gap":
		_, _, ok := parseGapShorthand(declaration.Value, 1, viewport)
		return ok
	case "background":
		_, ok := parseFirstComputedColor(declaration.Value)
		return ok
	case "padding":
		_, ok := parsePaddingLengths(declaration.Value, 1, viewport)
		return ok
	case "border", "border-top", "border-right", "border-bottom", "border-left":
		_, ok := parseBorderShorthand(declaration.Value, 1, viewport)
		return ok
	case "border-width":
		_, ok := parseBorderWidths(declaration.Value, 1, viewport)
		return ok
	case "border-style":
		_, ok := parseBorderStyles(declaration.Value)
		return ok
	case "border-color":
		_, ok := parseBorderColors(declaration.Value)
		return ok
	case "text-decoration":
		if keyword, ok := singleCSSKeyword(declaration.Value); ok && keyword == "none" {
			return true
		}
		return valueContainsKeyword(declaration.Value, "underline")
	case "list-style":
		_, ok := parseListStyleType(declaration.Value)
		return ok
	case "margin":
		_, ok := parseBoxLengths(declaration.Value, 1, viewport)
		return ok
	case "overflow":
		_, _, ok := parseOverflowShorthand(declaration.Value)
		return ok
	default:
		return false
	}
}

func parseGapShorthand(source string, fontSize float64, viewport Viewport) (length, length, bool) {
	parts := strings.Fields(source)
	if len(parts) < 1 || len(parts) > 2 {
		return length{}, length{}, false
	}
	row, ok := parseLength(parts[0], fontSize, fontSize, viewport)
	if !ok || row.unit == lengthAuto || !nonNegativeLength(row) {
		return length{}, length{}, false
	}
	column := row
	if len(parts) == 2 {
		column, ok = parseLength(parts[1], fontSize, fontSize, viewport)
		if !ok || column.unit == lengthAuto || !nonNegativeLength(column) {
			return length{}, length{}, false
		}
	}
	return row, column, true
}

func parseFlexShorthand(source string, fontSize float64, viewport Viewport) (float64, float64, length, bool) {
	if keyword, ok := singleCSSKeyword(source); ok {
		switch keyword {
		case "none":
			return 0, 0, length{unit: lengthAuto}, true
		case "auto":
			return 1, 1, length{unit: lengthAuto}, true
		case "initial":
			return 0, 1, length{unit: lengthAuto}, true
		}
	}
	parts := strings.Fields(source)
	if len(parts) < 1 || len(parts) > 3 {
		return 0, 0, length{}, false
	}
	readNumber := func(value string) (float64, bool) {
		token, ok := singleCSSNumber(value)
		return token.Number, ok && token.Number >= 0
	}
	readBasis := func(value string) (length, bool) {
		parsed, ok := parseLength(value, fontSize, fontSize, viewport)
		return parsed, ok && nonNegativeLength(parsed)
	}
	grow, ok := readNumber(parts[0])
	if !ok {
		basis, basisOK := readBasis(parts[0])
		return 1, 1, basis, basisOK && len(parts) == 1
	}
	shrink := 1.0
	basis := px(0)
	if len(parts) == 1 {
		return grow, shrink, basis, true
	}
	if second, numberOK := readNumber(parts[1]); numberOK {
		shrink = second
		if len(parts) == 3 {
			parsed, basisOK := readBasis(parts[2])
			return grow, shrink, parsed, basisOK
		}
		return grow, shrink, basis, true
	}
	parsed, basisOK := readBasis(parts[1])
	return grow, shrink, parsed, basisOK && len(parts) == 2
}

func applyDeclaration(style *computedStyle, property, source string, context propertyApplyContext) {
	if definition, ok := lookupPropertyDefinition(property); ok {
		definition.apply(style, source, context)
		return
	}

	switch property {
	case "background":
		if parsed, ok := parseFirstComputedColor(source); ok {
			style.background = parsed.value
			style.backgroundCurrent = parsed.currentColor
			style.hasBackground = !parsed.currentColor && parsed.value.A != 0
		}
	case "padding":
		if values, ok := parsePaddingLengths(source, style.fontSize, context.viewport); ok {
			style.paddingTop, style.paddingRight, style.paddingBottom, style.paddingLeft = values[0], values[1], values[2], values[3]
		}
	case "flex":
		if grow, shrink, basis, ok := parseFlexShorthand(source, style.fontSize, context.viewport); ok {
			style.flexGrow, style.flexShrink, style.flexBasis = grow, shrink, basis
		}
	case "gap":
		if row, column, ok := parseGapShorthand(source, style.fontSize, context.viewport); ok {
			style.rowGap, style.columnGap = row, column
		}
	case "border":
		if parsed, ok := parseBorderShorthand(source, style.fontSize, context.viewport); ok {
			style.borderTop, style.borderRight, style.borderBottom, style.borderLeft = parsed, parsed, parsed, parsed
		}
	case "border-top", "border-right", "border-bottom", "border-left":
		if parsed, ok := parseBorderShorthand(source, style.fontSize, context.viewport); ok {
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
		if parsed, ok := parseBorderWidths(source, style.fontSize, context.viewport); ok {
			style.borderTop.width, style.borderRight.width, style.borderBottom.width, style.borderLeft.width = parsed[0], parsed[1], parsed[2], parsed[3]
		}
	case "border-style":
		if parsed, ok := parseBorderStyles(source); ok {
			style.borderTop.style, style.borderRight.style, style.borderBottom.style, style.borderLeft.style = parsed[0], parsed[1], parsed[2], parsed[3]
		}
	case "border-color":
		if parsed, ok := parseBorderColors(source); ok {
			applyBorderColor(&style.borderTop, parsed[0])
			applyBorderColor(&style.borderRight, parsed[1])
			applyBorderColor(&style.borderBottom, parsed[2])
			applyBorderColor(&style.borderLeft, parsed[3])
		}
	case "text-decoration":
		if valueContainsKeyword(source, "underline") {
			style.textDecoration = TextDecorationUnderline
			style.underline = true
		} else if keyword, ok := singleCSSKeyword(source); ok && keyword == "none" {
			style.textDecoration = TextDecorationNone
			style.underline = style.ancestorUnderline
		}
	case "list-style":
		if parsed, ok := parseListStyleType(source); ok {
			style.listStyleType = parsed
		}
	case "margin":
		if values, ok := parseBoxLengths(source, style.fontSize, context.viewport); ok {
			style.marginTop, style.marginRight, style.marginBottom, style.marginLeft = values[0], values[1], values[2], values[3]
		}
	case "overflow":
		if x, y, ok := parseOverflowShorthand(source); ok {
			style.overflowX = x
			style.overflowY = y
		}
	}
}

func validOverflowKeyword(keyword string) bool {
	switch keyword {
	case "visible", "hidden", "scroll", "auto", "clip":
		return true
	default:
		return false
	}
}

func parseOverflowShorthand(source string) (OverflowMode, OverflowMode, bool) {
	value, ok := parsePropertyValue(source)
	if !ok || len(value.terms) < 1 || len(value.terms) > 2 {
		return OverflowVisible, OverflowVisible, false
	}
	first, ok := componentKeyword(value.terms[0])
	if !ok || !validOverflowKeyword(first) {
		return OverflowVisible, OverflowVisible, false
	}
	second := first
	if len(value.terms) == 2 {
		second, ok = componentKeyword(value.terms[1])
		if !ok || !validOverflowKeyword(second) {
			return OverflowVisible, OverflowVisible, false
		}
	}
	return parseOverflowMode(first), parseOverflowMode(second), true
}

func parseOverflowMode(keyword string) OverflowMode {
	switch keyword {
	case "hidden":
		return OverflowHidden
	case "scroll":
		return OverflowScroll
	case "auto":
		return OverflowAuto
	case "clip":
		return OverflowClip
	default:
		return OverflowVisible
	}
}

func serializeOverflowMode(mode OverflowMode) string {
	switch mode {
	case OverflowHidden:
		return "hidden"
	case OverflowScroll:
		return "scroll"
	case OverflowAuto:
		return "auto"
	case OverflowClip:
		return "clip"
	default:
		return "visible"
	}
}
