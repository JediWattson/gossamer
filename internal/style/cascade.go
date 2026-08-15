package style

import (
	"sort"

	"github.com/JediWattson/gossamer/internal/css"
	"github.com/JediWattson/gossamer/internal/dom"
)

// stylesheetSource retains the logical owner and order of a parsed sheet.
// Byte spans deliberately wait for the shared CSS Syntax tokenizer.
type stylesheetSource struct {
	stylesheet css.Stylesheet
	owner      *dom.Node
	kind       SourceKind
	order      int
}

type originStyleContext struct {
	sheets     []stylesheetSource
	layerRanks map[string]int
}

type cascadeStyleContext struct {
	userAgent        originStyleContext
	user             originStyleContext
	author           originStyleContext
	mediaEnvironment css.MediaEnvironment
}

// winningDeclaration is one validated declaration expanded to a longhand (or
// one custom property). The name is retained because the list is sorted in
// winning order, although rollback can deliberately select a later entry.
type winningDeclaration struct {
	declaration      css.Declaration
	target           string
	origin           CascadeOrigin
	kind             SourceKind
	owner            *dom.Node
	attribute        string
	specificity      css.Specificity
	order            int
	layer            string
	layerRank        int
	layered          bool
	inline           bool
	stylesheetOrder  int
	ruleOrder        int
	declarationOrder int
	authoredValue    string
}

func (candidate winningDeclaration) source() PropertySource {
	value := candidate.authoredValue
	if value == "" {
		value = candidate.declaration.Value
	}
	return PropertySource{
		Origin:              candidate.origin,
		Kind:                candidate.kind,
		DeclarationProperty: candidate.declaration.Property,
		DeclarationValue:    value,
		Attribute:           candidate.attribute,
		Important:           candidate.declaration.Important,
		Layer:               candidate.layer,
		LayerRank:           candidate.layerRank,
		Specificity:         candidate.specificity,
		StylesheetOrder:     candidate.stylesheetOrder,
		RuleOrder:           candidate.ruleOrder,
		DeclarationOrder:    candidate.declarationOrder,
		SourceOrder:         candidate.order,
		owner:               candidate.owner,
	}
}

// declarationPrecedence returns positive when left wins over right.
func declarationPrecedence(left, right winningDeclaration) int {
	if leftLevel, rightLevel := cascadeLevel(left), cascadeLevel(right); leftLevel != rightLevel {
		return compareInt(leftLevel, rightLevel)
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
	return compareInt(left.order, right.order)
}

func cascadeLevel(candidate winningDeclaration) int {
	if candidate.declaration.Important {
		switch candidate.origin {
		case CascadeOriginUserAgent:
			return 6
		case CascadeOriginUser:
			return 5
		case CascadeOriginAuthor:
			return 4
		}
	}
	switch candidate.origin {
	case CascadeOriginAuthor:
		return 3
	case CascadeOriginPresentationalHint:
		return 2
	case CascadeOriginUser:
		return 1
	default:
		return 0
	}
}

func sortCascadeCandidates(candidatesByTarget map[string][]winningDeclaration) {
	for target := range candidatesByTarget {
		candidates := candidatesByTarget[target]
		sort.SliceStable(candidates, func(left, right int) bool {
			return declarationPrecedence(candidates[left], candidates[right]) > 0
		})
	}
}

// nextCandidateAfterRevert returns the first declaration from the logically
// lower origin. Presentational hints are a distinct cascade origin, but author
// revert folds them into author and therefore skips them.
func nextCandidateAfterRevert(candidates []winningDeclaration, position int) int {
	current := candidates[position]
	for next := position + 1; next < len(candidates); next++ {
		if originSurvivesRollback(current.origin, candidates[next].origin) {
			return next
		}
	}
	return len(candidates)
}

func originSurvivesRollback(current, candidate CascadeOrigin) bool {
	switch current {
	case CascadeOriginAuthor, CascadeOriginPresentationalHint:
		return candidate == CascadeOriginUser || candidate == CascadeOriginUserAgent
	case CascadeOriginUser:
		return candidate == CascadeOriginUserAgent
	default:
		return false
	}
}

// nextCandidateAfterRevertLayer first searches the same origin for the layer
// exposed by rollback. Only when that origin is exhausted does it descend to a
// logically lower origin. This two-phase search matters for important origins,
// whose global order is the reverse of normal origins.
func nextCandidateAfterRevertLayer(candidates []winningDeclaration, position int) int {
	current := candidates[position]
	for next := position + 1; next < len(candidates); next++ {
		candidate := candidates[next]
		if candidate.origin == current.origin && survivesSameOriginRevertLayer(current, candidate) {
			return next
		}
	}
	for next := position + 1; next < len(candidates); next++ {
		if originSurvivesRevertLayer(current.origin, candidates[next].origin) {
			return next
		}
	}
	return len(candidates)
}

func originSurvivesRevertLayer(current, candidate CascadeOrigin) bool {
	switch current {
	case CascadeOriginAuthor:
		return candidate == CascadeOriginPresentationalHint || candidate == CascadeOriginUser || candidate == CascadeOriginUserAgent
	case CascadeOriginPresentationalHint:
		return candidate == CascadeOriginUser || candidate == CascadeOriginUserAgent
	case CascadeOriginUser:
		return candidate == CascadeOriginUserAgent
	default:
		return false
	}
}

func survivesSameOriginRevertLayer(current, candidate winningDeclaration) bool {
	// Element-attached styles form their own author-origin cascade step.
	// Important inline revert-layer is the Cascade 5 exception: it removes both
	// inline halves but preserves author-important rules.
	if current.inline {
		return !candidate.inline
	}
	if candidate.inline {
		return false
	}
	if !current.declaration.Important {
		if candidate.declaration.Important {
			return false
		}
		if current.layered {
			return candidate.layered && candidate.layerRank < current.layerRank
		}
		return candidate.layered
	}

	// Important layer order mirrors normal layer order. Rolling back an
	// important layer removes that layer and every lower-priority important
	// step, then resumes in the preceding normal layer.
	if candidate.declaration.Important || !candidate.layered {
		return false
	}
	if !current.layered {
		return true
	}
	return candidate.layerRank < current.layerRank
}

func originLayerRanks(sheets []stylesheetSource) map[string]int {
	ranks := make(map[string]int)
	for _, source := range sheets {
		for _, name := range source.stylesheet.LayerOrder {
			if name == "" {
				continue
			}
			if _, exists := ranks[name]; !exists {
				ranks[name] = len(ranks)
			}
		}
		for _, rule := range source.stylesheet.Rules {
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
