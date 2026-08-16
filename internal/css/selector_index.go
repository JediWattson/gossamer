package css

import (
	"sort"

	"github.com/JediWattson/gossamer/internal/dom"
)

type stylesheetSelectorIndex struct {
	universal    []int
	byType       map[string][]int
	byClass      map[string][]int
	byID         map[string][]int
	dependencies SelectorDependencies
}

// WithSelectorIndex returns a stylesheet sharing an immutable conservative
// selector index. Parse installs one eagerly. Stylesheets are immutable after
// publication; callers that compose a new Rules slice must invoke this method
// on that new value before sharing it.
func (stylesheet Stylesheet) WithSelectorIndex() Stylesheet {
	if stylesheet.selectorIndex != nil {
		return stylesheet
	}
	index := &stylesheetSelectorIndex{
		byType:  make(map[string][]int),
		byClass: make(map[string][]int),
		byID:    make(map[string][]int),
	}
	for ruleIndex, rule := range stylesheet.Rules {
		keys := make([]SelectorCandidateKey, 0, len(rule.Selectors))
		universal := false
		for _, selector := range rule.Selectors {
			collectSelectorDependencies(selector, &index.dependencies)
			key := selector.CandidateKey()
			if key.Kind == SelectorCandidateUniversal {
				universal = true
				continue
			}
			duplicate := false
			for _, existing := range keys {
				if existing == key {
					duplicate = true
					break
				}
			}
			if !duplicate {
				keys = append(keys, key)
			}
		}
		if universal || len(keys) == 0 {
			index.universal = append(index.universal, ruleIndex)
			continue
		}
		for _, key := range keys {
			switch key.Kind {
			case SelectorCandidateID:
				index.byID[key.Value] = append(index.byID[key.Value], ruleIndex)
			case SelectorCandidateClass:
				index.byClass[key.Value] = append(index.byClass[key.Value], ruleIndex)
			case SelectorCandidateType:
				index.byType[key.Value] = append(index.byType[key.Value], ruleIndex)
			default:
				index.universal = append(index.universal, ruleIndex)
			}
		}
	}
	stylesheet.selectorIndex = index
	return stylesheet
}

// RebuildSelectorIndex replaces any cached index after a caller has composed
// or otherwise replaced the exported Rules slice. Parsed stylesheets normally
// use WithSelectorIndex's immutable fast path.
func (stylesheet Stylesheet) RebuildSelectorIndex() Stylesheet {
	stylesheet.selectorIndex = nil
	return stylesheet.WithSelectorIndex()
}

// CandidateRuleIndexes appends the sorted, unique rule indexes that may match
// node to scratch. The stylesheet must have been passed through
// WithSelectorIndex; a missing index conservatively returns every rule.
func (stylesheet Stylesheet) CandidateRuleIndexes(node *dom.Node, scratch []int) []int {
	index := stylesheet.selectorIndex
	if index == nil || node == nil || node.Type != dom.ElementNode {
		for ruleIndex := range stylesheet.Rules {
			scratch = append(scratch, ruleIndex)
		}
		return scratch
	}
	scratch = append(scratch, index.universal...)
	scratch = append(scratch, index.byType[lowerASCII(node.Data)]...)
	if id, ok := attributeValue(node, "id"); ok {
		scratch = append(scratch, index.byID[id]...)
	}
	if classes, ok := attributeValue(node, "class"); ok {
		start := -1
		for position := 0; position <= len(classes); position++ {
			space := position == len(classes)
			if !space {
				switch classes[position] {
				case ' ', '\t', '\n', '\r', '\f':
					space = true
				}
			}
			if space {
				if start >= 0 {
					scratch = append(scratch, index.byClass[classes[start:position]]...)
					start = -1
				}
				continue
			}
			if start < 0 {
				start = position
			}
		}
	}
	if len(scratch) < 2 {
		return scratch
	}
	sort.Ints(scratch)
	write := 1
	for read := 1; read < len(scratch); read++ {
		if scratch[read] == scratch[write-1] {
			continue
		}
		scratch[write] = scratch[read]
		write++
	}
	return scratch[:write]
}
