package dom

import (
	"net/url"
	"regexp"
	"strconv"
	"strings"
)

// EvaluateConstraintValidity returns whether node is a candidate for HTML
// constraint validation, whether it satisfies the implemented constraints,
// and whether evaluation completed. take may bound tree work; returning false
// from it makes the evaluation fail closed through completed=false.
func EvaluateConstraintValidity(node *Node, take func() bool) (candidate, valid, completed bool) {
	if node == nil || node.Type != ElementNode || node.NamespaceURI != HTMLNamespace {
		return false, false, true
	}
	if node.Data != "input" && node.Data != "select" && node.Data != "textarea" {
		return false, false, true
	}
	disabled, ok := constraintActuallyDisabled(node, take)
	if !ok {
		return false, false, false
	}
	if disabled {
		return false, false, true
	}
	for ancestor := node.Parent; ancestor != nil; ancestor = ancestor.Parent {
		if !constraintTake(take) {
			return false, false, false
		}
		if isHTMLControl(ancestor, "datalist") {
			return false, false, true
		}
	}
	typeName := constraintInputType(node)
	if node.Data == "input" {
		switch typeName {
		case "hidden", "button", "reset", "submit", "image":
			return false, false, true
		}
		if hasAttributeValue(node, "readonly") && constraintInputSupportsReadOnly(typeName) {
			return false, false, true
		}
	} else if node.Data == "textarea" && hasAttributeValue(node, "readonly") {
		return false, false, true
	}

	value, selectedOption, firstOption, ok := constraintControlValue(node, take)
	if !ok {
		return true, false, false
	}
	if node.Data == "input" && typeName == "radio" {
		checked, required, complete := constraintRadioGroupState(node, take)
		if !complete {
			return true, false, false
		}
		if required && !checked {
			return true, false, true
		}
	} else if hasAttributeValue(node, "required") {
		switch {
		case node.Data == "input" && typeName == "checkbox":
			if !checkedLocked(node) {
				return true, false, true
			}
		case node.Data == "input" && !constraintInputSupportsRequired(typeName):
		case node.Data == "select":
			if constraintRequiredSelectMissing(node, selectedOption, firstOption, value) {
				return true, false, true
			}
		default:
			if value == "" {
				return true, false, true
			}
		}
	}
	if value == "" {
		return true, true, true
	}
	if node.Data == "input" {
		switch typeName {
		case "email":
			values := []string{value}
			if hasAttributeValue(node, "multiple") {
				values = strings.Split(value, ",")
			}
			for _, address := range values {
				address = strings.TrimSpace(address)
				at := strings.LastIndexByte(address, '@')
				if at <= 0 || at == len(address)-1 || strings.ContainsAny(address, " \t\r\n") {
					return true, false, true
				}
			}
		case "url":
			parsed, err := url.ParseRequestURI(value)
			if err != nil || parsed.Scheme == "" {
				return true, false, true
			}
		case "number", "range":
			number, err := strconv.ParseFloat(value, 64)
			if err != nil {
				return true, false, true
			}
			if minimum, err := strconv.ParseFloat(contentAttribute(node, "min"), 64); err == nil && number < minimum {
				return true, false, true
			}
			if maximum, err := strconv.ParseFloat(contentAttribute(node, "max"), 64); err == nil && number > maximum {
				return true, false, true
			}
		}
	}
	if pattern := contentAttribute(node, "pattern"); pattern != "" && node.Data == "input" && constraintInputSupportsPattern(typeName) {
		compiled, err := regexp.Compile("^(?:" + pattern + ")$")
		if err == nil && !compiled.MatchString(value) {
			return true, false, true
		}
	}
	if node.Data == "textarea" || node.Data == "input" && constraintInputSupportsTextLength(typeName) {
		length := utf16Length(value)
		if minimum, err := strconv.Atoi(contentAttribute(node, "minlength")); err == nil && minimum >= 0 && length < minimum {
			return true, false, true
		}
		if maximum, err := strconv.Atoi(contentAttribute(node, "maxlength")); err == nil && maximum >= 0 && length > maximum {
			return true, false, true
		}
	}
	return true, true, true
}

