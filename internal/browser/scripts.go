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
	kind     navigationScriptKind
	mode     navigationScriptMode
	source   ScriptSource
	external *resource.Reference
}

type navigationScriptKind uint8

const (
	navigationClassicScript navigationScriptKind = iota + 1
	navigationModuleScript
)

type navigationScriptMode uint8

const (
	navigationBlockingScript navigationScriptMode = iota + 1
	navigationDeferredScript
	navigationAsyncScript
)

type navigationScriptResultKind uint8

const (
	navigationScriptExecution navigationScriptResultKind = iota + 1
	navigationReadyInteractive
	navigationDOMContentLoaded
	navigationReadyComplete
)

type navigationScriptResult struct {
	kind   navigationScriptResultKind
	script navigationScript
	source ScriptSource
	module ModuleGraph
	err    error
}

type dynamicScriptRequest struct {
	target NodeHandle
	script navigationScript
	err    error
}

type dynamicScriptResult struct {
	target NodeHandle
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
		if node.Type == dom.ElementNode && strings.EqualFold(node.Data, "script") {
			kind, supported := scriptKind(node)
			if !supported {
				for _, child := range node.Children {
					visit(child)
				}
				return
			}
			if kind == navigationClassicScript {
				if _, noModule := nodeAttribute(node, "nomodule"); noModule {
					return
				}
			}
			script := navigationScript{kind: kind, mode: scriptMode(node, kind)}
			if reference, ok := external[node]; ok {
				copy := reference
				copy.URL = cloneURL(reference.URL)
				script.source = ScriptSource{URL: copy.URL.String()}
				script.external = &copy
				scripts = append(scripts, script)
			} else if _, hasSource := nodeAttribute(node, "src"); !hasSource {
				inlineIndex++
				script.source = ScriptSource{
					URL:    inlineScriptURL(location, inlineIndex),
					Source: scriptText(node),
				}
				scripts = append(scripts, script)
			}
		}
		for _, child := range node.Children {
			visit(child)
		}
	}
	visit(document.Root())
	return scripts, nil
}

func scriptKind(node *dom.Node) (navigationScriptKind, bool) {
	typeValue, _ := nodeAttribute(node, "type")
	typeValue = strings.ToLower(strings.TrimSpace(typeValue))
	if typeValue == "module" {
		return navigationModuleScript, true
	}
	if typeValue == "" || typeValue == "text/javascript" ||
		typeValue == "application/javascript" || typeValue == "text/ecmascript" ||
		typeValue == "application/ecmascript" {
		return navigationClassicScript, true
	}
	return 0, false
}

