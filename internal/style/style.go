// Package style computes immutable typed styles for DOM trees. It owns the
// cascade, inheritance, custom-property resolution, and computed-value parsing;
// layout and paint consume Snapshots without participating in the cascade.
package style

import (
	"fmt"
	"image/color"
	"math"
	"sort"
	"strconv"
	"strings"

	"github.com/JediWattson/gossamer/internal/css"
	"github.com/JediWattson/gossamer/internal/dom"
)

// Environment contains the CSS-pixel viewport and media state used while
// computing styles. Browser code owns changes to this environment; a Snapshot
// is valid only for the Environment with which it was computed.
type Environment struct {
	Width           int
	Height          int
	MediaType       string
	InitialFontSize float64
}

// Viewport is retained as a private implementation alias while the extracted
// value parsers are split into focused files.
type Viewport = Environment

// Input contains the non-DOM inputs to one complete style computation.
// Stylesheets are author-origin sheets keyed by the link element that owns
// each external sheet. UserStylesheets and UserAgentStylesheets are ordered
// origin inputs; built-in user-agent declarations precede supplied UA sheets.
type Input struct {
	Environment          Environment
	Stylesheets          map[*dom.Node]css.Stylesheet
	UserStylesheets      []css.Stylesheet
	UserAgentStylesheets []css.Stylesheet
	SelectorState        SelectorState
	selectorContext      css.MatchContext
}

// SelectorState identifies browser-controlled pseudo-class subjects by
// stable DOM identity. ComputeReadView resolves them within its coherent read.
type SelectorState struct {
	Hovered      dom.NodeID
	Active       dom.NodeID
	Focused      dom.NodeID
	FocusVisible bool
	Target       dom.NodeID
	TargetID     string
	// Visited contains same-document stable identities whose resolved hyperlink
	// destinations are visible to the browser's privacy-partitioned history
	// policy. The style package never receives raw browsing-history URLs.
	Visited []dom.NodeID
	// DefaultLanguage is the document's higher-level protocol fallback when no
	// element establishes a content language.
	DefaultLanguage string
}

// PseudoElement is shared with the selector AST so style, layout, and CSSOM
// use one generated-subject identity.
type PseudoElement = css.PseudoElement

const (
	PseudoElementNone   = css.PseudoElementNone
	PseudoElementBefore = css.PseudoElementBefore
	PseudoElementAfter  = css.PseudoElementAfter
)

// DisplayMode is the computed outer display mode supported by the current
// formatting model.
type DisplayMode uint8

const (
	DisplayInline DisplayMode = iota
	DisplayInlineBlock
	DisplayBlock
	DisplayListItem
	DisplayFlex
	DisplayInlineFlex
	DisplayNone
)

type displayMode = DisplayMode

const (
	displayInline      = DisplayInline
	displayInlineBlock = DisplayInlineBlock
	displayBlock       = DisplayBlock
	displayListItem    = DisplayListItem
	displayFlex        = DisplayFlex
	displayInlineFlex  = DisplayInlineFlex
	displayNone        = DisplayNone
)

type FlexDirection uint8

const (
	FlexDirectionRow FlexDirection = iota
	FlexDirectionRowReverse
	FlexDirectionColumn
	FlexDirectionColumnReverse
)

type JustifyContent uint8

const (
	JustifyFlexStart JustifyContent = iota
	JustifyFlexEnd
	JustifyCenter
	JustifySpaceBetween
	JustifySpaceAround
	JustifySpaceEvenly
)

type AlignItems uint8

const (
	AlignStretch AlignItems = iota
	AlignFlexStart
	AlignFlexEnd
	AlignCenterItems
)

type TextAlignment uint8

const (
	AlignLeft TextAlignment = iota
	AlignCenter
	AlignRight
	AlignStart
	AlignEnd
	AlignJustify
)

type textAlignment = TextAlignment

const (
	alignLeft    = AlignLeft
	alignCenter  = AlignCenter
	alignRight   = AlignRight
	alignStart   = AlignStart
	alignEnd     = AlignEnd
	alignJustify = AlignJustify
)

type LineHeight struct {
	value    float64
	absolute bool
	normal   bool
}

type computedLineHeight = LineHeight

type ListStyleType uint8

const (
	ListStyleDisc ListStyleType = iota
	ListStyleCircle
	ListStyleSquare
	ListStyleDecimal
	ListStyleNone
)

type listStyleType = ListStyleType

const (
	listStyleDisc    = ListStyleDisc
	listStyleCircle  = ListStyleCircle
	listStyleSquare  = ListStyleSquare
	listStyleDecimal = ListStyleDecimal
	listStyleNone    = ListStyleNone
)

type BorderStyle uint8

const (
	BorderStyleNone BorderStyle = iota
	BorderStyleSolid
	BorderStyleHidden
)

type borderStyle = BorderStyle

const (
	borderStyleNone   = BorderStyleNone
	borderStyleSolid  = BorderStyleSolid
	borderStyleHidden = BorderStyleHidden
)

type BorderSide struct {
	width    Length
	style    BorderStyle
	color    color.NRGBA
	hasColor bool
}

type borderSide = BorderSide

func (lineHeight LineHeight) Pixels(fontSize float64) float64 {
	if lineHeight.normal {
		return fontSize * 1.2
	}
	if lineHeight.absolute {
		return lineHeight.value
	}
	return fontSize * lineHeight.value
}

func (lineHeight LineHeight) pixels(fontSize float64) float64 { return lineHeight.Pixels(fontSize) }

func (lineHeight LineHeight) Value() float64 { return lineHeight.value }

func (lineHeight LineHeight) IsAbsolute() bool { return lineHeight.absolute }

func (lineHeight LineHeight) IsNormal() bool { return lineHeight.normal }

type LengthUnit uint8

const (
	LengthAuto LengthUnit = iota
	LengthPX
	LengthPercent
	LengthVW
	LengthVH
	LengthVMin
	LengthVMax
	// LengthCalculated is a bounded immutable CSS math expression. Callers
	// resolve it with the percentage base and viewport rather than inspecting a
	// single scalar unit.
	LengthCalculated
)

type lengthUnit = LengthUnit

const (
	lengthAuto    = LengthAuto
	lengthPX      = LengthPX
	lengthPercent = LengthPercent
	lengthVW      = LengthVW
	lengthVH      = LengthVH
	lengthVMin    = LengthVMin
	lengthVMax    = LengthVMax
	lengthCalc    = LengthCalculated
)

type Length struct {
	value       float64
	unit        LengthUnit
	calculation *lengthExpression
}

type length = Length

func (length Length) Value() float64 { return length.value }

func (length Length) Unit() LengthUnit { return length.unit }

func (length Length) IsAuto() bool { return length.unit == LengthAuto }

func (length Length) IsPercent() bool { return length.unit == LengthPercent }

// DependsOnPercent reports whether resolving the value needs a containing
// block percentage base. It is used by layout where percentage heights remain
// auto until a definite containing-block height is available.
func (length Length) DependsOnPercent() bool {
	if length.unit == LengthPercent {
		return true
	}
	return length.calculation != nil && length.calculation.dependsOnPercent()
}

// Resolve evaluates a non-auto length against its used-value inputs. The
// boolean is false for auto and for a non-finite result.
func (length Length) Resolve(percentBase, viewportWidth, viewportHeight float64) (float64, bool) {
	var resolved float64
	switch length.unit {
	case LengthPX:
		resolved = length.value
	case LengthPercent:
		resolved = percentBase * length.value / 100
	case LengthVW:
		resolved = viewportWidth * length.value / 100
	case LengthVH:
		resolved = viewportHeight * length.value / 100
	case LengthVMin:
		resolved = math.Min(viewportWidth, viewportHeight) * length.value / 100
	case LengthVMax:
		resolved = math.Max(viewportWidth, viewportHeight) * length.value / 100
	case LengthCalculated:
		if length.calculation == nil {
			return 0, false
		}
		resolved = length.calculation.resolve(percentBase, viewportWidth, viewportHeight)
	default:
		return 0, false
	}
	return resolved, isFinite(resolved)
}

func (side BorderSide) Width() Length { return side.width }

func (side BorderSide) Style() BorderStyle { return side.style }

func (side BorderSide) Color() (color.NRGBA, bool) { return side.color, side.hasColor }

// FontWeight selects the currently available normal and bold font faces.
type FontWeight uint8

const (
	FontWeightNormal FontWeight = iota
	FontWeightBold
)

type FontStyle uint8

const (
	FontStyleNormal FontStyle = iota
	FontStyleItalic
	FontStyleOblique
)

// FontFamily is the bundled face family selected after walking the computed
// font-family fallback list. The original normalized list is retained
// separately for CSSOM serialization.
type FontFamily uint8

const (
	FontFamilySerif FontFamily = iota
	FontFamilySansSerif
	FontFamilyMonospace
	FontFamilySystemUI
)

type TextDecorationLine uint8

const (
	TextDecorationNone TextDecorationLine = iota
	TextDecorationUnderline
)

