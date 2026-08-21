// Package webapi contains engine-neutral implementations of browser utility
// algorithms. JavaScript engines expose these values through their own object
// adapters while sharing the same parsing and mutation rules.
package webapi

import (
	"net/url"
	"sort"
	"strings"
)

type SearchParam struct {
	Name  string
	Value string
}

type URLSearchParams struct {
	Pairs []SearchParam
}

func ParseURLSearchParams(input string) URLSearchParams {
	input = strings.TrimPrefix(input, "?")
	if input == "" {
		return URLSearchParams{}
	}
	parts := strings.Split(input, "&")
	result := URLSearchParams{Pairs: make([]SearchParam, 0, len(parts))}
	for _, part := range parts {
		name, value, _ := strings.Cut(part, "=")
		result.Pairs = append(result.Pairs, SearchParam{
			Name: decodeFormComponent(name), Value: decodeFormComponent(value),
		})
	}
	return result
}

func NewURLSearchParams(pairs []SearchParam) URLSearchParams {
	return URLSearchParams{Pairs: append([]SearchParam(nil), pairs...)}
}

func (params *URLSearchParams) Append(name, value string) {
	params.Pairs = append(params.Pairs, SearchParam{Name: name, Value: value})
}

func (params *URLSearchParams) Delete(name string, value *string) {
	kept := params.Pairs[:0]
	for _, pair := range params.Pairs {
		if pair.Name == name && (value == nil || pair.Value == *value) {
			continue
		}
		kept = append(kept, pair)
	}
	params.Pairs = kept
}

func (params URLSearchParams) Get(name string) (string, bool) {
	for _, pair := range params.Pairs {
		if pair.Name == name {
			return pair.Value, true
		}
	}
	return "", false
}

func (params URLSearchParams) GetAll(name string) []string {
	values := make([]string, 0)
	for _, pair := range params.Pairs {
		if pair.Name == name {
			values = append(values, pair.Value)
		}
	}
	return values
}

func (params URLSearchParams) Has(name string, value *string) bool {
	for _, pair := range params.Pairs {
		if pair.Name == name && (value == nil || pair.Value == *value) {
			return true
		}
	}
	return false
}

func (params *URLSearchParams) Set(name, value string) {
	first := -1
	kept := params.Pairs[:0]
	for _, pair := range params.Pairs {
		if pair.Name != name {
			kept = append(kept, pair)
			continue
		}
		if first < 0 {
			first = len(kept)
			kept = append(kept, SearchParam{Name: name, Value: value})
		}
	}
	if first < 0 {
		kept = append(kept, SearchParam{Name: name, Value: value})
	}
	params.Pairs = kept
}

func (params *URLSearchParams) Sort() {
	sort.SliceStable(params.Pairs, func(left, right int) bool {
		return params.Pairs[left].Name < params.Pairs[right].Name
	})
}

func (params URLSearchParams) String() string {
	var result strings.Builder
	for index, pair := range params.Pairs {
		if index != 0 {
			result.WriteByte('&')
		}
		result.WriteString(encodeFormComponent(pair.Name))
		result.WriteByte('=')
		result.WriteString(encodeFormComponent(pair.Value))
	}
	return result.String()
}

func encodeFormComponent(value string) string {
	return strings.ReplaceAll(url.QueryEscape(value), "~", "%7E")
}

func decodeFormComponent(value string) string {
	decoded, err := url.QueryUnescape(value)
	if err == nil {
		return decoded
	}
	return strings.ReplaceAll(value, "+", " ")
}
