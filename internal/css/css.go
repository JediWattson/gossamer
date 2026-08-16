// Package css parses and matches the CSS subset used by Gossamer's rendering
// pipeline. It supports complex selectors, attribute selectors, logical and
// structural pseudo-classes, named top-level cascade layers, viewport media
// queries, and tokenized declarations consumed by the renderer. Selector
// namespaces, pseudo-elements, selector escapes, and the remaining at-rules
// are outside the current subset.
package css

import (
	"errors"

	"github.com/JediWattson/gossamer/internal/dom"
)

// ErrInvalidSelector reports an unforgiving selector-list parse failure. DOM
// query APIs surface this boundary to JavaScript instead of silently dropping
// a malformed selector as stylesheet parsing does.
var ErrInvalidSelector = errors.New("css: invalid selector")

// Stylesheet is an ordered collection of qualified CSS rules. LayerOrder lists
// supported named top-level layers in first-declaration order.
type Stylesheet struct {
	Rules      []Rule
	LayerOrder []string
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
}

// ParseSelectorList parses the unforgiving selector-list grammar used by DOM
// query APIs. Unlike stylesheet parsing, one invalid member rejects the whole
// list.
func ParseSelectorList(source string) ([]Selector, error) {
	cleaned, err := stripComments(source)
	if err != nil {
		return nil, err
	}
	selectors, ok := parseSelectorList(cleaned)
	if !ok {
		return nil, ErrInvalidSelector
	}
	return selectors, nil
}

// MatchesAny reports whether at least one selector matches node.
func MatchesAny(selectors []Selector, node *dom.Node) bool {
	for _, selector := range selectors {
		if selector.Matches(node) {
			return true
		}
	}
	return false
}

// Specificity returns the selector's static CSS specificity.
func (selector Selector) Specificity() Specificity {
	return selector.specificity
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
