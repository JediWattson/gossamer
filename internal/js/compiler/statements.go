package compiler

import (
	"fmt"

	"github.com/JediWattson/gossamer/internal/js/ast"
	"github.com/JediWattson/gossamer/internal/js/lexer"
	"github.com/JediWattson/gossamer/internal/js/program"
	browserruntime "github.com/JediWattson/gossamer/internal/runtime"
)

func (compiler *functionCompiler) compileStatement(statement ast.Statement) error {
	switch statement := statement.(type) {
	case *ast.EmptyStatement:
		return nil
	case *ast.BlockStatement:
		for _, child := range statement.Body {
			if err := compiler.compileStatement(child); err != nil {
				return err
			}
		}
		return nil
	case *ast.ExpressionStatement:
		if err := compiler.compileExpression(statement.Expression); err != nil {
			return err
		}
		return compiler.emit(browserruntime.Instruction{Op: browserruntime.OpPop}, statement.Span())
	case *ast.VariableDeclaration:
		return compiler.compileVariableDeclaration(statement)
	case *ast.IfStatement:
		return compiler.compileIf(statement)
	case *ast.WhileStatement:
		return compiler.compileWhile(statement)
	case *ast.BreakStatement:
		if len(compiler.loops) == 0 {
			return compiler.problem(statement.Span(), "break is only valid inside a loop")
		}
		target := compiler.loops[len(compiler.loops)-1]
		return compiler.emitCompletion(browserruntime.OpBreak, target.breakLabel, target.environmentDepth, target.handlerDepth, statement.Span())
	case *ast.ContinueStatement:
		if len(compiler.loops) == 0 {
			return compiler.problem(statement.Span(), "continue is only valid inside a loop")
		}
		target := compiler.loops[len(compiler.loops)-1]
		return compiler.emitCompletion(browserruntime.OpContinue, target.continueLabel, target.environmentDepth, target.handlerDepth, statement.Span())
	case *ast.FunctionDeclaration:
		return compiler.compileFunctionDeclaration(statement)
	case *ast.ReturnStatement:
		if !compiler.inFunction {
			return compiler.problem(statement.Span(), "return is only valid inside a Function")
		}
		if statement.Argument == nil {
			if err := compiler.emit(browserruntime.Instruction{Op: browserruntime.OpUndefined}, statement.Span()); err != nil {
				return err
			}
		} else if err := compiler.compileExpression(statement.Argument); err != nil {
			return err
		}
		return compiler.emit(browserruntime.Instruction{Op: browserruntime.OpReturn}, statement.Span())
	case *ast.ThrowStatement:
		if err := compiler.compileExpression(statement.Argument); err != nil {
			return err
		}
		return compiler.emit(browserruntime.Instruction{Op: browserruntime.OpThrow}, statement.Span())
	case *ast.TryStatement:
		return compiler.compileTry(statement)
	default:
		return compiler.problem(statement.Span(), fmt.Sprintf("unsupported statement %T", statement))
	}
}

func (compiler *functionCompiler) compileVariableDeclaration(declaration *ast.VariableDeclaration) error {
	mutable := declaration.Kind != ast.VariableConst
	for _, declarator := range declaration.Declarations {
		if err := compiler.declare(declarator.Name.Name, mutable, declarator.Name.Span()); err != nil {
			return err
		}
		name, err := compiler.stringConstant(declarator.Name.Name)
		if err != nil {
			return err
		}
		flag := uint32(0)
		if mutable {
			flag = 1
		}
		if err := compiler.emit(browserruntime.Instruction{Op: browserruntime.OpDeclareBinding, A: name, B: flag}, declarator.Name.Span()); err != nil {
			return err
		}
		if declarator.Init == nil {
			if err := compiler.emit(browserruntime.Instruction{Op: browserruntime.OpUndefined}, declarator.Span()); err != nil {
				return err
			}
		} else if err := compiler.compileExpression(declarator.Init); err != nil {
			return err
		}
		if err := compiler.emit(browserruntime.Instruction{Op: browserruntime.OpInitializeBinding, A: name}, declarator.Span()); err != nil {
			return err
		}
		if err := compiler.emit(browserruntime.Instruction{Op: browserruntime.OpPop}, declarator.Span()); err != nil {
			return err
		}
	}
	return nil
}

