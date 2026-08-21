package compiler

import (
	"fmt"

	"github.com/JediWattson/gossamer/internal/js/ast"
	"github.com/JediWattson/gossamer/internal/js/lexer"
	"github.com/JediWattson/gossamer/internal/js/parser"
	"github.com/JediWattson/gossamer/internal/js/program"
)

const defaultExportBinding = "*default*"

type pendingModuleExport struct {
	entry program.ModuleExport
	span  lexer.Span
}

func CompileModule(source string) (program.Module, error) {
	return CompileModuleWithOptions(source, Options{})
}

func CompileModuleWithOptions(source string, options Options) (program.Module, error) {
	script, err := parser.Parse(source)
	if err != nil {
		return program.Module{}, err
	}
	return CompileModuleASTWithOptions(script, options)
}

func CompileModuleASTWithOptions(script *ast.Script, options Options) (program.Module, error) {
	if script == nil {
		return program.Module{}, &Error{Message: "nil Script", Span: lexer.Span{Start: lexer.Position{Line: 1, Column: 1}}}
	}
	body := make([]ast.Statement, 0, len(script.Body))
	requests := make([]string, 0)
	requestSet := make(map[string]struct{})
	imports := make([]program.ModuleImport, 0)
	importBindings := make(map[string]binding)
	exports := make([]pendingModuleExport, 0)
	stars := make([]string, 0)
	exportNames := make(map[string]lexer.Span)

	addRequest := func(request string) {
		if _, exists := requestSet[request]; exists {
			return
		}
		requestSet[request] = struct{}{}
		requests = append(requests, request)
	}
	addExport := func(entry program.ModuleExport, span lexer.Span) error {
		if previous, duplicate := exportNames[entry.ExportName]; duplicate {
			return &Error{Span: span, Message: fmt.Sprintf("export %q already declared at %d:%d", entry.ExportName, previous.Start.Line, previous.Start.Column)}
		}
		exportNames[entry.ExportName] = span
		exports = append(exports, pendingModuleExport{entry: entry, span: span})
		return nil
	}

	for _, statement := range script.Body {
		switch statement := statement.(type) {
		case *ast.ImportDeclaration:
			addRequest(statement.Source)
			for _, specifier := range statement.Specifiers {
				name := specifier.Local.Name
				if previous, duplicate := importBindings[name]; duplicate {
					return program.Module{}, &Error{Span: specifier.Local.Span(), Message: fmt.Sprintf("binding %q already declared at %d:%d", name, previous.span.Start.Line, previous.span.Start.Column)}
				}
				importBindings[name] = binding{mutable: false, span: specifier.Local.Span(), kind: bindingLexical}
				imports = append(imports, program.ModuleImport{
					ModuleRequest: statement.Source,
					ImportName:    specifier.Imported,
					LocalName:     name,
					Namespace:     specifier.Kind == ast.ImportNamespace,
				})
			}

		case *ast.ExportNamedDeclaration:
			if statement.Declaration != nil {
				body = append(body, statement.Declaration)
				for _, name := range exportedDeclarationNames(statement.Declaration) {
					if err := addExport(program.ModuleExport{ExportName: name, LocalName: name}, statement.Span()); err != nil {
						return program.Module{}, err
					}
				}
				continue
			}
			if statement.Source != "" {
				addRequest(statement.Source)
			}
			for _, specifier := range statement.Specifiers {
				entry := program.ModuleExport{ExportName: specifier.Exported}
				if statement.Source == "" {
					entry.LocalName = specifier.Local
				} else {
					entry.ModuleRequest = statement.Source
					entry.ImportName = specifier.Local
				}
				if err := addExport(entry, specifier.Span()); err != nil {
					return program.Module{}, err
				}
			}

		case *ast.ExportDefaultDeclaration:
			identifier := &ast.Identifier{Base: ast.Base{Range: statement.Span()}, Name: defaultExportBinding}
			body = append(body, &ast.VariableDeclaration{
				Base: ast.Base{Range: statement.Span()}, Kind: ast.VariableConst,
				Declarations: []*ast.VariableDeclarator{{
					Base: ast.Base{Range: statement.Span()}, Name: identifier, Init: statement.Expression,
				}},
			})
			if err := addExport(program.ModuleExport{ExportName: "default", LocalName: defaultExportBinding}, statement.Span()); err != nil {
				return program.Module{}, err
			}

		case *ast.ExportAllDeclaration:
			addRequest(statement.Source)
			if statement.Exported == "" {
				stars = append(stars, statement.Source)
				continue
			}
			if err := addExport(program.ModuleExport{
				ExportName: statement.Exported, ModuleRequest: statement.Source, ImportName: "*", Namespace: true,
			}, statement.Span()); err != nil {
				return program.Module{}, err
			}

		default:
			body = append(body, statement)
		}
	}

	moduleScript := &ast.Script{Base: script.Base, Body: body}
	owner := &imageCompiler{functions: []program.FunctionTemplate{{}}, options: options}
	function := newFunctionCompiler(owner, nil, false)
	for name, imported := range importBindings {
		function.scopes[0][name] = imported
	}
	if err := function.compileScript(moduleScript); err != nil {
		return program.Module{}, err
	}
	template, err := function.finish("module", 0)
	if err != nil {
		return program.Module{}, err
	}
	owner.functions[0] = template
	image, err := program.New(owner.functions, 0)
	if err != nil {
		return program.Module{}, fmt.Errorf("%w: %v", ErrCompile, err)
	}

	bindings, localNames, err := moduleLocalBindings(body)
	if err != nil {
		return program.Module{}, err
	}
	for name := range importBindings {
		localNames[name] = struct{}{}
	}
	portableExports := make([]program.ModuleExport, 0, len(exports))
	for _, exported := range exports {
		if exported.entry.LocalName != "" {
			if _, found := localNames[exported.entry.LocalName]; !found {
				return program.Module{}, &Error{Span: exported.span, Message: fmt.Sprintf("exported binding %q is not declared", exported.entry.LocalName)}
			}
		}
		portableExports = append(portableExports, exported.entry)
	}
	return program.NewModule(image, requests, bindings, imports, portableExports, stars), nil
}

func exportedDeclarationNames(statement ast.Statement) []string {
	switch statement := statement.(type) {
	case *ast.VariableDeclaration:
		var names []string
		for _, declaration := range statement.Declarations {
			for _, identifier := range declaration.BindingIdentifiers() {
				names = append(names, identifier.Name)
			}
		}
		return names
	case *ast.FunctionDeclaration:
		return []string{statement.Name.Name}
	default:
		return nil
	}
}

func moduleLocalBindings(body []ast.Statement) ([]program.ModuleBinding, map[string]struct{}, error) {
	lexicals, problem := directLexicalDeclarations(body)
	if problem != nil {
		return nil, nil, &Error{Span: problem.span, Message: problem.message}
	}
	bindings := make([]program.ModuleBinding, 0, len(lexicals))
	names := make(map[string]struct{})
	for _, declaration := range lexicals {
		bindings = append(bindings, program.ModuleBinding{Name: declaration.name, Mutable: declaration.mutable})
		names[declaration.name] = struct{}{}
	}
	for _, declaration := range collectHoistedDeclarations(body) {
		if _, duplicate := names[declaration.name]; duplicate {
			continue
		}
		names[declaration.name] = struct{}{}
		bindings = append(bindings, program.ModuleBinding{
			Name: declaration.name, Mutable: true, InitializeUndefined: declaration.function == nil,
		})
	}
	return bindings, names, nil
}
