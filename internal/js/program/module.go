package program

import "strings"

// ModuleBinding describes one local environment slot created during module
// instantiation. Lexical bindings remain uninitialized until evaluation, var
// bindings receive undefined, and Function bindings receive a closure over the
// module environment before any dependency begins evaluating.
type ModuleBinding struct {
	Name                    string
	Mutable                 bool
	InitializeUndefined     bool
	FunctionIndex           uint32
	HasFunction             bool
	FunctionName            string
	InitializeImportMeta    bool
	InitializeDynamicImport bool
}

type ModuleImport struct {
	ModuleRequest string
	ImportName    string
	LocalName     string
	Namespace     bool
}

type ModuleExport struct {
	ExportName    string
	LocalName     string
	ModuleRequest string
	ImportName    string
	Namespace     bool
}

// Module is a portable source-unit image. Like Program it owns no RegionStore
// identity; all strings and slices are copied at construction and access.
type Module struct {
	program  Program
	requests []string
	bindings []ModuleBinding
	imports  []ModuleImport
	exports  []ModuleExport
	stars    []string
}

func NewModule(
	image Program,
	requests []string,
	bindings []ModuleBinding,
	imports []ModuleImport,
	exports []ModuleExport,
	stars []string,
) Module {
	return NewModuleOwned(
		image,
		append([]string(nil), requests...),
		append([]ModuleBinding(nil), bindings...),
		append([]ModuleImport(nil), imports...),
		append([]ModuleExport(nil), exports...),
		append([]string(nil), stars...),
	)
}

// NewModuleOwned adopts module metadata slices after detaching every retained
// string from parser source text. The caller relinquishes the supplied slices
// and must not mutate them after the call.
func NewModuleOwned(
	image Program,
	requests []string,
	bindings []ModuleBinding,
	imports []ModuleImport,
	exports []ModuleExport,
	stars []string,
) Module {
	stringsByValue := make(map[string]string)
	detach := func(value string) string {
		if value == "" {
			return ""
		}
		if detached, found := stringsByValue[value]; found {
			return detached
		}
		detached := strings.Clone(value)
		stringsByValue[value] = detached
		return detached
	}
	for index := range requests {
		requests[index] = detach(requests[index])
	}
	for index := range bindings {
		bindings[index].Name = detach(bindings[index].Name)
		bindings[index].FunctionName = detach(bindings[index].FunctionName)
	}
	for index := range imports {
		imports[index].ModuleRequest = detach(imports[index].ModuleRequest)
		imports[index].ImportName = detach(imports[index].ImportName)
		imports[index].LocalName = detach(imports[index].LocalName)
	}
	for index := range exports {
		exports[index].ExportName = detach(exports[index].ExportName)
		exports[index].LocalName = detach(exports[index].LocalName)
		exports[index].ModuleRequest = detach(exports[index].ModuleRequest)
		exports[index].ImportName = detach(exports[index].ImportName)
	}
	for index := range stars {
		stars[index] = detach(stars[index])
	}
	return Module{
		program:  image,
		requests: requests,
		bindings: bindings,
		imports:  imports,
		exports:  exports,
		stars:    stars,
	}
}

func (module Module) Program() Program { return module.program }

// ReleaseProgram drops the portable executable after an engine has loaded it
// into its own heap. Module linking metadata remains valid. Engines must not
// call this until every Function descriptor required by Program has been
// materialized somewhere with an independent lifetime.
func (module *Module) ReleaseProgram() {
	if module != nil {
		module.program = Program{}
	}
}

func (module Module) Requests() []string {
	return append([]string(nil), module.requests...)
}

func (module Module) Bindings() []ModuleBinding {
	return append([]ModuleBinding(nil), module.bindings...)
}

func (module Module) Imports() []ModuleImport {
	return append([]ModuleImport(nil), module.imports...)
}

func (module Module) Exports() []ModuleExport {
	return append([]ModuleExport(nil), module.exports...)
}

func (module Module) StarExports() []string {
	return append([]string(nil), module.stars...)
}
