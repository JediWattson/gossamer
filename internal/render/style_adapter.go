package render

import (
	"math"
	"strings"

	"github.com/JediWattson/gossamer/internal/dom"
	computed "github.com/JediWattson/gossamer/internal/style"
)

type displayMode = computed.DisplayMode

const (
	displayInline      = computed.DisplayInline
	displayInlineBlock = computed.DisplayInlineBlock
	displayBlock       = computed.DisplayBlock
	displayListItem    = computed.DisplayListItem
	displayNone        = computed.DisplayNone
)

type textAlignment = computed.TextAlignment

const (
	alignLeft    = computed.AlignLeft
	alignCenter  = computed.AlignCenter
	alignRight   = computed.AlignRight
	alignStart   = computed.AlignStart
	alignEnd     = computed.AlignEnd
	alignJustify = computed.AlignJustify
)

type listStyleType = computed.ListStyleType

const (
	listStyleDisc    = computed.ListStyleDisc
	listStyleCircle  = computed.ListStyleCircle
	listStyleSquare  = computed.ListStyleSquare
	listStyleDecimal = computed.ListStyleDecimal
	listStyleNone    = computed.ListStyleNone
)

type borderStyle = computed.BorderStyle

const (
	borderStyleNone   = computed.BorderStyleNone
	borderStyleSolid  = computed.BorderStyleSolid
	borderStyleHidden = computed.BorderStyleHidden
)

type lengthUnit = computed.LengthUnit

const (
	lengthAuto    = computed.LengthAuto
	lengthPX      = computed.LengthPX
	lengthPercent = computed.LengthPercent
	lengthVW      = computed.LengthVW
	lengthVH      = computed.LengthVH
)

type length = computed.Length
type computedLineHeight = computed.LineHeight
type borderSide = computed.BorderSide
type computedStyle = computed.ComputedStyle

type styledNode struct {
	node     *dom.Node
	style    computedStyle
	children []*styledNode
}

func projectStyleTree(node *dom.Node, snapshot *computed.Snapshot) *styledNode {
	if node == nil || snapshot == nil {
		return nil
	}
	value, ok := snapshot.Lookup(node)
	if !ok {
		return nil
	}
	projected := &styledNode{node: node, style: value}
	projected.children = make([]*styledNode, 0, len(node.Children))
	for _, child := range node.Children {
		if projectedChild := projectStyleTree(child, snapshot); projectedChild != nil {
			projected.children = append(projected.children, projectedChild)
		}
	}
	return projected
}

func projectReadAccessStyleTree(node *dom.Node, access *dom.ReadAccess, snapshot *computed.Snapshot) *styledNode {
	if node == nil || snapshot == nil {
		return nil
	}
	id, ok := access.ID(node)
	if !ok {
		return nil
	}
	value, ok := snapshot.LookupID(id)
	if !ok {
		return nil
	}
	projected := &styledNode{node: node, style: value}
	projected.children = make([]*styledNode, 0, len(node.Children))
	for _, child := range node.Children {
		if projectedChild := projectReadAccessStyleTree(child, access, snapshot); projectedChild != nil {
			projected.children = append(projected.children, projectedChild)
		}
	}
	return projected
}

func styleEnvironment(viewport Viewport) computed.Environment {
	return computed.Environment{
		Width:           viewport.Width,
		Height:          viewport.Height,
		MediaType:       "screen",
		InitialFontSize: 16,
	}
}

func resolveLength(value length, percentBase float64, viewport Viewport, autoValue float64) float64 {
	if resolved, ok := value.Resolve(percentBase, float64(viewport.Width), float64(viewport.Height)); ok {
		return resolved
	}
	return autoValue
}

func isBlockLevel(display displayMode) bool {
	return display == displayBlock || display == displayListItem
}

func attribute(node *dom.Node, name string) (string, bool) {
	for _, candidate := range node.Attributes {
		if strings.EqualFold(candidate.Name, name) {
			return candidate.Value, true
		}
	}
	return "", false
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
