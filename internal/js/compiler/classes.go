package compiler

import (
	"fmt"

	"github.com/JediWattson/gossamer/internal/js/ast"
	"github.com/JediWattson/gossamer/internal/js/lexer"
	browserruntime "github.com/JediWattson/gossamer/internal/runtime"
)

func (compiler *functionCompiler) compileClassDeclaration(declaration *ast.ClassDeclaration) error {
	expression := &ast.ClassExpression{
		Base: declaration.Base, Name: declaration.Name, SuperClass: declaration.SuperClass,
		SuperBinding: declaration.SuperBinding, Elements: declaration.Elements,
	}
	if err := compiler.compileClassExpression(expression); err != nil {
		return err
	}
	name, err := compiler.stringConstant(declaration.Name.Name)
	if err != nil {
		return err
	}
	if err := compiler.emit(browserruntime.Instruction{Op: browserruntime.OpInitializeBinding, A: name}, declaration.Span()); err != nil {
		return err
	}
	return compiler.emit(browserruntime.Instruction{Op: browserruntime.OpPop}, declaration.Span())
}

func (compiler *functionCompiler) compileClassExpression(class *ast.ClassExpression) error {
	if class == nil {
		return compiler.problem(lexer.Span{}, "nil class expression")
	}
	if class.SuperClass != nil {
		if class.SuperBinding == "" {
			return compiler.problem(class.Span(), "derived class is missing its super binding")
		}
		scope := compiler.scopes[len(compiler.scopes)-1]
		if _, duplicate := scope[class.SuperBinding]; duplicate {
			return compiler.problem(class.Span(), "duplicate internal class super binding")
		}
		scope[class.SuperBinding] = binding{mutable: false, span: class.Span(), kind: bindingLexical}
		name, err := compiler.stringConstant(class.SuperBinding)
		if err != nil {
			return err
		}
		if err := compiler.emit(browserruntime.Instruction{Op: browserruntime.OpDeclareBinding, A: name}, class.Span()); err != nil {
			return err
		}
		if err := compiler.compileExpression(class.SuperClass); err != nil {
			return err
		}
		if err := compiler.emit(browserruntime.Instruction{Op: browserruntime.OpInitializeBinding, A: name}, class.Span()); err != nil {
			return err
		}
		if err := compiler.emit(browserruntime.Instruction{Op: browserruntime.OpPop}, class.Span()); err != nil {
			return err
		}
	}

	constructor := classConstructor(class)
	constructorBody := classConstructorBody(class, constructor)
	className := ""
	if class.Name != nil {
		className = class.Name.Name
	}
	function, err := compiler.compileNestedFunction(
		className, constructor.Parameters, constructorBody, class.Span(), false, false,
	)
	if err != nil {
		return err
	}
	constant, err := compiler.addFunctionConstant(function)
	if err != nil {
		return err
	}
	if err := compiler.emit(browserruntime.Instruction{Op: browserruntime.OpCreateClosure, A: constant}, class.Span()); err != nil {
		return err
	}
	classTemporary := compiler.temporaryName("class")
	if err := compiler.initializeTemporary(classTemporary, class.Span()); err != nil {
		return err
	}

	if class.SuperClass != nil {
		if err := compiler.compileClassInheritance(classTemporary, class.SuperBinding, class.Span()); err != nil {
			return err
		}
	}
	for _, element := range class.Elements {
		if element.Kind == ast.ClassConstructor || element.Kind == ast.ClassField && !element.Static {
			continue
		}
		if err := compiler.compileClassElement(classTemporary, element); err != nil {
			return err
		}
	}
	return compiler.loadTemporary(classTemporary, class.Span())
}

func classConstructor(class *ast.ClassExpression) *ast.FunctionExpression {
	for _, element := range class.Elements {
		if element.Kind == ast.ClassConstructor && element.Function != nil {
			return element.Function
		}
	}
	return &ast.FunctionExpression{
		Base: class.Base,
		Body: &ast.BlockStatement{Base: class.Base},
	}
}

func classConstructorBody(class *ast.ClassExpression, constructor *ast.FunctionExpression) *ast.BlockStatement {
	body := make([]ast.Statement, 0, len(class.Elements)+len(constructor.Body.Body))
	for _, element := range class.Elements {
		if element.Kind != ast.ClassField || element.Static {
			continue
		}
		value := element.Value
		if value == nil {
			value = &ast.Identifier{Base: ast.Base{Range: element.Span()}, Name: "undefined"}
		}
		property := ast.Expression(&ast.Identifier{Base: ast.Base{Range: element.Span()}, Name: element.Key})
		if element.Computed {
			property = element.KeyExpression
		}
		member := &ast.MemberExpression{
			Base: ast.Base{Range: element.Span()}, Object: &ast.ThisExpression{Base: ast.Base{Range: element.Span()}},
			Property: property, Computed: element.Computed,
		}
		assignment := &ast.AssignmentExpression{
			Base: ast.Base{Range: element.Span()}, Operator: lexer.Assign, Left: member, Right: value,
		}
		body = append(body, &ast.ExpressionStatement{Base: ast.Base{Range: element.Span()}, Expression: assignment})
	}
	body = append(body, constructor.Body.Body...)
	return &ast.BlockStatement{Base: constructor.Body.Base, Body: body}
}

