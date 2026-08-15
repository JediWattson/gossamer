// Package css parses the small CSS subset used by Gossamer's first rendering
// pipeline. It intentionally represents only compound selectors; combinators,
// pseudo-elements, attribute selectors, and conditional at-rules are skipped.
package css

// Stylesheet is an ordered collection of qualified CSS rules.
type Stylesheet struct {
	Rules []Rule
}

// Rule associates one or more selectors with an ordered declaration block.
// Order is zero-based and increases for each successfully parsed rule.
type Rule struct {
	Selectors    []Selector
	Declarations []Declaration
	Order        int
}

// Selector is a simple compound selector. An empty Tag means no type selector;
// "*" is retained for an explicit universal selector.
type Selector struct {
	Tag           string
	ID            string
	Classes       []string
	PseudoClasses []string
	Specificity   Specificity
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
