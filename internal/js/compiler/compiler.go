// Package compiler lowers Gossamer's source-ranged JavaScript subset into a
// portable native program image without allocating RegionStore values.
package compiler

import (
	"errors"
	"fmt"
	"math"

	"github.com/JediWattson/gossamer/internal/js/ast"
	"github.com/JediWattson/gossamer/internal/js/lexer"
	"github.com/JediWattson/gossamer/internal/js/parser"
	"github.com/JediWattson/gossamer/internal/js/program"
	browserruntime "github.com/JediWattson/gossamer/internal/runtime"
)

var ErrCompile = errors.New("js/compiler: cannot compile source")

type Error struct {
	Span    lexer.Span
	Message string
}

func (problem *Error) Error() string {
	if problem == nil {
		return ErrCompile.Error()
	}
	return fmt.Sprintf("%s at %d:%d", problem.Message, problem.Span.Start.Line, problem.Span.Start.Column)
}

func (problem *Error) Unwrap() error { return ErrCompile }

func Compile(source string) (program.Program, error) {
	script, err := parser.Parse(source)
	if err != nil {
		return program.Program{}, err
	}
	return CompileAST(script)
}

func CompileAST(script *ast.Script) (program.Program, error) {
	if script == nil {
		return program.Program{}, &Error{Message: "nil Script", Span: lexer.Span{Start: lexer.Position{Line: 1, Column: 1}}}
	}
	owner := &imageCompiler{functions: []program.FunctionTemplate{{}}}
	function := newFunctionCompiler(owner, nil, false)
	if err := function.compileScript(script); err != nil {
		return program.Program{}, err
	}
	template, err := function.finish("script", 0)
	if err != nil {
		return program.Program{}, err
	}
	owner.functions[0] = template
	image, err := program.New(owner.functions, 0)
	if err != nil {
		return program.Program{}, fmt.Errorf("%w: %v", ErrCompile, err)
	}
	return image, nil
}

type imageCompiler struct {
	functions []program.FunctionTemplate
}

func (compiler *imageCompiler) reserveFunction() (uint32, error) {
	if uint64(len(compiler.functions)) > math.MaxUint32 {
		return 0, &Error{Message: "too many Functions"}
	}
	index := uint32(len(compiler.functions))
	compiler.functions = append(compiler.functions, program.FunctionTemplate{})
	return index, nil
}

type binding struct {
	mutable bool
	span    lexer.Span
	kind    bindingKind
}

type bindingKind uint8

const (
	bindingLexical bindingKind = iota + 1
	bindingParameter
	bindingHoisted
)

type loopTarget struct {
	breakLabel       browserruntime.Label
	continueLabel    browserruntime.Label
	environmentDepth int
	handlerDepth     int
}

type functionCompiler struct {
	owner            *imageCompiler
	parent           *functionCompiler
	builder          *browserruntime.BytecodeBuilder
	constants        []program.Constant
	constant         map[constantKey]uint32
	scopes           []map[string]binding
	loops            []loopTarget
	inFunction       bool
	environmentDepth int
	handlerDepth     int
}

type constantKey struct {
	kind     program.ConstantKind
	bits     uint64
	text     string
	function uint32
	boolean  bool
}

func newFunctionCompiler(owner *imageCompiler, parent *functionCompiler, inFunction bool) *functionCompiler {
	return &functionCompiler{
		owner: owner, parent: parent, builder: browserruntime.NewBytecodeBuilder(),
		constant: make(map[constantKey]uint32), scopes: []map[string]binding{make(map[string]binding)}, inFunction: inFunction,
	}
}

func (compiler *functionCompiler) compileScript(script *ast.Script) error {
	if err := compiler.instantiateFunctionScope(script.Body); err != nil {
		return err
	}
	lastValue := -1
	for index := len(script.Body) - 1; index >= 0; index-- {
		if _, empty := script.Body[index].(*ast.EmptyStatement); !empty {
			if _, expression := script.Body[index].(*ast.ExpressionStatement); expression {
				lastValue = index
			}
			break
		}
	}
	for index, statement := range script.Body {
		if index == lastValue {
			expression := statement.(*ast.ExpressionStatement)
			if err := compiler.compileExpression(expression.Expression); err != nil {
				return err
			}
			return compiler.emit(browserruntime.Instruction{Op: browserruntime.OpReturn}, expression.Span())
		}
		if err := compiler.compileStatement(statement); err != nil {
			return err
		}
	}
	if err := compiler.emit(browserruntime.Instruction{Op: browserruntime.OpUndefined}, script.Span()); err != nil {
		return err
	}
	return compiler.emit(browserruntime.Instruction{Op: browserruntime.OpReturn}, script.Span())
}