func (compiler *functionCompiler) compileClassInheritance(classTemporary, superBinding string, span lexer.Span) error {
	// Object.setPrototypeOf(Class.prototype, Super.prototype)
	if err := compiler.compileObjectSetPrototypeOf(
		func() error {
			if err := compiler.loadTemporary(classTemporary, span); err != nil {
				return err
			}
			return compiler.emitStringPropertyGet("prototype", span)
		},
		func() error {
			if err := compiler.loadBindingDirect(superBinding, span); err != nil {
				return err
			}
			return compiler.emitStringPropertyGet("prototype", span)
		}, span,
	); err != nil {
		return err
	}
	// Object.setPrototypeOf(Class, Super)
	return compiler.compileObjectSetPrototypeOf(
		func() error { return compiler.loadTemporary(classTemporary, span) },
		func() error { return compiler.loadBindingDirect(superBinding, span) }, span,
	)
}

func (compiler *functionCompiler) compileObjectSetPrototypeOf(target, prototype func() error, span lexer.Span) error {
	object := &ast.Identifier{Base: ast.Base{Range: span}, Name: "Object"}
	if err := compiler.compileExpression(object); err != nil {
		return err
	}
	method, err := compiler.stringConstant("setPrototypeOf")
	if err != nil {
		return err
	}
	if err := compiler.emit(browserruntime.Instruction{Op: browserruntime.OpConstant, A: method}, span); err != nil {
		return err
	}
	if err := target(); err != nil {
		return err
	}
	if err := prototype(); err != nil {
		return err
	}
	if err := compiler.emit(browserruntime.Instruction{Op: browserruntime.OpCallMethod, A: 2}, span); err != nil {
		return err
	}
	return compiler.emit(browserruntime.Instruction{Op: browserruntime.OpPop}, span)
}

func (compiler *functionCompiler) compileClassElement(classTemporary string, element *ast.ClassElement) error {
	if element == nil {
		return compiler.problem(lexer.Span{}, "nil class element")
	}
	if element.Static {
		if err := compiler.loadTemporary(classTemporary, element.Span()); err != nil {
			return err
		}
	} else {
		if err := compiler.loadTemporary(classTemporary, element.Span()); err != nil {
			return err
		}
		if err := compiler.emitStringPropertyGet("prototype", element.Span()); err != nil {
			return err
		}
	}
	if element.Computed {
		if err := compiler.compileExpression(element.KeyExpression); err != nil {
			return err
		}
	} else {
		key, err := compiler.stringConstant(element.Key)
		if err != nil {
			return err
		}
		if err := compiler.emit(browserruntime.Instruction{Op: browserruntime.OpConstant, A: key}, element.Span()); err != nil {
			return err
		}
	}

	if element.Kind == ast.ClassField {
		if element.Value == nil {
			if err := compiler.emit(browserruntime.Instruction{Op: browserruntime.OpUndefined}, element.Span()); err != nil {
				return err
			}
		} else if err := compiler.compileExpression(element.Value); err != nil {
			return err
		}
	} else {
		if element.Function == nil {
			return compiler.problem(element.Span(), fmt.Sprintf("class element %q has no Function", element.Key))
		}
		if err := compiler.compileFunctionExpression(element.Function); err != nil {
			return err
		}
	}

	opcode := browserruntime.OpSetOwnProperty
	mode := uint32(0)
	if element.Kind == ast.ClassGetter || element.Kind == ast.ClassSetter {
		opcode = browserruntime.OpDefineAccessor
		if element.Kind == ast.ClassSetter {
			mode = 1
		}
	} else if element.Computed {
		opcode = browserruntime.OpSetProperty
	}
	if err := compiler.emit(browserruntime.Instruction{Op: opcode, A: mode}, element.Span()); err != nil {
		return err
	}
	return compiler.emit(browserruntime.Instruction{Op: browserruntime.OpPop}, element.Span())
}

func (compiler *functionCompiler) emitStringPropertyGet(name string, span lexer.Span) error {
	key, err := compiler.stringConstant(name)
	if err != nil {
		return err
	}
	if err := compiler.emit(browserruntime.Instruction{Op: browserruntime.OpConstant, A: key}, span); err != nil {
		return err
	}
	return compiler.emit(browserruntime.Instruction{Op: browserruntime.OpGetProperty}, span)
}

func (compiler *functionCompiler) loadBindingDirect(name string, span lexer.Span) error {
	constant, err := compiler.stringConstant(name)
	if err != nil {
		return err
	}
	return compiler.emit(browserruntime.Instruction{Op: browserruntime.OpLoadBinding, A: constant}, span)
}