func constraintActuallyDisabled(node *Node, take func() bool) (bool, bool) {
	if hasAttributeValue(node, "disabled") {
		return true, true
	}
	for fieldset := node.Parent; fieldset != nil; fieldset = fieldset.Parent {
		if !constraintTake(take) {
			return false, false
		}
		if !isHTMLControl(fieldset, "fieldset") || !hasAttributeValue(fieldset, "disabled") {
			continue
		}
		var firstLegend *Node
		for _, child := range fieldset.Children {
			if !constraintTake(take) {
				return false, false
			}
			if isHTMLControl(child, "legend") {
				firstLegend = child
				break
			}
		}
		if firstLegend != nil {
			inside, complete := constraintInclusiveAncestor(firstLegend, node, take)
			if !complete {
				return false, false
			}
			if inside {
				continue
			}
		}
		return true, true
	}
	return false, true
}

func constraintInclusiveAncestor(ancestor, node *Node, take func() bool) (bool, bool) {
	for current := node; current != nil; current = current.Parent {
		if !constraintTake(take) {
			return false, false
		}
		if current == ancestor {
			return true, true
		}
	}
	return false, true
}

func constraintControlValue(node *Node, take func() bool) (value string, selectedOption, firstOption *Node, completed bool) {
	if node.Control != nil && node.Control.ValueDirty && (node.Data == "input" || node.Data == "textarea") {
		return node.Control.Value, nil, nil, true
	}
	if node.Data == "input" {
		return contentAttribute(node, "value"), nil, nil, true
	}
	if node.Data == "textarea" {
		value, ok := constraintDescendantText(node, take)
		return value, nil, nil, ok
	}
	options := make([]*Node, 0)
	stack := make([]*Node, 0, len(node.Children))
	for index := len(node.Children) - 1; index >= 0; index-- {
		stack = append(stack, node.Children[index])
	}
	for len(stack) != 0 {
		last := len(stack) - 1
		candidate := stack[last]
		stack = stack[:last]
		if !constraintTake(take) {
			return "", nil, nil, false
		}
		if candidate == nil {
			continue
		}
		if isHTMLControl(candidate, "option") {
			options = append(options, candidate)
		}
		for index := len(candidate.Children) - 1; index >= 0; index-- {
			stack = append(stack, candidate.Children[index])
		}
	}
	index := selectedIndexForOptions(node, options)
	if index < 0 {
		if len(options) != 0 {
			firstOption = options[0]
		}
		return "", nil, firstOption, true
	}
	option := options[index]
	if len(options) != 0 {
		firstOption = options[0]
	}
	if value, found := attributeValue(option, "value"); found {
		return value, option, firstOption, true
	}
	value, ok := constraintDescendantText(option, take)
	return strings.TrimSpace(value), option, firstOption, ok
}

func constraintRequiredSelectMissing(selectNode, selectedOption, firstOption *Node, selectedValue string) bool {
	if selectedOption == nil {
		return true
	}
	if hasAttributeValue(selectNode, "multiple") {
		return false
	}
	size := 1
	if parsed, err := strconv.Atoi(contentAttribute(selectNode, "size")); err == nil && parsed > 0 {
		size = parsed
	}
	if size != 1 || selectedOption != firstOption || selectedOption.Parent != selectNode {
		return false
	}
	return selectedValue == ""
}

func constraintDescendantText(node *Node, take func() bool) (string, bool) {
	var value strings.Builder
	stack := make([]*Node, 0, len(node.Children))
	for index := len(node.Children) - 1; index >= 0; index-- {
		stack = append(stack, node.Children[index])
	}
	for len(stack) != 0 {
		last := len(stack) - 1
		candidate := stack[last]
		stack = stack[:last]
		if !constraintTake(take) {
			return "", false
		}
		if candidate == nil {
			continue
		}
		if candidate.Type == TextNode {
			value.WriteString(candidate.Data)
		}
		for index := len(candidate.Children) - 1; index >= 0; index-- {
			stack = append(stack, candidate.Children[index])
		}
	}
	return value.String(), true
}

