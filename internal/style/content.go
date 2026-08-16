package style

import (
	"strings"

	"github.com/JediWattson/gossamer/internal/css"
	"github.com/JediWattson/gossamer/internal/dom"
)

type contentKind uint8

const (
	contentNormal contentKind = iota
	contentNone
	contentItems
)

type contentItemKind uint8

const (
	contentString contentItemKind = iota
	contentAttribute
)

type contentItem struct {
	kind  contentItemKind
	value string
}

// ContentValue is the immutable computed value of the content property. The
// current generated-content slice supports string tokens and attr(<ident>) in
// addition to normal and none.
type ContentValue struct {
	kind  contentKind
	items []contentItem
}

// GeneratedText evaluates a string/attribute content list for origin. The
// boolean distinguishes an empty generated string from normal or none, which
// suppress pseudo-element box generation.
func (content ContentValue) GeneratedText(origin *dom.Node) (string, bool) {
	if content.kind != contentItems {
		return "", false
	}
	var result strings.Builder
	for _, item := range content.items {
		switch item.kind {
		case contentAttribute:
			if value, ok := attribute(origin, item.value); ok {
				result.WriteString(value)
			}
		default:
			result.WriteString(item.value)
		}
	}
	return result.String(), true
}

func parseContentValue(source string) (ContentValue, bool) {
	values, err := css.ParseComponentValues(source)
	if err != nil {
		return ContentValue{}, false
	}
	values = trimValueWhitespace(values)
	if len(values) == 1 {
		if keyword, ok := componentKeyword(values[0]); ok {
			switch keyword {
			case "normal":
				return ContentValue{kind: contentNormal}, true
			case "none":
				return ContentValue{kind: contentNone}, true
			}
		}
	}

	items := make([]contentItem, 0, len(values))
	for _, value := range values {
		if valueWhitespace(value) {
			continue
		}
		if token, ok := componentToken(value); ok {
			if token.Kind != css.TokenString || token.Incomplete {
				return ContentValue{}, false
			}
			items = append(items, contentItem{kind: contentString, value: token.Value})
			continue
		}
		if value.Kind != css.ComponentFunction || !equalASCIIValue(value.Token.Value, "attr") {
			return ContentValue{}, false
		}
		arguments := trimValueWhitespace(value.Values)
		if len(arguments) != 1 {
			return ContentValue{}, false
		}
		name, ok := componentToken(arguments[0])
		if !ok || name.Kind != css.TokenIdent || name.Incomplete {
			return ContentValue{}, false
		}
		items = append(items, contentItem{kind: contentAttribute, value: lowerASCIIValue(name.Value)})
	}
	if len(items) == 0 {
		return ContentValue{}, false
	}
	return ContentValue{kind: contentItems, items: items}, true
}

func serializeContentValue(content ContentValue) string {
	switch content.kind {
	case contentNone:
		return "none"
	case contentItems:
		parts := make([]string, 0, len(content.items))
		for _, item := range content.items {
			if item.kind == contentAttribute {
				parts = append(parts, "attr("+item.value+")")
			} else {
				parts = append(parts, quoteCSSString(item.value))
			}
		}
		return strings.Join(parts, " ")
	default:
		return "normal"
	}
}

func equalASCIIValue(left, right string) bool {
	return lowerASCIIValue(left) == lowerASCIIValue(right)
}
