package nativeengine

import (
	"fmt"
	"net/url"
	"sort"

	"github.com/JediWattson/gossamer/internal/browser"
	"github.com/JediWattson/gossamer/internal/js/compiler"
	"github.com/JediWattson/gossamer/internal/js/program"
	browserruntime "github.com/JediWattson/gossamer/internal/runtime"
	"github.com/JediWattson/gossamer/internal/runtime/memory"
)

type nativeModuleStatus uint8

const (
	moduleUnlinked nativeModuleStatus = iota
	moduleLinking
	moduleLinked
	moduleEvaluating
	moduleEvaluated
	moduleErrored
)

type nativeModule struct {
	source   browser.ScriptSource
	image    program.Module
	compiled bool
	entry    memory.Ref
	status   nativeModuleStatus
	err      error
}

type moduleResolutionKey struct {
	referrer  string
	specifier string
}

type resolvedModuleExport struct {
	context      memory.Ref
	name         string
	namespaceURL string
	found        bool
	ambiguous    bool
}

type moduleExportKey struct {
	url  string
	name string
}

// EvaluateModule compiles a complete browser-resolved graph, links immutable
// live import aliases, and evaluates dependencies once per Realm.
func (realm *Realm) EvaluateModule(host browser.Host, graph browser.ModuleGraph) error {
	if realm == nil {
		return ErrRealmClosed
	}
	realm.mutex.Lock()
	defer realm.mutex.Unlock()
	if realm.closed {
		return ErrRealmClosed
	}
	if err := validateNativeModuleGraphRoot(graph); err != nil {
		return err
	}
	if existing := realm.modules[graph.RootURL]; existing != nil {
		switch existing.status {
		case moduleEvaluated:
			return nil
		case moduleErrored:
			return existing.err
		}
	}
	compiled, resolutions, compilations, err := compileNativeModuleGraph(graph, realm.modules)
	if err != nil {
		return err
	}
	realm.moduleCompilations += compilations
	for key, destination := range resolutions {
		if previous, exists := realm.moduleResolutions[key]; exists && previous != destination {
			return fmt.Errorf("%w: %q from %q resolved to both %q and %q", ErrModuleGraphInvalid, key.specifier, key.referrer, previous, destination)
		}
	}
	for url, candidate := range compiled {
		if existing, exists := realm.modules[url]; !exists {
			copy := candidate
			realm.modules[url] = &copy
		} else if !existing.compiled && candidate.compiled {
			existing.image = candidate.image
			existing.compiled = true
		}
	}
	for key, destination := range resolutions {
		realm.moduleResolutions[key] = destination
	}

	realm.host = host
	defer func() { realm.host = nil }()
	task, err := runtimeTask(host)
	if err != nil {
		return evaluationError(graph.RootURL, err)
	}
	scope, err := realm.beginTaskLocked(task)
	if err != nil {
		return evaluationError(graph.RootURL, err)
	}
	for url := range compiled {
		if _, err := realm.moduleContextLocked(scope, url); err != nil {
			return evaluationError(url, err)
		}
	}
	if err := realm.linkModuleLocked(scope, graph.RootURL); err != nil {
		return err
	}
	return realm.evaluateModuleLocked(scope, graph.RootURL)
}

func validateNativeModuleGraphRoot(graph browser.ModuleGraph) error {
	if graph.RootURL == "" || len(graph.Sources) == 0 {
		return fmt.Errorf("%w: graph has no root source", ErrModuleGraphInvalid)
	}
	seen := make(map[string]struct{}, len(graph.Sources))
	rootFound := false
	for _, source := range graph.Sources {
		if source.URL == "" {
			return fmt.Errorf("%w: source has no URL", ErrModuleGraphInvalid)
		}
		if _, duplicate := seen[source.URL]; duplicate {
			return fmt.Errorf("%w: duplicate source %q", ErrModuleGraphInvalid, source.URL)
		}
		seen[source.URL] = struct{}{}
		rootFound = rootFound || source.URL == graph.RootURL
	}
	if !rootFound {
		return fmt.Errorf("%w: root %q is absent", ErrModuleGraphInvalid, graph.RootURL)
	}
	return nil
}