func constraintRadioGroupState(node *Node, take func() bool) (checked, required, completed bool) {
	checked = checkedLocked(node)
	required = hasAttributeValue(node, "required")
	name, named := attributeValue(node, "name")
	if !named || name == "" {
		return checked, required, true
	}
	owner, ok := constraintFormOwner(node, take)
	if !ok {
		return false, false, false
	}
	root := treeRoot(node)
	stack := []*Node{root}
	for len(stack) != 0 {
		last := len(stack) - 1
		candidate := stack[last]
		stack = stack[:last]
		if !constraintTake(take) {
			return false, false, false
		}
		if candidate != nil && candidate != node && isHTMLControl(candidate, "input") && constraintInputType(candidate) == "radio" {
			candidateName, found := attributeValue(candidate, "name")
			if found && candidateName == name {
				candidateOwner, complete := constraintFormOwner(candidate, take)
				if !complete {
					return false, false, false
				}
				if candidateOwner == owner {
					checked = checked || checkedLocked(candidate)
					required = required || hasAttributeValue(candidate, "required")
				}
			}
		}
		if candidate != nil {
			for index := len(candidate.Children) - 1; index >= 0; index-- {
				stack = append(stack, candidate.Children[index])
			}
		}
	}
	return checked, required, true
}

func constraintFormOwner(node *Node, take func() bool) (*Node, bool) {
	if explicit, found := attributeValue(node, "form"); found {
		root := treeRoot(node)
		if root != nil && root.Type == DocumentNode {
			if explicit == "" {
				return nil, true
			}
			stack := []*Node{root}
			for len(stack) != 0 {
				last := len(stack) - 1
				candidate := stack[last]
				stack = stack[:last]
				if !constraintTake(take) {
					return nil, false
				}
				if candidate != nil && candidate.Type == ElementNode {
					if id, found := attributeValue(candidate, "id"); found && id == explicit {
						if isHTMLControl(candidate, "form") {
							return candidate, true
						}
						return nil, true
					}
				}
				if candidate != nil {
					for index := len(candidate.Children) - 1; index >= 0; index-- {
						stack = append(stack, candidate.Children[index])
					}
				}
			}
			return nil, true
		}
	}
	for ancestor := node.Parent; ancestor != nil; ancestor = ancestor.Parent {
		if !constraintTake(take) {
			return nil, false
		}
		if isHTMLControl(ancestor, "form") {
			return ancestor, true
		}
	}
	return nil, true
}

func constraintInputType(node *Node) string {
	typeName := strings.ToLower(contentAttribute(node, "type"))
	switch typeName {
	case "hidden", "text", "search", "tel", "url", "email", "password", "date", "month", "week", "time",
		"datetime-local", "number", "range", "color", "checkbox", "radio", "file", "submit", "image", "reset", "button":
		return typeName
	default:
		return "text"
	}
}

func constraintInputSupportsReadOnly(typeName string) bool {
	switch typeName {
	case "text", "search", "tel", "url", "email", "password", "date", "month", "week", "time", "datetime-local", "number":
		return true
	default:
		return false
	}
}

func constraintInputSupportsRequired(typeName string) bool {
	switch typeName {
	case "hidden", "range", "color", "submit", "image", "reset", "button":
		return false
	default:
		return true
	}
}

func constraintInputSupportsPattern(typeName string) bool {
	switch typeName {
	case "text", "search", "tel", "url", "email", "password":
		return true
	default:
		return false
	}
}

func constraintInputSupportsTextLength(typeName string) bool {
	return constraintInputSupportsPattern(typeName)
}

func constraintTake(take func() bool) bool {
	return take == nil || take()
}