func (compiler *functionCompiler) compileIf(statement *ast.IfStatement) error {
	if err := compiler.compileExpression(statement.Test); err != nil {
		return err
	}
	alternate := compiler.builder.NewLabel()
	end := alternate
	if statement.Alternate != nil {
		end = compiler.builder.NewLabel()
	}
	if err := compiler.emitJump(browserruntime.OpJumpIfFalse, alternate, statement.Test.Span()); err != nil {
		return err
	}
	if err := compiler.compileStatement(statement.Consequent); err != nil {
		return err
	}
	if statement.Alternate != nil {
		if err := compiler.emitJump(browserruntime.OpJump, end, statement.Consequent.Span()); err != nil {
			return err
		}
	}
	if err := compiler.builder.Mark(alternate); err != nil {
		return fmt.Errorf("%w: %v", ErrCompile, err)
	}
	if statement.Alternate != nil {
		if err := compiler.compileStatement(statement.Alternate); err != nil {
			return err
		}
		if err := compiler.builder.Mark(end); err != nil {
			return fmt.Errorf("%w: %v", ErrCompile, err)
		}
	}
	return nil
}

func (compiler *functionCompiler) compileWhile(statement *ast.WhileStatement) error {
	start := compiler.builder.NewLabel()
	end := compiler.builder.NewLabel()
	if err := compiler.builder.Mark(start); err != nil {
		return fmt.Errorf("%w: %v", ErrCompile, err)
	}
	if err := compiler.compileExpression(statement.Test); err != nil {
		return err
	}
	if err := compiler.emitJump(browserruntime.OpJumpIfFalse, end, statement.Test.Span()); err != nil {
		return err
	}
	compiler.loops = append(compiler.loops, loopTarget{
		breakLabel: end, continueLabel: start,
		environmentDepth: compiler.environmentDepth,
		handlerDepth:     compiler.handlerDepth,
	})
	err := compiler.compileStatement(statement.Body)
	compiler.loops = compiler.loops[:len(compiler.loops)-1]
	if err != nil {
		return err
	}
	if err := compiler.emitJump(browserruntime.OpJump, start, statement.Body.Span()); err != nil {
		return err
	}
	if err := compiler.builder.Mark(end); err != nil {
		return fmt.Errorf("%w: %v", ErrCompile, err)
	}
	return nil
}

func (compiler *functionCompiler) compileFunctionDeclaration(declaration *ast.FunctionDeclaration) error {
	if err := compiler.declare(declaration.Name.Name, true, declaration.Name.Span()); err != nil {
		return err
	}
	name, err := compiler.stringConstant(declaration.Name.Name)
	if err != nil {
		return err
	}
	if err := compiler.emit(browserruntime.Instruction{Op: browserruntime.OpDeclareBinding, A: name, B: 1}, declaration.Name.Span()); err != nil {
		return err
	}
	function, err := compiler.compileNestedFunction(declaration.Name.Name, declaration.Parameters, declaration.Body, declaration.Span())
	if err != nil {
		return err
	}
	constant, err := compiler.addFunctionConstant(function)
	if err != nil {
		return err
	}
	if err := compiler.emit(browserruntime.Instruction{Op: browserruntime.OpCreateClosure, A: constant}, declaration.Span()); err != nil {
		return err
	}
	if err := compiler.emit(browserruntime.Instruction{Op: browserruntime.OpInitializeBinding, A: name}, declaration.Span()); err != nil {
		return err
	}
	return compiler.emit(browserruntime.Instruction{Op: browserruntime.OpPop}, declaration.Span())
}

func (compiler *functionCompiler) compileNestedFunction(name string, parameters []*ast.Identifier, body *ast.BlockStatement, span lexer.Span) (uint32, error) {
	index, err := compiler.owner.reserveFunction()
	if err != nil {
		return 0, err
	}
	child := newFunctionCompiler(compiler.owner, compiler, true)
	if err := child.emit(browserruntime.Instruction{Op: browserruntime.OpEnterScope}, span); err != nil {
		return 0, err
	}
	child.environmentDepth++
	for parameterIndex, parameter := range parameters {
		if err := child.declare(parameter.Name, true, parameter.Span()); err != nil {
			return 0, err
		}
		constant, err := child.stringConstant(parameter.Name)
		if err != nil {
			return 0, err
		}
		if err := child.emit(browserruntime.Instruction{Op: browserruntime.OpDeclareBinding, A: constant, B: 1}, parameter.Span()); err != nil {
			return 0, err
		}
		if err := child.emit(browserruntime.Instruction{Op: browserruntime.OpArgument, A: uint32(parameterIndex)}, parameter.Span()); err != nil {
			return 0, err
		}
		if err := child.emit(browserruntime.Instruction{Op: browserruntime.OpInitializeBinding, A: constant}, parameter.Span()); err != nil {
			return 0, err
		}
		if err := child.emit(browserruntime.Instruction{Op: browserruntime.OpPop}, parameter.Span()); err != nil {
			return 0, err
		}
	}
	for _, statement := range body.Body {
		if err := child.compileStatement(statement); err != nil {
			return 0, err
		}
	}
	if err := child.emit(browserruntime.Instruction{Op: browserruntime.OpUndefined}, body.Span()); err != nil {
		return 0, err
	}
	if err := child.emit(browserruntime.Instruction{Op: browserruntime.OpReturn}, body.Span()); err != nil {
		return 0, err
	}
	template, err := child.finish(name, uint32(len(parameters)))
	if err != nil {
		return 0, err
	}
	compiler.owner.functions[index] = template
	return index, nil
}