func compileNativeModuleGraph(graph browser.ModuleGraph, cached map[string]*nativeModule) (map[string]nativeModule, map[moduleResolutionKey]string, uint64, error) {
	if err := validateNativeModuleGraphRoot(graph); err != nil {
		return nil, nil, 0, err
	}
	compiled := make(map[string]nativeModule, len(graph.Sources))
	var compilations uint64
	for _, source := range graph.Sources {
		if existing := cached[source.URL]; existing != nil {
			compiled[source.URL] = *existing
			continue
		}
		compiled[source.URL] = nativeModule{source: source}
	}
	resolutions := make(map[moduleResolutionKey]string, len(graph.Resolutions))
	for _, resolution := range graph.Resolutions {
		if _, found := compiled[resolution.Referrer]; !found {
			return nil, nil, compilations, fmt.Errorf("%w: resolution referrer %q is absent", ErrModuleGraphInvalid, resolution.Referrer)
		}
		if _, found := compiled[resolution.URL]; !found {
			return nil, nil, compilations, fmt.Errorf("%w: resolution target %q is absent", ErrModuleGraphInvalid, resolution.URL)
		}
		if resolution.Specifier == "" {
			return nil, nil, compilations, fmt.Errorf("%w: empty specifier from %q", ErrModuleGraphInvalid, resolution.Referrer)
		}
		key := moduleResolutionKey{referrer: resolution.Referrer, specifier: resolution.Specifier}
		if previous, duplicate := resolutions[key]; duplicate && previous != resolution.URL {
			return nil, nil, compilations, fmt.Errorf("%w: conflicting resolution for %q from %q", ErrModuleGraphInvalid, resolution.Specifier, resolution.Referrer)
		}
		resolutions[key] = resolution.URL
	}
	compiling := make(map[string]bool)
	var compileStatic func(string) error
	compileStatic = func(moduleURL string) error {
		module, found := compiled[moduleURL]
		if !found {
			return fmt.Errorf("%w: missing source %q", ErrModuleGraphInvalid, moduleURL)
		}
		if module.compiled || compiling[moduleURL] {
			return nil
		}
		image, err := compiler.CompileModuleWithOptions(module.source.Source, compiler.Options{AllowUnresolvedGlobals: true})
		if err != nil {
			return evaluationError(moduleURL, err)
		}
		module.image = image
		module.compiled = true
		compiled[moduleURL] = module
		compilations++
		compiling[moduleURL] = true
		defer delete(compiling, moduleURL)
		for _, request := range image.Requests() {
			target, found := resolutions[moduleResolutionKey{referrer: moduleURL, specifier: request}]
			if !found {
				return fmt.Errorf("%w: unresolved request %q from %q", ErrModuleGraphInvalid, request, moduleURL)
			}
			if err := compileStatic(target); err != nil {
				return err
			}
		}
		return nil
	}
	if err := compileStatic(graph.RootURL); err != nil {
		return nil, nil, compilations, err
	}
	reachable := make(map[string]struct{}, len(compiled))
	var visit func(string)
	visit = func(url string) {
		if _, seen := reachable[url]; seen {
			return
		}
		reachable[url] = struct{}{}
		for key, target := range resolutions {
			if key.referrer == url {
				visit(target)
			}
		}
	}
	visit(graph.RootURL)
	if len(reachable) != len(compiled) {
		return nil, nil, compilations, fmt.Errorf("%w: graph contains %d unreachable source(s)", ErrModuleGraphInvalid, len(compiled)-len(reachable))
	}
	return compiled, resolutions, compilations, nil
}

