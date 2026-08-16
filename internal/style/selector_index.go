package style

import (
	"github.com/JediWattson/gossamer/internal/css"
	"github.com/JediWattson/gossamer/internal/dom"
)

// selectorRuleIndex conservatively narrows one stylesheet to rules whose
// rightmost required type, class, or ID can match a subject. Full selector
// matching remains authoritative, so indexed false positives are harmless.
type selectorRuleIndex struct {
	sourceOrder []int
	all         []int
}

func buildOriginStyleContext(sheets []stylesheetSource, environment css.MediaEnvironment) originStyleContext {
	context := originStyleContext{
		sheets:     sheets,
		layerRanks: originLayerRanks(sheets, environment),
	}
	order := 0
	for index := range context.sheets {
		context.sheets[index].stylesheet = context.sheets[index].stylesheet.WithSelectorIndex()
		context.sheets[index].ruleIndex = buildSelectorRuleIndex(context.sheets[index].stylesheet, order)
		for _, rule := range context.sheets[index].stylesheet.Rules {
			order += len(rule.Declarations)
		}
	}
	context.declarationCount = order
	return context
}

func buildSelectorRuleIndex(stylesheet css.Stylesheet, sourceOrderBase int) selectorRuleIndex {
	index := selectorRuleIndex{
		sourceOrder: make([]int, len(stylesheet.Rules)),
		all:         make([]int, len(stylesheet.Rules)),
	}
	order := sourceOrderBase
	for ruleIndex, rule := range stylesheet.Rules {
		index.all[ruleIndex] = ruleIndex
		index.sourceOrder[ruleIndex] = order
		order += len(rule.Declarations)

	}
	return index
}

func (index selectorRuleIndex) candidates(stylesheet css.Stylesheet, node *dom.Node, disabled bool, scratch []int) []int {
	if disabled {
		return append(scratch, index.all...)
	}
	return stylesheet.CandidateRuleIndexes(node, scratch)
}