// OverflowMode is the computed overflow behavior used by scroll-container
// layout, paint clipping, and CSSOM View. Auto, scroll, and hidden all create
// a programmatically scrollable clipping container in the current slice.
type OverflowMode uint8

const (
	OverflowVisible OverflowMode = iota
	OverflowHidden
	OverflowScroll
	OverflowAuto
	OverflowClip
)

// PositionMode is the computed positioning scheme used by layout. Relative
// boxes remain in normal flow, absolute boxes use their nearest positioned
// ancestor, and fixed boxes use the viewport.
type PositionMode uint8

const (
	PositionStatic PositionMode = iota
	PositionRelative
	PositionAbsolute
	PositionFixed
)

// ZIndex preserves the distinction between the initial auto value and an
// explicit integer, including zero. Layout uses that distinction to decide
// whether a positioned box establishes an isolated stacking context.
type ZIndex struct {
	value int
	auto  bool
}

func (index ZIndex) Value() int   { return index.value }
func (index ZIndex) IsAuto() bool { return index.auto }

// BoxSizing selects whether specified sizes describe the content box or the
// border box. Layout converts the computed value into used content geometry.
type BoxSizing uint8

const (
	BoxSizingContentBox BoxSizing = iota
	BoxSizingBorderBox
)

type Visibility uint8

const (
	VisibilityVisible Visibility = iota
	VisibilityHidden
	VisibilityCollapse
)

type WhiteSpaceMode uint8

const (
	WhiteSpaceNormal WhiteSpaceMode = iota
	WhiteSpaceNoWrap
	WhiteSpacePre
	WhiteSpacePreWrap
	WhiteSpacePreLine
	WhiteSpaceBreakSpaces
)

// ComputedStyle is the typed, layout-independent result of cascade,
// inheritance, and computed-value resolution for one DOM node. Snapshot
// lookups return it by value, so callers cannot mutate stored styles.
type ComputedStyle struct {
	display           DisplayMode
	flexDirection     FlexDirection
	justifyContent    JustifyContent
	alignItems        AlignItems
	rowGap            Length
	columnGap         Length
	content           ContentValue
	flexGrow          float64
	flexShrink        float64
	flexBasis         Length
	order             int
	color             color.NRGBA
	background        color.NRGBA
	hasBackground     bool
	backgroundCurrent bool
	fontSize          float64
	fontFamily        FontFamily
	fontFamilyValue   string
	fontWeightValue   int
	fontStyle         FontStyle
	lineHeight        LineHeight
	textDecoration    TextDecorationLine
	ancestorUnderline bool
	underline         bool
	textAlign         TextAlignment
	listStyleType     ListStyleType
	opacity           float64
	overflowX         OverflowMode
	overflowY         OverflowMode
	position          PositionMode
	top               Length
	right             Length
	bottom            Length
	left              Length
	zIndex            ZIndex
	boxSizing         BoxSizing
	visibility        Visibility
	whiteSpace        WhiteSpaceMode
	width             Length
	height            Length
	minHeight         Length
	maxHeight         Length
	minWidth          Length
	maxWidth          Length
	paddingTop        Length
	paddingRight      Length
	paddingBottom     Length
	paddingLeft       Length
	borderTop         BorderSide
	borderRight       BorderSide
	borderBottom      BorderSide
	borderLeft        BorderSide
	marginTop         Length
	marginRight       Length
	marginBottom      Length
	marginLeft        Length
	customProperties  css.CustomProperties
}

type computedStyle = ComputedStyle

func (computed ComputedStyle) Display() DisplayMode { return computed.display }
func (computed ComputedStyle) FlexDirection() FlexDirection {
	return computed.flexDirection
}
func (computed ComputedStyle) JustifyContent() JustifyContent { return computed.justifyContent }
func (computed ComputedStyle) AlignItems() AlignItems         { return computed.alignItems }
func (computed ComputedStyle) RowGap() Length                 { return computed.rowGap }
func (computed ComputedStyle) ColumnGap() Length              { return computed.columnGap }
func (computed ComputedStyle) Content() ContentValue          { return computed.content }
func (computed ComputedStyle) FlexGrow() float64              { return computed.flexGrow }
func (computed ComputedStyle) FlexShrink() float64            { return computed.flexShrink }
func (computed ComputedStyle) FlexBasis() Length              { return computed.flexBasis }
func (computed ComputedStyle) Order() int                     { return computed.order }
func (computed ComputedStyle) Color() color.NRGBA             { return computed.color }
func (computed ComputedStyle) Background() (color.NRGBA, bool) {
	if computed.backgroundCurrent {
		return computed.color, computed.color.A != 0
	}
	return computed.background, computed.hasBackground
}
func (computed ComputedStyle) FontSize() float64 { return computed.fontSize }
func (computed ComputedStyle) FontFamily() FontFamily {
	return computed.fontFamily
}
func (computed ComputedStyle) FontWeight() FontWeight {
	if computed.fontWeightValue >= 600 {
		return FontWeightBold
	}
	return FontWeightNormal
}
func (computed ComputedStyle) FontWeightValue() int                   { return computed.fontWeightValue }
func (computed ComputedStyle) FontStyle() FontStyle                   { return computed.fontStyle }
func (computed ComputedStyle) LineHeight() LineHeight                 { return computed.lineHeight }
func (computed ComputedStyle) TextDecorationLine() TextDecorationLine { return computed.textDecoration }
func (computed ComputedStyle) Underline() bool                        { return computed.underline }
func (computed ComputedStyle) TextAlignment() TextAlignment           { return computed.textAlign }
func (computed ComputedStyle) ListStyleType() ListStyleType           { return computed.listStyleType }
func (computed ComputedStyle) Opacity() float64                       { return computed.opacity }
func (computed ComputedStyle) OverflowX() OverflowMode                { return computed.overflowX }
func (computed ComputedStyle) OverflowY() OverflowMode                { return computed.overflowY }
func (computed ComputedStyle) Position() PositionMode                 { return computed.position }
func (computed ComputedStyle) Top() Length                            { return computed.top }
func (computed ComputedStyle) Right() Length                          { return computed.right }
func (computed ComputedStyle) Bottom() Length                         { return computed.bottom }
func (computed ComputedStyle) Left() Length                           { return computed.left }
func (computed ComputedStyle) ZIndex() ZIndex                         { return computed.zIndex }
func (computed ComputedStyle) BoxSizing() BoxSizing                   { return computed.boxSizing }
func (computed ComputedStyle) Visibility() Visibility                 { return computed.visibility }
func (computed ComputedStyle) WhiteSpace() WhiteSpaceMode             { return computed.whiteSpace }
func (computed ComputedStyle) Width() Length                          { return computed.width }
func (computed ComputedStyle) Height() Length                         { return computed.height }
func (computed ComputedStyle) MinHeight() Length                      { return computed.minHeight }
func (computed ComputedStyle) MaxHeight() Length                      { return computed.maxHeight }
func (computed ComputedStyle) MinWidth() Length                       { return computed.minWidth }
func (computed ComputedStyle) MaxWidth() Length                       { return computed.maxWidth }
func (computed ComputedStyle) PaddingTop() Length                     { return computed.paddingTop }
func (computed ComputedStyle) PaddingRight() Length                   { return computed.paddingRight }
func (computed ComputedStyle) PaddingBottom() Length                  { return computed.paddingBottom }
func (computed ComputedStyle) PaddingLeft() Length                    { return computed.paddingLeft }
func (computed ComputedStyle) BorderTop() BorderSide                  { return computed.borderTop }
func (computed ComputedStyle) BorderRight() BorderSide                { return computed.borderRight }
func (computed ComputedStyle) BorderBottom() BorderSide               { return computed.borderBottom }
func (computed ComputedStyle) BorderLeft() BorderSide                 { return computed.borderLeft }
func (computed ComputedStyle) MarginTop() Length                      { return computed.marginTop }
func (computed ComputedStyle) MarginRight() Length                    { return computed.marginRight }
func (computed ComputedStyle) MarginBottom() Length                   { return computed.marginBottom }
func (computed ComputedStyle) MarginLeft() Length                     { return computed.marginLeft }
func (computed ComputedStyle) CustomProperties() css.CustomProperties {
	return computed.customProperties
}

type styledNode struct {
	node         *dom.Node
	parent       *styledNode
	style        ComputedStyle
	explanations map[string]PropertyExplanation
	pseudos      map[css.PseudoElement]pseudoStyledNode
	children     []*styledNode
}

type pseudoStyledNode struct {
	style        ComputedStyle
	explanations map[string]PropertyExplanation
}

type pointerPseudoKey struct {
	node   *dom.Node
	pseudo css.PseudoElement
}

type stablePseudoKey struct {
	id     dom.NodeID
	pseudo css.PseudoElement
}

