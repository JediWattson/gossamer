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
	displayListItem
	displayNone
)

func (display displayMode) isBlockLevel() bool {
	return display == displayBlock || display == displayListItem
}

type textAlignment uint8

const (
	alignLeft textAlignment = iota
	alignCenter
	alignRight
)

type computedLineHeight struct {
	value    float64
	absolute bool
}

type listStyleType uint8

const (
	listStyleDisc listStyleType = iota
	listStyleCircle
	listStyleSquare
	listStyleDecimal
	listStyleNone
)

type borderStyle uint8

const (
	borderStyleNone borderStyle = iota
	borderStyleSolid
	borderStyleHidden
)

type borderSide struct {
	width    length
	style    borderStyle
	color    color.NRGBA
	hasColor bool
}

func (lineHeight computedLineHeight) pixels(fontSize float64) float64 {
	if lineHeight.absolute {
		return lineHeight.value
	}
	return fontSize * lineHeight.value
}

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
	display          displayMode
	color            color.NRGBA
	background       color.NRGBA
	hasBackground    bool
	fontSize         float64
	fontWeight       FontWeight
	lineHeight       computedLineHeight
	underline        bool
	textAlign        textAlignment
	listStyleType    listStyleType
	opacity          float64
	width            length
	height           length
	minWidth         length
	maxWidth         length
	paddingTop       length
	paddingRight     length
	paddingBottom    length
	paddingLeft      length
	borderTop        borderSide
	borderRight      borderSide
	borderBottom     borderSide
	borderLeft       borderSide
	marginTop        length
	marginRight      length
	marginBottom     length
	marginLeft       length
	customProperties css.CustomProperties
}

type styledNode struct {
	node     *dom.Node
	style    computedStyle
	children []*styledNode
}

type authorStyleContext struct {
	sheets           []css.Stylesheet
	layerRanks       map[string]int
	mediaEnvironment css.MediaEnvironment
}

const maxCustomPropertyCascadePasses = 128

type winningDeclaration struct {
	declaration css.Declaration
	target      string
	specificity css.Specificity
	order       int
	layerRank   int
	layered     bool
	inline      bool
}

func buildStyleTree(document *dom.Node, viewport Viewport, external map[*dom.Node]css.Stylesheet) *styledNode {
	stylesheets := collectAuthorStyles(document, external, viewport)
	author := authorStyleContext{
		sheets:           stylesheets,
		layerRanks:       authorLayerRanks(stylesheets),
		mediaEnvironment: screenMediaEnvironment(viewport),
	}
	return styleNode(document, nil, author, viewport)
}

