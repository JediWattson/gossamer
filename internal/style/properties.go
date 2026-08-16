package style

import (
	"image/color"
	"math"
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
	propertyAlignContent propertyKind = iota
	propertyAlignItems
	propertyAlignSelf
	propertyBackgroundColor
	propertyBorderColor
	propertyBorderCollapse
	propertyBorderSpacing
	propertyBorderStyle
	propertyBorderWidth
	propertyBoxSizing
	propertyCaptionSide
	propertyColor
	propertyContent
	propertyDisplay
	propertyEmptyCells
	propertyFlexBasis
	propertyFlexDirection
	propertyFlexGrow
	propertyFlexShrink
	propertyFontFamily
	propertyFontSize
	propertyFontStyle
	propertyFontWeight
	propertyGridAutoColumns
	propertyGridAutoFlow
	propertyGridAutoRows
	propertyGridColumnEnd
	propertyGridColumnStart
	propertyGridRowEnd
	propertyGridRowStart
	propertyGridTemplateAreas
	propertyGridTemplateColumns
	propertyGridTemplateRows
	propertyHeight
	propertyInset
	propertyJustifyContent
	propertyJustifyItems
	propertyJustifySelf
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
	propertyTableLayout
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
	{name: "align-content", kind: propertyAlignContent, invalidation: propertyInvalidatesLayout},
	{name: "align-items", kind: propertyAlignItems, invalidation: propertyInvalidatesLayout},
	{name: "align-self", kind: propertyAlignSelf, invalidation: propertyInvalidatesLayout},
	{name: "background-color", kind: propertyBackgroundColor, invalidation: propertyInvalidatesPaint},
	{name: "border-bottom-color", kind: propertyBorderColor, edge: propertyBottom, invalidation: propertyInvalidatesPaint},
	{name: "border-bottom-style", kind: propertyBorderStyle, edge: propertyBottom, invalidation: propertyInvalidatesLayout | propertyInvalidatesPaint},
	{name: "border-bottom-width", kind: propertyBorderWidth, edge: propertyBottom, invalidation: propertyInvalidatesLayout | propertyInvalidatesPaint},
	{name: "border-collapse", kind: propertyBorderCollapse, inherited: true, invalidation: propertyInvalidatesLayout | propertyInvalidatesPaint},
	{name: "border-left-color", kind: propertyBorderColor, edge: propertyLeft, invalidation: propertyInvalidatesPaint},
	{name: "border-left-style", kind: propertyBorderStyle, edge: propertyLeft, invalidation: propertyInvalidatesLayout | propertyInvalidatesPaint},
	{name: "border-left-width", kind: propertyBorderWidth, edge: propertyLeft, invalidation: propertyInvalidatesLayout | propertyInvalidatesPaint},
	{name: "border-right-color", kind: propertyBorderColor, edge: propertyRight, invalidation: propertyInvalidatesPaint},
	{name: "border-right-style", kind: propertyBorderStyle, edge: propertyRight, invalidation: propertyInvalidatesLayout | propertyInvalidatesPaint},
	{name: "border-right-width", kind: propertyBorderWidth, edge: propertyRight, invalidation: propertyInvalidatesLayout | propertyInvalidatesPaint},
	{name: "border-spacing", kind: propertyBorderSpacing, inherited: true, invalidation: propertyInvalidatesLayout},
	{name: "border-top-color", kind: propertyBorderColor, edge: propertyTop, invalidation: propertyInvalidatesPaint},
	{name: "border-top-style", kind: propertyBorderStyle, edge: propertyTop, invalidation: propertyInvalidatesLayout | propertyInvalidatesPaint},
	{name: "border-top-width", kind: propertyBorderWidth, edge: propertyTop, invalidation: propertyInvalidatesLayout | propertyInvalidatesPaint},
	{name: "bottom", kind: propertyInset, edge: propertyBottom, invalidation: propertyInvalidatesLayout | propertyInvalidatesPaint},
	{name: "box-sizing", kind: propertyBoxSizing, invalidation: propertyInvalidatesLayout},
	{name: "caption-side", kind: propertyCaptionSide, inherited: true, invalidation: propertyInvalidatesLayout | propertyInvalidatesPaint},
	{name: "color", kind: propertyColor, inherited: true, invalidation: propertyInvalidatesPaint},
	{name: "column-gap", kind: propertyGap, edge: propertyRight, invalidation: propertyInvalidatesLayout},
	{name: "content", kind: propertyContent, invalidation: propertyInvalidatesLayout | propertyInvalidatesPaint},
	{name: "display", kind: propertyDisplay, invalidation: propertyInvalidatesLayout | propertyInvalidatesPaint},
	{name: "empty-cells", kind: propertyEmptyCells, inherited: true, invalidation: propertyInvalidatesPaint},
	{name: "flex-basis", kind: propertyFlexBasis, invalidation: propertyInvalidatesLayout},
	{name: "flex-direction", kind: propertyFlexDirection, invalidation: propertyInvalidatesLayout},
	{name: "flex-grow", kind: propertyFlexGrow, invalidation: propertyInvalidatesLayout},
	{name: "flex-shrink", kind: propertyFlexShrink, invalidation: propertyInvalidatesLayout},
	{name: "font-family", kind: propertyFontFamily, inherited: true, invalidation: propertyInvalidatesLayout | propertyInvalidatesPaint},
	{name: "font-size", kind: propertyFontSize, inherited: true, computeEarly: true, invalidation: propertyInvalidatesLayout | propertyInvalidatesPaint},
	{name: "font-style", kind: propertyFontStyle, inherited: true, invalidation: propertyInvalidatesLayout | propertyInvalidatesPaint},
	{name: "font-weight", kind: propertyFontWeight, inherited: true, invalidation: propertyInvalidatesLayout | propertyInvalidatesPaint},
	{name: "grid-auto-columns", kind: propertyGridAutoColumns, invalidation: propertyInvalidatesLayout},
	{name: "grid-auto-flow", kind: propertyGridAutoFlow, invalidation: propertyInvalidatesLayout | propertyInvalidatesPaint},
	{name: "grid-auto-rows", kind: propertyGridAutoRows, invalidation: propertyInvalidatesLayout},
	{name: "grid-column-end", kind: propertyGridColumnEnd, invalidation: propertyInvalidatesLayout | propertyInvalidatesPaint},
	{name: "grid-column-start", kind: propertyGridColumnStart, invalidation: propertyInvalidatesLayout | propertyInvalidatesPaint},
	{name: "grid-row-end", kind: propertyGridRowEnd, invalidation: propertyInvalidatesLayout | propertyInvalidatesPaint},
	{name: "grid-row-start", kind: propertyGridRowStart, invalidation: propertyInvalidatesLayout | propertyInvalidatesPaint},
	{name: "grid-template-areas", kind: propertyGridTemplateAreas, invalidation: propertyInvalidatesLayout},
	{name: "grid-template-columns", kind: propertyGridTemplateColumns, invalidation: propertyInvalidatesLayout},
	{name: "grid-template-rows", kind: propertyGridTemplateRows, invalidation: propertyInvalidatesLayout},
	{name: "height", kind: propertyHeight, invalidation: propertyInvalidatesLayout},
	{name: "justify-content", kind: propertyJustifyContent, invalidation: propertyInvalidatesLayout},
	{name: "justify-items", kind: propertyJustifyItems, invalidation: propertyInvalidatesLayout},
	{name: "justify-self", kind: propertyJustifySelf, invalidation: propertyInvalidatesLayout},
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
	{name: "table-layout", kind: propertyTableLayout, invalidation: propertyInvalidatesLayout},
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
	"grid-column":     {"grid-column-start", "grid-column-end"},
	"grid-area":       {"grid-row-start", "grid-column-start", "grid-row-end", "grid-column-end"},
	"grid-row":        {"grid-row-start", "grid-row-end"},
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
	case propertyAlignContent:
		destination.alignContent = source.alignContent
		destination.alignContentOvf = source.alignContentOvf
	case propertyAlignItems:
		destination.alignItems = source.alignItems
		destination.alignItemsOvf = source.alignItemsOvf
	case propertyAlignSelf:
		destination.alignSelf = source.alignSelf
		destination.alignSelfOvf = source.alignSelfOvf
	case propertyBackgroundColor:
		destination.background = source.background
		destination.hasBackground = source.hasBackground
		destination.backgroundCurrent = source.backgroundCurrent
	case propertyBorderColor:
		destinationSide := definition.borderSide(destination)
		sourceSide := definition.borderSide(&source)
		destinationSide.color = sourceSide.color
		destinationSide.hasColor = sourceSide.hasColor
	case propertyBorderCollapse:
		destination.borderCollapse = source.borderCollapse
	case propertyBorderSpacing:
		destination.borderSpacing = source.borderSpacing
	case propertyBorderStyle:
		definition.borderSide(destination).style = definition.borderSide(&source).style
	case propertyBorderWidth:
		definition.borderSide(destination).width = definition.borderSide(&source).width
	case propertyBoxSizing:
		destination.boxSizing = source.boxSizing
	case propertyCaptionSide:
		destination.captionSide = source.captionSide
	case propertyColor:
		destination.color = source.color
	case propertyContent:
		destination.content = source.content
	case propertyDisplay:
		destination.display = source.display
	case propertyEmptyCells:
		destination.emptyCells = source.emptyCells
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
	case propertyGridAutoColumns:
		destination.gridAutoColumns = source.gridAutoColumns
	case propertyGridAutoFlow:
		destination.gridAutoFlow = source.gridAutoFlow
	case propertyGridAutoRows:
		destination.gridAutoRows = source.gridAutoRows
	case propertyGridColumnEnd:
		destination.gridColumnEnd = source.gridColumnEnd
	case propertyGridColumnStart:
		destination.gridColumnStart = source.gridColumnStart
	case propertyGridRowEnd:
		destination.gridRowEnd = source.gridRowEnd
	case propertyGridRowStart:
		destination.gridRowStart = source.gridRowStart
	case propertyGridTemplateAreas:
		destination.gridTemplateAreas = source.gridTemplateAreas
	case propertyGridTemplateColumns:
		destination.gridTemplateCols = source.gridTemplateCols
	case propertyGridTemplateRows:
		destination.gridTemplateRows = source.gridTemplateRows
	case propertyHeight:
		destination.height = source.height
	case propertyInset:
		*definition.boxLength(destination) = *definition.boxLength(&source)
	case propertyJustifyContent:
		destination.justifyContent = source.justifyContent
		destination.justifyContentOvf = source.justifyContentOvf
	case propertyJustifyItems:
		destination.justifyItems = source.justifyItems
		destination.justifyItemsOvf = source.justifyItemsOvf
	case propertyJustifySelf:
		destination.justifySelf = source.justifySelf
		destination.justifySelfOvf = source.justifySelfOvf
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
		if definition.edge == propertyBottom {
			destination.rowGapNormal = source.rowGapNormal
		} else {
			destination.columnGapNormal = source.columnGapNormal
		}
	case propertyTableLayout:
		destination.tableLayout = source.tableLayout
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
	case propertyAlignContent:
		_, ok := parseContentAlignment(source, true)
		return ok
	case propertyJustifyContent:
		_, ok := parseContentAlignment(source, false)
		return ok
	case propertyAlignItems, propertyJustifyItems:
		_, ok := parseSelfAlignment(source, false)
		return ok
	case propertyAlignSelf, propertyJustifySelf:
		_, ok := parseSelfAlignment(source, true)
		return ok
	case propertyBackgroundColor:
		_, ok := parseComputedColor(source)
		return ok
	case propertyBorderColor:
		_, ok := parseBorderColor(source)
		return ok
	case propertyBorderCollapse:
		keyword, ok := singleCSSKeyword(source)
		return ok && (keyword == "separate" || keyword == "collapse")
	case propertyBorderSpacing:
		_, ok := parseBorderSpacing(source, 1, viewport)
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
	case propertyCaptionSide:
		keyword, ok := singleCSSKeyword(source)
		return ok && (keyword == "top" || keyword == "bottom")
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
		case "none", "block", "list-item", "inline", "inline-block", "flex", "inline-flex", "grid", "inline-grid",
			"table", "inline-table", "table-row-group", "table-header-group", "table-footer-group",
			"table-row", "table-cell", "table-column-group", "table-column", "table-caption":
			return true
		default:
			return false
		}
	case propertyEmptyCells:
		keyword, ok := singleCSSKeyword(source)
		return ok && (keyword == "show" || keyword == "hide")
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
	case propertyGridAutoColumns, propertyGridAutoRows:
		_, ok := parseGridAutoTrackList(source, 1, viewport)
		return ok
	case propertyGridAutoFlow:
		_, ok := parseGridAutoFlow(source)
		return ok
	case propertyGridColumnEnd, propertyGridColumnStart, propertyGridRowEnd, propertyGridRowStart:
		_, ok := parseGridLine(source)
		return ok
	case propertyGridTemplateAreas:
		_, ok := parseGridTemplateAreas(source)
		return ok
	case propertyGridTemplateColumns, propertyGridTemplateRows:
		_, ok := parseGridTrackList(source, 1, viewport)
		return ok
	case propertyHeight, propertyMinHeight, propertyMinWidth, propertyWidth:
		parsed, ok := parseLength(source, 1, 1, viewport)
		return ok && nonNegativeLength(parsed)
	case propertyInset:
		_, ok := parseLength(source, 1, 1, viewport)
		return ok
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
		_, ok := parseGapValue(source, 1, viewport)
		return ok
	case propertyTableLayout:
		keyword, ok := singleCSSKeyword(source)
		return ok && (keyword == "auto" || keyword == "fixed")
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
	case propertyAlignContent:
		if parsed, ok := parseContentAlignment(source, true); ok {
			style.alignContent = parsed.position
			style.alignContentOvf = parsed.overflow
		}
	case propertyAlignItems:
		if parsed, ok := parseSelfAlignment(source, false); ok {
			style.alignItems = parsed.position
			style.alignItemsOvf = parsed.overflow
		}
	case propertyAlignSelf:
		if parsed, ok := parseSelfAlignment(source, true); ok {
			style.alignSelf = parsed.position
			style.alignSelfOvf = parsed.overflow
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
	case propertyBorderCollapse:
		keyword, _ := singleCSSKeyword(source)
		if keyword == "collapse" {
			style.borderCollapse = BorderCollapseCollapse
		} else {
			style.borderCollapse = BorderCollapseSeparate
		}
	case propertyBorderSpacing:
		if parsed, ok := parseBorderSpacing(source, style.fontSize, context.viewport); ok {
			style.borderSpacing = parsed
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
	case propertyCaptionSide:
		keyword, _ := singleCSSKeyword(source)
		if keyword == "bottom" {
			style.captionSide = CaptionSideBottom
		} else {
			style.captionSide = CaptionSideTop
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
		case "grid":
			style.display = displayGrid
		case "inline-grid":
			style.display = displayInlineGrid
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
	case propertyEmptyCells:
		keyword, _ := singleCSSKeyword(source)
		if keyword == "hide" {
			style.emptyCells = EmptyCellsHide
		} else {
			style.emptyCells = EmptyCellsShow
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
	case propertyGridAutoColumns:
		if parsed, ok := parseGridAutoTrackList(source, style.fontSize, context.viewport); ok {
			style.gridAutoColumns = parsed
		}
	case propertyGridAutoFlow:
		if parsed, ok := parseGridAutoFlow(source); ok {
			style.gridAutoFlow = parsed
		}
	case propertyGridAutoRows:
		if parsed, ok := parseGridAutoTrackList(source, style.fontSize, context.viewport); ok {
			style.gridAutoRows = parsed
		}
	case propertyGridColumnEnd:
		if parsed, ok := parseGridLine(source); ok {
			style.gridColumnEnd = parsed
		}
	case propertyGridColumnStart:
		if parsed, ok := parseGridLine(source); ok {
			style.gridColumnStart = parsed
		}
	case propertyGridRowEnd:
		if parsed, ok := parseGridLine(source); ok {
			style.gridRowEnd = parsed
		}
	case propertyGridRowStart:
		if parsed, ok := parseGridLine(source); ok {
			style.gridRowStart = parsed
		}
	case propertyGridTemplateAreas:
		if parsed, ok := parseGridTemplateAreas(source); ok {
			style.gridTemplateAreas = parsed
		}
	case propertyGridTemplateColumns:
		if parsed, ok := parseGridTrackList(source, style.fontSize, context.viewport); ok {
			style.gridTemplateCols = parsed
		}
	case propertyGridTemplateRows:
		if parsed, ok := parseGridTrackList(source, style.fontSize, context.viewport); ok {
			style.gridTemplateRows = parsed
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
		if parsed, ok := parseContentAlignment(source, false); ok {
			style.justifyContent = parsed.position
			style.justifyContentOvf = parsed.overflow
		}
	case propertyJustifyItems:
		if parsed, ok := parseSelfAlignment(source, false); ok {
			style.justifyItems = parsed.position
			style.justifyItemsOvf = parsed.overflow
		}
	case propertyJustifySelf:
		if parsed, ok := parseSelfAlignment(source, true); ok {
			style.justifySelf = parsed.position
			style.justifySelfOvf = parsed.overflow
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
		if parsed, ok := parseGapValue(source, style.fontSize, context.viewport); ok {
			*definition.boxLength(style) = parsed.length
			if definition.edge == propertyBottom {
				style.rowGapNormal = parsed.normal
			} else {
				style.columnGapNormal = parsed.normal
			}
		}
	case propertyTableLayout:
		keyword, _ := singleCSSKeyword(source)
		if keyword == "fixed" {
			style.tableLayout = TableLayoutFixed
		} else {
			style.tableLayout = TableLayoutAuto
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
	case propertyAlignContent:
		return serializeContentAlignment(computed.alignContent, computed.alignContentOvf)
	case propertyAlignItems:
		return serializeSelfAlignment(computed.alignItems, computed.alignItemsOvf)
	case propertyAlignSelf:
		return serializeSelfAlignment(computed.alignSelf, computed.alignSelfOvf)
	case propertyBackgroundColor:
		background, _ := computed.Background()
		return serializeComputedColor(background)
	case propertyBorderColor:
		return serializeComputedBorderColor(*definition.borderSide(&computed), computed.color)
	case propertyBorderCollapse:
		if computed.borderCollapse == BorderCollapseCollapse {
			return "collapse"
		}
		return "separate"
	case propertyBorderSpacing:
		horizontal := serializeComputedLength(computed.borderSpacing.horizontal)
		vertical := serializeComputedLength(computed.borderSpacing.vertical)
		if horizontal == vertical {
			return horizontal
		}
		return horizontal + " " + vertical
	case propertyBorderStyle:
		return serializeComputedBorderStyle(definition.borderSide(&computed).style)
	case propertyBorderWidth:
		return serializeComputedBorderWidth(*definition.borderSide(&computed))
	case propertyBoxSizing:
		if computed.boxSizing == BoxSizingBorderBox {
			return "border-box"
		}
		return "content-box"
	case propertyCaptionSide:
		if computed.captionSide == CaptionSideBottom {
			return "bottom"
		}
		return "top"
	case propertyColor:
		return serializeComputedColor(computed.color)
	case propertyContent:
		return serializeContentValue(computed.content)
	case propertyDisplay:
		return serializeComputedDisplay(computed.display)
	case propertyEmptyCells:
		if computed.emptyCells == EmptyCellsHide {
			return "hide"
		}
		return "show"
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
	case propertyGridAutoColumns:
		return serializeGridTrackList(computed.gridAutoColumns)
	case propertyGridAutoFlow:
		return serializeGridAutoFlow(computed.gridAutoFlow)
	case propertyGridAutoRows:
		return serializeGridTrackList(computed.gridAutoRows)
	case propertyGridColumnEnd:
		return serializeGridLine(computed.gridColumnEnd)
	case propertyGridColumnStart:
		return serializeGridLine(computed.gridColumnStart)
	case propertyGridRowEnd:
		return serializeGridLine(computed.gridRowEnd)
	case propertyGridRowStart:
		return serializeGridLine(computed.gridRowStart)
	case propertyGridTemplateAreas:
		return serializeGridTemplateAreas(computed.gridTemplateAreas)
	case propertyGridTemplateColumns:
		return serializeGridTrackList(computed.gridTemplateCols)
	case propertyGridTemplateRows:
		return serializeGridTrackList(computed.gridTemplateRows)
	case propertyHeight:
		return serializeComputedLength(computed.height)
	case propertyInset:
		return serializeComputedLength(*definition.boxLength(&computed))
	case propertyJustifyContent:
		return serializeContentAlignment(computed.justifyContent, computed.justifyContentOvf)
	case propertyJustifyItems:
		return serializeSelfAlignment(computed.justifyItems, computed.justifyItemsOvf)
	case propertyJustifySelf:
		return serializeSelfAlignment(computed.justifySelf, computed.justifySelfOvf)
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
		if definition.edge == propertyBottom && computed.rowGapNormal || definition.edge == propertyRight && computed.columnGapNormal {
			return "normal"
		}
		return serializeComputedLength(*definition.boxLength(&computed))
	case propertyTableLayout:
		if computed.tableLayout == TableLayoutFixed {
			return "fixed"
		}
		return "auto"
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
	case "grid-column", "grid-row":
		_, _, ok := parseGridLineShorthand(declaration.Value)
		return ok
	case "grid-area":
		_, _, _, _, ok := parseGridAreaShorthand(declaration.Value)
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

func parseBorderSpacing(source string, fontSize float64, viewport Viewport) (BorderSpacing, bool) {
	value, ok := parsePropertyValue(source)
	if !ok || len(value.terms) < 1 || len(value.terms) > 2 {
		return BorderSpacing{}, false
	}
	parsed := [2]Length{}
	for index, term := range value.terms {
		candidate, candidateOK := parseLengthComponent(term, value.source, fontSize, viewport)
		if !candidateOK || candidate.unit == lengthAuto || candidate.DependsOnPercent() {
			return BorderSpacing{}, false
		}
		resolved := resolveLength(candidate, 0, viewport, math.NaN())
		if !isFinite(resolved) || resolved < 0 {
			return BorderSpacing{}, false
		}
		parsed[index] = px(resolved)
	}
	if len(value.terms) == 1 {
		parsed[1] = parsed[0]
	}
	return BorderSpacing{horizontal: parsed[0], vertical: parsed[1]}, true
}

type gapValue struct {
	length length
	normal bool
}

func parseGapValue(source string, fontSize float64, viewport Viewport) (gapValue, bool) {
	value, ok := parsePropertyValue(source)
	if !ok || len(value.terms) != 1 {
		return gapValue{}, false
	}
	return parseGapComponent(value.terms[0], value.source, fontSize, viewport)
}

func parseGapComponent(component css.ComponentValue, source string, fontSize float64, viewport Viewport) (gapValue, bool) {
	if keyword, ok := componentKeyword(component); ok && keyword == "normal" {
		return gapValue{length: px(0), normal: true}, true
	}
	parsed, ok := parseLengthComponent(component, source, fontSize, viewport)
	if !ok || parsed.unit == lengthAuto || !nonNegativeLength(parsed) {
		return gapValue{}, false
	}
	return gapValue{length: parsed}, true
}

func parseGapShorthand(source string, fontSize float64, viewport Viewport) (gapValue, gapValue, bool) {
	value, ok := parsePropertyValue(source)
	if !ok || len(value.terms) < 1 || len(value.terms) > 2 {
		return gapValue{}, gapValue{}, false
	}
	row, ok := parseGapComponent(value.terms[0], value.source, fontSize, viewport)
	if !ok {
		return gapValue{}, gapValue{}, false
	}
	column := row
	if len(value.terms) == 2 {
		column, ok = parseGapComponent(value.terms[1], value.source, fontSize, viewport)
		if !ok {
			return gapValue{}, gapValue{}, false
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
			style.rowGap, style.columnGap = row.length, column.length
			style.rowGapNormal, style.columnGapNormal = row.normal, column.normal
		}
	case "grid-column":
		if start, end, ok := parseGridLineShorthand(source); ok {
			style.gridColumnStart, style.gridColumnEnd = start, end
		}
	case "grid-row":
		if start, end, ok := parseGridLineShorthand(source); ok {
			style.gridRowStart, style.gridRowEnd = start, end
		}
	case "grid-area":
		if rowStart, columnStart, rowEnd, columnEnd, ok := parseGridAreaShorthand(source); ok {
			style.gridRowStart, style.gridColumnStart = rowStart, columnStart
			style.gridRowEnd, style.gridColumnEnd = rowEnd, columnEnd
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

type selfAlignmentValue struct {
	position AlignItems
	overflow OverflowAlignment
}

type contentAlignmentValue struct {
	position JustifyContent
	overflow OverflowAlignment
}

func parseAlignmentKeywords(source string) (string, OverflowAlignment, bool) {
	value, ok := parsePropertyValue(source)
	if !ok || len(value.terms) < 1 || len(value.terms) > 2 {
		return "", OverflowAlignmentDefault, false
	}
	overflow := OverflowAlignmentDefault
	keywordIndex := 0
	if len(value.terms) == 2 {
		prefix, prefixOK := componentKeyword(value.terms[0])
		if !prefixOK {
			return "", OverflowAlignmentDefault, false
		}
		switch prefix {
		case "safe":
			overflow = OverflowAlignmentSafe
		case "unsafe":
			overflow = OverflowAlignmentUnsafe
		default:
			return "", OverflowAlignmentDefault, false
		}
		keywordIndex = 1
	}
	keyword, ok := componentKeyword(value.terms[keywordIndex])
	return keyword, overflow, ok
}

func parseBaselinePosition(source string) (bool, bool) {
	value, ok := parsePropertyValue(source)
	if !ok || len(value.terms) < 1 || len(value.terms) > 2 {
		return false, false
	}
	first, ok := componentKeyword(value.terms[0])
	if !ok {
		return false, false
	}
	if len(value.terms) == 1 {
		return false, first == "baseline"
	}
	second, ok := componentKeyword(value.terms[1])
	if !ok {
		return false, false
	}
	switch {
	case first == "baseline" && second == "first", first == "first" && second == "baseline":
		return false, true
	case first == "baseline" && second == "last", first == "last" && second == "baseline":
		return true, true
	default:
		return false, false
	}
}

func parseSelfAlignment(source string, allowAuto bool) (selfAlignmentValue, bool) {
	if last, ok := parseBaselinePosition(source); ok {
		position := AlignBaseline
		if last {
			position = AlignLastBaseline
		}
		return selfAlignmentValue{position: position}, true
	}
	keyword, overflow, ok := parseAlignmentKeywords(source)
	if !ok {
		return selfAlignmentValue{}, false
	}
	parsed := selfAlignmentValue{overflow: overflow}
	switch keyword {
	case "auto":
		parsed.position = AlignAuto
		return parsed, allowAuto && overflow == OverflowAlignmentDefault
	case "normal":
		parsed.position = AlignNormal
		return parsed, allowAuto || overflow == OverflowAlignmentDefault
	case "stretch":
		parsed.position = AlignStretch
		return parsed, overflow == OverflowAlignmentDefault
	case "start":
		parsed.position = AlignStartItems
	case "end":
		parsed.position = AlignEndItems
	case "flex-start":
		parsed.position = AlignFlexStart
	case "flex-end":
		parsed.position = AlignFlexEnd
	case "self-start":
		parsed.position = AlignSelfStart
	case "self-end":
		parsed.position = AlignSelfEnd
	case "center":
		parsed.position = AlignCenterItems
	default:
		return selfAlignmentValue{}, false
	}
	return parsed, true
}

func serializeAlignmentOverflow(overflow OverflowAlignment) string {
	switch overflow {
	case OverflowAlignmentSafe:
		return "safe "
	case OverflowAlignmentUnsafe:
		return "unsafe "
	default:
		return ""
	}
}

func serializeSelfAlignment(alignment AlignItems, overflow OverflowAlignment) string {
	value := "center"
	switch alignment {
	case AlignAuto:
		value = "auto"
	case AlignNormal:
		value = "normal"
	case AlignStretch:
		value = "stretch"
	case AlignStartItems:
		value = "start"
	case AlignEndItems:
		value = "end"
	case AlignFlexStart:
		value = "flex-start"
	case AlignFlexEnd:
		value = "flex-end"
	case AlignSelfStart:
		value = "self-start"
	case AlignSelfEnd:
		value = "self-end"
	case AlignBaseline:
		value = "baseline"
	case AlignLastBaseline:
		value = "last baseline"
	}
	return serializeAlignmentOverflow(overflow) + value
}

func parseContentAlignment(source string, allowBaseline bool) (contentAlignmentValue, bool) {
	if last, ok := parseBaselinePosition(source); ok {
		if !allowBaseline {
			return contentAlignmentValue{}, false
		}
		position := JustifyBaseline
		if last {
			position = JustifyLastBaseline
		}
		return contentAlignmentValue{position: position}, true
	}
	keyword, overflow, ok := parseAlignmentKeywords(source)
	if !ok {
		return contentAlignmentValue{}, false
	}
	parsed := contentAlignmentValue{overflow: overflow}
	switch keyword {
	case "normal":
		parsed.position = JustifyNormal
		return parsed, overflow == OverflowAlignmentDefault
	case "stretch":
		parsed.position = JustifyStretch
		return parsed, overflow == OverflowAlignmentDefault
	case "start":
		parsed.position = JustifyStart
	case "end":
		parsed.position = JustifyEnd
	case "flex-start":
		parsed.position = JustifyFlexStart
	case "flex-end":
		parsed.position = JustifyFlexEnd
	case "center":
		parsed.position = JustifyCenter
	case "space-between":
		parsed.position = JustifySpaceBetween
		return parsed, overflow == OverflowAlignmentDefault
	case "space-around":
		parsed.position = JustifySpaceAround
		return parsed, overflow == OverflowAlignmentDefault
	case "space-evenly":
		parsed.position = JustifySpaceEvenly
		return parsed, overflow == OverflowAlignmentDefault
	default:
		return contentAlignmentValue{}, false
	}
	return parsed, true
}

func serializeContentAlignment(alignment JustifyContent, overflow OverflowAlignment) string {
	value := "normal"
	switch alignment {
	case JustifyStretch:
		value = "stretch"
	case JustifyStart:
		value = "start"
	case JustifyEnd:
		value = "end"
	case JustifyFlexStart:
		value = "flex-start"
	case JustifyFlexEnd:
		value = "flex-end"
	case JustifyCenter:
		value = "center"
	case JustifySpaceBetween:
		value = "space-between"
	case JustifySpaceAround:
		value = "space-around"
	case JustifySpaceEvenly:
		value = "space-evenly"
	case JustifyBaseline:
		value = "baseline"
	case JustifyLastBaseline:
		value = "last baseline"
	}
	return serializeAlignmentOverflow(overflow) + value
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
