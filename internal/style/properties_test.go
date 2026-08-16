package style

import (
	"slices"
	"testing"
)

func TestPropertyDefinitionsAreCanonicalAndComplete(t *testing.T) {
	if len(propertyDefinitions) != 79 {
		t.Fatalf("property definition count = %d, want 79", len(propertyDefinitions))
	}
	if !slices.IsSorted(computedPropertyNames) {
		t.Fatalf("computed property names are not sorted: %q", computedPropertyNames)
	}
	if !slices.Equal(computedPropertyNames, allPropertyNames) {
		t.Fatalf("all targets = %q, want current ordinary longhands %q", allPropertyNames, computedPropertyNames)
	}

	viewport := Environment{Width: 800, Height: 600, InitialFontSize: 16}
	initial := cssInitialStyle(viewport)
	early := []string(nil)
	for index := range propertyDefinitions {
		definition := &propertyDefinitions[index]
		if definition.computeEarly {
			early = append(early, definition.name)
		}
		t.Run(definition.name, func(t *testing.T) {
			if index > 0 && propertyDefinitions[index-1].name == definition.name {
				t.Fatalf("duplicate property definition %q", definition.name)
			}
			if definition.invalidation == 0 {
				t.Fatal("property has no invalidation metadata")
			}
			lookup, ok := lookupPropertyDefinition(definition.name)
			if !ok || lookup != definition {
				t.Fatalf("lookupPropertyDefinition(%q) = %p, %t, want %p, true", definition.name, lookup, ok, definition)
			}

			serialized := definition.serialize(initial)
			if serialized == "" {
				t.Fatal("initial value has no computed serializer")
			}
			validSource := registryTestValue(*definition)
			if !definition.valid(validSource, viewport) {
				t.Fatalf("representative value %q is rejected by property grammar", validSource)
			}
			context := propertyApplyContext{
				parentFontSize:   16,
				parentFontWeight: 400,
				viewport:         viewport,
			}
			applied := initial
			definition.apply(&applied, validSource, context)
			if got := definition.serialize(applied); got == "" {
				t.Fatal("applied value has no computed serializer")
			}

			copied := initial
			definition.apply(&copied, validSource, context)
			definition.copy(&copied, initial)
			if got := definition.serialize(copied); got != serialized {
				t.Fatalf("copied initial serializes as %q, want %q", got, serialized)
			}

			reset := initial
			definition.apply(&reset, validSource, context)
			definition.resetToInitial(&reset, viewport)
			if got := definition.serialize(reset); got != serialized {
				t.Fatalf("reset initial serializes as %q, want %q", got, serialized)
			}
		})
	}
	if !slices.Equal(early, []string{"font-size"}) {
		t.Fatalf("early computed properties = %q, want [font-size]", early)
	}
}

func registryTestValue(definition propertyDefinition) string {
	switch definition.kind {
	case propertyAlignContent:
		return "space-evenly"
	case propertyAlignItems, propertyJustifyItems:
		return "center"
	case propertyAlignSelf, propertyJustifySelf:
		return "self-end"
	case propertyBackgroundColor, propertyBorderColor, propertyColor:
		return "red"
	case propertyBorderStyle:
		return "solid"
	case propertyBorderWidth:
		return "1px"
	case propertyBorderCollapse:
		return "collapse"
	case propertyBorderSpacing:
		return "3px 4px"
	case propertyBoxSizing:
		return "border-box"
	case propertyCaptionSide:
		return "bottom"
	case propertyContent:
		return `"prefix" attr(data-label)`
	case propertyDisplay:
		return "block"
	case propertyEmptyCells:
		return "hide"
	case propertyFlexBasis:
		return "10px"
	case propertyFlexDirection:
		return "column"
	case propertyFlexGrow, propertyFlexShrink:
		return "2"
	case propertyFontFamily:
		return `"Go Mono", monospace`
	case propertyFontSize:
		return "18px"
	case propertyFontStyle:
		return "italic"
	case propertyFontWeight:
		return "700"
	case propertyGridAutoColumns, propertyGridAutoRows:
		return "2fr"
	case propertyGridAutoFlow:
		return "column dense"
	case propertyGridColumnEnd, propertyGridRowEnd:
		return "span 2"
	case propertyGridColumnStart, propertyGridRowStart:
		return "2"
	case propertyGridTemplateAreas:
		return `"main"`
	case propertyGridTemplateColumns, propertyGridTemplateRows:
		return "40px 1fr auto"
	case propertyHeight, propertyInset, propertyMargin, propertyMaxHeight, propertyMaxWidth, propertyMinHeight, propertyMinWidth, propertyPadding, propertyWidth:
		return "2px"
	case propertyLineHeight:
		return "1.5"
	case propertyJustifyContent:
		return "space-between"
	case propertyListStyleType:
		return "square"
	case propertyOpacity:
		return "0.5"
	case propertyOrder:
		return "-1"
	case propertyOverflowX, propertyOverflowY:
		return "auto"
	case propertyPosition:
		return "absolute"
	case propertyGap:
		return "3px"
	case propertyTableLayout:
		return "fixed"
	case propertyTextAlign:
		return "center"
	case propertyTextDecorationLine:
		return "underline"
	case propertyVerticalAlign:
		return "25%"
	case propertyZIndex:
		return "2"
	case propertyVisibility:
		return "hidden"
	case propertyWhiteSpace:
		return "pre-wrap"
	default:
		panic("uncovered property kind")
	}
}

func TestDeclarationTargetsAreRegistryBacked(t *testing.T) {
	if got := declarationTargets("all"); !slices.Equal(got, allPropertyNames) {
		t.Fatalf("all targets = %q, want %q", got, allPropertyNames)
	}
	if slices.Contains(declarationTargets("all"), "all") {
		t.Fatal("all expands to itself")
	}
	if got := declarationTargets("--theme"); !slices.Equal(got, []string{"--theme"}) {
		t.Fatalf("custom property targets = %q", got)
	}
	for index := range propertyDefinitions {
		name := propertyDefinitions[index].name
		if got := declarationTargets(name); !slices.Equal(got, []string{name}) {
			t.Errorf("longhand %q targets = %q", name, got)
		}
	}
	for shorthand, targets := range shorthandTargets {
		seen := make(map[string]bool, len(targets))
		for _, target := range targets {
			if _, ok := lookupPropertyDefinition(target); !ok {
				t.Errorf("shorthand %q has unregistered target %q", shorthand, target)
			}
			if seen[target] {
				t.Errorf("shorthand %q repeats target %q", shorthand, target)
			}
			seen[target] = true
		}
	}
}