func collectAuthorStyles(root *dom.Node, external map[*dom.Node]css.Stylesheet, viewport Viewport) []css.Stylesheet {
	var stylesheets []css.Stylesheet
	var visit func(*dom.Node)
	visit = func(node *dom.Node) {
		if node == nil {
			return
		}
		if node.Type == dom.ElementNode {
			switch node.Data {
			case "style":
				if !authorStyleOwnerApplies(node, viewport) {
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
				if stylesheet, ok := external[node]; ok && authorStyleOwnerApplies(node, viewport) {
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

func styleNode(node *dom.Node, parent *styledNode, author authorStyleContext, viewport Viewport) *styledNode {
	style := initialStyle(node, parent)
	if node != nil && node.Type == dom.ElementNode {
		applyAuthorStyles(&style, node, author, viewport, parent)
	}
	styled := &styledNode{node: node, style: style}
	for _, child := range node.Children {
		styled.children = append(styled.children, styleNode(child, styled, author, viewport))
	}
	return styled
}

func initialStyle(node *dom.Node, parent *styledNode) computedStyle {
	style := cssInitialStyle()
	if parent != nil {
		style.color = parent.style.color
		style.fontSize = parent.style.fontSize
		style.fontWeight = parent.style.fontWeight
		style.lineHeight = parent.style.lineHeight
		style.underline = parent.style.underline
		style.textAlign = parent.style.textAlign
		style.listStyleType = parent.style.listStyleType
		style.customProperties = parent.style.customProperties
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

	applyUserAgentStyle(&style, node)
	return style
}

func cssInitialStyle() computedStyle {
	return computedStyle{
		display:       displayInline,
		color:         color.NRGBA{A: 0xff},
		fontSize:      16,
		lineHeight:    computedLineHeight{value: 1.2},
		opacity:       1,
		width:         length{unit: lengthAuto},
		height:        length{unit: lengthAuto},
		minWidth:      px(0),
		maxWidth:      length{unit: lengthAuto},
		paddingTop:    px(0),
		paddingRight:  px(0),
		paddingBottom: px(0),
		paddingLeft:   px(0),
		borderTop:     initialBorderSide(),
		borderRight:   initialBorderSide(),
		borderBottom:  initialBorderSide(),
		borderLeft:    initialBorderSide(),
		marginTop:     length{unit: lengthPX},
		marginRight:   length{unit: lengthPX},
		marginBottom:  length{unit: lengthPX},
		marginLeft:    length{unit: lengthPX},
	}
}

func applyUserAgentStyle(style *computedStyle, node *dom.Node) {
	switch node.Data {
	case "html", "body", "address", "article", "aside", "blockquote", "div", "dl", "dt", "dd", "fieldset", "figcaption", "figure", "footer", "form", "header", "hgroup", "main", "nav", "ol", "p", "pre", "section", "table", "ul", "h1", "h2", "h3", "h4", "h5", "h6":
		style.display = displayBlock
	case "li":
		style.display = displayListItem
	case "noscript":
		// Gossamer has no scripting engine, so body fallback content participates
		// in normal flow. Treating the transparent element as a block keeps its
		// block descendants visible until inline box splitting is implemented.
		style.display = displayBlock
	case "head", "base", "link", "meta", "title", "style", "script", "template":
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
	case "ul", "ol":
		style.marginTop = px(style.fontSize)
		style.marginBottom = px(style.fontSize)
		style.paddingLeft = px(40)
		if node.Data == "ol" {
			style.listStyleType = listStyleDecimal
		} else {
			style.listStyleType = listStyleDisc
		}
	case "blockquote":
		style.marginTop = px(style.fontSize)
		style.marginRight = px(40)
		style.marginBottom = px(style.fontSize)
		style.marginLeft = px(40)
	case "dd":
		style.marginLeft = px(40)
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
}

func applyAuthorStyles(style *computedStyle, node *dom.Node, author authorStyleContext, viewport Viewport, parent *styledNode) {
	candidatesByTarget := make(map[string][]winningDeclaration)
	sourceOrder := 0
	record := func(declaration css.Declaration, specificity css.Specificity, order int, layer string, inline bool) {
		targets := declarationTargets(declaration.Property)
		if len(targets) == 0 {
			return
		}
		custom := strings.HasPrefix(declaration.Property, "--")
		deferred := css.ContainsVarFunction(declaration.Value)
		if custom {
			if !css.ValidCustomPropertyValue(declaration.Value) {
				return
			}
		} else {
			// Substitution defers the property's own grammar, but the declaration's
			// component values and every var() fallback must still be syntactically
			// valid before the declaration participates in the cascade.
			if !css.ValidDeclarationValue(declaration.Value) {
				return
			}
			if !deferred && cssWideKeyword(declaration.Value) == "" && !validCascadedDeclaration(declaration, viewport) {
				return
			}
		}
		layerRank, layered := author.layerRanks[layer]
		for _, target := range targets {
			candidate := winningDeclaration{
				declaration: declaration,
				target:      target,
				specificity: specificity,
				order:       order,
				layerRank:   layerRank,
				layered:     layered && !inline,
				inline:      inline,
			}
			candidatesByTarget[target] = append(candidatesByTarget[target], candidate)
		}
	}
	for _, sheet := range author.sheets {
		for _, rule := range sheet.Rules {
			specificity, matches := rule.Match(node)
			matches = matches && rule.MatchesMedia(author.mediaEnvironment)
			for _, declaration := range rule.Declarations {
				order := sourceOrder
				sourceOrder++
				if !matches {
					continue
				}
				record(declaration, specificity, order, rule.Layer, false)
			}
		}
	}

	if source, ok := attribute(node, "style"); ok {
		declarations, _ := css.ParseRawDeclarationList(source)
		for _, declaration := range declarations {
			record(declaration, css.Specificity{}, sourceOrder, "", true)
			sourceOrder++
		}
	}

	for target := range candidatesByTarget {
		candidates := candidatesByTarget[target]
		sort.SliceStable(candidates, func(left, right int) bool {
			return declarationPrecedence(candidates[left], candidates[right]) > 0
		})
	}

	customPropertyCandidates := make(map[string][]winningDeclaration)
	for target, candidates := range candidatesByTarget {
		if strings.HasPrefix(target, "--") {
			customPropertyCandidates[target] = candidates
			delete(candidatesByTarget, target)
		}
	}
	style.customProperties = resolveCustomPropertyCandidates(style.customProperties, customPropertyCandidates)

	// Font size computes before em lengths in every other supported property.
	if candidates, ok := candidatesByTarget["font-size"]; ok {
		applyDeclarationCandidates(style, parent, candidates, viewport)
		delete(candidatesByTarget, "font-size")
	}
	targets := make([]string, 0, len(candidatesByTarget))
	for target := range candidatesByTarget {
		targets = append(targets, target)
	}
	sort.Strings(targets)
	for _, target := range targets {
		applyDeclarationCandidates(style, parent, candidatesByTarget[target], viewport)
	}
}

func resolveCustomPropertyCandidates(parent css.CustomProperties, candidatesByName map[string][]winningDeclaration) css.CustomProperties {
	if len(candidatesByName) == 0 {
		return parent
	}
	names := make([]string, 0, len(candidatesByName))
	positions := make(map[string]int, len(candidatesByName))
	overrides := make(map[string]string, len(candidatesByName))
	settled := make(map[string]bool, len(candidatesByName))
	for name := range candidatesByName {
		names = append(names, name)
	}
	sort.Strings(names)

	for _, name := range names {
		positions[name] = normalizeCustomPropertyPosition(candidatesByName[name], 0)
	}

	for pass := 0; ; pass++ {
		specified := make(map[string]string, len(names))
		for _, name := range names {
			if override, ok := overrides[name]; ok {
				specified[name] = override
				continue
			}
			position := positions[name]
			if position < len(candidatesByName[name]) {
				specified[name] = candidatesByName[name][position].declaration.Value
			}
		}

		resolved := css.ResolveCustomProperties(parent, specified)
		cssWideValues := make(map[string]string)
		dependencies := make(map[string][]string)
		for _, name := range names {
			if settled[name] {
				continue
			}
			position := positions[name]
			if position >= len(candidatesByName[name]) {
				continue
			}
			value, ok := resolved.Value(name)
			if !ok {
				continue
			}
			keyword := css.CSSWideKeyword(value)
			if keyword == "" {
				continue
			}
			cssWideValues[name] = keyword
			references, valid := css.CustomPropertyReferences(candidatesByName[name][position].declaration.Value)
			if valid {
				dependencies[name] = references
			}
		}
		if len(cssWideValues) == 0 {
			return resolved
		}
		if pass >= maxCustomPropertyCascadePasses-1 {
			// Resolution is monotone, but adversarial dependency ordering can force
			// one CSS-wide property to settle per pass. Fail closed at the same order
			// of magnitude as the variable nesting limit so hostile CSS cannot make
			// cascade work quadratic without bound. A property that is not currently
			// a keyword can become one after a dependency is removed, so fail closed
			// over the complete reverse-reference closure, not just the current
			// keyword values.
			affected := customPropertyDependents(cssWideValues, names, settled, positions, candidatesByName)
			for name := range affected {
				specified[name] = "initial"
			}
			return css.ResolveCustomProperties(parent, specified)
		}

		// A CSS-wide keyword can flow through var() references. Apply it to the
		// declaration that produced it before recomputing dependents. This makes
		// --dependent:var(--source) observe the source's post-keyword value instead
		// of applying the propagated keyword to --dependent too.
		advanced := false
		for _, name := range names {
			keyword, hasCSSWideValue := cssWideValues[name]
			if !hasCSSWideValue || dependsOnCSSWideValue(dependencies[name], cssWideValues) {
				continue
			}
			candidates := candidatesByName[name]
			position := positions[name]
			switch keyword {
			case "initial", "inherit", "unset":
				// Re-feed the computed keyword as the specified value so the bounded
				// custom-property resolver applies its native CSS-wide semantics.
				overrides[name] = keyword
				settled[name] = true
			case "revert":
				// Gossamer has no user-origin custom properties. Removing all author
				// candidates leaves the inherited custom-property set in place.
				position = len(candidates)
				delete(overrides, name)
				settled[name] = true
			case "revert-layer":
				position = nextCandidateAfterRevertLayer(candidates, position)
				delete(overrides, name)
				settled[name] = false
			}
			positions[name] = normalizeCustomPropertyPosition(candidates, position)
			advanced = true
		}
		if !advanced {
			// ResolveCustomProperties invalidates cyclic var() graphs, so a graph of
			// resolved CSS-wide values always has a dependency leaf. Keep this guard
			// as a deterministic fail-safe if that invariant changes.
			return resolved
		}
	}
}

func customPropertyDependents(
	seeds map[string]string,
	names []string,
	settled map[string]bool,
	positions map[string]int,
	candidatesByName map[string][]winningDeclaration,
) map[string]bool {
	affected := make(map[string]bool, len(seeds))
	reverseReferences := make(map[string][]string)
	queue := make([]string, 0, len(seeds))
	for name := range seeds {
		affected[name] = true
		queue = append(queue, name)
	}
	for _, name := range names {
		if settled[name] {
			continue
		}
		position := positions[name]
		candidates := candidatesByName[name]
		if position >= len(candidates) {
			continue
		}
		references, valid := css.CustomPropertyReferences(candidates[position].declaration.Value)
		if !valid {
			continue
		}
		for _, reference := range references {
			reverseReferences[reference] = append(reverseReferences[reference], name)
		}
	}
	for len(queue) > 0 {
		dependency := queue[0]
		queue = queue[1:]
		for _, dependent := range reverseReferences[dependency] {
			if affected[dependent] {
				continue
			}
			affected[dependent] = true
			queue = append(queue, dependent)
		}
	}
	return affected
}

func normalizeCustomPropertyPosition(candidates []winningDeclaration, position int) int {
	for position < len(candidates) {
		switch css.CSSWideKeyword(candidates[position].declaration.Value) {
		case "revert":
			return len(candidates)
		case "revert-layer":
			position = nextCandidateAfterRevertLayer(candidates, position)
		default:
			return position
		}
	}
	return len(candidates)
}

func dependsOnCSSWideValue(references []string, cssWideValues map[string]string) bool {
	for _, reference := range references {
		if _, ok := cssWideValues[reference]; ok {
			return true
		}
	}
	return false
}

func declarationTargets(property string) []string {
	if strings.HasPrefix(property, "--") {
		return []string{property}
	}
	switch property {
	case "font":
		return []string{"font-size", "font-weight", "line-height"}
	case "background":
		return []string{"background-color"}
	case "margin":
		return []string{"margin-top", "margin-right", "margin-bottom", "margin-left"}
	case "padding":
		return []string{"padding-top", "padding-right", "padding-bottom", "padding-left"}
	case "border":
		return []string{
			"border-top-width", "border-right-width", "border-bottom-width", "border-left-width",
			"border-top-style", "border-right-style", "border-bottom-style", "border-left-style",
			"border-top-color", "border-right-color", "border-bottom-color", "border-left-color",
		}
	case "border-top", "border-right", "border-bottom", "border-left":
		side := strings.TrimPrefix(property, "border-")
		return []string{"border-" + side + "-width", "border-" + side + "-style", "border-" + side + "-color"}
	case "border-width":
		return []string{"border-top-width", "border-right-width", "border-bottom-width", "border-left-width"}
	case "border-style":
		return []string{"border-top-style", "border-right-style", "border-bottom-style", "border-left-style"}
	case "border-color":
		return []string{"border-top-color", "border-right-color", "border-bottom-color", "border-left-color"}
	case "text-decoration":
		return []string{"text-decoration-line"}
	case "list-style":
		return []string{"list-style-type"}
	case "display", "color", "background-color", "font-size", "font-weight", "line-height",
		"text-decoration-line", "opacity", "width", "height", "min-width", "max-width",
		"padding-top", "padding-right", "padding-bottom", "padding-left",
		"border-top-width", "border-right-width", "border-bottom-width", "border-left-width",
		"border-top-style", "border-right-style", "border-bottom-style", "border-left-style",
		"border-top-color", "border-right-color", "border-bottom-color", "border-left-color",
		"text-align", "list-style-type", "margin-top", "margin-right", "margin-bottom", "margin-left":
		return []string{property}
	default:
		return nil
	}
}

func validCascadedDeclaration(declaration css.Declaration, viewport Viewport) bool {
	if declaration.Property == "font" {
		_, _, _, _, ok := parseFontShorthand(declaration.Value, viewport)
		return ok
	}
	return validComputedDeclaration(declaration, viewport)
}

func cssWideKeyword(source string) string {
	return css.CSSWideKeyword(source)
}

func applyDeclarationCandidates(style *computedStyle, parent *styledNode, candidates []winningDeclaration, viewport Viewport) {
	for position := 0; position < len(candidates); {
		candidate := candidates[position]
		resolved, ok := style.customProperties.Substitute(candidate.declaration.Value)
		if !ok {
			// A winning declaration whose var() cannot be substituted is invalid at
			// computed-value time. It computes as unset without reviving a loser.
			applyCSSWideKeyword(style, parent, candidate.target, "unset")
			return
		}

		switch keyword := cssWideKeyword(resolved); keyword {
		case "revert":
			// There is no user origin in this renderer. Leaving the pre-author
			// style untouched exposes the inherited/UA result.
			return
		case "revert-layer":
			position = nextCandidateAfterRevertLayer(candidates, position)
			continue
		case "inherit", "initial", "unset":
			applyCSSWideKeyword(style, parent, candidate.target, keyword)
			return
		}

		declaration := candidate.declaration
		declaration.Value = resolved
		if !validCascadedDeclaration(declaration, viewport) {
			// A declaration containing var() participates in the cascade before its
			// computed value is known. If substitution produces an invalid value, it
			// computes as unset; a lower-priority declaration is not resurrected.
			applyCSSWideKeyword(style, parent, candidate.target, "unset")
			return
		}
		applyTargetDeclaration(style, parentFontSize(parent), candidate.target, declaration, viewport)
		return
	}
}

func nextCandidateAfterRevertLayer(candidates []winningDeclaration, position int) int {
	current := candidates[position]
	for position++; position < len(candidates); position++ {
		if survivesRevertLayer(current, candidates[position]) {
			return position
		}
	}
	return len(candidates)
}

func survivesRevertLayer(current, candidate winningDeclaration) bool {
	// Element-attached styles form their own cascade step. CSS Cascade 5 makes
	// important inline revert-layer an explicit exception: it removes inline
	// declarations but does not remove intervening author-important rules.
	if current.inline {
		return !candidate.inline
	}
	if !current.declaration.Important {
		return !sameCascadeLayer(current, candidate)
	}

	// Important layer order mirrors normal layer order. Rolling back an
	// important layer removes that layer and every cascade step between its
	// important and normal halves, then resumes in the preceding normal layer.
	if candidate.declaration.Important || candidate.inline || !candidate.layered {
		return false
	}
	if !current.layered {
		return true
	}
	return candidate.layerRank < current.layerRank
}

func sameCascadeLayer(left, right winningDeclaration) bool {
	if left.inline || right.inline {
		return left.inline && right.inline
	}
	if left.layered || right.layered {
		return left.layered && right.layered && left.layerRank == right.layerRank
	}
	return true
}

func applyTargetDeclaration(style *computedStyle, parentSize float64, target string, declaration css.Declaration, viewport Viewport) {
	if declaration.Property == "font" {
		size, lineHeight, weight, _, ok := parseFontShorthand(declaration.Value, viewport)
		if !ok {
			return
		}
		switch target {
		case "font-size":
			declaration = css.Declaration{Property: target, Value: size, Important: declaration.Important}
		case "font-weight":
			declaration = css.Declaration{Property: target, Value: weight, Important: declaration.Important}
		case "line-height":
			declaration = css.Declaration{Property: target, Value: lineHeight, Important: declaration.Important}
		}
	}
	if target == "font-size" {
		if value, ok := parseLength(declaration.Value, parentSize, parentSize, viewport); ok && value.unit != lengthAuto {
			resolved := resolveLength(value, parentSize, viewport, parentSize)
			if resolved > 0 && isFinite(resolved) {
				style.fontSize = resolved
			}
		}
		return
	}
	if declaration.Property == target {
		applyDeclaration(style, target, declaration.Value, viewport)
		return
	}
	temporary := *style
	applyDeclaration(&temporary, declaration.Property, declaration.Value, viewport)
	copyComputedProperty(style, temporary, target)
}

func applyCSSWideKeyword(style *computedStyle, parent *styledNode, target, keyword string) {
	initial := cssInitialStyle()
	source := initial
	switch keyword {
	case "inherit":
		if parent != nil {
			source = parent.style
		}
	case "unset":
		if inheritedProperty(target) && parent != nil {
			source = parent.style
		}
	}
	copyComputedProperty(style, source, target)
}

func inheritedProperty(property string) bool {
	switch property {
	case "color", "font-size", "font-weight", "line-height", "text-align", "list-style-type":
		return true
	default:
		return false
	}
}

func copyComputedProperty(destination *computedStyle, source computedStyle, property string) {
	switch property {
	case "display":
		destination.display = source.display
	case "color":
		destination.color = source.color
	case "background-color":
		destination.background = source.background
		destination.hasBackground = source.hasBackground
	case "font-size":
		destination.fontSize = source.fontSize
	case "font-weight":
		destination.fontWeight = source.fontWeight
	case "line-height":
		destination.lineHeight = source.lineHeight
	case "text-decoration-line":
		destination.underline = source.underline
	case "text-align":
		destination.textAlign = source.textAlign
	case "list-style-type":
		destination.listStyleType = source.listStyleType
	case "opacity":
		destination.opacity = source.opacity
	case "width":
		destination.width = source.width
	case "height":
		destination.height = source.height
	case "min-width":
		destination.minWidth = source.minWidth
	case "max-width":
		destination.maxWidth = source.maxWidth
	case "padding-top":
		destination.paddingTop = source.paddingTop
	case "padding-right":
		destination.paddingRight = source.paddingRight
	case "padding-bottom":
		destination.paddingBottom = source.paddingBottom
	case "padding-left":
		destination.paddingLeft = source.paddingLeft
	case "margin-top":
		destination.marginTop = source.marginTop
	case "margin-right":
		destination.marginRight = source.marginRight
	case "margin-bottom":
		destination.marginBottom = source.marginBottom
	case "margin-left":
		destination.marginLeft = source.marginLeft
	case "border-top-width":
		destination.borderTop.width = source.borderTop.width
	case "border-right-width":
		destination.borderRight.width = source.borderRight.width
	case "border-bottom-width":
		destination.borderBottom.width = source.borderBottom.width
	case "border-left-width":
		destination.borderLeft.width = source.borderLeft.width
	case "border-top-style":
		destination.borderTop.style = source.borderTop.style
	case "border-right-style":
		destination.borderRight.style = source.borderRight.style
	case "border-bottom-style":
		destination.borderBottom.style = source.borderBottom.style
	case "border-left-style":
		destination.borderLeft.style = source.borderLeft.style
	case "border-top-color":
		destination.borderTop.color = source.borderTop.color
		destination.borderTop.hasColor = source.borderTop.hasColor
	case "border-right-color":
		destination.borderRight.color = source.borderRight.color
		destination.borderRight.hasColor = source.borderRight.hasColor
	case "border-bottom-color":
		destination.borderBottom.color = source.borderBottom.color
		destination.borderBottom.hasColor = source.borderBottom.hasColor
	case "border-left-color":
		destination.borderLeft.color = source.borderLeft.color
		destination.borderLeft.hasColor = source.borderLeft.hasColor
	}
}

func authorLayerRanks(sheets []css.Stylesheet) map[string]int {
	ranks := make(map[string]int)
	for _, sheet := range sheets {
		for _, name := range sheet.LayerOrder {
			if name == "" {
				continue
			}
			if _, exists := ranks[name]; exists {
				continue
			}
			ranks[name] = len(ranks)
		}
		for _, rule := range sheet.Rules {
			if rule.Layer == "" {
				continue
			}
			if _, exists := ranks[rule.Layer]; !exists {
				ranks[rule.Layer] = len(ranks)
			}
		}
	}
	return ranks
}

func screenMediaEnvironment(viewport Viewport) css.MediaEnvironment {
	return css.MediaEnvironment{
		Type:            "screen",
		Width:           float64(viewport.Width),
		Height:          float64(viewport.Height),
		InitialFontSize: 16,
	}
}

func validComputedDeclaration(declaration css.Declaration, viewport Viewport) bool {
	value := strings.TrimSpace(strings.ToLower(declaration.Value))
	switch declaration.Property {
	case "display":
		switch value {
		case "none", "block", "list-item", "inline", "inline-block":
			return true
		default:
			return false
		}
	case "color":
		_, ok := parseColor(value)
		return ok
	case "background", "background-color":
		_, ok := parseColor(firstCSSValue(value))
		return ok
	case "font-size":
		parsed, ok := parseLength(value, 1, 1, viewport)
		if !ok || parsed.unit == lengthAuto {
			return false
		}
		resolved := resolveLength(parsed, 1, viewport, 1)
		return resolved > 0 && isFinite(resolved)
	case "font-weight":
		if value == "bold" || value == "bolder" || value == "normal" || value == "lighter" {
			return true
		}
		numeric, err := strconv.Atoi(value)
		return err == nil && numeric >= 1 && numeric <= 1000
	case "line-height":
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
	case "text-decoration", "text-decoration-line":
		if value == "none" {
			return true
		}
		for _, token := range strings.Fields(value) {
			if token == "underline" {
				return true
			}
		}
		return false
	case "opacity":
		numeric, err := strconv.ParseFloat(value, 64)
		return err == nil && isFinite(numeric)
	case "width", "height", "min-width":
		parsed, ok := parseLength(value, 1, 1, viewport)
		return ok && nonNegativeLength(parsed)
	case "max-width":
		if value == "none" {
			return true
		}
		parsed, ok := parseLength(value, 1, 1, viewport)
		return ok && nonNegativeLength(parsed)
	case "padding":
		_, ok := parsePaddingLengths(value, 1, viewport)
		return ok
	case "padding-top", "padding-right", "padding-bottom", "padding-left":
		parsed, ok := parseLength(value, 1, 1, viewport)
		return ok && parsed.unit != lengthAuto && nonNegativeLength(parsed)
	case "border", "border-top", "border-right", "border-bottom", "border-left":
		_, ok := parseBorderShorthand(value, 1, viewport)
		return ok
	case "border-width":
		_, ok := parseBorderWidths(value, 1, viewport)
		return ok
	case "border-top-width", "border-right-width", "border-bottom-width", "border-left-width":
		_, ok := parseBorderWidth(value, 1, viewport)
		return ok
	case "border-style":
		_, ok := parseBorderStyles(value)
		return ok
	case "border-top-style", "border-right-style", "border-bottom-style", "border-left-style":
		_, ok := parseBorderStyle(value)
		return ok
	case "border-color":
		_, ok := parseBorderColors(value)
		return ok
	case "border-top-color", "border-right-color", "border-bottom-color", "border-left-color":
		_, ok := parseBorderColor(value)
		return ok
	case "text-align":
		switch value {
		case "center", "right", "end", "left", "start", "justify":
			return true
		default:
			return false
		}
	case "list-style", "list-style-type":
		_, ok := parseListStyleType(value)
		return ok
	case "margin":
		_, ok := parseBoxLengths(value, 1, viewport)
		return ok
	case "margin-top", "margin-right", "margin-bottom", "margin-left":
		_, ok := parseLength(value, 1, 1, viewport)
		return ok
	default:
		return false
	}
}

func parseFontShorthand(source string, viewport Viewport) (size, lineHeight, weight, family string, ok bool) {
	fields := strings.Fields(strings.TrimSpace(source))
	if len(fields) < 2 {
		return "", "", "", "", false
	}
	weight = "normal"
	lineHeight = "normal"
	for index := 0; index < len(fields); index++ {
		field := fields[index]
		sizeSource := field
		inlineLineHeight := ""
		if slash := strings.IndexByte(field, '/'); slash >= 0 {
			sizeSource = field[:slash]
			inlineLineHeight = field[slash+1:]
		}
		parsedSize, validSize := parseLength(sizeSource, 1, 1, viewport)
		if !validSize || parsedSize.unit == lengthAuto || !nonNegativeLength(parsedSize) {
			if !parseFontPrefixToken(field, &weight) {
				return "", "", "", "", false
			}
			continue
		}

		size = sizeSource
		familyStart := index + 1
		if strings.Contains(field, "/") {
			if inlineLineHeight == "" {
				if familyStart >= len(fields) {
					return "", "", "", "", false
				}
				inlineLineHeight = fields[familyStart]
				familyStart++
			}
			lineHeight = inlineLineHeight
		} else if familyStart < len(fields) {
			next := fields[familyStart]
			switch {
			case next == "/":
				familyStart++
				if familyStart >= len(fields) {
					return "", "", "", "", false
				}
				lineHeight = fields[familyStart]
				familyStart++
			case strings.HasPrefix(next, "/"):
				lineHeight = strings.TrimPrefix(next, "/")
				familyStart++
			}
		}
		if !validFontLineHeight(lineHeight, viewport) || familyStart >= len(fields) {
			return "", "", "", "", false
		}
		family = strings.Join(fields[familyStart:], " ")
		return size, lineHeight, weight, family, true
	}
	return "", "", "", "", false
}

func parseFontPrefixToken(token string, weight *string) bool {
	token = strings.ToLower(token)
	switch token {
	case "normal":
		*weight = "normal"
		return true
	case "bold", "bolder", "lighter":
		*weight = token
		return true
	case "italic", "oblique", "small-caps", "condensed", "expanded":
		return true
	}
	if numeric, err := strconv.Atoi(token); err == nil && numeric >= 1 && numeric <= 1000 {
		*weight = token
		return true
	}
	return false
}

func validFontLineHeight(source string, viewport Viewport) bool {
	if strings.EqualFold(source, "normal") {
		return true
	}
	if numeric, err := strconv.ParseFloat(source, 64); err == nil {
		return numeric > 0 && isFinite(numeric)
	}
	parsed, ok := parseLength(source, 1, 1, viewport)
	return ok && parsed.unit != lengthAuto && nonNegativeLength(parsed)
}

func authorStyleOwnerApplies(node *dom.Node, viewport Viewport) bool {
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
	return css.MediaQueryListMatches(media, screenMediaEnvironment(viewport))
}

func containsHTMLToken(source, token string) bool {
	for _, candidate := range strings.Fields(source) {
		if strings.EqualFold(candidate, token) {
			return true
		}
	}
	return false
}

func declarationPrecedence(left, right winningDeclaration) int {
	if left.declaration.Important != right.declaration.Important {
		if left.declaration.Important {
			return 1
		}
		return -1
	}
	if left.inline != right.inline {
		if left.inline {
			return 1
		}
		return -1
	}
	if left.layered != right.layered {
		if left.declaration.Important {
			if left.layered {
				return 1
			}
			return -1
		}
		if left.layered {
			return -1
		}
		return 1
	}
	if left.layered && left.layerRank != right.layerRank {
		if left.declaration.Important {
			return compareInt(right.layerRank, left.layerRank)
		}
		return compareInt(left.layerRank, right.layerRank)
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

func compareInt(left, right int) int {
	switch {
	case left < right:
		return -1
	case left > right:
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
		case "list-item":
			style.display = displayListItem
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
		} else if numeric, err := strconv.Atoi(value); err == nil && numeric >= 1 && numeric <= 1000 {
			if numeric >= 600 {
				style.fontWeight = FontWeightBold
			} else {
				style.fontWeight = FontWeightNormal
			}
		} else if value == "normal" || value == "lighter" {
			style.fontWeight = FontWeightNormal
		}
	case "line-height":
		if value == "normal" {
			style.lineHeight = computedLineHeight{value: 1.2}
		} else if numeric, err := strconv.ParseFloat(value, 64); err == nil && numeric > 0 && isFinite(numeric) {
			style.lineHeight = computedLineHeight{value: numeric}
		} else if parsed, ok := parseLength(value, style.fontSize, style.fontSize, viewport); ok && parsed.unit != lengthAuto {
			resolved := resolveLength(parsed, style.fontSize, viewport, style.lineHeight.pixels(style.fontSize))
			if resolved > 0 && isFinite(resolved) {
				style.lineHeight = computedLineHeight{value: resolved, absolute: true}
			}
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
	case "min-width":
		if value == "auto" {
			style.minWidth = px(0)
		} else if parsed, ok := parseLength(value, style.fontSize, style.fontSize, viewport); ok && nonNegativeLength(parsed) {
			style.minWidth = parsed
		}
	case "max-width":
		if value == "none" {
			style.maxWidth = length{unit: lengthAuto}
		} else if parsed, ok := parseLength(value, style.fontSize, style.fontSize, viewport); ok && nonNegativeLength(parsed) {
			style.maxWidth = parsed
		}
	case "padding":
		if values, ok := parsePaddingLengths(value, style.fontSize, viewport); ok {
			style.paddingTop, style.paddingRight, style.paddingBottom, style.paddingLeft = values[0], values[1], values[2], values[3]
		}
	case "padding-top", "padding-right", "padding-bottom", "padding-left":
		if parsed, ok := parseLength(value, style.fontSize, style.fontSize, viewport); ok && parsed.unit != lengthAuto && nonNegativeLength(parsed) {
			switch property {
			case "padding-top":
				style.paddingTop = parsed
			case "padding-right":
				style.paddingRight = parsed
			case "padding-bottom":
				style.paddingBottom = parsed
			case "padding-left":
				style.paddingLeft = parsed
			}
		}
	case "border":
		if parsed, ok := parseBorderShorthand(value, style.fontSize, viewport); ok {
			style.borderTop, style.borderRight, style.borderBottom, style.borderLeft = parsed, parsed, parsed, parsed
		}
	case "border-top", "border-right", "border-bottom", "border-left":
		if parsed, ok := parseBorderShorthand(value, style.fontSize, viewport); ok {
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
		if parsed, ok := parseBorderWidths(value, style.fontSize, viewport); ok {
			style.borderTop.width, style.borderRight.width, style.borderBottom.width, style.borderLeft.width = parsed[0], parsed[1], parsed[2], parsed[3]
		}
	case "border-top-width", "border-right-width", "border-bottom-width", "border-left-width":
		if parsed, ok := parseBorderWidth(value, style.fontSize, viewport); ok {
			borderSideForProperty(style, property).width = parsed
		}
	case "border-style":
		if parsed, ok := parseBorderStyles(value); ok {
			style.borderTop.style, style.borderRight.style, style.borderBottom.style, style.borderLeft.style = parsed[0], parsed[1], parsed[2], parsed[3]
		}
	case "border-top-style", "border-right-style", "border-bottom-style", "border-left-style":
		if parsed, ok := parseBorderStyle(value); ok {
			borderSideForProperty(style, property).style = parsed
		}
	case "border-color":
		if parsed, ok := parseBorderColors(value); ok {
			applyBorderColor(&style.borderTop, parsed[0])
			applyBorderColor(&style.borderRight, parsed[1])
			applyBorderColor(&style.borderBottom, parsed[2])
			applyBorderColor(&style.borderLeft, parsed[3])
		}
	case "border-top-color", "border-right-color", "border-bottom-color", "border-left-color":
		if parsed, ok := parseBorderColor(value); ok {
			applyBorderColor(borderSideForProperty(style, property), parsed)
		}
	case "text-align":
		switch value {
		case "center":
			style.textAlign = alignCenter
		case "right", "end":
			style.textAlign = alignRight
		case "left", "start", "justify":
			style.textAlign = alignLeft
		}
	case "list-style", "list-style-type":
		if parsed, ok := parseListStyleType(value); ok {
			style.listStyleType = parsed
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

func initialBorderSide() borderSide {
	return borderSide{width: px(3)}
}

func parseBorderShorthand(source string, fontSize float64, viewport Viewport) (borderSide, bool) {
	fields := strings.Fields(source)
	if len(fields) == 0 || len(fields) > 3 {
		return borderSide{}, false
	}
	result := initialBorderSide()
	seenWidth := false
	seenStyle := false
	seenColor := false
	for _, field := range fields {
		if width, ok := parseBorderWidth(field, fontSize, viewport); ok && !seenWidth {
			result.width = width
			seenWidth = true
			continue
		}
		if style, ok := parseBorderStyle(field); ok && !seenStyle {
			result.style = style
			seenStyle = true
			continue
		}
		if parsedColor, ok := parseBorderColor(field); ok && !seenColor {
			applyBorderColor(&result, parsedColor)
			seenColor = true
			continue
		}
		return borderSide{}, false
	}
	return result, true
}

func parseBorderWidth(source string, fontSize float64, viewport Viewport) (length, bool) {
	switch source {
	case "thin":
		return px(1), true
	case "medium":
		return px(3), true
	case "thick":
		return px(5), true
	}
	parsed, ok := parseLength(source, fontSize, fontSize, viewport)
	if !ok || parsed.unit == lengthAuto || parsed.unit == lengthPercent || !nonNegativeLength(parsed) {
		return length{}, false
	}
	return parsed, true
}

func parseBorderWidths(source string, fontSize float64, viewport Viewport) ([4]length, bool) {
	parts := strings.Fields(source)
	if len(parts) < 1 || len(parts) > 4 {
		return [4]length{}, false
	}
	parsed := make([]length, len(parts))
	for index, part := range parts {
		value, ok := parseBorderWidth(part, fontSize, viewport)
		if !ok {
			return [4]length{}, false
		}
		parsed[index] = value
	}
	return expandFourSides(parsed), true
}

func parseBorderStyle(source string) (borderStyle, bool) {
	switch source {
	case "none":
		return borderStyleNone, true
	case "solid":
		return borderStyleSolid, true
	case "hidden":
		return borderStyleHidden, true
	default:
		return borderStyleNone, false
	}
}

func parseBorderStyles(source string) ([4]borderStyle, bool) {
	parts := strings.Fields(source)
	if len(parts) < 1 || len(parts) > 4 {
		return [4]borderStyle{}, false
	}
	parsed := make([]borderStyle, len(parts))
	for index, part := range parts {
		value, ok := parseBorderStyle(part)
		if !ok {
			return [4]borderStyle{}, false
		}
		parsed[index] = value
	}
	return expandFourSides(parsed), true
}

type borderColor struct {
	value    color.NRGBA
	explicit bool
}

func parseBorderColor(source string) (borderColor, bool) {
	if source == "currentcolor" {
		return borderColor{}, true
	}
	parsed, ok := parseColor(source)
	return borderColor{value: parsed, explicit: ok}, ok
}

func parseBorderColors(source string) ([4]borderColor, bool) {
	parts := strings.Fields(source)
	if len(parts) < 1 || len(parts) > 4 {
		return [4]borderColor{}, false
	}
	parsed := make([]borderColor, len(parts))
	for index, part := range parts {
		value, ok := parseBorderColor(part)
		if !ok {
			return [4]borderColor{}, false
		}
		parsed[index] = value
	}
	return expandFourSides(parsed), true
}

func expandFourSides[T any](parsed []T) [4]T {
	var result [4]T
	switch len(parsed) {
	case 1:
		result = [4]T{parsed[0], parsed[0], parsed[0], parsed[0]}
	case 2:
		result = [4]T{parsed[0], parsed[1], parsed[0], parsed[1]}
	case 3:
		result = [4]T{parsed[0], parsed[1], parsed[2], parsed[1]}
	case 4:
		copy(result[:], parsed)
	}
	return result
}

func applyBorderColor(side *borderSide, parsed borderColor) {
	side.color = parsed.value
	side.hasColor = parsed.explicit
}

func borderSideForProperty(style *computedStyle, property string) *borderSide {
	switch {
	case strings.Contains(property, "top"):
		return &style.borderTop
	case strings.Contains(property, "right"):
		return &style.borderRight
	case strings.Contains(property, "bottom"):
		return &style.borderBottom
	default:
		return &style.borderLeft
	}
}

func parseListStyleType(source string) (listStyleType, bool) {
	for _, token := range strings.Fields(source) {
		switch token {
		case "disc":
			return listStyleDisc, true
		case "circle":
			return listStyleCircle, true
		case "square":
			return listStyleSquare, true
		case "decimal":
			return listStyleDecimal, true
		case "none":
			return listStyleNone, true
		}
	}
	return listStyleDisc, false
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

func parsePaddingLengths(source string, fontSize float64, viewport Viewport) ([4]length, bool) {
	values, ok := parseBoxLengths(source, fontSize, viewport)
	if !ok {
		return [4]length{}, false
	}
	for _, value := range values {
		if value.unit == lengthAuto || !nonNegativeLength(value) {
			return [4]length{}, false
		}
	}
	return values, true
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
