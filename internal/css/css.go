// Package css parses and matches the CSS subset used by Gossamer's rendering
// pipeline. It supports complex selectors, attribute selectors, logical and
// structural and linguistic pseudo-classes, common CSS nesting forms, nested
// cascade layers, viewport media queries, escaped selector identifiers, and
// tokenized declarations consumed by the renderer. Selector namespaces,
// pseudo-elements beyond ::before/::after, and the remaining at-rules are
// outside the current subset.
package css

import (
	"errors"

	"github.com/JediWattson/gossamer/internal/dom"
)

// ErrInvalidSelector reports an unforgiving selector-list parse failure. DOM
// query APIs surface this boundary to JavaScript instead of silently dropping
// a malformed selector as stylesheet parsing does.
var ErrInvalidSelector = errors.New("css: invalid selector")

// Stylesheet is an ordered collection of qualified CSS rules. LayerOrder is a
// flattened low-to-high ordering of nested layer identities; a parent layer's
// implicit declarations follow its explicit child layers.
type Stylesheet struct {
	Rules             []Rule
	LayerOrder        []string
	LayerDeclarations []LayerDeclaration
	Imports           []ImportRule
	selectorIndex     *stylesheetSelectorIndex
}

// LayerDeclaration is one appearance of a layer identity in source order.
// Media and Supports capture enclosing document-global conditions so the
// style engine can establish the environment-dependent layer order.
type LayerDeclaration struct {
	Name     string
	Media    []string
	Supports []string
	Order    int
}

// ImportRule is one valid top-level @import that precedes qualified rules.
// URL is the decoded string or url-token value. Layered distinguishes an
// anonymous layer from no layer; Layer names the supported single named layer.
// Supports and Media retain their authored component-value source for the
// browser-owned stylesheet graph to evaluate in the importing context.
type ImportRule struct {
	URL             string
	Layer           string
	Layered         bool
	Supports        string
	Media           string
	Order           int
	AppearanceOrder int
}

// Rule associates one or more selectors with an ordered declaration block.
// Order is zero-based and increases for each successfully parsed rule. Layer is
// empty for unlayered rules. Media contains the outer-to-inner @media query
// lists that must all match for the rule to apply.
type Rule struct {
	Selectors          []Selector
	Declarations       []Declaration
	DeclarationSources []DeclarationSource
	Order              int
	Layer              string
	Media              []string
	Supports           []string
}

// DeclarationSource identifies the original source ranges for one parsed
// declaration. Span covers the declaration without its terminating semicolon,
// NameSpan covers the authored property token, and ValueSpan covers the value
// before an optional !important annotation. Stylesheet rule spans are absolute
// within the stylesheet source; declaration-list spans are relative to the
// declaration-list source.
type DeclarationSource struct {
	Span      Span
	NameSpan  Span
	ValueSpan Span
}

// SourcedDeclaration pairs the existing cascade value with its source ranges.
// Keeping source metadata parallel avoids making Declaration identity depend
// on diagnostics and preserves its small value semantics for CSSOM callers.
type SourcedDeclaration struct {
	Declaration Declaration
	Source      DeclarationSource
}

// Selector is a parsed complex selector. Its representation is deliberately
// private so matching and specificity cannot drift apart. Selectors are
// created by Parse.
type Selector struct {
	specificity Specificity
	compounds   []compoundSelector
	combinators []selectorCombinator
	leading     selectorCombinator
	pseudo      PseudoElement
}

// SelectorCandidateKind identifies the required rightmost simple selector
// used by the style engine's conservative rule index. A candidate key can
// produce false positives, which the full selector matcher rejects, but never
// false negatives.
type SelectorCandidateKind uint8

const (
	SelectorCandidateUniversal SelectorCandidateKind = iota
	SelectorCandidateType
	SelectorCandidateClass
	SelectorCandidateID
)

// SelectorCandidateKey is one conservative lookup key for a selector's
// subject compound. Value is decoded selector text; type values are normalized
// with CSS's ASCII case folding while class and ID values remain case-sensitive.
type SelectorCandidateKey struct {
	Kind  SelectorCandidateKind
	Value string
}

// PseudoElement identifies the generated subject selected after the final
// compound of a complex selector. Zero selects the originating element.
type PseudoElement uint8

const (
	PseudoElementNone PseudoElement = iota
	PseudoElementBefore
	PseudoElementAfter
)

// String returns the canonical pseudo-element selector spelling.
func (pseudo PseudoElement) String() string {
	switch pseudo {
	case PseudoElementBefore:
		return "::before"
	case PseudoElementAfter:
		return "::after"
	default:
		return ""
	}
}

