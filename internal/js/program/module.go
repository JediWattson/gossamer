package program

// ModuleBinding describes one local environment slot created before module
// evaluation. InitializeUndefined distinguishes var bindings from lexical and
// Function bindings that remain uninitialized until their bytecode prologue.
type ModuleBinding struct {
	Name                string
	Mutable             bool
	InitializeUndefined bool
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
	return Module{
		program:  image,
		requests: append([]string(nil), requests...),
		bindings: append([]ModuleBinding(nil), bindings...),
		imports:  append([]ModuleImport(nil), imports...),
		exports:  append([]ModuleExport(nil), exports...),
		stars:    append([]string(nil), stars...),
	}
}

func (module Module) Program() Program { return module.program }

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