func (compiler *functionCompiler) finish(name string, arity uint32) (program.FunctionTemplate, error) {
	code, locations, err := compiler.builder.BuildCode(len(compiler.constants))
	if err != nil {
		return program.FunctionTemplate{}, fmt.Errorf("%w: %v", ErrCompile, err)
	}
	return program.FunctionTemplate{
		Name: name, Arity: arity, Code: code,
		Constants: append([]program.Constant(nil), compiler.constants...), Locations: locations,
	}, nil
}

func (compiler *functionCompiler) emit(instruction browserruntime.Instruction, span lexer.Span) error {
	_, err := compiler.builder.EmitAt(instruction, runtimeSpan(span))
	if err != nil {
		return fmt.Errorf("%w: %v", ErrCompile, err)
	}
	return nil
}

func (compiler *functionCompiler) emitJump(opcode browserruntime.Opcode, label browserruntime.Label, span lexer.Span) error {
	_, err := compiler.builder.EmitJump(opcode, label, runtimeSpan(span))
	if err != nil {
		return fmt.Errorf("%w: %v", ErrCompile, err)
	}
	return nil
}

func (compiler *functionCompiler) emitHandler(kind browserruntime.ExceptionHandlerKind, label browserruntime.Label, span lexer.Span) error {
	_, err := compiler.builder.EmitHandler(kind, label, runtimeSpan(span))
	if err != nil {
		return fmt.Errorf("%w: %v", ErrCompile, err)
	}
	return nil
}

func (compiler *functionCompiler) emitCompletion(opcode browserruntime.Opcode, label browserruntime.Label, environmentDepth, handlerDepth int, span lexer.Span) error {
	_, err := compiler.builder.EmitCompletion(opcode, label, environmentDepth, handlerDepth, runtimeSpan(span))
	if err != nil {
		return fmt.Errorf("%w: %v", ErrCompile, err)
	}
	return nil
}

func (compiler *functionCompiler) mark(label browserruntime.Label) error {
	if err := compiler.builder.Mark(label); err != nil {
		return fmt.Errorf("%w: %v", ErrCompile, err)
	}
	return nil
}

func runtimeSpan(span lexer.Span) browserruntime.SourceSpan {
	return browserruntime.SourceSpan{Start: span.Start.Offset, End: span.End.Offset}
}

func (compiler *functionCompiler) addConstant(constant program.Constant) (uint32, error) {
	key := constantKey{kind: constant.Kind()}
	switch constant.Kind() {
	case program.ConstantBool:
		key.boolean = constant.Bool()
	case program.ConstantNumber:
		key.bits = math.Float64bits(constant.Number())
	case program.ConstantString:
		key.text = constant.String()
	case program.ConstantFunction:
		key.function = constant.Function()
	}
	if index, exists := compiler.constant[key]; exists {
		return index, nil
	}
	if uint64(len(compiler.constants)) > math.MaxUint32 {
		return 0, compiler.problem(lexer.Span{}, "too many constants")
	}
	index := uint32(len(compiler.constants))
	compiler.constants = append(compiler.constants, constant)
	compiler.constant[key] = index
	return index, nil
}

func (compiler *functionCompiler) stringConstant(value string) (uint32, error) {
	return compiler.addConstant(program.String(value))
}

func (compiler *functionCompiler) declare(name string, mutable bool, span lexer.Span) error {
	scope := compiler.scopes[len(compiler.scopes)-1]
	if previous, exists := scope[name]; exists {
		return compiler.problem(span, fmt.Sprintf("binding %q already declared at %d:%d", name, previous.span.Start.Line, previous.span.Start.Column))
	}
	scope[name] = binding{mutable: mutable, span: span, kind: bindingLexical}
	return nil
}

func (compiler *functionCompiler) declareParameter(name string, span lexer.Span) error {
	scope := compiler.scopes[len(compiler.scopes)-1]
	if previous, exists := scope[name]; exists {
		return compiler.problem(span, fmt.Sprintf("binding %q already declared at %d:%d", name, previous.span.Start.Line, previous.span.Start.Column))
	}
	scope[name] = binding{mutable: true, span: span, kind: bindingParameter}
	return nil
}

func (compiler *functionCompiler) resolve(name string) (binding, bool) {
	for current := compiler; current != nil; current = current.parent {
		for index := len(current.scopes) - 1; index >= 0; index-- {
			if result, exists := current.scopes[index][name]; exists {
				return result, true
			}
		}
	}
	return binding{}, false
}

func (compiler *functionCompiler) pushScope() {
	compiler.scopes = append(compiler.scopes, make(map[string]binding))
}

func (compiler *functionCompiler) popScope() {
	compiler.scopes[len(compiler.scopes)-1] = nil
	compiler.scopes = compiler.scopes[:len(compiler.scopes)-1]
}

func (compiler *functionCompiler) problem(span lexer.Span, message string) error {
	return &Error{Span: span, Message: message}
}