func (realm *Realm) moduleContextLocked(context *browserruntime.TaskContext, url string) (memory.Ref, error) {
	key, err := newString(context, "context:"+url)
	if err != nil {
		return memory.Ref{}, err
	}
	if cached, found, err := context.MapGet(realm.bindings.moduleCache, key); err != nil {
		return memory.Ref{}, err
	} else if found && cached.IsRef() {
		return cached.Ref(), nil
	}
	environment, err := context.NewContext(memory.RefValue(realm.active.Global))
	if err != nil {
		return memory.Ref{}, err
	}
	if err := context.MapSet(realm.bindings.moduleCache, key, memory.RefValue(environment)); err != nil {
		return memory.Ref{}, err
	}
	return environment, nil
}

func (realm *Realm) linkModuleLocked(context *browserruntime.TaskContext, url string) (result error) {
	module := realm.modules[url]
	if module == nil {
		return fmt.Errorf("%w: missing source %q", ErrModuleLink, url)
	}
	if err := realm.ensureModuleCompiledLocked(url, make(map[string]bool)); err != nil {
		return err
	}
	switch module.status {
	case moduleLinked, moduleEvaluating, moduleEvaluated:
		return nil
	case moduleLinking:
		return nil
	case moduleErrored:
		return module.err
	}
	module.status = moduleLinking
	defer func() {
		if result != nil {
			module.status = moduleErrored
			module.err = result
		}
	}()
	environment, err := realm.moduleContextLocked(context, url)
	if err != nil {
		return err
	}
	if err := realm.instantiateModuleLocalsLocked(context, module, environment); err != nil {
		return evaluationError(url, err)
	}
	for _, request := range module.image.Requests() {
		dependency, err := realm.resolveModuleRequest(url, request)
		if err != nil {
			return err
		}
		if err := realm.linkModuleLocked(context, dependency); err != nil {
			return err
		}
	}
	for _, imported := range module.image.Imports() {
		dependency, err := realm.resolveModuleRequest(url, imported.ModuleRequest)
		if err != nil {
			return err
		}
		localName, err := context.NewString(imported.LocalName)
		if err != nil {
			return err
		}
		if imported.Namespace {
			namespace, err := realm.moduleNamespaceLocked(context, dependency)
			if err != nil {
				return err
			}
			if err := context.DeclareBinding(environment, localName, false); err != nil {
				return err
			}
			if err := context.InitializeBinding(environment, localName, memory.RefValue(namespace)); err != nil {
				return err
			}
			continue
		}
		resolved, err := realm.resolveModuleExportLocked(context, dependency, imported.ImportName, make(map[moduleExportKey]struct{}))
		if err != nil {
			return err
		}
		if !resolved.found || resolved.ambiguous {
			return fmt.Errorf("%w: import %q from %q is missing or ambiguous", ErrModuleLink, imported.ImportName, dependency)
		}
		if resolved.namespaceURL != "" {
			namespace, err := realm.moduleNamespaceLocked(context, resolved.namespaceURL)
			if err != nil {
				return err
			}
			if err := context.DeclareBinding(environment, localName, false); err != nil {
				return err
			}
			if err := context.InitializeBinding(environment, localName, memory.RefValue(namespace)); err != nil {
				return err
			}
			continue
		}
		targetName, err := context.NewString(resolved.name)
		if err != nil {
			return err
		}
		if err := context.DeclareIndirectBinding(environment, localName, resolved.context, targetName); err != nil {
			return err
		}
	}
	module.status = moduleLinked
	return nil
}

func (realm *Realm) ensureModuleCompiledLocked(url string, compiling map[string]bool) error {
	module := realm.modules[url]
	if module == nil {
		return fmt.Errorf("%w: missing source %q", ErrModuleLink, url)
	}
	if module.compiled || compiling[url] {
		return nil
	}
	image, err := compiler.CompileModuleWithOptions(module.source.Source, compiler.Options{AllowUnresolvedGlobals: true})
	if err != nil {
		return evaluationError(url, err)
	}
	module.image = image
	module.compiled = true
	realm.moduleCompilations++
	compiling[url] = true
	defer delete(compiling, url)
	for _, request := range image.Requests() {
		dependency, err := realm.resolveModuleRequest(url, request)
		if err != nil {
			return err
		}
		if err := realm.ensureModuleCompiledLocked(dependency, compiling); err != nil {
			return err
		}
	}
	return nil
}

