package browser

import (
	"context"
	"errors"
	"fmt"
	"mime"
	"net/url"
	"strconv"
	"strings"

	"github.com/JediWattson/gossamer/internal/dom"
	"github.com/JediWattson/gossamer/internal/resource"
	browserruntime "github.com/JediWattson/gossamer/internal/runtime"
)

type navigationScript struct {
	source   ScriptSource
	external *resource.Reference
}

type navigationScriptResult struct {
	source ScriptSource
	err    error
}

func discoverNavigationScripts(document *dom.Document, location *url.URL) ([]navigationScript, error) {
	graph, err := resource.Discover(document.Root(), location)
	if err != nil {
		return nil, err
	}
	external := make(map[*dom.Node]resource.Reference)
	for _, reference := range graph.References {
		if reference.Kind == resource.Script && reference.Attribute == "src" {
			external[reference.Node] = reference
		}
	}

	var scripts []navigationScript
	inlineIndex := 0
	var visit func(*dom.Node)
	visit = func(node *dom.Node) {
		if node == nil {
			return
		}
		if node.Type == dom.ElementNode && strings.EqualFold(node.Data, "script") && classicScript(node) {
			if reference, ok := external[node]; ok {
				copy := reference
				copy.URL = cloneURL(reference.URL)
				scripts = append(scripts, navigationScript{
					source:   ScriptSource{URL: copy.URL.String()},
					external: &copy,
				})
			} else if _, hasSource := nodeAttribute(node, "src"); !hasSource {
				inlineIndex++
				scripts = append(scripts, navigationScript{source: ScriptSource{
					URL:    inlineScriptURL(location, inlineIndex),
					Source: scriptText(node),
				}})
			}
		}
		for _, child := range node.Children {
			visit(child)
		}
	}
	visit(document.Root())
	return scripts, nil
}

func classicScript(node *dom.Node) bool {
	typeValue, _ := nodeAttribute(node, "type")
	typeValue = strings.ToLower(strings.TrimSpace(typeValue))
	return typeValue == "" || typeValue == "text/javascript" ||
		typeValue == "application/javascript" || typeValue == "text/ecmascript" ||
		typeValue == "application/ecmascript"
}

func scriptText(node *dom.Node) string {
	var builder strings.Builder
	for _, child := range node.Children {
		if child.Type == dom.TextNode {
			builder.WriteString(child.Data)
		}
	}
	return builder.String()
}

func inlineScriptURL(location *url.URL, index int) string {
	copy := cloneURL(location)
	copy.Fragment = "inline-script-" + strconv.Itoa(index)
	return copy.String()
}

func (page *Page) loadNavigationScripts(
	ctx context.Context,
	id NavigationID,
	generation DocumentGeneration,
	fetcher resource.Fetcher,
	scripts []navigationScript,
) {
	var pipeline *resource.Pipeline
	if fetcher != nil {
		pipeline = resource.NewPipeline(fetcher, resource.PipelineOptions{})
	}
	err := loadNavigationScriptSequence(ctx, pipeline, scripts, func(result navigationScriptResult) error {
		_, _, enqueueErr := page.browser.scheduler.EnqueueExternalTask(page.Realm, func(task *browserruntime.TaskContext) error {
			return page.applyNavigationScript(task, id, generation, result)
		})
		return enqueueErr
	})
	if err == nil {
		return
	}
	if ctxErr := ctx.Err(); ctxErr != nil {
		page.enqueueNavigationFailure(id, fmt.Errorf("browser: load page scripts: %w", ctxErr))
		return
	}
	page.enqueueNavigationFailure(id, fmt.Errorf("browser: load page scripts: %w", err))
}

func loadNavigationScriptSequence(
	ctx context.Context,
	pipeline *resource.Pipeline,
	scripts []navigationScript,
	deliver func(navigationScriptResult) error,
) error {
	for _, script := range scripts {
		if err := ctx.Err(); err != nil {
			return err
		}
		result := navigationScriptResult{source: script.source}
		if script.external != nil {
			if pipeline == nil {
				result.err = ErrResourceLoaderUnavailable
				if err := deliver(result); err != nil {
					return err
				}
				continue
			}
			asset, err := pipeline.Fetch(ctx, *script.external)
			if err != nil {
				if ctxErr := ctx.Err(); ctxErr != nil {
					return ctxErr
				}
				result.err = err
			} else if !usableAsset(asset) {
				result.err = fmt.Errorf("browser: unusable script response status %d", asset.StatusCode)
			} else if !isJavaScriptAsset(asset) {
				result.err = fmt.Errorf("browser: script response has unsupported MIME type")
			} else {
				result.source.Source = string(asset.Bytes())
				if asset.URL != nil {
					result.source.URL = asset.URL.String()
				}
			}
		}
		if err := deliver(result); err != nil {
			return err
		}
	}
	return nil
}

func (page *Page) applyNavigationScript(
	task *browserruntime.TaskContext,
	id NavigationID,
	generation DocumentGeneration,
	result navigationScriptResult,
) error {
	page.mutex.RLock()
	if !page.matchesNavigationLocked(id, generation) || page.navigation.state != NavigationLoadingScripts {
		page.mutex.RUnlock()
		return nil
	}
	script := page.script
	page.mutex.RUnlock()

	scriptErr := result.err
	if scriptErr == nil && script != nil {
		host := &taskHost{page: page, task: task, generation: generation, autoRender: false}
		scriptErr = errors.Join(
			script.Evaluate(host, result.source),
			script.DrainMicrotasks(host),
			host.finish(),
		)
	}

	page.mutex.Lock()
	defer page.mutex.Unlock()
	if !page.matchesNavigationLocked(id, generation) || page.navigation.state != NavigationLoadingScripts {
		return nil
	}
	if scriptErr != nil {
		page.navigation.scriptsFailed++
	}
	if page.navigation.scriptsPending > 0 {
		page.navigation.scriptsPending--
	}
	if page.navigation.scriptsPending != 0 {
		return nil
	}
	page.navigation.state = NavigationRendering
	return page.queueNavigationRenderLocked(task, id, generation)
}

func isJavaScriptAsset(asset *resource.Asset) bool {
	mediaType, _, err := mime.ParseMediaType(asset.ContentType())
	if err != nil {
		return false
	}
	switch strings.ToLower(mediaType) {
	case "text/javascript", "application/javascript", "text/ecmascript", "application/ecmascript":
		return true
	default:
		return false
	}
}