func scriptMode(node *dom.Node, kind navigationScriptKind) navigationScriptMode {
	_, external := nodeAttribute(node, "src")
	if _, async := nodeAttribute(node, "async"); async && (external || kind == navigationModuleScript) {
		return navigationAsyncScript
	}
	if kind == navigationModuleScript {
		return navigationDeferredScript
	}
	if _, deferred := nodeAttribute(node, "defer"); deferred && external {
		return navigationDeferredScript
	}
	return navigationBlockingScript
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

func documentScriptNodes(document *dom.Document) map[dom.NodeID]struct{} {
	started := make(map[dom.NodeID]struct{})
	if document == nil {
		return started
	}
	var visit func(*dom.Node)
	visit = func(node *dom.Node) {
		if node == nil {
			return
		}
		if node.Type == dom.ElementNode && strings.EqualFold(node.Data, "script") {
			if id, ok := document.ID(node); ok {
				started[id] = struct{}{}
			}
		}
		for _, child := range node.Children {
			visit(child)
		}
	}
	visit(document.Root())
	return started
}

func cloneStartedScripts(source map[dom.NodeID]struct{}) map[dom.NodeID]struct{} {
	clone := make(map[dom.NodeID]struct{}, len(source))
	for id := range source {
		clone[id] = struct{}{}
	}
	return clone
}

// syncAndLoadDynamicScripts discovers classic external scripts that became
// connected after the active document was published. Fetch and evaluation are
// kept outside the mutating Realm entry; completion returns through the Page
// queue and fires load or error on the originating element.
func (page *Page) syncAndLoadDynamicScripts() error {
	page.mutex.Lock()
	if page.closed {
		page.mutex.Unlock()
		return ErrPageClosed
	}
	requests, err := page.dynamicScriptRequestsLocked()
	if err != nil {
		page.mutex.Unlock()
		return err
	}
	if len(requests) == 0 || page.documentContext == nil {
		page.mutex.Unlock()
		return nil
	}
	ctx := page.documentContext
	generation := page.documentGeneration
	fetcher := page.resourceFetcher
	page.mutex.Unlock()

	go page.loadDynamicScripts(ctx, generation, fetcher, requests)
	return nil
}

func (page *Page) dynamicScriptRequestsLocked() ([]dynamicScriptRequest, error) {
	if page.document == nil || page.location == nil {
		return nil, nil
	}
	if page.startedScripts == nil {
		page.startedScripts = make(map[dom.NodeID]struct{})
	}
	graph, err := resource.Discover(page.document.Root(), page.location)
	if err != nil {
		return nil, err
	}
	external := make(map[*dom.Node]resource.Reference)
	for _, reference := range graph.References {
		if reference.Kind == resource.Script && reference.Attribute == "src" {
			external[reference.Node] = reference
		}
	}

	var requests []dynamicScriptRequest
	var visit func(*dom.Node)
	visit = func(node *dom.Node) {
		if node == nil {
			return
		}
		if node.Type == dom.ElementNode && strings.EqualFold(node.Data, "script") {
			id, indexed := page.document.ID(node)
			_, started := page.startedScripts[id]
			if indexed && !started {
				source, hasSource := nodeAttribute(node, "src")
				if hasSource && strings.TrimSpace(source) != "" {
					page.startedScripts[id] = struct{}{}
					kind, supported := scriptKind(node)
					_, noModule := nodeAttribute(node, "nomodule")
					if !supported || kind != navigationClassicScript || noModule {
						// Non-JavaScript data blocks, modules, and nomodule scripts are
						// outside this classic-script slice. Preparing the connected
						// element is still a one-time decision, but it is inert rather
						// than an element error.
					} else {
						request := dynamicScriptRequest{target: NodeHandle{Document: page.documentGeneration, Node: id}}
						if reference, ok := external[node]; ok {
							copy := reference
							copy.URL = cloneURL(reference.URL)
							request.script = navigationScript{
								kind:     navigationClassicScript,
								mode:     navigationAsyncScript,
								source:   ScriptSource{URL: copy.URL.String()},
								external: &copy,
							}
						} else {
							request.err = fmt.Errorf("browser: dynamically inserted script has an invalid source URL")
						}
						requests = append(requests, request)
					}
				}
			}
		}
		for _, child := range node.Children {
			visit(child)
		}
	}
	visit(page.document.Root())
	return requests, nil
}

func (page *Page) loadDynamicScripts(
	ctx context.Context,
	generation DocumentGeneration,
	fetcher resource.Fetcher,
	requests []dynamicScriptRequest,
) {
	var pipeline *resource.Pipeline
	if fetcher != nil {
		pipeline = resource.NewPipeline(fetcher, resource.PipelineOptions{})
	}
	for _, request := range requests {
		if ctx.Err() != nil {
			return
		}
		result := dynamicScriptResult{target: request.target, err: request.err}
		if result.err == nil {
			loaded := loadNavigationScript(ctx, pipeline, request.script)
			result.source = loaded.source
			result.err = loaded.err
		}
		_, _, err := page.browser.scheduler.EnqueueExternalTask(page.Realm, func(task *browserruntime.TaskContext) error {
			return page.applyDynamicScript(task, generation, result)
		})
		if err != nil {
			return
		}
	}
}

func (page *Page) applyDynamicScript(
	task *browserruntime.TaskContext,
	generation DocumentGeneration,
	result dynamicScriptResult,
) error {
	page.mutex.RLock()
	if page.closed || page.documentGeneration != generation {
		page.mutex.RUnlock()
		return nil
	}
	script := page.script
	_, targetExists := page.document.Resolve(result.target.Node)
	page.mutex.RUnlock()
	if script == nil || !targetExists {
		return nil
	}

	scriptErr := result.err
	if scriptErr == nil {
		host := beginTaskHost(&taskHost{page: page, task: task, generation: generation, autoRender: true})
		scriptErr = script.Evaluate(host, result.source)
		scriptErr = errors.Join(scriptErr, script.DrainMicrotasks(host), host.finish())
	}
	eventType := InputLoad
	if scriptErr != nil {
		eventType = InputError
	}
	host := beginTaskHost(&taskHost{page: page, task: task, generation: generation, autoRender: true})
	_, dispatchErr := script.DispatchEvent(host, InputEvent{Type: eventType, Target: result.target})
	return errors.Join(dispatchErr, script.DrainMicrotasks(host), host.finish())
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
	deliver := func(result navigationScriptResult) error {
		_, _, enqueueErr := page.browser.scheduler.EnqueueExternalTask(page.Realm, func(task *browserruntime.TaskContext) error {
			return page.applyNavigationScript(task, id, generation, result)
		})
		return enqueueErr
	}
	err := loadNavigationScriptSequence(ctx, pipeline, scripts, deliver)
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
	blocking := make([]navigationScript, 0, len(scripts))
	deferred := make([]navigationScript, 0, len(scripts))
	asyncScripts := make([]navigationScript, 0, len(scripts))
	for _, script := range scripts {
		switch script.mode {
		case navigationAsyncScript:
			asyncScripts = append(asyncScripts, script)
		case navigationDeferredScript:
			deferred = append(deferred, script)
		default:
			blocking = append(blocking, script)
		}
	}

	asyncDone := make(chan error, len(asyncScripts))
	for _, script := range asyncScripts {
		script := script
		go func() {
			asyncDone <- deliver(loadNavigationScript(ctx, pipeline, script))
		}()
	}
	for _, script := range blocking {
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := deliver(loadNavigationScript(ctx, pipeline, script)); err != nil {
			return err
		}
	}
	if err := deliver(navigationScriptResult{kind: navigationReadyInteractive}); err != nil {
		return err
	}
	for _, script := range deferred {
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := deliver(loadNavigationScript(ctx, pipeline, script)); err != nil {
			return err
		}
	}
	if err := deliver(navigationScriptResult{kind: navigationDOMContentLoaded}); err != nil {
		return err
	}
	for range asyncScripts {
		if err := <-asyncDone; err != nil {
			return err
		}
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	return deliver(navigationScriptResult{kind: navigationReadyComplete})
}

func loadNavigationScript(ctx context.Context, pipeline *resource.Pipeline, script navigationScript) navigationScriptResult {
	result := navigationScriptResult{kind: navigationScriptExecution, script: script, source: script.source}
	if script.external != nil {
		if pipeline == nil {
			result.err = ErrResourceLoaderUnavailable
			return result
		}
		asset, err := pipeline.Fetch(ctx, *script.external)
		if err != nil {
			result.err = err
			return result
		}
		if !usableAsset(asset) {
			result.err = fmt.Errorf("browser: unusable script response status %d", asset.StatusCode)
			return result
		}
		if !isJavaScriptAsset(asset) {
			result.err = fmt.Errorf("browser: script response has unsupported MIME type")
			return result
		}
		result.source.Source = string(asset.Bytes())
		if asset.URL != nil {
			finalURL := cloneURL(asset.URL)
			finalURL.Fragment = script.external.URL.Fragment
			result.source.URL = finalURL.String()
		}
	}
	if script.kind == navigationModuleScript && result.err == nil {
		result.module, result.err = loadModuleGraph(ctx, pipeline, result.source)
	}
	return result
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

	switch result.kind {
	case navigationReadyInteractive:
		page.mutex.Lock()
		if page.matchesNavigationLocked(id, generation) && page.navigation.state == NavigationLoadingScripts {
			page.readyState = "interactive"
		}
		page.mutex.Unlock()
		return nil
	case navigationDOMContentLoaded:
		if err := page.dispatchNavigationLifecycleEvent(task, id, generation, InputDOMContentLoaded); err != nil {
			page.recordNavigationScriptFailure(id, generation, ScriptFailure{
				Phase: "DOMContentLoaded", Message: err.Error(),
			})
		}
		return nil
	case navigationReadyComplete:
		page.mutex.Lock()
		if !page.matchesNavigationLocked(id, generation) || page.navigation.state != NavigationLoadingScripts {
			page.mutex.Unlock()
			return nil
		}
		page.readyState = "complete"
		page.mutex.Unlock()
		if err := page.dispatchNavigationLifecycleEvent(task, id, generation, InputLoad); err != nil {
			page.recordNavigationScriptFailure(id, generation, ScriptFailure{
				Phase: "load", Message: err.Error(),
			})
		}
		if err := page.dispatchWindowNavigationLifecycleEvent(task, id, generation, InputPageShow, false); err != nil {
			page.recordNavigationScriptFailure(id, generation, ScriptFailure{
				Phase: "pageshow", Message: err.Error(),
			})
		}
		page.mutex.Lock()
		defer page.mutex.Unlock()
		if !page.matchesNavigationLocked(id, generation) || page.navigation.state != NavigationLoadingScripts {
			return nil
		}
		page.navigation.state = NavigationRendering
		return page.queueNavigationRenderLocked(task, id, generation)
	case navigationScriptExecution:
	default:
		return fmt.Errorf("browser: unknown navigation script result %d", result.kind)
	}

	phase := "load"
	scriptErr := result.err
	if scriptErr == nil && script != nil {
		phase = "evaluate"
		host := beginTaskHost(&taskHost{page: page, task: task, generation: generation, autoRender: false})
		if result.script.kind == navigationModuleScript {
			moduleRealm, ok := script.(JSModuleRealm)
			if !ok {
				scriptErr = ErrModuleScriptsUnsupported
			} else {
				scriptErr = moduleRealm.EvaluateModule(host, result.module)
			}
		} else {
			scriptErr = script.Evaluate(host, result.source)
		}
		scriptErr = errors.Join(scriptErr, script.DrainMicrotasks(host), host.finish())
	}

	page.mutex.Lock()
	defer page.mutex.Unlock()
	if !page.matchesNavigationLocked(id, generation) || page.navigation.state != NavigationLoadingScripts {
		return nil
	}
	if scriptErr != nil {
		page.navigation.scriptsFailed++
		page.appendScriptFailureLocked(ScriptFailure{
			URL: result.source.URL, Phase: phase, Message: scriptErr.Error(),
		})
	}
	if page.navigation.scriptsPending > 0 {
		page.navigation.scriptsPending--
	}
	return nil
}

func (page *Page) recordNavigationScriptFailure(id NavigationID, generation DocumentGeneration, failure ScriptFailure) {
	page.mutex.Lock()
	if page.matchesNavigationLocked(id, generation) && page.navigation.state == NavigationLoadingScripts {
		page.navigation.scriptsFailed++
		page.appendScriptFailureLocked(failure)
	}
	page.mutex.Unlock()
}

func (page *Page) appendScriptFailureLocked(failure ScriptFailure) {
	const (
		maximumScriptFailureDiagnostics = 64
		maximumScriptFailureMessage     = 4096
	)
	if len(page.navigation.scriptFailures) >= maximumScriptFailureDiagnostics {
		return
	}
	failure.URL = strings.TrimSpace(failure.URL)
	failure.Phase = strings.TrimSpace(failure.Phase)
	failure.Message = strings.TrimSpace(failure.Message)
	if len(failure.Message) > maximumScriptFailureMessage {
		failure.Message = failure.Message[:maximumScriptFailureMessage] + "..."
	}
	page.navigation.scriptFailures = append(page.navigation.scriptFailures, failure)
}

func (page *Page) dispatchNavigationLifecycleEvent(
	task *browserruntime.TaskContext,
	id NavigationID,
	generation DocumentGeneration,
	eventType InputEventType,
) error {
	page.mutex.RLock()
	if !page.matchesNavigationLocked(id, generation) || page.navigation.state != NavigationLoadingScripts {
		page.mutex.RUnlock()
		return nil
	}
	script := page.script
	target := NodeHandle{Document: generation, Node: page.document.RootID()}
	page.mutex.RUnlock()
	if script == nil {
		return nil
	}
	host := beginTaskHost(&taskHost{page: page, task: task, generation: generation, autoRender: false})
	_, dispatchErr := script.DispatchEvent(host, InputEvent{Type: eventType, Target: target})
	return errors.Join(dispatchErr, script.DrainMicrotasks(host), host.finish())
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

var ErrModuleScriptsUnsupported = errors.New("browser: JavaScript engine does not support module scripts")