// ParsePseudoElement recognizes the generated pseudo-elements implemented by
// the engine, including their legacy single-colon spellings.
func ParsePseudoElement(source string) (PseudoElement, bool) {
	switch lowerASCII(source) {
	case "::before", ":before":
		return PseudoElementBefore, true
	case "::after", ":after":
		return PseudoElementAfter, true
	default:
		return PseudoElementNone, false
	}
}

// ParseSelectorList parses the unforgiving selector-list grammar used by DOM
// query APIs. Unlike stylesheet parsing, one invalid member rejects the whole
// list.
func ParseSelectorList(source string) ([]Selector, error) {
	selectors, ok := parseSelectorList(source)
	if !ok {
		return nil, ErrInvalidSelector
	}
	return selectors, nil
}

// MatchesAny reports whether at least one selector matches node.
func MatchesAny(selectors []Selector, node *dom.Node) bool {
	return MatchesAnyWithContext(selectors, node, MatchContext{})
}

// MatchesAnyWithContext reports whether a selector matches under context.
func MatchesAnyWithContext(selectors []Selector, node *dom.Node, context MatchContext) bool {
	state := newSelectorMatchState(context)
	for _, selector := range selectors {
		if selector.matchesWithState(node, context, state) {
			return true
		}
	}
	return false
}

// Specificity returns the selector's static CSS specificity.
func (selector Selector) Specificity() Specificity {
	return selector.specificity
}

// PseudoElement returns the selector's generated subject, or zero when it
// selects an ordinary element.
func (selector Selector) PseudoElement() PseudoElement {
	return selector.pseudo
}

// CandidateKey returns the most selective directly required key on the
// selector's rightmost compound. Logical pseudo-class arguments are not
// promoted because their branches may require different keys.
func (selector Selector) CandidateKey() SelectorCandidateKey {
	if len(selector.compounds) == 0 {
		return SelectorCandidateKey{Kind: SelectorCandidateUniversal}
	}
	compound := selector.compounds[len(selector.compounds)-1]
	if len(compound.ids) != 0 {
		return SelectorCandidateKey{Kind: SelectorCandidateID, Value: compound.ids[0]}
	}
	if len(compound.classes) != 0 {
		return SelectorCandidateKey{Kind: SelectorCandidateClass, Value: compound.classes[0]}
	}
	if compound.typeName != "" && compound.typeName != "*" {
		return SelectorCandidateKey{Kind: SelectorCandidateType, Value: lowerASCII(compound.typeName)}
	}
	return SelectorCandidateKey{Kind: SelectorCandidateUniversal}
}

type compoundSelector struct {
	typeName   string
	ids        []string
	classes    []string
	attributes []attributeSelector
	pseudos    []pseudoClassSelector
}

type selectorCombinator uint8

const (
	descendantCombinator selectorCombinator = iota + 1
	childCombinator
	adjacentSiblingCombinator
	generalSiblingCombinator
)

type attributeSelector struct {
	name      string
	operator  attributeOperator
	value     string
	valueCase attributeCase
}

type attributeOperator uint8

const (
	attributeExists attributeOperator = iota
	attributeEquals
	attributeIncludes
	attributeDashMatch
	attributePrefix
	attributeSuffix
	attributeSubstring
)

type attributeCase uint8

const (
	attributeCaseDefault attributeCase = iota
	attributeCaseInsensitive
	attributeCaseSensitive
)

type pseudoClassSelector struct {
	name      string
	selectors []Selector
	arguments []string
	nth       nthExpression
}

type nthExpression struct {
	a int
	b int
}

// Specificity contains the three components of CSS selector specificity. IDs
// are compared first, then Classes, then Types.
type Specificity struct {
	IDs     int
	Classes int
	Types   int
}

// Compare returns -1 when specificity is lower than other, 1 when it is
// higher, and 0 when the two values are equal.
func (specificity Specificity) Compare(other Specificity) int {
	if specificity.IDs != other.IDs {
		return compareInt(specificity.IDs, other.IDs)
	}
	if specificity.Classes != other.Classes {
		return compareInt(specificity.Classes, other.Classes)
	}
	return compareInt(specificity.Types, other.Types)
}

func (specificity Specificity) add(other Specificity) Specificity {
	return Specificity{
		IDs:     specificity.IDs + other.IDs,
		Classes: specificity.Classes + other.Classes,
		Types:   specificity.Types + other.Types,
	}
}

func greatestSpecificity(selectors []Selector) Specificity {
	var greatest Specificity
	for _, selector := range selectors {
		if selector.specificity.Compare(greatest) > 0 {
			greatest = selector.specificity
		}
	}
	return greatest
}

// Declaration is one property/value pair. Value is the trimmed source value;
// a trailing !important marker is removed and represented by Important.
type Declaration struct {
	Property  string
	Value     string
	Important bool
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
