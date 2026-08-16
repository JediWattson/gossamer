package css

import "strings"

type declarationSegment struct {
	values []ComponentValue
	start  int
	end    int
}

func parseSourcedDeclarationList(source string, baseOffset int) ([]SourcedDeclaration, error) {
	values, parseErr := ParseComponentValues(source)
	segments := splitDeclarationComponents(values, len(source))
	declarations := make([]SourcedDeclaration, 0, len(segments))
	for _, segment := range segments {
		declaration, ok := parseDeclarationSegment(source, segment, baseOffset)
		if ok {
			declarations = append(declarations, declaration)
		}
	}
	return declarations, parseErr
}

func splitDeclarationComponents(values []ComponentValue, sourceSize int) []declarationSegment {
	segments := make([]declarationSegment, 0)
	start := 0
	segmentStart := 0
	for index, value := range values {
		if value.Kind != ComponentToken || value.Token.Kind != TokenSemicolon {
			continue
		}
		segments = append(segments, declarationSegment{
			values: values[start:index],
			start:  segmentStart,
			end:    value.Span.Start,
		})
		start = index + 1
		segmentStart = value.Span.End
	}
	segments = append(segments, declarationSegment{
		values: values[start:],
		start:  segmentStart,
		end:    sourceSize,
	})
	return segments
}

func parseDeclarationSegment(source string, segment declarationSegment, baseOffset int) (SourcedDeclaration, bool) {
	values := trimComponentWhitespace(segment.values)
	if len(values) == 0 || componentValuesContainBadToken(values) {
		return SourcedDeclaration{}, false
	}

	colon := -1
	for index, value := range values {
		if value.Kind == ComponentToken && value.Token.Kind == TokenColon {
			colon = index
			break
		}
	}
	if colon < 0 {
		return SourcedDeclaration{}, false
	}

	nameValues := trimComponentWhitespace(values[:colon])
	if len(nameValues) != 1 || nameValues[0].Kind != ComponentToken || nameValues[0].Token.Kind != TokenIdent {
		return SourcedDeclaration{}, false
	}
	property := nameValues[0].Token.Value
	custom := strings.HasPrefix(property, "--")
	if !custom {
		property = lowerASCII(property)
	}

	valueValues := trimComponentWhitespace(values[colon+1:])
	important := false
	if len(valueValues) >= 2 {
		lastIndex := len(valueValues) - 1
		previousIndex := lastIndex - 1
		for previousIndex >= 0 && isWhitespaceComponent(valueValues[previousIndex]) {
			previousIndex--
		}
		last := valueValues[lastIndex]
		previous := ComponentValue{}
		if previousIndex >= 0 {
			previous = valueValues[previousIndex]
		}
		if last.Kind == ComponentToken && last.Token.Kind == TokenIdent && equalASCIIFold(last.Token.Value, "important") &&
			previous.Kind == ComponentToken && previous.Token.Kind == TokenDelim && previous.Token.Value == "!" {
			important = true
			valueValues = trimComponentWhitespace(valueValues[:previousIndex])
		}
	}

	valueStart := values[colon].Span.End
	valueEnd := valueStart
	if len(valueValues) > 0 {
		valueStart = valueValues[0].Span.Start
		valueEnd = valueValues[len(valueValues)-1].Span.End
	}
	value := cleanDeclarationFragment(source[valueStart:valueEnd])
	if value == "" && !custom {
		return SourcedDeclaration{}, false
	}
	if custom && !ValidCustomPropertyValue(value) {
		return SourcedDeclaration{}, false
	}

	spanEnd := values[len(values)-1].Span.End
	return SourcedDeclaration{
		Declaration: Declaration{Property: property, Value: value, Important: important},
		Source: DeclarationSource{
			Span:      offsetSpan(Span{Start: nameValues[0].Span.Start, End: spanEnd}, baseOffset),
			NameSpan:  offsetSpan(nameValues[0].Span, baseOffset),
			ValueSpan: offsetSpan(Span{Start: valueStart, End: valueEnd}, baseOffset),
		},
	}, true
}

func trimComponentWhitespace(values []ComponentValue) []ComponentValue {
	start := 0
	for start < len(values) && isWhitespaceComponent(values[start]) {
		start++
	}
	end := len(values)
	for end > start && isWhitespaceComponent(values[end-1]) {
		end--
	}
	return values[start:end]
}

func isWhitespaceComponent(value ComponentValue) bool {
	return value.Kind == ComponentToken && value.Token.Kind == TokenWhitespace
}

func componentValuesContainBadToken(values []ComponentValue) bool {
	for _, value := range values {
		if value.Kind == ComponentToken && (value.Token.Kind == TokenBadString || value.Token.Kind == TokenBadURL || value.Token.Incomplete) {
			return true
		}
		if componentValuesContainBadToken(value.Values) {
			return true
		}
	}
	return false
}

func cleanDeclarationFragment(source string) string {
	cleaned, _ := stripComments(source)
	return strings.TrimSpace(normalizeCommentBoundaries(cleaned))
}

func splitSourcedDeclarations(sourced []SourcedDeclaration) ([]Declaration, []DeclarationSource) {
	declarations := make([]Declaration, len(sourced))
	sources := make([]DeclarationSource, len(sourced))
	for index := range sourced {
		declarations[index] = sourced[index].Declaration
		sources[index] = sourced[index].Source
	}
	return declarations, sources
}

func offsetSpan(span Span, offset int) Span {
	span.Start += offset
	span.End += offset
	return span
}
