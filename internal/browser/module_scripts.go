package browser

import (
	"context"
	"fmt"
	"net/url"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/JediWattson/gossamer/internal/resource"
)

func loadModuleGraph(ctx context.Context, pipeline *resource.Pipeline, root ScriptSource) (ModuleGraph, error) {
	if strings.TrimSpace(root.URL) == "" {
		return ModuleGraph{}, fmt.Errorf("browser: module root has no URL")
	}
	graph := ModuleGraph{RootURL: root.URL}
	visited := make(map[string]bool)
	var visit func(ScriptSource) error
	visit = func(source ScriptSource) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		if visited[source.URL] {
			return nil
		}
		visited[source.URL] = true
		graph.Sources = append(graph.Sources, source)
		specifiers, err := staticModuleSpecifiers(source.Source)
		if err != nil {
			return fmt.Errorf("browser: scan module %q: %w", source.URL, err)
		}
		for _, specifier := range specifiers {
			resolved, err := resolveModuleSpecifier(source.URL, specifier)
			if err != nil {
				return err
			}
			graph.Resolutions = append(graph.Resolutions, ModuleResolution{
				Referrer: source.URL, Specifier: specifier, URL: resolved.String(),
			})
			if visited[resolved.String()] {
				continue
			}
			if pipeline == nil {
				return ErrResourceLoaderUnavailable
			}
			asset, fetchErr := pipeline.Fetch(ctx, resource.Reference{Kind: resource.Script, URL: resolved})
			if fetchErr != nil {
				return fetchErr
			}
			if !usableAsset(asset) {
				return fmt.Errorf("browser: unusable module response status %d", asset.StatusCode)
			}
			if !isJavaScriptAsset(asset) {
				return fmt.Errorf("browser: module response has unsupported MIME type")
			}
			dependencyURL := resolved.String()
			if asset.URL != nil {
				finalURL := cloneURL(asset.URL)
				finalURL.Fragment = resolved.Fragment
				dependencyURL = finalURL.String()
				graph.Resolutions[len(graph.Resolutions)-1].URL = dependencyURL
			}
			if err := visit(ScriptSource{URL: dependencyURL, Source: string(asset.Bytes())}); err != nil {
				return err
			}
		}
		return nil
	}
	if err := visit(root); err != nil {
		return ModuleGraph{}, err
	}
	return graph, nil
}

func resolveModuleSpecifier(referrer, specifier string) (*url.URL, error) {
	base, err := url.Parse(referrer)
	if err != nil || !base.IsAbs() {
		return nil, fmt.Errorf("browser: invalid module referrer %q", referrer)
	}
	trimmed := strings.TrimSpace(specifier)
	if trimmed == "" {
		return nil, fmt.Errorf("browser: empty module specifier from %q", referrer)
	}
	reference, err := url.Parse(trimmed)
	if err != nil {
		return nil, fmt.Errorf("browser: invalid module specifier %q from %q: %w", specifier, referrer, err)
	}
	if reference.Scheme == "" && !strings.HasPrefix(trimmed, "/") &&
		!strings.HasPrefix(trimmed, "./") && !strings.HasPrefix(trimmed, "../") &&
		!strings.HasPrefix(trimmed, "//") {
		return nil, fmt.Errorf("browser: bare module specifier %q is unsupported", specifier)
	}
	resolved := base.ResolveReference(reference)
	if resolved.Scheme != "http" && resolved.Scheme != "https" {
		return nil, fmt.Errorf("browser: unsupported module URL %q", resolved)
	}
	return resolved, nil
}

type moduleLexemeKind uint8

const (
	moduleIdentifier moduleLexemeKind = iota + 1
	moduleString
	modulePunctuator
)

type moduleLexeme struct {
	kind moduleLexemeKind
	text string
}

