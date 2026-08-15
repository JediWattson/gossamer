package render

import (
	"image/color"
	"math"
	"sort"
	"strconv"
	"strings"
	"unicode"

	"github.com/JediWattson/gossamer/internal/css"
	"github.com/JediWattson/gossamer/internal/dom"
)

type displayMode uint8

const (
	displayInline displayMode = iota
	displayBlock
	displayNone
)

type lengthUnit uint8

const (
	lengthAuto lengthUnit = iota
	lengthPX
	lengthPercent
	lengthVW
	lengthVH
)

type length struct {
	value float64
	unit  lengthUnit
}

type computedStyle struct {
	display       displayMode
	color         color.NRGBA
	background    color.NRGBA
	hasBackground bool
	fontSize      float64
	fontWeight    FontWeight
	lineHeight    float64
	underline     bool
	opacity       float64
	width         length
	height        length
	marginTop     length
	marginRight   length
	marginBottom  length
	marginLeft    length
}

type styledNode struct {
	node     *dom.Node
	style    computedStyle
	children []*styledNode
}

type winningDeclaration struct {
	declaration css.Declaration
	specificity css.Specificity
	order       int
}

func buildStyleTree(document *dom.Node, viewport Viewport, external map[*dom.Node]css.Stylesheet) *styledNode {
	stylesheets := collectAuthorStyles(document, external)
	return styleNode(document, nil, stylesheets, viewport)
}

func collectAuthorStyles(root *dom.Node, external map[*dom.Node]css.Stylesheet) []css.Stylesheet {
	var stylesheets []css.Stylesheet
	var visit func(*dom.Node)
	visit = func(node *dom.Node) {
		if node == nil {
			return
		}
		if node.Type == dom.ElementNode {
			switch node.Data {
			case "style":
				if !authorStyleOwnerApplies(node) {
					break
				}
				var source strings.Builder
				for _, child := range node.Children {
					if child.Type == dom.TextNode {
						source.WriteString(child.Data)
					}
				}
				// CSS error recovery keeps all safely parsed rules. A malformed
				// author sheet must not prevent the document from rendering.
				stylesheet, _ := css.Parse(source.String())
				stylesheets = append(stylesheets, stylesheet)
			case "link":
				if stylesheet, ok := external[node]; ok && authorStyleOwnerApplies(node) {
					stylesheets = append(stylesheets, stylesheet)
				}
			}
		}
		for _, child := range node.Children {
			visit(child)
		}
	}
	visit(root)
	return stylesheets
}

func styleNode(node *dom.Node, parent *styledNode, sheets []css.Stylesheet, viewport Viewport) *styledNode {
	style := initialStyle(node, parent)
	if node != nil && node.Type == dom.ElementNode {
		applyAuthorStyles(&style, node, sheets, viewport, parentFontSize(parent))
	}
	styled := &styledNode{node: node, style: style}
	for _, child := range node.Children {
		styled.children = append(styled.children, styleNode(child, styled, sheets, viewport))
	}
	return styled
}