// Snapshot is an immutable set of computed styles for one DOM tree and one
// Environment. It deliberately does not own browser document generations or
// invalidation state.
type Snapshot struct {
	root             *dom.Node
	documentIdentity dom.DocumentIdentity
	rootID           dom.NodeID
	version          uint64
	environment      Environment
	byNode           map[*dom.Node]ComputedStyle
	byID             map[dom.NodeID]ComputedStyle
	byPseudoNode     map[pointerPseudoKey]ComputedStyle
	byPseudoID       map[stablePseudoKey]ComputedStyle
	provenance       provenanceStore
}

// Compute performs a complete style pass over an unindexed DOM tree. Browser
// code should prefer ComputeReadView so snapshots carry stable node identity
// and the coherent document version from which they were built.
func Compute(root *dom.Node, input Input) *Snapshot {
	input.selectorContext.DefaultLanguage = input.SelectorState.DefaultLanguage
	styledRoot := buildStyleTree(root, input)
	snapshot := &Snapshot{
		root:         root,
		environment:  input.Environment,
		byNode:       make(map[*dom.Node]ComputedStyle),
		byPseudoNode: make(map[pointerPseudoKey]ComputedStyle),
	}
	indexPointerStyles(styledRoot, snapshot.byNode)
	indexPointerPseudoStyles(styledRoot, snapshot.byPseudoNode)
	snapshot.provenance = indexPointerProvenance(styledRoot)
	return snapshot
}

// ComputeReadView performs a complete style pass over one coherent Document
// read. One acquired access remains open across raw-tree traversal and stable
// identity indexing. The returned Snapshot does not retain the ReadView,
// ReadAccess, or backing node pointers.
func ComputeReadView(view dom.ReadView, input Input) (*Snapshot, error) {
	access, err := view.Acquire()
	if err != nil {
		return nil, err
	}
	defer access.Close()
	root := access.Root()
	if root == nil || root.Type != dom.DocumentNode {
		return nil, fmt.Errorf("%w: read view root must be a document node", dom.ErrInvalidDocument)
	}
	input.selectorContext = css.MatchContext{
		Hovered:         resolveSelectorStateNode(access, input.SelectorState.Hovered),
		Active:          resolveSelectorStateNode(access, input.SelectorState.Active),
		Focused:         resolveSelectorStateNode(access, input.SelectorState.Focused),
		FocusVisible:    input.SelectorState.FocusVisible,
		Target:          resolveSelectorTarget(root, access, input.SelectorState),
		DefaultLanguage: input.SelectorState.DefaultLanguage,
	}
	visited := resolveSelectorStateNodes(access, input.SelectorState.Visited)
	if len(visited) != 0 {
		input.selectorContext.Visited = func(node *dom.Node) bool {
			_, ok := visited[node]
			return ok
		}
	}
	styledRoot := buildStyleTree(root, input)
	snapshot := &Snapshot{
		documentIdentity: access.Identity(),
		version:          access.Version(),
		environment:      input.Environment,
		byID:             make(map[dom.NodeID]ComputedStyle),
		byPseudoID:       make(map[stablePseudoKey]ComputedStyle),
	}
	indexStableStyles(styledRoot, snapshot.byID, access)
	indexStablePseudoStyles(styledRoot, snapshot.byPseudoID, access)
	snapshot.provenance = indexStableProvenance(styledRoot, access)
	snapshot.rootID, _ = access.ID(root)
	return snapshot, nil
}

func resolveSelectorStateNodes(access *dom.ReadAccess, ids []dom.NodeID) map[*dom.Node]struct{} {
	if len(ids) == 0 {
		return nil
	}
	nodes := make(map[*dom.Node]struct{}, len(ids))
	for _, id := range ids {
		if node, ok := access.Resolve(id); ok && node != nil {
			nodes[node] = struct{}{}
		}
	}
	return nodes
}

func resolveSelectorStateNode(access *dom.ReadAccess, id dom.NodeID) *dom.Node {
	if id == dom.InvalidNodeID {
		return nil
	}
	node, _ := access.Resolve(id)
	return node
}

func resolveSelectorTarget(root *dom.Node, access *dom.ReadAccess, state SelectorState) *dom.Node {
	if target := resolveSelectorStateNode(access, state.Target); target != nil {
		return target
	}
	if state.TargetID == "" {
		return nil
	}
	var found *dom.Node
	var visit func(*dom.Node)
	visit = func(node *dom.Node) {
		if node == nil || found != nil {
			return
		}
		if node.Type == dom.ElementNode {
			for _, attribute := range node.Attributes {
				if attribute.Name == "id" && attribute.Value == state.TargetID {
					found = node
					return
				}
			}
		}
		for _, child := range node.Children {
			visit(child)
		}
	}
	visit(root)
	return found
}

func indexPointerStyles(node *styledNode, destination map[*dom.Node]ComputedStyle) {
	if node == nil {
		return
	}
	if node.node != nil {
		destination[node.node] = node.style
	}
	for _, child := range node.children {
		indexPointerStyles(child, destination)
	}
}

func indexStableStyles(node *styledNode, destination map[dom.NodeID]ComputedStyle, access *dom.ReadAccess) {
	if node == nil {
		return
	}
	if node.node != nil {
		if id, ok := access.ID(node.node); ok {
			destination[id] = node.style
		}
	}
	for _, child := range node.children {
		indexStableStyles(child, destination, access)
	}
}

func indexPointerPseudoStyles(node *styledNode, destination map[pointerPseudoKey]ComputedStyle) {
	if node == nil {
		return
	}
	for pseudo, styled := range node.pseudos {
		destination[pointerPseudoKey{node: node.node, pseudo: pseudo}] = styled.style
	}
	for _, child := range node.children {
		indexPointerPseudoStyles(child, destination)
	}
}

func indexStablePseudoStyles(node *styledNode, destination map[stablePseudoKey]ComputedStyle, access *dom.ReadAccess) {
	if node == nil {
		return
	}
	if id, ok := access.ID(node.node); ok {
		for pseudo, styled := range node.pseudos {
			destination[stablePseudoKey{id: id, pseudo: pseudo}] = styled.style
		}
	}
	for _, child := range node.children {
		indexStablePseudoStyles(child, destination, access)
	}
}

func (snapshot *Snapshot) Root() *dom.Node {
	if snapshot == nil {
		return nil
	}
	return snapshot.root
}

// DocumentIdentity returns the opaque identity captured by ComputeReadView.
// It is the zero token for a pointer-based Snapshot built with Compute.
func (snapshot *Snapshot) DocumentIdentity() dom.DocumentIdentity {
	if snapshot == nil {
		return dom.DocumentIdentity{}
	}
	return snapshot.documentIdentity
}

// RootID returns the stable identity of the document root. It is zero for a
// pointer-based Snapshot built with Compute.
func (snapshot *Snapshot) RootID() dom.NodeID {
	if snapshot == nil {
		return dom.InvalidNodeID
	}
	return snapshot.rootID
}

// Version returns the DOM mutation version captured by ComputeReadView. It is
// zero for a pointer-based Snapshot built with Compute.
func (snapshot *Snapshot) Version() uint64 {
	if snapshot == nil {
		return 0
	}
	return snapshot.version
}

func (snapshot *Snapshot) Environment() Environment {
	if snapshot == nil {
		return Environment{}
	}
	return snapshot.environment
}

func (snapshot *Snapshot) Lookup(node *dom.Node) (ComputedStyle, bool) {
	if snapshot == nil || node == nil {
		return ComputedStyle{}, false
	}
	computed, ok := snapshot.byNode[node]
	return computed, ok
}

// LookupID returns the computed value for one connected stable node identity.
// Detached nodes are intentionally absent until they are reconnected and a
// new Snapshot is computed.
func (snapshot *Snapshot) LookupID(id dom.NodeID) (ComputedStyle, bool) {
	if snapshot == nil || id == dom.InvalidNodeID {
		return ComputedStyle{}, false
	}
	computed, ok := snapshot.byID[id]
	return computed, ok
}

// LookupPseudo returns the computed style for ::before or ::after on an
// element in a pointer snapshot. Pseudo defaults are synthesized from the
// originating element so snapshots retain only matched pseudo overrides.
func (snapshot *Snapshot) LookupPseudo(node *dom.Node, pseudo css.PseudoElement) (ComputedStyle, bool) {
	if snapshot == nil || node == nil || node.Type != dom.ElementNode || pseudo == css.PseudoElementNone {
		return ComputedStyle{}, false
	}
	origin, ok := snapshot.byNode[node]
	if !ok {
		return ComputedStyle{}, false
	}
	if value, found := snapshot.byPseudoNode[pointerPseudoKey{node: node, pseudo: pseudo}]; found {
		return value, true
	}
	return pseudoInitialStyle(origin, snapshot.environment), true
}