func (realm *Realm) instantiateModuleLocalsLocked(context *browserruntime.TaskContext, module *nativeModule, environment memory.Ref) error {
	loaded, err := program.Load(context, module.image.Program(), memory.RefValue(environment))
	if err != nil {
		return err
	}
	module.entry = loaded.Entry
	bindings := module.image.Bindings()
	names := make(map[string]memory.Ref, len(bindings))
	for _, binding := range bindings {
		name, err := context.NewString(binding.Name)
		if err != nil {
			return err
		}
		if err := context.DeclareBinding(environment, name, binding.Mutable); err != nil {
			return err
		}
		names[binding.Name] = name
	}
	for _, binding := range bindings {
		name := names[binding.Name]
		switch {
		case binding.InitializeImportMeta:
			meta, err := context.NewHeapObject()
			if err != nil {
				return err
			}
			if err := context.SetPrototype(meta, memory.NullValue()); err != nil {
				return err
			}
			urlName, err := context.NewString("url")
			if err != nil {
				return err
			}
			urlValue, err := context.NewString(module.source.URL)
			if err != nil {
				return err
			}
			if err := context.DefineProperty(meta, urlName, memory.DataProperty(memory.RefValue(urlValue), true, true, true)); err != nil {
				return err
			}
			resolve, err := realm.newNativeFunction(context, "resolve", 1, nativeModuleImportMetaResolve)
			if err != nil {
				return err
			}
			if err := defineData(context, meta, "resolve", memory.RefValue(resolve), true, true, true); err != nil {
				return err
			}
			if err := context.InitializeBinding(environment, name, memory.RefValue(meta)); err != nil {
				return err
			}
		case binding.InitializeDynamicImport:
			importer, err := context.NewHeapObject()
			if err != nil {
				return err
			}
			if err := context.SetPrototype(importer, memory.NullValue()); err != nil {
				return err
			}
			referrer, err := context.NewString(module.source.URL)
			if err != nil {
				return err
			}
			if err := defineData(context, importer, "referrer", memory.RefValue(referrer), false, false, false); err != nil {
				return err
			}
			importFunction, err := realm.newNativeFunction(context, "import", 1, nativeModuleDynamicImport)
			if err != nil {
				return err
			}
			if err := defineData(context, importer, "import", memory.RefValue(importFunction), false, false, false); err != nil {
				return err
			}
			if err := context.InitializeBinding(environment, name, memory.RefValue(importer)); err != nil {
				return err
			}
		case binding.InitializeUndefined:
			if err := context.InitializeBinding(environment, name, memory.UndefinedValue()); err != nil {
				return err
			}
		case binding.HasFunction:
			if uint64(binding.FunctionIndex) >= uint64(len(loaded.Functions)) {
				return fmt.Errorf("%w: binding %q references function %d", program.ErrInvalidProgram, binding.Name, binding.FunctionIndex)
			}
			template, err := context.LoadFunction(loaded.Functions[binding.FunctionIndex])
			if err != nil {
				return err
			}
			functionName := template.Name
			if binding.FunctionName != "" {
				name, err := context.NewString(binding.FunctionName)
				if err != nil {
					return err
				}
				functionName = memory.RefValue(name)
			}
			closure, err := context.NewBytecodeFunction(
				functionName,
				memory.RefValue(environment),
				template.Arity,
				template.Code,
				template.Constants,
			)
			if err != nil {
				return err
			}
			if err := context.InitializeBinding(environment, name, memory.RefValue(closure)); err != nil {
				return err
			}
		}
	}
	return nil
}

