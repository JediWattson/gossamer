package browser

import (
	"context"
	"fmt"
	"net/url"
	"strings"

	"github.com/JediWattson/gossamer/internal/js/lexer"
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
		specifiers, err := moduleSpecifiers(source.Source)
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

func moduleSpecifiers(source string) ([]string, error) {
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
		if token.kind != moduleIdentifier || (token.text != "import" && token.text != "export") {
			continue
		}
		if token.text == "import" && index+1 < len(tokens) {
			next := tokens[index+1]
			if next.kind == modulePunctuator && next.text == "(" {
				if index+3 < len(tokens) && tokens[index+2].kind == moduleString &&
					tokens[index+3].kind == modulePunctuator && tokens[index+3].text == ")" {
					result = append(result, tokens[index+2].text)
				}
				continue
			}
		}
		if braceDepth != 0 || parenDepth != 0 {
			continue
		}
		if token.text == "import" && index+1 < len(tokens) {
			next := tokens[index+1]
			if next.kind == moduleString {
				result = append(result, next.text)
				index++
				continue
			}
			if next.kind == modulePunctuator && next.text == "." {
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
	lexed, err := lexer.LexSurface(source)
	if err != nil {
		return nil, err
	}
	tokens := make([]moduleLexeme, 0, len(lexed))
	for _, token := range lexed {
		switch token.Kind {
		case lexer.EOF:
			continue
		case lexer.Import, lexer.Export:
			tokens = append(tokens, moduleLexeme{kind: moduleIdentifier, text: token.Lexeme})
		case lexer.Identifier:
			tokens = append(tokens, moduleLexeme{kind: moduleIdentifier, text: token.Text})
		case lexer.String, lexer.TemplateTail:
			tokens = append(tokens, moduleLexeme{kind: moduleString, text: token.Text})
		default:
			tokens = append(tokens, moduleLexeme{kind: modulePunctuator, text: token.Lexeme})
		}
	}
	return tokens, nil
}
