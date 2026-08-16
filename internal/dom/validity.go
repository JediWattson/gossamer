package dom

import (
	"math"
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
	candidate, completed = constraintValidationCandidate(node, take)
	if !completed || !candidate {
		return candidate, false, completed
	}
	typeName := constraintInputType(node)

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
		case "date", "month", "week", "time", "datetime-local", "number", "range":
			if _, parsed := constraintRangeScalar(typeName, value); !parsed && typeName != "range" {
				return true, false, true
			}
			participates, inRange, complete := EvaluateRangeValidity(node, take)
			if !complete {
				return true, false, false
			}
			if participates && !inRange {
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

func constraintValidationCandidate(node *Node, take func() bool) (candidate, completed bool) {
	if node == nil || node.Type != ElementNode || node.NamespaceURI != HTMLNamespace {
		return false, true
	}
	if node.Data != "input" && node.Data != "select" && node.Data != "textarea" {
		return false, true
	}
	disabled, ok := constraintActuallyDisabled(node, take)
	if !ok {
		return false, false
	}
	if disabled {
		return false, true
	}
	for ancestor := node.Parent; ancestor != nil; ancestor = ancestor.Parent {
		if !constraintTake(take) {
			return false, false
		}
		if isHTMLControl(ancestor, "datalist") {
			return false, true
		}
	}
	typeName := constraintInputType(node)
	if node.Data == "input" {
		switch typeName {
		case "hidden", "button", "reset", "submit", "image":
			return false, true
		}
		if hasAttributeValue(node, "readonly") && constraintInputSupportsReadOnly(typeName) {
			return false, true
		}
	} else if node.Data == "textarea" && hasAttributeValue(node, "readonly") {
		return false, true
	}
	return true, true
}

// EvaluateRangeValidity returns whether node participates in HTML's range
// pseudo-classes, whether it is in range, and whether bounded evaluation
// completed. Empty or unparsable values are in range when valid min/max
// limitations exist because they suffer from neither underflow nor overflow.
func EvaluateRangeValidity(node *Node, take func() bool) (participates, inRange, completed bool) {
	candidate, completed := constraintValidationCandidate(node, take)
	if !completed || !candidate || node.Data != "input" {
		return false, false, completed
	}
	typeName := constraintInputType(node)
	switch typeName {
	case "date", "month", "week", "time", "datetime-local", "number", "range":
	default:
		return false, false, true
	}

	minimum, hasMinimum := constraintRangeScalar(typeName, contentAttribute(node, "min"))
	maximum, hasMaximum := constraintRangeScalar(typeName, contentAttribute(node, "max"))
	if typeName == "range" {
		if !hasMinimum {
			minimum, hasMinimum = 0, true
		}
		if !hasMaximum {
			maximum, hasMaximum = 100, true
		}
	}
	if !hasMinimum && !hasMaximum {
		return false, false, true
	}
	valueText, _, _, ok := constraintControlValue(node, take)
	if !ok {
		return true, false, false
	}
	value, hasValue := constraintRangeScalar(typeName, valueText)
	if typeName == "range" {
		if !hasValue {
			value = minimum
			if maximum >= minimum {
				value = minimum + (maximum-minimum)/2
			}
		}
		if value < minimum {
			value = minimum
		}
		if value > maximum && maximum >= minimum {
			value = maximum
		}
		hasValue = true
	}
	if !hasValue {
		return true, true, true
	}
	if typeName == "time" && hasMinimum && hasMaximum && minimum > maximum {
		return true, !(value > maximum && value < minimum), true
	}
	underflow := hasMinimum && value < minimum
	overflow := hasMaximum && value > maximum
	return true, !underflow && !overflow, true
}

var (
	constraintNumberPattern = regexp.MustCompile(`^-?(?:[0-9]+(?:\.[0-9]*)?|\.[0-9]+)(?:[eE][+-]?[0-9]+)?$`)
	constraintDatePattern   = regexp.MustCompile(`^([0-9]{4,})-([0-9]{2})-([0-9]{2})$`)
	constraintMonthPattern  = regexp.MustCompile(`^([0-9]{4,})-([0-9]{2})$`)
	constraintWeekPattern   = regexp.MustCompile(`^([0-9]{4,})-W([0-9]{2})$`)
	constraintTimePattern   = regexp.MustCompile(`^([0-9]{2}):([0-9]{2})(?::([0-9]{2})(?:\.([0-9]+))?)?$`)
)

func constraintRangeScalar(typeName, value string) (float64, bool) {
	switch typeName {
	case "number", "range":
		if !constraintNumberPattern.MatchString(value) {
			return 0, false
		}
		number, err := strconv.ParseFloat(value, 64)
		return number, err == nil && !math.IsInf(number, 0) && !math.IsNaN(number)
	case "date":
		year, month, day, ok := constraintParseDate(value)
		if !ok {
			return 0, false
		}
		return float64(constraintDaysFromCivil(year, month, day)), true
	case "month":
		match := constraintMonthPattern.FindStringSubmatch(value)
		if match == nil {
			return 0, false
		}
		year, yearOK := constraintPositiveYear(match[1])
		month, monthErr := strconv.ParseInt(match[2], 10, 64)
		if !yearOK || monthErr != nil || month < 1 || month > 12 {
			return 0, false
		}
		return float64(year*12 + month - 1), true
	case "week":
		match := constraintWeekPattern.FindStringSubmatch(value)
		if match == nil {
			return 0, false
		}
		year, yearOK := constraintPositiveYear(match[1])
		week, weekErr := strconv.ParseInt(match[2], 10, 64)
		if !yearOK || weekErr != nil || week < 1 || week > int64(constraintISOWeeksInYear(year)) {
			return 0, false
		}
		jan4 := constraintDaysFromCivil(year, 1, 4)
		weekOneMonday := jan4 - int64(constraintISOWeekday(jan4)-1)
		return float64(weekOneMonday + (week-1)*7), true
	case "time":
		return constraintParseTime(value)
	case "datetime-local":
		separator := strings.IndexAny(value, "T ")
		if separator <= 0 || separator == len(value)-1 || strings.IndexAny(value[separator+1:], "T ") >= 0 {
			return 0, false
		}
		year, month, day, dateOK := constraintParseDate(value[:separator])
		timeValue, timeOK := constraintParseTime(value[separator+1:])
		if !dateOK || !timeOK {
			return 0, false
		}
		return float64(constraintDaysFromCivil(year, month, day))*86_400_000 + timeValue, true
	default:
		return 0, false
	}
}

func constraintParseDate(value string) (year, month, day int64, ok bool) {
	match := constraintDatePattern.FindStringSubmatch(value)
	if match == nil {
		return 0, 0, 0, false
	}
	year, ok = constraintPositiveYear(match[1])
	month, monthErr := strconv.ParseInt(match[2], 10, 64)
	day, dayErr := strconv.ParseInt(match[3], 10, 64)
	if !ok || monthErr != nil || dayErr != nil || month < 1 || month > 12 || day < 1 || day > int64(constraintDaysInMonth(year, month)) {
		return 0, 0, 0, false
	}
	return year, month, day, true
}

func constraintParseTime(value string) (float64, bool) {
	match := constraintTimePattern.FindStringSubmatch(value)
	if match == nil {
		return 0, false
	}
	hour, hourErr := strconv.ParseInt(match[1], 10, 64)
	minute, minuteErr := strconv.ParseInt(match[2], 10, 64)
	second := int64(0)
	var secondErr error
	if match[3] != "" {
		second, secondErr = strconv.ParseInt(match[3], 10, 64)
	}
	if hourErr != nil || minuteErr != nil || secondErr != nil || hour > 23 || minute > 59 || second > 59 {
		return 0, false
	}
	fraction := 0.0
	if match[4] != "" {
		parsed, err := strconv.ParseFloat("0."+match[4], 64)
		if err != nil {
			return 0, false
		}
		fraction = parsed
	}
	return float64(hour*3_600_000+minute*60_000+second*1_000) + fraction*1_000, true
}

func constraintPositiveYear(value string) (int64, bool) {
	year, err := strconv.ParseInt(value, 10, 64)
	return year, err == nil && year > 0 && year <= 999_999_999
}

func constraintDaysInMonth(year, month int64) int {
	switch month {
	case 4, 6, 9, 11:
		return 30
	case 2:
		if constraintLeapYear(year) {
			return 29
		}
		return 28
	default:
		return 31
	}
}

func constraintLeapYear(year int64) bool {
	return year%4 == 0 && (year%100 != 0 || year%400 == 0)
}

func constraintDaysFromCivil(year, month, day int64) int64 {
	if month <= 2 {
		year--
	}
	era := year / 400
	yearOfEra := year - era*400
	adjustedMonth := month
	if month > 2 {
		adjustedMonth -= 3
	} else {
		adjustedMonth += 9
	}
	dayOfYear := (153*adjustedMonth+2)/5 + day - 1
	dayOfEra := yearOfEra*365 + yearOfEra/4 - yearOfEra/100 + dayOfYear
	return era*146097 + dayOfEra - 719468
}

func constraintISOWeekday(days int64) int {
	weekday := int((days+3)%7) + 1
	if weekday <= 0 {
		weekday += 7
	}
	return weekday
}

func constraintISOWeeksInYear(year int64) int {
	jan1 := constraintISOWeekday(constraintDaysFromCivil(year, 1, 1))
	if jan1 == 4 || jan1 == 3 && constraintLeapYear(year) {
		return 53
	}
	return 52
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