func (realm *Realm) moduleImportMetaResolve(context *browserruntime.TaskContext, this memory.Value, arguments []memory.Value) (memory.Value, error) {
	if !this.IsRef() {
		return memory.Value{}, fmt.Errorf("%w: import.meta.resolve has no module metadata receiver", ErrModuleLink)
	}
	urlName, err := context.NewString("url")
	if err != nil {
		return memory.Value{}, err
	}
	baseValue, found, err := context.GetOwnProperty(this.Ref(), urlName)
	if err != nil || !found || !baseValue.IsRef() {
		return memory.Value{}, fmt.Errorf("%w: import.meta.resolve has no module URL", ErrModuleLink)
	}
	baseText, err := context.DerefString(baseValue.Ref())
	if err != nil {
		return memory.Value{}, err
	}
	base, err := url.Parse(baseText)
	if err != nil {
		return memory.Value{}, err
	}
	specifier, err := stringArgument(context, arguments, 0)
	if err != nil {
		return memory.Value{}, err
	}
	reference, err := url.Parse(specifier)
	if err != nil {
		return memory.Value{}, err
	}
	return newString(context, base.ResolveReference(reference).String())
}

func (realm *Realm) moduleDynamicImport(context *browserruntime.TaskContext, this memory.Value, arguments []memory.Value) (memory.Value, error) {
	promise, err := context.NewPromise()
	if err != nil {
		return memory.Value{}, err
	}
	reject := func(cause error) (memory.Value, error) {
		reason, thrown := browserruntime.ThrownValue(cause)
		if !thrown {
			message, messageErr := context.NewString(cause.Error())
			if messageErr != nil {
				return memory.Value{}, messageErr
			}
			errorRef, errorErr := context.NewError(memory.ErrorType, memory.RefValue(message))
			if errorErr != nil {
				return memory.Value{}, errorErr
			}
			reason = memory.RefValue(errorRef)
		}
		if rejectErr := context.RejectPromise(promise, reason); rejectErr != nil {
			return memory.Value{}, rejectErr
		}
		return memory.RefValue(promise), nil
	}
	if !this.IsRef() {
		return reject(fmt.Errorf("%w: dynamic import has no module referrer", ErrModuleLink))
	}
	referrerName, err := context.NewString("referrer")
	if err != nil {
		return memory.Value{}, err
	}
	referrerValue, found, err := context.GetOwnProperty(this.Ref(), referrerName)
	if err != nil || !found || !referrerValue.IsRef() {
		return reject(fmt.Errorf("%w: dynamic import has no module referrer", ErrModuleLink))
	}
	referrer, err := context.DerefString(referrerValue.Ref())
	if err != nil {
		return memory.Value{}, err
	}
	specifier, err := stringArgument(context, arguments, 0)
	if err != nil {
		return reject(err)
	}
	target, err := realm.resolveModuleRequest(referrer, specifier)
	if err != nil {
		return reject(err)
	}
	if err := realm.linkModuleLocked(context, target); err != nil {
		return reject(err)
	}
	if err := realm.evaluateModuleLocked(context, target); err != nil {
		return reject(err)
	}
	namespace, err := realm.moduleNamespaceLocked(context, target)
	if err != nil {
		return reject(err)
	}
	if err := context.ResolvePromise(promise, memory.RefValue(namespace)); err != nil {
		return memory.Value{}, err
	}
	return memory.RefValue(promise), nil
}