func staticModuleSpecifiers(source string) ([]string, error) {
	tokens, err := lexModuleSurface(source)
	if err != nil {
		return nil, err
	}
	var result []string
	braceDepth := 0
	parenDepth := 0
	for index := 0; index < len(tokens); index++ {
		token := tokens[index]
		if token.kind == modulePunctuator {
			switch token.text {
			case "{":
				braceDepth++
			case "}":
				if braceDepth > 0 {
					braceDepth--
				}
			case "(":
				parenDepth++
			case ")":
				if parenDepth > 0 {
					parenDepth--
				}
			}
			continue
		}
		if token.kind != moduleIdentifier || braceDepth != 0 || parenDepth != 0 ||
			(token.text != "import" && token.text != "export") {
			continue
		}
		if token.text == "import" && index+1 < len(tokens) {
			next := tokens[index+1]
			if next.kind == moduleString {
				result = append(result, next.text)
				index++
				continue
			}
			if next.kind == modulePunctuator && (next.text == "(" || next.text == ".") {
				continue
			}
		}
		for cursor := index + 1; cursor+1 < len(tokens); cursor++ {
			if tokens[cursor].kind == modulePunctuator && tokens[cursor].text == ";" {
				break
			}
			if tokens[cursor].kind == moduleIdentifier && tokens[cursor].text == "from" &&
				tokens[cursor+1].kind == moduleString {
				result = append(result, tokens[cursor+1].text)
				index = cursor + 1
				break
			}
		}
	}
	return result, nil
}

func lexModuleSurface(source string) ([]moduleLexeme, error) {
	tokens := make([]moduleLexeme, 0, len(source)/5)
	for offset := 0; offset < len(source); {
		r, width := utf8.DecodeRuneInString(source[offset:])
		if r == utf8.RuneError && width == 1 {
			return nil, fmt.Errorf("invalid UTF-8 at byte %d", offset)
		}
		if unicode.IsSpace(r) {
			offset += width
			continue
		}
		if strings.HasPrefix(source[offset:], "//") {
			offset += 2
			for offset < len(source) && source[offset] != '\n' && source[offset] != '\r' {
				offset++
			}
			continue
		}
		if strings.HasPrefix(source[offset:], "/*") {
			end := strings.Index(source[offset+2:], "*/")
			if end < 0 {
				return nil, fmt.Errorf("unterminated block comment")
			}
			offset += end + 4
			continue
		}
		if r == '\'' || r == '"' {
			value, next, err := scanModuleString(source, offset, byte(r))
			if err != nil {
				return nil, err
			}
			tokens = append(tokens, moduleLexeme{kind: moduleString, text: value})
			offset = next
			continue
		}
		if r == '`' {
			next, err := skipModuleTemplate(source, offset)
			if err != nil {
				return nil, err
			}
			offset = next
			continue
		}
		if r == '$' || r == '_' || unicode.IsLetter(r) {
			start := offset
			offset += width
			for offset < len(source) {
				next, nextWidth := utf8.DecodeRuneInString(source[offset:])
				if next != '$' && next != '_' && !unicode.IsLetter(next) && !unicode.IsDigit(next) {
					break
				}
				offset += nextWidth
			}
			tokens = append(tokens, moduleLexeme{kind: moduleIdentifier, text: source[start:offset]})
			continue
		}
		tokens = append(tokens, moduleLexeme{kind: modulePunctuator, text: string(r)})
		offset += width
	}
	return tokens, nil
}

func scanModuleString(source string, start int, quote byte) (string, int, error) {
	var value strings.Builder
	for offset := start + 1; offset < len(source); offset++ {
		current := source[offset]
		if current == quote {
			return value.String(), offset + 1, nil
		}
		if current == '\n' || current == '\r' {
			return "", 0, fmt.Errorf("unterminated module string at byte %d", start)
		}
		if current == '\\' {
			offset++
			if offset >= len(source) {
				break
			}
			escaped := source[offset]
			switch escaped {
			case '\\', '\'', '"':
				value.WriteByte(escaped)
			case 'n':
				value.WriteByte('\n')
			case 'r':
				value.WriteByte('\r')
			case 't':
				value.WriteByte('\t')
			default:
				value.WriteByte(escaped)
			}
			continue
		}
		value.WriteByte(current)
	}
	return "", 0, fmt.Errorf("unterminated module string at byte %d", start)
}

func skipModuleTemplate(source string, start int) (int, error) {
	for offset := start + 1; offset < len(source); offset++ {
		if source[offset] == '\\' {
			offset++
			continue
		}
		if source[offset] == '`' {
			return offset + 1, nil
		}
	}
	return 0, fmt.Errorf("unterminated template literal at byte %d", start)
}