func (compiler *functionCompiler) compileTry(statement *ast.TryStatement) error {
	baseHandlerDepth := compiler.handlerDepth
	var finallyLabel browserruntime.Label
	if statement.Finalizer != nil {
		finallyLabel = compiler.builder.NewLabel()
		if err := compiler.emitHandler(browserruntime.HandlerFinally, finallyLabel, statement.Span()); err != nil {
			return err
		}
		compiler.handlerDepth++
	}

	var catchLabel browserruntime.Label
	if statement.Handler != nil {
		catchLabel = compiler.builder.NewLabel()
		if err := compiler.emitHandler(browserruntime.HandlerCatch, catchLabel, statement.Span()); err != nil {
			return err
		}
		compiler.handlerDepth++
	}
	if err := compiler.compileStatement(statement.Body); err != nil {
		return err
	}
	if statement.Handler != nil {
		if err := compiler.emit(browserruntime.Instruction{Op: browserruntime.OpLeaveTry}, statement.Body.Span()); err != nil {
			return err
		}
		compiler.handlerDepth--
		catchDone := compiler.builder.NewLabel()
		if err := compiler.emitJump(browserruntime.OpJump, catchDone, statement.Body.Span()); err != nil {
			return err
		}
		if err := compiler.mark(catchLabel); err != nil {
			return err
		}
		if err := compiler.emit(browserruntime.Instruction{Op: browserruntime.OpEnterCatch}, statement.Handler.Span()); err != nil {
			return err
		}
		if err := compiler.compileCatch(statement.Handler); err != nil {
			return err
		}
		if err := compiler.mark(catchDone); err != nil {
			return err
		}
	}

	if statement.Finalizer != nil {
		if err := compiler.emit(browserruntime.Instruction{Op: browserruntime.OpLeaveTry}, statement.Span()); err != nil {
			return err
		}
		compiler.handlerDepth--
		if err := compiler.emitJump(browserruntime.OpJump, finallyLabel, statement.Span()); err != nil {
			return err
		}
		if err := compiler.mark(finallyLabel); err != nil {
			return err
		}
		if err := compiler.emit(browserruntime.Instruction{Op: browserruntime.OpEnterFinally}, statement.Finalizer.Span()); err != nil {
			return err
		}
		if err := compiler.compileStatement(statement.Finalizer); err != nil {
			return err
		}
		if err := compiler.emit(browserruntime.Instruction{Op: browserruntime.OpEndFinally}, statement.Finalizer.Span()); err != nil {
			return err
		}
	}
	if compiler.handlerDepth != baseHandlerDepth {
		return compiler.problem(statement.Span(), "internal handler-depth imbalance")
	}
	return nil
}

func (compiler *functionCompiler) compileCatch(clause *ast.CatchClause) error {
	if clause.Parameter == nil {
		if err := compiler.emit(browserruntime.Instruction{Op: browserruntime.OpPop}, clause.Span()); err != nil {
			return err
		}
		return compiler.compileStatement(clause.Body)
	}
	if err := compiler.emit(browserruntime.Instruction{Op: browserruntime.OpEnterScope}, clause.Span()); err != nil {
		return err
	}
	compiler.environmentDepth++
	compiler.pushScope()
	defer compiler.popScope()
	if err := compiler.declare(clause.Parameter.Name, true, clause.Parameter.Span()); err != nil {
		return err
	}
	name, err := compiler.stringConstant(clause.Parameter.Name)
	if err != nil {
		return err
	}
	if err := compiler.emit(browserruntime.Instruction{Op: browserruntime.OpDeclareBinding, A: name, B: 1}, clause.Parameter.Span()); err != nil {
		return err
	}
	if err := compiler.emit(browserruntime.Instruction{Op: browserruntime.OpInitializeBinding, A: name}, clause.Parameter.Span()); err != nil {
		return err
	}
	if err := compiler.emit(browserruntime.Instruction{Op: browserruntime.OpPop}, clause.Parameter.Span()); err != nil {
		return err
	}
	if err := compiler.compileStatement(clause.Body); err != nil {
		return err
	}
	if err := compiler.emit(browserruntime.Instruction{Op: browserruntime.OpLeaveScope}, clause.Span()); err != nil {
		return err
	}
	compiler.environmentDepth--
	return nil
}

func (compiler *functionCompiler) addFunctionConstant(index uint32) (uint32, error) {
	return compiler.addConstant(program.Function(index))
}