func (realm *Realm) evaluateModuleLocked(context *browserruntime.TaskContext, url string) (result error) {
	module := realm.modules[url]
	if module == nil {
		return fmt.Errorf("%w: missing source %q", ErrModuleLink, url)
	}
	switch module.status {
	case moduleEvaluated:
		return nil
	case moduleEvaluating:
		return nil
	case moduleErrored:
		return module.err
	case moduleUnlinked, moduleLinking:
		return fmt.Errorf("%w: source %q was not linked", ErrModuleLink, url)
	}
	module.status = moduleEvaluating
	defer func() {
		module.entry = memory.Ref{}
		if result != nil {
			module.status = moduleErrored
			module.err = result
		}
	}()
	for _, request := range module.image.Requests() {
		dependency, err := realm.resolveModuleRequest(url, request)
		if err != nil {
			return err
		}
		if err := realm.evaluateModuleLocked(context, dependency); err != nil {
			return err
		}
	}
	if module.entry == (memory.Ref{}) {
		return evaluationError(url, fmt.Errorf("%w: source %q has no instantiated entry", ErrModuleLink, url))
	}
	realm.evaluations++
	realm.sourceBytes += uint64(len(module.source.Source))
	if _, err := realm.interpreter.ExecuteWithoutCheckpoint(context, module.entry); err != nil {
		return evaluationError(url, describeExecutionError(context, err))
	}
	module.status = moduleEvaluated
	return nil
}

func (realm *Realm) resolveModuleRequest(referrer, request string) (string, error) {
	resolved, found := realm.moduleResolutions[moduleResolutionKey{referrer: referrer, specifier: request}]
	if !found {
		return "", fmt.Errorf("%w: unresolved request %q from %q", ErrModuleLink, request, referrer)
	}
	return resolved, nil
}

func (realm *Realm) resolveModuleExportLocked(context *browserruntime.TaskContext, url, name string, seen map[moduleExportKey]struct{}) (resolvedModuleExport, error) {
	key := moduleExportKey{url: url, name: name}
	if _, cycle := seen[key]; cycle {
		return resolvedModuleExport{}, nil
	}
	seen[key] = struct{}{}
	defer delete(seen, key)
	module := realm.modules[url]
	if module == nil {
		return resolvedModuleExport{}, fmt.Errorf("%w: missing source %q", ErrModuleLink, url)
	}
	for _, exported := range module.image.Exports() {
		if exported.ExportName != name {
			continue
		}
		if exported.LocalName != "" {
			environment, err := realm.moduleContextLocked(context, url)
			return resolvedModuleExport{context: environment, name: exported.LocalName, found: err == nil}, err
		}
		dependency, err := realm.resolveModuleRequest(url, exported.ModuleRequest)
		if err != nil {
			return resolvedModuleExport{}, err
		}
		if exported.Namespace {
			return resolvedModuleExport{namespaceURL: dependency, found: true}, nil
		}
		return realm.resolveModuleExportLocked(context, dependency, exported.ImportName, seen)
	}
	if name == "default" {
		return resolvedModuleExport{}, nil
	}
	var match resolvedModuleExport
	for _, request := range module.image.StarExports() {
		dependency, err := realm.resolveModuleRequest(url, request)
		if err != nil {
			return resolvedModuleExport{}, err
		}
		candidate, err := realm.resolveModuleExportLocked(context, dependency, name, seen)
		if err != nil {
			return resolvedModuleExport{}, err
		}
		if !candidate.found {
			continue
		}
		if candidate.ambiguous {
			return candidate, nil
		}
		if !match.found {
			match = candidate
			continue
		}
		if match.context != candidate.context || match.name != candidate.name || match.namespaceURL != candidate.namespaceURL {
			return resolvedModuleExport{found: true, ambiguous: true}, nil
		}
	}
	return match, nil
}

