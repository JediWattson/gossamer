package browser

import (
	"context"
	"encoding/base64"
	"fmt"
	"net/url"
	"strings"

	"github.com/JediWattson/gossamer/internal/css"
	"github.com/JediWattson/gossamer/internal/render"
	"github.com/JediWattson/gossamer/internal/resource"
	computed "github.com/JediWattson/gossamer/internal/style"
)

const (
	maxNavigationFontFaces = 128
	maxNavigationFontBytes = 64 << 20
)

type loadedFontFace struct {
	rule css.FontFaceRule
	face render.WebFontFace
}

type fontLoadCacheEntry struct {
	data   []byte
	failed bool
}

type fontLoadBudget struct {
	faces int
	bytes int
	cache map[string]fontLoadCacheEntry
}

func newFontLoadBudget() *fontLoadBudget {
	return &fontLoadBudget{cache: make(map[string]fontLoadCacheEntry)}
}

func loadStylesheetFonts(ctx context.Context, pipeline *resource.Pipeline, stylesheet css.Stylesheet, budget *fontLoadBudget) ([]loadedFontFace, error) {
	if budget == nil {
		budget = newFontLoadBudget()
	}
	loaded := make([]loadedFontFace, 0, len(stylesheet.FontFaces))
	for _, rule := range stylesheet.FontFaces {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if budget.faces >= maxNavigationFontFaces {
			break
		}
		for _, source := range rule.Sources {
			if !supportedFontFormat(source.Format) {
				continue
			}
			data, ok, err := loadFontSource(ctx, pipeline, source.URL, budget)
			if err != nil {
				return nil, err
			}
			if !ok {
				continue
			}
			style := render.FontStyleNormal
			switch rule.Style {
			case "italic":
				style = render.FontStyleItalic
			case "oblique":
				style = render.FontStyleOblique
			}
			loaded = append(loaded, loadedFontFace{
				rule: rule,
				face: render.WebFontFace{
					Family: rule.Family, Style: style,
					WeightMin: rule.Weight.Min, WeightMax: rule.Weight.Max,
					Source: source.URL, Data: data, UnicodeRanges: append([]css.UnicodeRange(nil), rule.UnicodeRanges...),
				},
			})
			budget.faces++
			break
		}
	}
	return loaded, nil
}

func loadFontSource(ctx context.Context, pipeline *resource.Pipeline, source string, budget *fontLoadBudget) ([]byte, bool, error) {
	if cached, ok := budget.cache[source]; ok {
		return cached.data, !cached.failed, nil
	}
	var normalized []byte
	var err error
	if strings.HasPrefix(strings.ToLower(strings.TrimSpace(source)), "data:") {
		var raw []byte
		raw, err = decodeFontDataURL(source)
		if err == nil {
			normalized, err = resource.DecodeFontBytes(raw)
		}
	} else {
		parsed, parseErr := url.Parse(source)
		if parseErr != nil || parsed == nil || (!strings.EqualFold(parsed.Scheme, "http") && !strings.EqualFold(parsed.Scheme, "https")) {
			err = fmt.Errorf("browser: invalid font URL")
		} else {
			asset, fetchErr := pipeline.Fetch(ctx, resource.Reference{Kind: resource.Font, URL: parsed})
			if fetchErr != nil {
				err = fetchErr
			} else if !usableAsset(asset) {
				err = fmt.Errorf("browser: unusable font response status %d", asset.StatusCode)
			} else {
				normalized, err = resource.DecodeFont(asset)
			}
		}
	}
	if err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return nil, false, ctxErr
		}
		budget.cache[source] = fontLoadCacheEntry{failed: true}
		return nil, false, nil
	}
	if budget.bytes+len(normalized) > maxNavigationFontBytes {
		budget.cache[source] = fontLoadCacheEntry{failed: true}
		return nil, false, nil
	}
	budget.bytes += len(normalized)
	budget.cache[source] = fontLoadCacheEntry{data: normalized}
	return normalized, true, nil
}

func supportedFontFormat(format string) bool {
	switch strings.ToLower(strings.TrimSpace(format)) {
	case "", "woff", "woff2", "woff2-variations", "truetype", "opentype":
		return true
	default:
		return false
	}
}

func decodeFontDataURL(source string) ([]byte, error) {
	comma := strings.IndexByte(source, ',')
	if comma < len("data:") {
		return nil, fmt.Errorf("browser: malformed font data URL")
	}
	metadata := strings.Split(source[len("data:"):comma], ";")
	payload := source[comma+1:]
	base64Encoded := false
	for _, parameter := range metadata[1:] {
		if strings.EqualFold(strings.TrimSpace(parameter), "base64") {
			base64Encoded = true
			break
		}
	}
	var data []byte
	var err error
	if base64Encoded {
		if int64(base64.StdEncoding.DecodedLen(len(payload))) > resource.DefaultMaxResourceBytes {
			return nil, resource.ErrResourceTooLarge
		}
		data, err = base64.StdEncoding.DecodeString(payload)
	} else {
		if len(payload) > int(resource.DefaultMaxResourceBytes)*3 {
			return nil, resource.ErrResourceTooLarge
		}
		var decoded string
		decoded, err = url.PathUnescape(payload)
		data = []byte(decoded)
	}
	if err != nil {
		return nil, fmt.Errorf("browser: decode font data URL: %w", err)
	}
	if len(data) > int(resource.DefaultMaxResourceBytes) {
		return nil, resource.ErrResourceTooLarge
	}
	return data, nil
}

func fontFaceConditionsMatch(rule css.FontFaceRule, environment css.MediaEnvironment) bool {
	for _, query := range rule.Media {
		if !css.MediaQueryListMatches(query, environment) {
			return false
		}
	}
	for _, condition := range rule.Supports {
		if !css.SupportsConditionMatches(condition, computed.SupportsDeclaration) {
			return false
		}
	}
	return true
}

func cloneLoadedFontFaces(fonts []loadedFontFace) []loadedFontFace {
	result := make([]loadedFontFace, len(fonts))
	dataBySource := make(map[string][]byte)
	for index, loaded := range fonts {
		result[index] = loaded
		data := dataBySource[loaded.face.Source]
		if data == nil || loaded.face.Source == "" {
			data = append([]byte(nil), loaded.face.Data...)
			if loaded.face.Source != "" {
				dataBySource[loaded.face.Source] = data
			}
		}
		result[index].face.Data = data
		result[index].face.UnicodeRanges = append([]css.UnicodeRange(nil), loaded.face.UnicodeRanges...)
		result[index].rule.Sources = append([]css.FontSource(nil), loaded.rule.Sources...)
		result[index].rule.Media = append([]string(nil), loaded.rule.Media...)
		result[index].rule.Supports = append([]string(nil), loaded.rule.Supports...)
		result[index].rule.UnicodeRanges = append([]css.UnicodeRange(nil), loaded.rule.UnicodeRanges...)
	}
	return result
}

func cloneWebFontFaces(fonts []render.WebFontFace) []render.WebFontFace {
	result := make([]render.WebFontFace, len(fonts))
	dataBySource := make(map[string][]byte)
	for index, face := range fonts {
		result[index] = face
		data := dataBySource[face.Source]
		if data == nil || face.Source == "" {
			data = append([]byte(nil), face.Data...)
			if face.Source != "" {
				dataBySource[face.Source] = data
			}
		}
		result[index].Data = data
		result[index].UnicodeRanges = append([]css.UnicodeRange(nil), face.UnicodeRanges...)
	}
	return result
}
