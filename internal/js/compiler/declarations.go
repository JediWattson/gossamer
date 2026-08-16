package compiler

import (
	"fmt"

	"github.com/JediWattson/gossamer/internal/js/ast"
	"github.com/JediWattson/gossamer/internal/js/lexer"
	browserruntime "github.com/JediWattson/gossamer/internal/runtime"
)

type lexicalDeclaration struct {
	name    string
	mutable bool
	span    lexer.Span
}

type hoistedDeclaration struct {
	name     string
	span     lexer.Span
	function *ast.FunctionDeclaration
}

func (compiler *functionCompiler) instantiateFunctionScope(statements []ast.Statement) error {
	lexicals, err := directLexicalDeclarations(statements)
	if err != nil {
		return compiler.problem(err.span, err.message)
	}
	hoisted := collectHoistedDeclarations(statements)
	hoistedByName := make(map[string]hoistedDeclaration, len(hoisted))
	for _, declaration := range hoisted {
		if previous, exists := hoistedByName[declaration.name]; exists {
			if declaration.function != nil {
				previous.function = declaration.function
				previous.span = declaration.span
				hoistedByName[declaration.name] = previous
			}
			continue
		}
		hoistedByName[declaration.name] = declaration
	}
	for _, declaration := range lexicals {
		if hoistedDeclaration, conflict := hoistedByName[declaration.name]; conflict {
			return compiler.problem(declaration.span, fmt.Sprintf("lexical binding %q conflicts with hoisted declaration at %d:%d", declaration.name, hoistedDeclaration.span.Start.Line, hoistedDeclaration.span.Start.Column))
		}
		if err := compiler.declare(declaration.name, declaration.mutable, declaration.span); err != nil {
			return err
		}
	}

	type hoistedAction struct {
		declaration hoistedDeclaration
		initialize  bool
	}
	actions := make([]hoistedAction, 0, len(hoistedByName))
	seen := make(map[string]struct{}, len(hoistedByName))
	scope := compiler.scopes[len(compiler.scopes)-1]
	for _, declaration := range hoisted {
		if _, duplicate := seen[declaration.name]; duplicate {
			continue
		}
		seen[declaration.name] = struct{}{}
		declaration = hoistedByName[declaration.name]
		initialize := true
		if existing, exists := scope[declaration.name]; exists {
			if existing.kind == bindingLexical {
				return compiler.problem(declaration.span, fmt.Sprintf("hoisted binding %q conflicts with lexical declaration at %d:%d", declaration.name, existing.span.Start.Line, existing.span.Start.Column))
			}
			initialize = false
		} else {
			scope[declaration.name] = binding{mutable: true, span: declaration.span, kind: bindingHoisted}
		}
		actions = append(actions, hoistedAction{declaration: declaration, initialize: initialize})
	}

	// All names exist before any initializer runs. This is what lets mutually
	// recursive Function declarations capture the complete Function scope.
	for _, action := range actions {
		if !action.initialize {
			continue
		}
		name, err := compiler.stringConstant(action.declaration.name)
		if err != nil {
			return err
		}
		if err := compiler.emit(browserruntime.Instruction{Op: browserruntime.OpDeclareBinding, A: name, B: 1}, action.declaration.span); err != nil {
			return err
		}
	}
	for _, declaration := range lexicals {
		name, err := compiler.stringConstant(declaration.name)
		if err != nil {
			return err
		}
		flag := uint32(0)
		if declaration.mutable {
			flag = 1
		}
		if err := compiler.emit(browserruntime.Instruction{Op: browserruntime.OpDeclareBinding, A: name, B: flag}, declaration.span); err != nil {
			return err
		}
	}
	for _, action := range actions {
		if action.declaration.function != nil {
			if err := compiler.compileHoistedFunction(action.declaration.function, action.initialize); err != nil {
				return err
			}
			continue
		}
		if !action.initialize {
			continue
		}
		name, err := compiler.stringConstant(action.declaration.name)
		if err != nil {
			return err
		}
		if err := compiler.emit(browserruntime.Instruction{Op: browserruntime.OpUndefined}, action.declaration.span); err != nil {
			return err
		}
		if err := compiler.emit(browserruntime.Instruction{Op: browserruntime.OpInitializeBinding, A: name}, action.declaration.span); err != nil {
			return err
		}
		if err := compiler.emit(browserruntime.Instruction{Op: browserruntime.OpPop}, action.declaration.span); err != nil {
			return err
		}
	}
	return nil
}

func (compiler *functionCompiler) instantiateLexicalScope(statements []ast.Statement) error {
	declarations, err := directLexicalDeclarations(statements)
	if err != nil {
		return compiler.problem(err.span, err.message)
	}
	for _, declaration := range declarations {
		if err := compiler.declare(declaration.name, declaration.mutable, declaration.span); err != nil {
			return err
		}
		name, err := compiler.stringConstant(declaration.name)
		if err != nil {
			return err
		}
		flag := uint32(0)
		if declaration.mutable {
			flag = 1
		}
		if err := compiler.emit(browserruntime.Instruction{Op: browserruntime.OpDeclareBinding, A: name, B: flag}, declaration.span); err != nil {
			return err
		}
	}
	return nil
}

type declarationProblem struct {
	span    lexer.Span
	message string
}

func directLexicalDeclarations(statements []ast.Statement) ([]lexicalDeclaration, *declarationProblem) {
	declarations := make([]lexicalDeclaration, 0)
	seen := make(map[string]lexer.Span)
	for _, statement := range statements {
		declaration, ok := statement.(*ast.VariableDeclaration)
		if !ok || declaration.Kind == ast.VariableVar {
			continue
		}
		for _, declarator := range declaration.Declarations {
			name := declarator.Name.Name
			if previous, duplicate := seen[name]; duplicate {
				return nil, &declarationProblem{span: declarator.Name.Span(), message: fmt.Sprintf("binding %q already declared at %d:%d", name, previous.Start.Line, previous.Start.Column)}
			}
			seen[name] = declarator.Name.Span()
			declarations = append(declarations, lexicalDeclaration{name: name, mutable: declaration.Kind == ast.VariableLet, span: declarator.Name.Span()})
		}
	}
	return declarations, nil
}

func collectHoistedDeclarations(statements []ast.Statement) []hoistedDeclaration {
	var declarations []hoistedDeclaration
	var visit func(ast.Statement)
	visit = func(statement ast.Statement) {
		switch statement := statement.(type) {
		case *ast.VariableDeclaration:
			if statement.Kind != ast.VariableVar {
				return
			}
			for _, declarator := range statement.Declarations {
				declarations = append(declarations, hoistedDeclaration{name: declarator.Name.Name, span: declarator.Name.Span()})
			}
		case *ast.FunctionDeclaration:
			declarations = append(declarations, hoistedDeclaration{name: statement.Name.Name, span: statement.Name.Span(), function: statement})
		case *ast.BlockStatement:
			for _, child := range statement.Body {
				visit(child)
			}
		case *ast.IfStatement:
			visit(statement.Consequent)
			if statement.Alternate != nil {
				visit(statement.Alternate)
			}
		case *ast.WhileStatement:
			visit(statement.Body)
		case *ast.TryStatement:
			visit(statement.Body)
			if statement.Handler != nil {
				visit(statement.Handler.Body)
			}
			if statement.Finalizer != nil {
				visit(statement.Finalizer)
			}
		}
	}
	for _, statement := range statements {
		visit(statement)
	}
	return declarations
}