// LookupPseudoID returns the computed style for ::before or ::after on a
// stable element identity. Callers must validate that id denotes an element.
func (snapshot *Snapshot) LookupPseudoID(id dom.NodeID, pseudo css.PseudoElement) (ComputedStyle, bool) {
	if snapshot == nil || id == dom.InvalidNodeID || pseudo == css.PseudoElementNone {
		return ComputedStyle{}, false
	}
	origin, ok := snapshot.byID[id]
	if !ok {
		return ComputedStyle{}, false
	}
	if value, found := snapshot.byPseudoID[stablePseudoKey{id: id, pseudo: pseudo}]; found {
		return value, true
	}
	return pseudoInitialStyle(origin, snapshot.environment), true
}

func pseudoInitialStyle(origin ComputedStyle, viewport Viewport) ComputedStyle {
	style := cssInitialStyle(viewport)
	for index := range propertyDefinitions {
		definition := propertyDefinitions[index]
		if definition.inherited {
			definition.copy(&style, origin)
		}
	}
	style.ancestorUnderline = origin.underline
	style.underline = origin.underline
	style.customProperties = origin.customProperties
	style.content = ContentValue{kind: contentNone}
	return style
}

const maxCustomPropertyCascadePasses = 128

func buildStyleTree(document *dom.Node, input Input) *styledNode {
	if document == nil {
		return nil
	}
	authorSheets := collectAuthorStyles(document, input.Stylesheets, input.Environment)
	userSheets := inputStylesheets(input.UserStylesheets, SourceUserStylesheet)
	userAgentSheets := append([]stylesheetSource{{
		stylesheet: builtInUserAgentStylesheet,
		kind:       SourceUserAgentRule,
		order:      -1,
	}}, inputStylesheets(input.UserAgentStylesheets, SourceUserAgentRule)...)
	mediaEnvironment := screenMediaEnvironment(input.Environment)
	context := cascadeStyleContext{
		userAgent:        originStyleContext{sheets: userAgentSheets, layerRanks: originLayerRanks(userAgentSheets, mediaEnvironment)},
		user:             originStyleContext{sheets: userSheets, layerRanks: originLayerRanks(userSheets, mediaEnvironment)},
		author:           originStyleContext{sheets: authorSheets, layerRanks: originLayerRanks(authorSheets, mediaEnvironment)},
		mediaEnvironment: mediaEnvironment,
		selectorContext:  input.selectorContext,
	}
	for _, origin := range []originStyleContext{context.userAgent, context.user, context.author} {
		for _, source := range origin.sheets {
			for _, rule := range source.stylesheet.Rules {
				for _, selector := range rule.Selectors {
					pseudo := selector.PseudoElement()
					if pseudo > css.PseudoElementNone && int(pseudo) < len(context.pseudoElements) {
						context.pseudoElements[pseudo] = true
					}
				}
			}
		}
	}
	return styleNode(document, nil, context, input.Environment)
}

func inputStylesheets(stylesheets []css.Stylesheet, kind SourceKind) []stylesheetSource {
	sources := make([]stylesheetSource, len(stylesheets))
	for index, stylesheet := range stylesheets {
		sources[index] = stylesheetSource{stylesheet: stylesheet, kind: kind, order: index}
	}
	return sources
}

