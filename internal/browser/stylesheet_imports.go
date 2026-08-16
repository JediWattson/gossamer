package browser

import (
	"context"
	"fmt"
	"net/url"
	"strings"

	"github.com/JediWattson/gossamer/internal/css"
	"github.com/JediWattson/gossamer/internal/resource"
	computed "github.com/JediWattson/gossamer/internal/style"
)

const (
	maxStylesheetImportDepth = 16
	maxStylesheetImportCount = 128
	maxStylesheetImportBytes = 32 << 20
)

type stylesheetImportBudget struct {
	count         int
	bytes         int
	nextAnonymous int
}

func loadStylesheetWithImports(
	ctx context.Context,
	pipeline *resource.Pipeline,
	root *resource.Asset,
) (css.Stylesheet, error) {
	if root == nil || root.URL == nil {
		return css.Stylesheet{}, fmt.Errorf("browser: stylesheet has no response URL")
	}
	return loadStylesheetSourceWithImports(ctx, pipeline, root.URL, string(root.Bytes()))
}

func loadStylesheetSourceWithImports(
	ctx context.Context,
	pipeline *resource.Pipeline,
	base *url.URL,
	source string,
) (css.Stylesheet, error) {
	if base == nil {
		return css.Stylesheet{}, fmt.Errorf("browser: stylesheet has no base URL")
	}
	budget := &stylesheetImportBudget{}
	ancestry := map[string]bool{canonicalStylesheetURL(base): true}
	return expandStylesheetImports(ctx, pipeline, base, source, 0, ancestry, budget)
}

func expandStylesheetImports(
	ctx context.Context,
	pipeline *resource.Pipeline,
	base *url.URL,
	source string,
	depth int,
	ancestry map[string]bool,
	budget *stylesheetImportBudget,
) (css.Stylesheet, error) {
	if err := ctx.Err(); err != nil {
		return css.Stylesheet{}, err
	}
	if depth > maxStylesheetImportDepth {
		return css.Stylesheet{}, fmt.Errorf("browser: stylesheet import depth exceeds %d", maxStylesheetImportDepth)
	}
	budget.count++
	budget.bytes += len(source)
	if budget.count > maxStylesheetImportCount {
		return css.Stylesheet{}, fmt.Errorf("browser: stylesheet import count exceeds %d", maxStylesheetImportCount)
	}
	if budget.bytes > maxStylesheetImportBytes {
		return css.Stylesheet{}, fmt.Errorf("browser: stylesheet imports exceed %d bytes", maxStylesheetImportBytes)
	}

	parsed, _ := css.Parse(source)
	flattened := css.Stylesheet{}
	for _, imported := range parsed.Imports {
		if imported.Supports != "" && !importSupportsMatches(imported.Supports) {
			continue
		}
		resolved, ok := resolveStylesheetImport(base, imported.URL)
		if !ok {
			continue
		}
		key := canonicalStylesheetURL(resolved)
		if ancestry[key] || depth == maxStylesheetImportDepth || budget.count >= maxStylesheetImportCount {
			continue
		}
		childAsset, err := pipeline.Fetch(ctx, resource.Reference{Kind: resource.Stylesheet, URL: resolved})
		if err != nil {
			if ctxErr := ctx.Err(); ctxErr != nil {
				return css.Stylesheet{}, ctxErr
			}
			continue
		}
		if !usableAsset(childAsset) || !isCSSAsset(childAsset) || budget.bytes+childAsset.Size() > maxStylesheetImportBytes {
			continue
		}
		ancestry[key] = true
		child, childErr := expandStylesheetImports(ctx, pipeline, childAsset.URL, string(childAsset.Bytes()), depth+1, ancestry, budget)
		delete(ancestry, key)
		if childErr != nil {
			if ctxErr := ctx.Err(); ctxErr != nil {
				return css.Stylesheet{}, ctxErr
			}
			continue
		}
		child = applyImportRuleContext(child, imported, budget)
		appendFlattenedStylesheet(&flattened, child)
	}
	parsed.Imports = nil
	appendFlattenedStylesheet(&flattened, parsed)
	return flattened, nil
}

func applyImportRuleContext(stylesheet css.Stylesheet, imported css.ImportRule, budget *stylesheetImportBudget) css.Stylesheet {
	layer := imported.Layer
	if imported.Layered && layer == "" {
		budget.nextAnonymous++
		// A NUL-prefixed identity cannot collide with an authored CSS identifier.
		layer = fmt.Sprintf("\x00import-%d", budget.nextAnonymous)
	}
	if imported.Layered {
		layers := make([]string, 0, len(stylesheet.LayerOrder)+1)
		layers = append(layers, layer)
		for _, childLayer := range stylesheet.LayerOrder {
			layers = appendUniqueString(layers, layer+"."+childLayer)
		}
		stylesheet.LayerOrder = layers
	}
	for index := range stylesheet.Rules {
		rule := &stylesheet.Rules[index]
		if imported.Layered {
			if rule.Layer == "" {
				rule.Layer = layer
			} else {
				rule.Layer = layer + "." + rule.Layer
			}
		}
		if imported.Media != "" {
			rule.Media = append([]string{imported.Media}, rule.Media...)
		}
	}
	return stylesheet
}

func appendFlattenedStylesheet(destination *css.Stylesheet, source css.Stylesheet) {
	for _, layer := range source.LayerOrder {
		destination.LayerOrder = appendUniqueString(destination.LayerOrder, layer)
	}
	for _, rule := range source.Rules {
		rule.Order = len(destination.Rules)
		destination.Rules = append(destination.Rules, rule)
	}
}

func appendUniqueString(values []string, candidate string) []string {
	for _, value := range values {
		if value == candidate {
			return values
		}
	}
	return append(values, candidate)
}

func importSupportsMatches(source string) bool {
	return css.SupportsImportConditionMatches(source, computed.SupportsDeclaration)
}

func resolveStylesheetImport(base *url.URL, source string) (*url.URL, bool) {
	if base == nil {
		return nil, false
	}
	parsed, err := url.Parse(strings.TrimSpace(source))
	if err != nil {
		return nil, false
	}
	resolved := base.ResolveReference(parsed)
	if (!strings.EqualFold(resolved.Scheme, "http") && !strings.EqualFold(resolved.Scheme, "https")) || resolved.Hostname() == "" {
		return nil, false
	}
	resolved.Scheme = strings.ToLower(resolved.Scheme)
	resolved.Fragment = ""
	return resolved, true
}

func canonicalStylesheetURL(source *url.URL) string {
	if source == nil {
		return ""
	}
	clone := *source
	clone.Fragment = ""
	clone.Scheme = strings.ToLower(clone.Scheme)
	return clone.String()
}