func initialStyle(node *dom.Node, parent *styledNode) computedStyle {
	style := computedStyle{
		display:      displayInline,
		color:        color.NRGBA{A: 0xff},
		fontSize:     16,
		lineHeight:   1.2,
		opacity:      1,
		width:        length{unit: lengthAuto},
		height:       length{unit: lengthAuto},
		marginTop:    length{unit: lengthPX},
		marginRight:  length{unit: lengthPX},
		marginBottom: length{unit: lengthPX},
		marginLeft:   length{unit: lengthPX},
	}
	if parent != nil {
		style.color = parent.style.color
		style.fontSize = parent.style.fontSize
		style.fontWeight = parent.style.fontWeight
		style.lineHeight = parent.style.lineHeight
		style.underline = parent.style.underline
	}
	if node == nil {
		return style
	}
	if node.Type == dom.DocumentNode {
		style.display = displayBlock
		return style
	}
	if node.Type != dom.ElementNode {
		return style
	}

	switch node.Data {
	case "html", "body", "address", "article", "aside", "blockquote", "div", "dl", "fieldset", "figcaption", "figure", "footer", "form", "header", "hgroup", "main", "nav", "ol", "p", "pre", "section", "table", "ul", "h1", "h2", "h3", "h4", "h5", "h6":
		style.display = displayBlock
	case "head", "base", "link", "meta", "title", "style", "script", "template", "noscript":
		style.display = displayNone
	}

	switch node.Data {
	case "body":
		style.marginTop = px(8)
		style.marginRight = px(8)
		style.marginBottom = px(8)
		style.marginLeft = px(8)
	case "h1":
		style.fontSize *= 2
		style.fontWeight = FontWeightBold
		style.marginTop = px(style.fontSize * .67)
		style.marginBottom = px(style.fontSize * .67)
	case "h2":
		style.fontSize *= 1.5
		style.fontWeight = FontWeightBold
		style.marginTop = px(style.fontSize * .83)
		style.marginBottom = px(style.fontSize * .83)
	case "h3":
		style.fontSize *= 1.17
		style.fontWeight = FontWeightBold
		style.marginTop = px(style.fontSize)
		style.marginBottom = px(style.fontSize)
	case "h4", "h5", "h6":
		style.fontWeight = FontWeightBold
		style.marginTop = px(style.fontSize * 1.33)
		style.marginBottom = px(style.fontSize * 1.33)
	case "p":
		style.marginTop = px(style.fontSize)
		style.marginBottom = px(style.fontSize)
	case "a":
		if _, ok := attribute(node, "href"); ok {
			style.color = color.NRGBA{R: 0, G: 0, B: 0xee, A: 0xff}
			style.underline = true
		}
	case "strong", "b":
		style.fontWeight = FontWeightBold
	case "img":
		if value, ok := dimensionAttribute(node, "width"); ok {
			style.width = px(value)
		}
		if value, ok := dimensionAttribute(node, "height"); ok {
			style.height = px(value)
		}
	}
	return style
}

func applyAuthorStyles(style *computedStyle, node *dom.Node, sheets []css.Stylesheet, viewport Viewport, parentSize float64) {
	winners := make(map[string]winningDeclaration)
	sourceOrder := 0
	for _, sheet := range sheets {
		for _, rule := range sheet.Rules {
			specificity, matches := rule.Match(node)
			for _, declaration := range rule.Declarations {
				order := sourceOrder
				sourceOrder++
				if !matches {
					continue
				}
				candidate := winningDeclaration{
					declaration: declaration,
					specificity: specificity,
					order:       order,
				}
				if current, ok := winners[declaration.Property]; !ok || declarationWins(candidate, current) {
					winners[declaration.Property] = candidate
				}
			}
		}
	}

	if source, ok := attribute(node, "style"); ok {
		inlineSheet, _ := css.Parse("*{" + source + "}")
		if len(inlineSheet.Rules) != 0 {
			for _, declaration := range inlineSheet.Rules[0].Declarations {
				candidate := winningDeclaration{
					declaration: declaration,
					specificity: css.Specificity{IDs: 1_000_000},
					order:       sourceOrder,
				}
				sourceOrder++
				if current, ok := winners[declaration.Property]; !ok || declarationWins(candidate, current) {
					winners[declaration.Property] = candidate
				}
			}
		}
	}

	// Font size must be computed before em lengths in the remaining properties.
	if winner, ok := winners["font-size"]; ok {
		if value, ok := parseLength(winner.declaration.Value, parentSize, parentSize, viewport); ok && value.unit != lengthAuto {
			resolved := resolveLength(value, parentSize, viewport, parentSize)
			if resolved > 0 && isFinite(resolved) {
				style.fontSize = resolved
			}
		}
	}
	orderedWinners := make([]winningDeclaration, 0, len(winners))
	for property, winner := range winners {
		if property == "font-size" {
			continue
		}
		orderedWinners = append(orderedWinners, winner)
	}
	sort.SliceStable(orderedWinners, func(left, right int) bool {
		return declarationPrecedence(orderedWinners[left], orderedWinners[right]) < 0
	})
	for _, winner := range orderedWinners {
		applyDeclaration(style, winner.declaration.Property, winner.declaration.Value, viewport)
	}
}

func authorStyleOwnerApplies(node *dom.Node) bool {
	if node == nil || node.Type != dom.ElementNode {
		return false
	}
	if node.Data == "style" {
		if sourceType, ok := attribute(node, "type"); ok && strings.TrimSpace(sourceType) != "" {
			essence := strings.TrimSpace(strings.SplitN(sourceType, ";", 2)[0])
			if !strings.EqualFold(essence, "text/css") {
				return false
			}
		}
	}
	if node.Data == "link" {
		if _, disabled := attribute(node, "disabled"); disabled {
			return false
		}
		rel, _ := attribute(node, "rel")
		if containsHTMLToken(rel, "alternate") {
			return false
		}
	}
	media, _ := attribute(node, "media")
	return mediaTypeMayMatchScreen(media)
}