func (realm *Realm) moduleNamespaceLocked(context *browserruntime.TaskContext, url string) (memory.Ref, error) {
	key, err := newString(context, "namespace:"+url)
	if err != nil {
		return memory.Ref{}, err
	}
	if cached, found, err := context.MapGet(realm.bindings.moduleCache, key); err != nil {
		return memory.Ref{}, err
	} else if found && cached.IsRef() {
		return cached.Ref(), nil
	}
	namespace, err := context.NewHeapObject()
	if err != nil {
		return memory.Ref{}, err
	}
	if err := context.SetPrototype(namespace, memory.NullValue()); err != nil {
		return memory.Ref{}, err
	}
	if err := context.MapSet(realm.bindings.moduleCache, key, memory.RefValue(namespace)); err != nil {
		return memory.Ref{}, err
	}
	names, err := realm.exportedModuleNames(url, make(map[string]struct{}))
	if err != nil {
		return memory.Ref{}, err
	}
	for _, name := range names {
		resolved, err := realm.resolveModuleExportLocked(context, url, name, make(map[moduleExportKey]struct{}))
		if err != nil {
			return memory.Ref{}, err
		}
		if !resolved.found || resolved.ambiguous {
			continue
		}
		if resolved.namespaceURL != "" {
			child, err := realm.moduleNamespaceLocked(context, resolved.namespaceURL)
			if err != nil {
				return memory.Ref{}, err
			}
			propertyName, err := context.NewString(name)
			if err != nil {
				return memory.Ref{}, err
			}
			if err := context.DefineProperty(namespace, propertyName, memory.DataProperty(memory.RefValue(child), false, true, false)); err != nil {
				return memory.Ref{}, err
			}
			continue
		}
		getter, err := moduleNamespaceGetter(context, resolved.context, resolved.name)
		if err != nil {
			return memory.Ref{}, err
		}
		propertyName, err := context.NewString(name)
		if err != nil {
			return memory.Ref{}, err
		}
		if err := context.DefineProperty(namespace, propertyName, memory.AccessorProperty(memory.RefValue(getter), memory.UndefinedValue(), true, false)); err != nil {
			return memory.Ref{}, err
		}
	}
	if err := context.SetObjectIntegrity(namespace, true, true); err != nil {
		return memory.Ref{}, err
	}
	return namespace, nil
}

func moduleNamespaceGetter(context *browserruntime.TaskContext, target memory.Ref, targetName string) (memory.Ref, error) {
	environment, err := context.NewContext(memory.NullValue())
	if err != nil {
		return memory.Ref{}, err
	}
	localName, err := context.NewString("value")
	if err != nil {
		return memory.Ref{}, err
	}
	remoteName, err := context.NewString(targetName)
	if err != nil {
		return memory.Ref{}, err
	}
	if err := context.DeclareIndirectBinding(environment, localName, target, remoteName); err != nil {
		return memory.Ref{}, err
	}
	builder := browserruntime.NewBytecodeBuilder()
	constant, err := builder.AddConstant(memory.RefValue(localName))
	if err != nil {
		return memory.Ref{}, err
	}
	if _, err := builder.Emit(browserruntime.Instruction{Op: browserruntime.OpLoadBinding, A: constant}); err != nil {
		return memory.Ref{}, err
	}
	if _, err := builder.Emit(browserruntime.Instruction{Op: browserruntime.OpReturn}); err != nil {
		return memory.Ref{}, err
	}
	chunk, err := builder.Build()
	if err != nil {
		return memory.Ref{}, err
	}
	return context.NewBytecodeFunction(memory.NullValue(), memory.RefValue(environment), 0, chunk.Code, chunk.Constants)
}

func (realm *Realm) exportedModuleNames(url string, seen map[string]struct{}) ([]string, error) {
	if _, cycle := seen[url]; cycle {
		return nil, nil
	}
	seen[url] = struct{}{}
	defer delete(seen, url)
	module := realm.modules[url]
	if module == nil {
		return nil, fmt.Errorf("%w: missing source %q", ErrModuleLink, url)
	}
	names := make(map[string]struct{})
	for _, exported := range module.image.Exports() {
		names[exported.ExportName] = struct{}{}
	}
	for _, request := range module.image.StarExports() {
		dependency, err := realm.resolveModuleRequest(url, request)
		if err != nil {
			return nil, err
		}
		children, err := realm.exportedModuleNames(dependency, seen)
		if err != nil {
			return nil, err
		}
		for _, name := range children {
			if name != "default" {
				names[name] = struct{}{}
			}
		}
	}
	result := make([]string, 0, len(names))
	for name := range names {
		result = append(result, name)
	}
	sort.Strings(result)
	return result, nil
}