func collectAuthorStyles(root *dom.Node, external map[*dom.Node]css.Stylesheet, viewport Viewport) []stylesheetSource {
	var stylesheets []stylesheetSource
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
				if stylesheet, ok := external[node]; ok {
					stylesheets = append(stylesheets, stylesheetSource{
						stylesheet: stylesheet,
						owner:      node,
						kind:       SourceAuthorStylesheet,
						order:      len(stylesheets),
					})
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
				stylesheets = append(stylesheets, stylesheetSource{
					stylesheet: stylesheet,
					owner:      node,
					kind:       SourceAuthorStylesheet,
					order:      len(stylesheets),
				})
			case "link":
				if stylesheet, ok := external[node]; ok && authorStyleOwnerApplies(node, viewport) {
					stylesheets = append(stylesheets, stylesheetSource{
						stylesheet: stylesheet,
						owner:      node,
						kind:       SourceAuthorStylesheet,
						order:      len(stylesheets),
					})
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

func styleNode(node *dom.Node, parent *styledNode, context cascadeStyleContext, viewport Viewport) *styledNode {
	style, explanations := initialStyle(node, parent, viewport)
	if node != nil && node.Type == dom.ElementNode {
		applyCascade(&style, explanations, node, css.PseudoElementNone, context, viewport, parent)
	}
	styled := &styledNode{node: node, parent: parent, style: style, explanations: explanations}
	if node != nil && node.Type == dom.ElementNode {
		for _, pseudo := range []css.PseudoElement{css.PseudoElementBefore, css.PseudoElementAfter} {
			if !context.pseudoElements[pseudo] {
				continue
			}
			pseudoStyle, pseudoExplanations := initialStyle(node, styled, viewport)
			if !applyCascade(&pseudoStyle, pseudoExplanations, node, pseudo, context, viewport, styled) {
				continue
			}
			if pseudoStyle.content.kind == contentNormal {
				pseudoStyle.content = ContentValue{kind: contentNone}
			}
			if styled.pseudos == nil {
				styled.pseudos = make(map[css.PseudoElement]pseudoStyledNode)
			}
			styled.pseudos[pseudo] = pseudoStyledNode{style: pseudoStyle, explanations: pseudoExplanations}
		}
	}
	for _, child := range node.Children {
		styled.children = append(styled.children, styleNode(child, styled, context, viewport))
	}
	return styled
}

func initialStyle(node *dom.Node, parent *styledNode, viewport Viewport) (computedStyle, map[string]PropertyExplanation) {
	style := cssInitialStyle(viewport)
	explanations := make(map[string]PropertyExplanation, len(propertyDefinitions))
	hasElementParent := parent != nil && parent.node != nil && parent.node.Type == dom.ElementNode
	if hasElementParent {
		for index := range propertyDefinitions {
			definition := propertyDefinitions[index]
			if definition.inherited {
				definition.copy(&style, parent.style)
			}
		}
		style.ancestorUnderline = parent.style.underline
		style.underline = parent.style.underline
		style.customProperties = parent.style.customProperties
	}
	for index := range propertyDefinitions {
		definition := propertyDefinitions[index]
		if definition.inherited && hasElementParent {
			parentExplanation := parent.explanations[definition.name]
			controller := PropertySource{Kind: SourceInherited, DeclarationProperty: definition.name, owner: parent.node}
			explanations[definition.name] = PropertyExplanation{
				Property:    definition.name,
				Resolution:  ResolutionInherited,
				Controller:  controller,
				ValueSource: parentExplanation.ValueSource,
			}
			continue
		}
		source := PropertySource{Kind: SourceInitial, DeclarationProperty: definition.name}
		explanations[definition.name] = PropertyExplanation{
			Property: definition.name, Resolution: ResolutionInitial,
			Controller: source, ValueSource: source,
		}
	}
	if node == nil {
		return style, explanations
	}
	if node.Type == dom.DocumentNode {
		style.display = displayBlock
		source := PropertySource{Origin: CascadeOriginUserAgent, Kind: SourceUserAgentRule, DeclarationProperty: "display", DeclarationValue: "block"}
		explanations["display"] = PropertyExplanation{
			Property: "display", Resolution: ResolutionSpecified,
			Controller: source, ValueSource: source,
		}
	}
	return style, explanations
}

func cssInitialStyle(viewport Viewport) computedStyle {
	return computedStyle{
		display:         displayInline,
		flexDirection:   FlexDirectionRow,
		justifyContent:  JustifyFlexStart,
		alignItems:      AlignStretch,
		rowGap:          px(0),
		columnGap:       px(0),
		content:         ContentValue{kind: contentNormal},
		flexShrink:      1,
		flexBasis:       length{unit: lengthAuto},
		color:           color.NRGBA{A: 0xff},
		fontSize:        environmentInitialFontSize(viewport),
		fontFamily:      FontFamilySerif,
		fontFamilyValue: "serif",
		fontWeightValue: 400,
		lineHeight:      computedLineHeight{value: 1.2, normal: true},
		textAlign:       alignStart,
		opacity:         1,
		position:        PositionStatic,
		top:             length{unit: lengthAuto},
		right:           length{unit: lengthAuto},
		bottom:          length{unit: lengthAuto},
		left:            length{unit: lengthAuto},
		zIndex:          ZIndex{auto: true},
		width:           length{unit: lengthAuto},
		height:          length{unit: lengthAuto},
		minHeight:       px(0),
		maxHeight:       length{unit: lengthAuto},
		minWidth:        px(0),
		maxWidth:        length{unit: lengthAuto},
		paddingTop:      px(0),
		paddingRight:    px(0),
		paddingBottom:   px(0),
		paddingLeft:     px(0),
		borderTop:       initialBorderSide(),
		borderRight:     initialBorderSide(),
		borderBottom:    initialBorderSide(),
		borderLeft:      initialBorderSide(),
		marginTop:       length{unit: lengthPX},
		marginRight:     length{unit: lengthPX},
		marginBottom:    length{unit: lengthPX},
		marginLeft:      length{unit: lengthPX},
	}
}

var builtInUserAgentStylesheet = mustParseBuiltInUserAgentStylesheet(`
html, body, address, article, aside, blockquote, div, dl, dt, dd,
fieldset, figcaption, figure, footer, form, header, hgroup, main, nav,
ol, p, pre, section, table, ul, h1, h2, h3, h4, h5, h6 { display:block }
li { display:list-item }
pre { white-space:pre }
/* Gossamer has no scripting engine, so body fallback content remains visible. */
noscript { display:block }
head, base, link, meta, title, style, script, template { display:none }
body { margin:8px }
h1 { font-size:2em; font-weight:700; margin-top:.67em; margin-bottom:.67em }
h2 { font-size:1.5em; font-weight:700; margin-top:.83em; margin-bottom:.83em }
h3 { font-size:1.17em; font-weight:700; margin-top:1em; margin-bottom:1em }
h4, h5, h6 { font-weight:700; margin-top:1.33em; margin-bottom:1.33em }
p { margin-top:1em; margin-bottom:1em }
ul, ol { margin-top:1em; margin-bottom:1em; padding-left:40px }
ul { list-style-type:disc }
ol { list-style-type:decimal }
blockquote { margin-top:1em; margin-right:40px; margin-bottom:1em; margin-left:40px }
dd { margin-left:40px }
a[href] { color:#0000ee; text-decoration-line:underline }
strong, b { font-weight:700 }
`)

func mustParseBuiltInUserAgentStylesheet(source string) css.Stylesheet {
	stylesheet, err := css.Parse(source)
	if err != nil {
		panic("style: invalid built-in user-agent stylesheet: " + err.Error())
	}
	return stylesheet
}

func applyCascade(style *computedStyle, explanations map[string]PropertyExplanation, node *dom.Node, pseudo css.PseudoElement, context cascadeStyleContext, viewport Viewport, parent *styledNode) bool {
	candidatesByTarget := make(map[string][]winningDeclaration)
	recorded := false
	sourceOrders := make(map[CascadeOrigin]int)
	layerRanks := func(origin CascadeOrigin) map[layerIdentity]int {
		switch origin {
		case CascadeOriginUserAgent:
			return context.userAgent.layerRanks
		case CascadeOriginUser:
			return context.user.layerRanks
		default:
			return context.author.layerRanks
		}
	}
	record := func(candidate winningDeclaration) {
		declaration := candidate.declaration
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
		candidate.layerRank, candidate.layered = layerRanks(candidate.origin)[layerIdentityFor(candidate.stylesheetOrder, candidate.layer)]
		candidate.layered = candidate.layered && !candidate.inline
		for _, target := range targets {
			expanded := candidate
			expanded.target = target
			candidatesByTarget[target] = append(candidatesByTarget[target], expanded)
			recorded = true
		}
	}
	reserveSourceOrder := func(origin CascadeOrigin) int {
		order := sourceOrders[origin]
		sourceOrders[origin]++
		return order
	}
	recordInSourceOrder := func(candidate winningDeclaration) {
		candidate.order = reserveSourceOrder(candidate.origin)
		record(candidate)
	}
	recordSheets := func(origin CascadeOrigin, originContext originStyleContext) {
		for _, source := range originContext.sheets {
			for ruleIndex, rule := range source.stylesheet.Rules {
				var specificity css.Specificity
				var matches bool
				if pseudo == css.PseudoElementNone {
					specificity, matches = rule.MatchWithContext(node, context.selectorContext)
				} else {
					specificity, matches = rule.MatchPseudoWithContext(node, pseudo, context.selectorContext)
				}
				matches = matches && rule.MatchesMedia(context.mediaEnvironment)
				matches = matches && rule.MatchesSupports(SupportsDeclaration)
				for declarationIndex, declaration := range rule.Declarations {
					order := reserveSourceOrder(origin)
					if !matches {
						continue
					}
					var declarationSource css.DeclarationSource
					if declarationIndex < len(rule.DeclarationSources) {
						declarationSource = rule.DeclarationSources[declarationIndex]
					}
					record(winningDeclaration{
						declaration: declaration, origin: origin, kind: source.kind, owner: source.owner,
						declarationSource: declarationSource,
						specificity:       specificity, layer: rule.Layer, stylesheetOrder: source.order,
						ruleOrder: ruleIndex, declarationOrder: declarationIndex, order: order,
					})
				}
			}
		}
	}

	recordSheets(CascadeOriginUserAgent, context.userAgent)
	recordSheets(CascadeOriginUser, context.user)

	if pseudo == css.PseudoElementNone && node.Data == "img" {
		for declarationIndex, name := range []string{"width", "height"} {
			order := reserveSourceOrder(CascadeOriginPresentationalHint)
			value, ok := dimensionAttribute(node, name)
			if !ok {
				continue
			}
			authored, _ := attribute(node, name)
			record(winningDeclaration{
				declaration: css.Declaration{Property: name, Value: strconv.FormatFloat(value, 'f', -1, 64) + "px"},
				origin:      CascadeOriginPresentationalHint, kind: SourcePresentationalHint,
				owner: node, attribute: name, authoredValue: authored,
				stylesheetOrder: -1, ruleOrder: -1, declarationOrder: declarationIndex, order: order,
			})
		}
	}

	recordSheets(CascadeOriginAuthor, context.author)

	if pseudo == css.PseudoElementNone {
		if source, ok := attribute(node, "style"); ok {
			declarations, _ := css.ParseRawDeclarationListWithSources(source)
			for declarationIndex, sourced := range declarations {
				recordInSourceOrder(winningDeclaration{
					declaration: sourced.Declaration, declarationSource: sourced.Source, origin: CascadeOriginAuthor, kind: SourceInlineStyle,
					owner: node, inline: true, stylesheetOrder: -1, ruleOrder: -1,
					declarationOrder: declarationIndex,
				})
			}
		}
	}

	sortCascadeCandidates(candidatesByTarget)

	customPropertyCandidates := make(map[string][]winningDeclaration)
	for target, candidates := range candidatesByTarget {
		if strings.HasPrefix(target, "--") {
			customPropertyCandidates[target] = candidates
			delete(candidatesByTarget, target)
		}
	}
	style.customProperties = resolveCustomPropertyCandidates(style.customProperties, customPropertyCandidates, explanations, parent)

	for index := range propertyDefinitions {
		definition := propertyDefinitions[index]
		if !definition.computeEarly {
			continue
		}
		if candidates, ok := candidatesByTarget[definition.name]; ok {
			applyDeclarationCandidates(style, explanations, parent, candidates, viewport)
			delete(candidatesByTarget, definition.name)
		}
	}
	targets := make([]string, 0, len(candidatesByTarget))
	for target := range candidatesByTarget {
		targets = append(targets, target)
	}
	sort.Strings(targets)
	for _, target := range targets {
		applyDeclarationCandidates(style, explanations, parent, candidatesByTarget[target], viewport)
	}
	return recorded
}

func resolveCustomPropertyCandidates(parentProperties css.CustomProperties, candidatesByName map[string][]winningDeclaration, explanations map[string]PropertyExplanation, parent *styledNode) css.CustomProperties {
	if len(candidatesByName) == 0 {
		return parentProperties
	}
	names := make([]string, 0, len(candidatesByName))
	positions := make(map[string]int, len(candidatesByName))
	overrides := make(map[string]string, len(candidatesByName))
	settled := make(map[string]bool, len(candidatesByName))
	rollbacks := make(map[string][]PropertySource, len(candidatesByName))
	rollbackKinds := make(map[string]ResolutionKind, len(candidatesByName))
	keywordResolutions := make(map[string]ResolutionKind, len(candidatesByName))
	for name := range candidatesByName {
		names = append(names, name)
	}
	sort.Strings(names)

	for _, name := range names {
		positions[name] = normalizeCustomPropertyPosition(candidatesByName[name], 0, rollbacks, rollbackKinds, name)
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

		resolved := css.ResolveCustomProperties(parentProperties, specified)
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
			updateCustomPropertyExplanations(resolved, candidatesByName, positions, rollbacks, rollbackKinds, keywordResolutions, explanations, parent)
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
			resolved = css.ResolveCustomProperties(parentProperties, specified)
			updateCustomPropertyExplanations(resolved, candidatesByName, positions, rollbacks, rollbackKinds, keywordResolutions, explanations, parent)
			return resolved
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
				switch keyword {
				case "initial":
					keywordResolutions[name] = ResolutionInitial
				case "inherit":
					keywordResolutions[name] = ResolutionInherited
				default:
					keywordResolutions[name] = ResolutionUnset
				}
			case "revert":
				rollbacks[name] = append(rollbacks[name], candidates[position].source())
				if _, ok := rollbackKinds[name]; !ok {
					rollbackKinds[name] = ResolutionRevert
				}
				position = nextCandidateAfterRevert(candidates, position)
				delete(overrides, name)
				settled[name] = false
			case "revert-layer":
				rollbacks[name] = append(rollbacks[name], candidates[position].source())
				if _, ok := rollbackKinds[name]; !ok {
					rollbackKinds[name] = ResolutionRevertLayer
				}
				position = nextCandidateAfterRevertLayer(candidates, position)
				delete(overrides, name)
				settled[name] = false
			}
			positions[name] = normalizeCustomPropertyPosition(candidates, position, rollbacks, rollbackKinds, name)
			advanced = true
		}
		if !advanced {
			// ResolveCustomProperties invalidates cyclic var() graphs, so a graph of
			// resolved CSS-wide values always has a dependency leaf. Keep this guard
			// as a deterministic fail-safe if that invariant changes.
			updateCustomPropertyExplanations(resolved, candidatesByName, positions, rollbacks, rollbackKinds, keywordResolutions, explanations, parent)
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

func normalizeCustomPropertyPosition(candidates []winningDeclaration, position int, rollbacks map[string][]PropertySource, kinds map[string]ResolutionKind, name string) int {
	for position < len(candidates) {
		switch css.CSSWideKeyword(candidates[position].declaration.Value) {
		case "revert":
			rollbacks[name] = append(rollbacks[name], candidates[position].source())
			if _, ok := kinds[name]; !ok {
				kinds[name] = ResolutionRevert
			}
			position = nextCandidateAfterRevert(candidates, position)
		case "revert-layer":
			rollbacks[name] = append(rollbacks[name], candidates[position].source())
			if _, ok := kinds[name]; !ok {
				kinds[name] = ResolutionRevertLayer
			}
			position = nextCandidateAfterRevertLayer(candidates, position)
		default:
			return position
		}
	}
	return len(candidates)
}

func updateCustomPropertyExplanations(
	resolved css.CustomProperties,
	candidatesByName map[string][]winningDeclaration,
	positions map[string]int,
	rollbacks map[string][]PropertySource,
	rollbackKinds map[string]ResolutionKind,
	keywordResolutions map[string]ResolutionKind,
	explanations map[string]PropertyExplanation,
	parent *styledNode,
) {
	for name, candidates := range candidatesByName {
		if len(candidates) == 0 {
			continue
		}
		controller := candidates[0].source()
		_, present := resolved.Value(name)
		if !present {
			// Computed custom-property enumeration omits guaranteed-invalid and
			// initial values. Keep retained provenance equally sparse instead of
			// allowing absent declarations to allocate unobservable snapshot state.
			delete(explanations, name)
			continue
		}
		valueSource := PropertySource{Kind: SourceInitial, DeclarationProperty: name}
		resolution := ResolutionSpecified
		position := positions[name]
		var effectiveSource PropertySource
		hasEffectiveSource := false
		if inherited, ok := inheritedCustomPropertyExplanation(parent, name); ok {
			valueSource = inherited.ValueSource
		}
		if position < len(candidates) {
			effective := candidates[position]
			effectiveSource = effective.source()
			hasEffectiveSource = true
			valueSource = effectiveSource
			switch css.CSSWideKeyword(effective.declaration.Value) {
			case "initial":
				resolution = ResolutionInitial
				valueSource = PropertySource{Kind: SourceInitial, DeclarationProperty: name}
			case "inherit":
				resolution = ResolutionInherited
				if inherited, ok := inheritedCustomPropertyExplanation(parent, name); ok {
					valueSource = inherited.ValueSource
				}
			case "unset":
				resolution = ResolutionUnset
				if inherited, ok := inheritedCustomPropertyExplanation(parent, name); ok {
					valueSource = inherited.ValueSource
				}
			default:
				if keywordResolution, ok := keywordResolutions[name]; ok {
					resolution = keywordResolution
					if keywordResolution != ResolutionInitial {
						if inherited, inheritedOK := inheritedCustomPropertyExplanation(parent, name); inheritedOK {
							valueSource = inherited.ValueSource
						}
					} else {
						valueSource = PropertySource{Kind: SourceInitial, DeclarationProperty: name}
					}
				}
			}
		} else {
			resolution = ResolutionInherited
		}
		if rollbackResolution, ok := rollbackKinds[name]; ok {
			resolution = rollbackResolution
		}
		rollback := append([]PropertySource(nil), rollbacks[name]...)
		if len(rollback) > 0 && rollback[0] == controller {
			rollback = rollback[1:]
		}
		if _, rolledBack := rollbackKinds[name]; rolledBack && hasEffectiveSource && effectiveSource != controller && effectiveSource != valueSource {
			rollback = append(rollback, effectiveSource)
		}
		explanations[name] = PropertyExplanation{
			Property: name, Resolution: resolution,
			Controller: controller, ValueSource: valueSource, Rollback: rollback,
		}
	}
}

func inheritedCustomPropertyExplanation(parent *styledNode, name string) (PropertyExplanation, bool) {
	if parent == nil {
		return PropertyExplanation{}, false
	}
	if _, ok := parent.style.customProperties.Value(name); !ok {
		return PropertyExplanation{}, false
	}
	for current := parent; current != nil; current = current.parent {
		if explanation, ok := current.explanations[name]; ok {
			return explanation, true
		}
	}
	return PropertyExplanation{}, false
}

func dependsOnCSSWideValue(references []string, cssWideValues map[string]string) bool {
	for _, reference := range references {
		if _, ok := cssWideValues[reference]; ok {
			return true
		}
	}
	return false
}

func cssWideKeyword(source string) string {
	return css.CSSWideKeyword(source)
}

func applyDeclarationCandidates(style *computedStyle, explanations map[string]PropertyExplanation, parent *styledNode, candidates []winningDeclaration, viewport Viewport) {
	if len(candidates) == 0 {
		return
	}
	explanations[candidates[0].target] = applyDeclarationCandidate(style, parent, candidates, 0, viewport)
}

func applyDeclarationCandidate(style *computedStyle, parent *styledNode, candidates []winningDeclaration, position int, viewport Viewport) PropertyExplanation {
	candidate := candidates[position]
	source := candidate.source()
	resolved, ok := style.customProperties.Substitute(candidate.declaration.Value)
	if !ok {
		// A winning declaration whose var() cannot be substituted is invalid at
		// computed-value time. It computes as unset without reviving a loser.
		return applyCSSWideKeywordExplanation(style, parent, candidate.target, "unset", viewport, source, ResolutionInvalidAtComputedValue)
	}

	switch keyword := cssWideKeyword(resolved); keyword {
	case "revert":
		return applyRollbackCandidate(style, parent, candidates, position, viewport, source, ResolutionRevert, nextCandidateAfterRevert(candidates, position))
	case "revert-layer":
		return applyRollbackCandidate(style, parent, candidates, position, viewport, source, ResolutionRevertLayer, nextCandidateAfterRevertLayer(candidates, position))
	case "inherit":
		return applyCSSWideKeywordExplanation(style, parent, candidate.target, keyword, viewport, source, ResolutionInherited)
	case "initial":
		return applyCSSWideKeywordExplanation(style, parent, candidate.target, keyword, viewport, source, ResolutionInitial)
	case "unset":
		return applyCSSWideKeywordExplanation(style, parent, candidate.target, keyword, viewport, source, ResolutionUnset)
	}

	declaration := candidate.declaration
	declaration.Value = resolved
	if !validCascadedDeclaration(declaration, viewport) {
		// A declaration containing var() participates in the cascade before its
		// computed value is known. If substitution produces an invalid value, it
		// computes as unset; a lower-priority declaration is not resurrected.
		return applyCSSWideKeywordExplanation(style, parent, candidate.target, "unset", viewport, source, ResolutionInvalidAtComputedValue)
	}
	applyTargetDeclaration(style, parent, candidate.target, declaration, viewport)
	return PropertyExplanation{
		Property: candidate.target, Resolution: ResolutionSpecified,
		Controller: source, ValueSource: source,
	}
}

func applyRollbackCandidate(
	style *computedStyle,
	parent *styledNode,
	candidates []winningDeclaration,
	position int,
	viewport Viewport,
	controller PropertySource,
	resolution ResolutionKind,
	next int,
) PropertyExplanation {
	var value PropertyExplanation
	if next < len(candidates) {
		value = applyDeclarationCandidate(style, parent, candidates, next, viewport)
	} else {
		value = applyCSSWideKeywordExplanation(style, parent, candidates[position].target, "unset", viewport, controller, ResolutionUnset)
	}
	rollback := make([]PropertySource, 0, 1+len(value.Rollback))
	if value.Controller != controller && value.Controller != value.ValueSource {
		rollback = append(rollback, value.Controller)
	}
	rollback = append(rollback, value.Rollback...)
	return PropertyExplanation{
		Property: candidates[position].target, Resolution: resolution,
		Controller: controller, ValueSource: value.ValueSource, Rollback: rollback,
	}
}

func applyTargetDeclaration(style *computedStyle, parent *styledNode, target string, declaration css.Declaration, viewport Viewport) {
	context := propertyApplyContext{
		parentFontSize:   parentFontSize(parent, viewport),
		parentFontWeight: parentFontWeight(parent),
		parentColor:      parentColor(parent, viewport),
		viewport:         viewport,
	}
	if declaration.Property == "font" {
		size, lineHeight, weight, fontStyle, family, ok := parseFontShorthand(declaration.Value, viewport)
		if !ok {
			return
		}
		switch target {
		case "font-family":
			declaration = css.Declaration{Property: target, Value: family, Important: declaration.Important}
		case "font-size":
			declaration = css.Declaration{Property: target, Value: size, Important: declaration.Important}
		case "font-weight":
			declaration = css.Declaration{Property: target, Value: weight, Important: declaration.Important}
		case "font-style":
			declaration = css.Declaration{Property: target, Value: fontStyle, Important: declaration.Important}
		case "line-height":
			declaration = css.Declaration{Property: target, Value: lineHeight, Important: declaration.Important}
		}
	}
	if declaration.Property == target {
		applyDeclaration(style, target, declaration.Value, context)
		return
	}
	temporary := *style
	applyDeclaration(&temporary, declaration.Property, declaration.Value, context)
	copyComputedProperty(style, temporary, target)
}

func applyCSSWideKeyword(style *computedStyle, parent *styledNode, target, keyword string, viewport Viewport) {
	definition, ok := lookupPropertyDefinition(target)
	if !ok {
		return
	}
	switch keyword {
	case "inherit":
		if parent != nil && parent.node != nil && parent.node.Type == dom.ElementNode {
			definition.copy(style, parent.style)
			return
		}
	case "unset":
		if definition.inherited && parent != nil && parent.node != nil && parent.node.Type == dom.ElementNode {
			definition.copy(style, parent.style)
			return
		}
	}
	definition.resetToInitial(style, viewport)
}

func applyCSSWideKeywordExplanation(
	style *computedStyle,
	parent *styledNode,
	target string,
	keyword string,
	viewport Viewport,
	controller PropertySource,
	resolution ResolutionKind,
) PropertyExplanation {
	applyCSSWideKeyword(style, parent, target, keyword, viewport)
	valueSource := PropertySource{Kind: SourceInitial, DeclarationProperty: target}
	definition, hasDefinition := lookupPropertyDefinition(target)
	usesParent := keyword == "inherit" || (keyword == "unset" && hasDefinition && definition.inherited)
	if usesParent && parent != nil && parent.node != nil && parent.node.Type == dom.ElementNode {
		if inherited, ok := parent.explanations[target]; ok {
			valueSource = inherited.ValueSource
		}
	}
	return PropertyExplanation{
		Property: target, Resolution: resolution,
		Controller: controller, ValueSource: valueSource,
	}
}

func screenMediaEnvironment(viewport Viewport) css.MediaEnvironment {
	mediaType := viewport.MediaType
	if mediaType == "" {
		mediaType = "screen"
	}
	return css.MediaEnvironment{
		Type:            mediaType,
		Width:           float64(viewport.Width),
		Height:          float64(viewport.Height),
		InitialFontSize: environmentInitialFontSize(viewport),
	}
}

func environmentInitialFontSize(environment Environment) float64 {
	if environment.InitialFontSize > 0 && isFinite(environment.InitialFontSize) {
		return environment.InitialFontSize
	}
	return 16
}

// parseFontShorthand retains the pieces represented by the current computed
// style. Family and unsupported variant/stretch tokens are recognized far
// enough to identify the size boundary and remain available for later growth.
func parseFontShorthand(source string, viewport Viewport) (size, lineHeight, weight, fontStyle, family string, ok bool) {
	value, valid := parsePropertyValue(source)
	if !valid || len(value.terms) < 2 {
		return "", "", "", "", "", false
	}
	weight = "normal"
	fontStyle = "normal"
	lineHeight = "normal"
	for index := 0; index < len(value.terms); index++ {
		term := value.terms[index]
		parsedSize, validSize := parseLengthComponent(term, value.source, 1, viewport)
		if !validSize || parsedSize.unit == lengthAuto || !nonNegativeLength(parsedSize) {
			if !parseFontPrefixComponent(term, &weight, &fontStyle) {
				return "", "", "", "", "", false
			}
			continue
		}

		size = value.raw(term)
		familyStart := index + 1
		if familyStart < len(value.terms) && isValueDelimiter(value.terms[familyStart], "/") {
			familyStart++
			if familyStart >= len(value.terms) {
				return "", "", "", "", "", false
			}
			lineHeight = value.raw(value.terms[familyStart])
			familyStart++
		}
		if !validFontLineHeight(lineHeight, viewport) || familyStart >= len(value.terms) {
			return "", "", "", "", "", false
		}
		familyParts := make([]string, 0, len(value.terms)-familyStart)
		for _, familyTerm := range value.terms[familyStart:] {
			familyParts = append(familyParts, value.raw(familyTerm))
		}
		family = strings.Join(familyParts, " ")
		if _, _, validFamily := parseFontFamily(family); !validFamily {
			return "", "", "", "", "", false
		}
		return size, lineHeight, weight, fontStyle, family, true
	}
	return "", "", "", "", "", false
}

func parseFontFamily(source string) (serialized string, selected FontFamily, ok bool) {
	values, err := css.ParseComponentValues(source)
	if err != nil {
		return "", 0, false
	}
	groups := make([][]css.ComponentValue, 1)
	for _, value := range values {
		if token, tokenOK := componentToken(value); tokenOK && token.Kind == css.TokenComma {
			groups = append(groups, nil)
			continue
		}
		groups[len(groups)-1] = append(groups[len(groups)-1], value)
	}
	serializedGroups := make([]string, 0, len(groups))
	selected = FontFamilySerif
	selectedAvailable := false
	for _, group := range groups {
		group = trimValueWhitespace(group)
		if len(group) == 0 {
			return "", 0, false
		}
		name, generic, groupOK := parseFontFamilyGroup(group)
		if !groupOK {
			return "", 0, false
		}
		if generic {
			serializedGroups = append(serializedGroups, lowerASCIIValue(name))
		} else {
			serializedGroups = append(serializedGroups, quoteCSSString(name))
		}
		if !selectedAvailable {
			if family, available := availableFontFamily(name, generic); available {
				selected, selectedAvailable = family, true
			}
		}
	}
	return strings.Join(serializedGroups, ", "), selected, true
}

func parseFontFamilyGroup(values []css.ComponentValue) (name string, generic bool, ok bool) {
	if len(values) == 1 {
		token, tokenOK := componentToken(values[0])
		if !tokenOK || token.Incomplete {
			return "", false, false
		}
		switch token.Kind {
		case css.TokenString:
			return token.Value, false, token.Value != ""
		case css.TokenIdent:
			name = token.Value
			return name, isGenericFontFamily(lowerASCIIValue(name)), true
		default:
			return "", false, false
		}
	}

	parts := make([]string, 0, len(values))
	for _, value := range values {
		if valueWhitespace(value) {
			continue
		}
		token, tokenOK := componentToken(value)
		if !tokenOK || token.Kind != css.TokenIdent || token.Incomplete {
			return "", false, false
		}
		if isGenericFontFamily(lowerASCIIValue(token.Value)) {
			return "", false, false
		}
		parts = append(parts, token.Value)
	}
	if len(parts) == 0 {
		return "", false, false
	}
	return strings.Join(parts, " "), false, true
}

func isGenericFontFamily(name string) bool {
	switch name {
	case "serif", "sans-serif", "monospace", "cursive", "fantasy", "system-ui", "ui-serif", "ui-sans-serif", "ui-monospace", "ui-rounded", "math", "fangsong":
		return true
	default:
		return false
	}
}

func availableFontFamily(name string, generic bool) (FontFamily, bool) {
	lower := lowerASCIIValue(name)
	if !generic {
		switch lower {
		case "go mono":
			return FontFamilyMonospace, true
		case "go", "go sans":
			return FontFamilySansSerif, true
		default:
			return 0, false
		}
	}
	switch lower {
	case "monospace", "ui-monospace":
		return FontFamilyMonospace, true
	case "sans-serif", "ui-sans-serif":
		return FontFamilySansSerif, true
	case "system-ui":
		return FontFamilySystemUI, true
	case "serif", "ui-serif":
		return FontFamilySerif, true
	default:
		return 0, false
	}
}

func quoteCSSString(value string) string {
	var result strings.Builder
	result.Grow(len(value) + 2)
	result.WriteByte('"')
	for _, current := range value {
		switch current {
		case '\\', '"':
			result.WriteByte('\\')
			result.WriteRune(current)
		case '\n':
			result.WriteString("\\a ")
		case '\r':
			result.WriteString("\\d ")
		case '\f':
			result.WriteString("\\c ")
		default:
			result.WriteRune(current)
		}
	}
	result.WriteByte('"')
	return result.String()
}

func parseFontPrefixComponent(component css.ComponentValue, weight, fontStyle *string) bool {
	if keyword, ok := componentKeyword(component); ok {
		switch keyword {
		case "normal":
			*weight = "normal"
			return true
		case "bold", "bolder", "lighter":
			*weight = keyword
			return true
		case "italic", "oblique":
			*fontStyle = keyword
			return true
		case "small-caps", "condensed", "expanded":
			return true
		}
	}
	token, ok := componentToken(component)
	if ok && token.Kind == css.TokenNumber && token.Integer && token.Number >= 1 && token.Number <= 1000 {
		*weight = token.Representation
		return true
	}
	return false
}

func isValueDelimiter(component css.ComponentValue, delimiter string) bool {
	token, ok := componentToken(component)
	return ok && token.Kind == css.TokenDelim && token.Value == delimiter
}

func validFontLineHeight(source string, viewport Viewport) bool {
	if keyword, ok := singleCSSKeyword(source); ok && keyword == "normal" {
		return true
	}
	if token, ok := singleCSSNumber(source); ok {
		return token.Number > 0
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

func relativeFontWeight(inherited int, bolder bool) int {
	switch {
	case inherited < 100:
		if bolder {
			return 400
		}
		return inherited
	case inherited < 350:
		if bolder {
			return 400
		}
		return 100
	case inherited < 550:
		if bolder {
			return 700
		}
		return 100
	case inherited < 750:
		if bolder {
			return 900
		}
		return 400
	case inherited < 900:
		if bolder {
			return 900
		}
		return 700
	default:
		if bolder {
			return inherited
		}
		return 700
	}
}

func initialBorderSide() borderSide {
	return borderSide{width: px(3)}
}

func parseBorderShorthand(source string, fontSize float64, viewport Viewport) (borderSide, bool) {
	value, ok := parsePropertyValue(source)
	if !ok || len(value.terms) == 0 || len(value.terms) > 3 {
		return borderSide{}, false
	}
	result := initialBorderSide()
	seenWidth := false
	seenStyle := false
	seenColor := false
	for _, term := range value.terms {
		if width, ok := parseBorderWidthComponent(term, value.source, fontSize, viewport); ok && !seenWidth {
			result.width = width
			seenWidth = true
			continue
		}
		if style, ok := parseBorderStyleComponent(term); ok && !seenStyle {
			result.style = style
			seenStyle = true
			continue
		}
		if parsedColor, ok := parseBorderColorComponent(term); ok && !seenColor {
			applyBorderColor(&result, parsedColor)
			seenColor = true
			continue
		}
		return borderSide{}, false
	}
	return result, true
}

func parseBorderWidth(source string, fontSize float64, viewport Viewport) (length, bool) {
	value, ok := parsePropertyValue(source)
	if !ok {
		return length{}, false
	}
	component, ok := value.single()
	if !ok {
		return length{}, false
	}
	return parseBorderWidthComponent(component, value.source, fontSize, viewport)
}

func parseBorderWidthComponent(component css.ComponentValue, source string, fontSize float64, viewport Viewport) (length, bool) {
	keyword, _ := componentKeyword(component)
	switch keyword {
	case "thin":
		return px(1), true
	case "medium":
		return px(3), true
	case "thick":
		return px(5), true
	}
	parsed, ok := parseLengthComponent(component, source, fontSize, viewport)
	if !ok || parsed.unit == lengthAuto || parsed.DependsOnPercent() || !nonNegativeLength(parsed) {
		return length{}, false
	}
	return parsed, true
}

func parseBorderWidths(source string, fontSize float64, viewport Viewport) ([4]length, bool) {
	value, ok := parsePropertyValue(source)
	if !ok || len(value.terms) < 1 || len(value.terms) > 4 {
		return [4]length{}, false
	}
	parsed := make([]length, len(value.terms))
	for index, term := range value.terms {
		parsedValue, ok := parseBorderWidthComponent(term, value.source, fontSize, viewport)
		if !ok {
			return [4]length{}, false
		}
		parsed[index] = parsedValue
	}
	return expandFourSides(parsed), true
}

func parseBorderStyle(source string) (borderStyle, bool) {
	value, ok := parsePropertyValue(source)
	if !ok {
		return borderStyleNone, false
	}
	component, ok := value.single()
	if !ok {
		return borderStyleNone, false
	}
	return parseBorderStyleComponent(component)
}

func parseBorderStyleComponent(component css.ComponentValue) (borderStyle, bool) {
	keyword, _ := componentKeyword(component)
	switch keyword {
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
	value, ok := parsePropertyValue(source)
	if !ok || len(value.terms) < 1 || len(value.terms) > 4 {
		return [4]borderStyle{}, false
	}
	parsed := make([]borderStyle, len(value.terms))
	for index, term := range value.terms {
		parsedValue, ok := parseBorderStyleComponent(term)
		if !ok {
			return [4]borderStyle{}, false
		}
		parsed[index] = parsedValue
	}
	return expandFourSides(parsed), true
}

type borderColor struct {
	value    color.NRGBA
	explicit bool
}

func parseBorderColor(source string) (borderColor, bool) {
	value, ok := parsePropertyValue(source)
	if !ok {
		return borderColor{}, false
	}
	component, ok := value.single()
	if !ok {
		return borderColor{}, false
	}
	return parseBorderColorComponent(component)
}

func parseBorderColorComponent(component css.ComponentValue) (borderColor, bool) {
	if keyword, ok := componentKeyword(component); ok && keyword == "currentcolor" {
		return borderColor{}, true
	}
	parsed, ok := parseColorComponent(component)
	return borderColor{value: parsed, explicit: ok}, ok
}

func parseBorderColors(source string) ([4]borderColor, bool) {
	value, ok := parsePropertyValue(source)
	if !ok || len(value.terms) < 1 || len(value.terms) > 4 {
		return [4]borderColor{}, false
	}
	parsed := make([]borderColor, len(value.terms))
	for index, term := range value.terms {
		parsedValue, ok := parseBorderColorComponent(term)
		if !ok {
			return [4]borderColor{}, false
		}
		parsed[index] = parsedValue
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

func parseListStyleType(source string) (listStyleType, bool) {
	value, ok := parsePropertyValue(source)
	if !ok {
		return listStyleDisc, false
	}
	for _, term := range value.terms {
		keyword, _ := componentKeyword(term)
		switch keyword {
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
	value, ok := parsePropertyValue(source)
	if !ok || len(value.terms) < 1 || len(value.terms) > 4 {
		return [4]length{}, false
	}
	parsed := make([]length, len(value.terms))
	for index, term := range value.terms {
		parsedValue, ok := parseLengthComponent(term, value.source, fontSize, viewport)
		if !ok {
			return [4]length{}, false
		}
		parsed[index] = parsedValue
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

func parseLength(source string, emBase, _ float64, viewport Viewport) (length, bool) {
	value, ok := parsePropertyValue(source)
	if !ok {
		return length{}, false
	}
	component, ok := value.single()
	if !ok {
		return length{}, false
	}
	return parseLengthComponent(component, value.source, emBase, viewport)
}

func resolveLength(value length, percentBase float64, viewport Viewport, autoValue float64) float64 {
	if resolved, ok := value.Resolve(percentBase, float64(viewport.Width), float64(viewport.Height)); ok {
		return resolved
	}
	return autoValue
}

func parseColor(source string) (color.NRGBA, bool) {
	value, ok := parsePropertyValue(source)
	if !ok {
		return color.NRGBA{}, false
	}
	component, ok := value.single()
	if !ok {
		return color.NRGBA{}, false
	}
	return parseColorComponent(component)
}

type computedColorValue struct {
	value        color.NRGBA
	currentColor bool
}

func parseComputedColor(source string) (computedColorValue, bool) {
	value, ok := parsePropertyValue(source)
	if !ok {
		return computedColorValue{}, false
	}
	component, ok := value.single()
	if !ok {
		return computedColorValue{}, false
	}
	return parseComputedColorComponent(component)
}

func parseComputedColorComponent(component css.ComponentValue) (computedColorValue, bool) {
	if keyword, ok := componentKeyword(component); ok && keyword == "currentcolor" {
		return computedColorValue{currentColor: true}, true
	}
	parsed, ok := parseColorComponent(component)
	return computedColorValue{value: parsed}, ok
}

func parseFirstComputedColor(source string) (computedColorValue, bool) {
	value, ok := parsePropertyValue(source)
	if !ok || len(value.terms) == 0 {
		return computedColorValue{}, false
	}
	return parseComputedColorComponent(value.terms[0])
}

func parentFontSize(parent *styledNode, viewport Viewport) float64 {
	if parent == nil {
		return environmentInitialFontSize(viewport)
	}
	return parent.style.fontSize
}

func parentFontWeight(parent *styledNode) int {
	if parent == nil {
		return 400
	}
	return parent.style.fontWeightValue
}

func parentColor(parent *styledNode, viewport Viewport) color.NRGBA {
	if parent == nil {
		return cssInitialStyle(viewport).color
	}
	return parent.style.color
}

func px(value float64) length {
	return length{value: value, unit: lengthPX}
}

func nonNegativeLength(value length) bool {
	return value.unit == lengthAuto || value.unit == lengthCalc || value.value >= 0
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