// mediaTypeMayMatchScreen evaluates only the media type. Feature expressions
// are intentionally left for the future media-query evaluator, so a screen or
// all query remains eligible regardless of its trailing conditions.
func mediaTypeMayMatchScreen(source string) bool {
	if strings.TrimSpace(source) == "" {
		return true
	}
	for _, rawQuery := range strings.Split(strings.ToLower(source), ",") {
		fields := strings.Fields(strings.TrimSpace(rawQuery))
		if len(fields) == 0 {
			continue
		}
		if fields[0] == "only" {
			fields = fields[1:]
		}
		negated := false
		if len(fields) != 0 && fields[0] == "not" {
			negated = true
			fields = fields[1:]
		}
		if len(fields) == 0 {
			continue
		}
		matches := false
		switch {
		case strings.HasPrefix(fields[0], "("):
			matches = true
		case fields[0] == "all", fields[0] == "screen":
			matches = true
		case fields[0] == "print":
			matches = false
		default:
			matches = false
		}
		if negated {
			matches = !matches
		}
		if matches {
			return true
		}
	}
	return false
}

func containsHTMLToken(source, token string) bool {
	for _, candidate := range strings.Fields(source) {
		if strings.EqualFold(candidate, token) {
			return true
		}
	}
	return false
}

func declarationWins(candidate, current winningDeclaration) bool {
	return declarationPrecedence(candidate, current) >= 0
}

func declarationPrecedence(left, right winningDeclaration) int {
	if left.declaration.Important != right.declaration.Important {
		if left.declaration.Important {
			return 1
		}
		return -1
	}
	if comparison := left.specificity.Compare(right.specificity); comparison != 0 {
		return comparison
	}
	switch {
	case left.order < right.order:
		return -1
	case left.order > right.order:
		return 1
	default:
		return 0
	}
}

func applyDeclaration(style *computedStyle, property, source string, viewport Viewport) {
	value := strings.TrimSpace(strings.ToLower(source))
	switch property {
	case "display":
		switch value {
		case "none":
			style.display = displayNone
		case "block":
			style.display = displayBlock
		case "inline", "inline-block":
			style.display = displayInline
		}
	case "color":
		if parsed, ok := parseColor(value); ok {
			style.color = parsed
		}
	case "background", "background-color":
		if parsed, ok := parseColor(firstCSSValue(value)); ok {
			style.background = parsed
			style.hasBackground = parsed.A != 0
		}
	case "font-weight":
		if value == "bold" || value == "bolder" {
			style.fontWeight = FontWeightBold
		} else if numeric, err := strconv.Atoi(value); err == nil && numeric >= 600 {
			style.fontWeight = FontWeightBold
		} else if value == "normal" || value == "lighter" {
			style.fontWeight = FontWeightNormal
		}
	case "line-height":
		if numeric, err := strconv.ParseFloat(value, 64); err == nil && numeric > 0 && isFinite(numeric) {
			style.lineHeight = numeric
		}
	case "text-decoration", "text-decoration-line":
		if strings.Contains(value, "underline") {
			style.underline = true
		} else if value == "none" {
			style.underline = false
		}
	case "opacity":
		if numeric, err := strconv.ParseFloat(value, 64); err == nil && isFinite(numeric) {
			style.opacity = clamp(numeric, 0, 1)
		}
	case "width":
		if parsed, ok := parseLength(value, style.fontSize, style.fontSize, viewport); ok && nonNegativeLength(parsed) {
			style.width = parsed
		}
	case "height":
		if parsed, ok := parseLength(value, style.fontSize, style.fontSize, viewport); ok && nonNegativeLength(parsed) {
			style.height = parsed
		}
	case "margin":
		if values, ok := parseBoxLengths(value, style.fontSize, viewport); ok {
			style.marginTop, style.marginRight, style.marginBottom, style.marginLeft = values[0], values[1], values[2], values[3]
		}
	case "margin-top", "margin-right", "margin-bottom", "margin-left":
		if parsed, ok := parseLength(value, style.fontSize, style.fontSize, viewport); ok {
			switch property {
			case "margin-top":
				style.marginTop = parsed
			case "margin-right":
				style.marginRight = parsed
			case "margin-bottom":
				style.marginBottom = parsed
			case "margin-left":
				style.marginLeft = parsed
			}
		}
	}
}

func attribute(node *dom.Node, name string) (string, bool) {
	for _, candidate := range node.Attributes {
		if strings.EqualFold(candidate.Name, name) {
			return candidate.Value, true
		}
	}
	return "", false
}

func dimensionAttribute(node *dom.Node, name string) (float64, bool) {
	source, ok := attribute(node, name)
	if !ok {
		return 0, false
	}
	value, err := strconv.ParseUint(strings.TrimSpace(source), 10, 31)
	if err != nil {
		return 0, false
	}
	return float64(value), true
}

func parseBoxLengths(source string, fontSize float64, viewport Viewport) ([4]length, bool) {
	parts := strings.Fields(source)
	if len(parts) < 1 || len(parts) > 4 {
		return [4]length{}, false
	}
	parsed := make([]length, len(parts))
	for index, part := range parts {
		value, ok := parseLength(part, fontSize, fontSize, viewport)
		if !ok {
			return [4]length{}, false
		}
		parsed[index] = value
	}
	var result [4]length
	switch len(parsed) {
	case 1:
		result = [4]length{parsed[0], parsed[0], parsed[0], parsed[0]}
	case 2:
		result = [4]length{parsed[0], parsed[1], parsed[0], parsed[1]}
	case 3:
		result = [4]length{parsed[0], parsed[1], parsed[2], parsed[1]}
	case 4:
		copy(result[:], parsed)
	}
	return result, true
}

func parseLength(source string, emBase, percentBase float64, viewport Viewport) (length, bool) {
	value := strings.TrimSpace(strings.ToLower(source))
	if value == "auto" {
		return length{unit: lengthAuto}, true
	}
	if value == "0" {
		return px(0), true
	}
	units := []struct {
		suffix string
		unit   lengthUnit
		scale  float64
	}{
		{"rem", lengthPX, 16},
		{"px", lengthPX, 1},
		{"em", lengthPX, emBase},
		{"vw", lengthVW, 1},
		{"vh", lengthVH, 1},
		{"%", lengthPercent, 1},
	}
	for _, candidate := range units {
		if !strings.HasSuffix(value, candidate.suffix) {
			continue
		}
		numeric, err := strconv.ParseFloat(strings.TrimSpace(strings.TrimSuffix(value, candidate.suffix)), 64)
		if err != nil || !isFinite(numeric) {
			return length{}, false
		}
		scaled := numeric * candidate.scale
		if !isFinite(scaled) {
			return length{}, false
		}
		return length{value: scaled, unit: candidate.unit}, true
	}
	return length{}, false
}

func resolveLength(value length, percentBase float64, viewport Viewport, autoValue float64) float64 {
	switch value.unit {
	case lengthPX:
		return value.value
	case lengthPercent:
		return percentBase * value.value / 100
	case lengthVW:
		return float64(viewport.Width) * value.value / 100
	case lengthVH:
		return float64(viewport.Height) * value.value / 100
	default:
		return autoValue
	}
}

func parseColor(source string) (color.NRGBA, bool) {
	value := strings.TrimSpace(strings.ToLower(source))
	if strings.HasPrefix(value, "#") {
		hex := strings.TrimPrefix(value, "#")
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
	}
	switch value {
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
	}
	return color.NRGBA{}, false
}

func firstCSSValue(source string) string {
	for index, runeValue := range source {
		if unicode.IsSpace(runeValue) {
			return source[:index]
		}
	}
	return source
}

func parentFontSize(parent *styledNode) float64 {
	if parent == nil {
		return 16
	}
	return parent.style.fontSize
}

func px(value float64) length {
	return length{value: value, unit: lengthPX}
}

func nonNegativeLength(value length) bool {
	return value.unit == lengthAuto || value.value >= 0
}

func isFinite(value float64) bool {
	return !math.IsNaN(value) && !math.IsInf(value, 0)
}

func clamp(value, minimum, maximum float64) float64 {
	if value < minimum {
		return minimum
	}
	if value > maximum {
		return maximum
	}
	return value
}
