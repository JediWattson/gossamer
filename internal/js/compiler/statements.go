package compiler

import (
	"fmt"

	"github.com/JediWattson/gossamer/internal/js/ast"
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
		return compiler.emitJump(browserruntime.OpJump, compiler.loops[len(compiler.loops)-1].breakLabel, statement.Span())
	case *ast.ContinueStatement:
		if len(compiler.loops) == 0 {
			return compiler.problem(statement.Span(), "continue is only valid inside a loop")
		}
		return compiler.emitJump(browserruntime.OpJump, compiler.loops[len(compiler.loops)-1].continueLabel, statement.Span())
	case *ast.FunctionDeclaration, *ast.ReturnStatement, *ast.ThrowStatement, *ast.TryStatement:
		return compiler.problem(statement.Span(), fmt.Sprintf("%T requires the N6 function compiler", statement))
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
	compiler.loops = append(compiler.loops, loopTarget{breakLabel: end, continueLabel: start})
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

func (compiler *functionCompiler) addFunctionConstant(index uint32) (uint32, error) {
	return compiler.addConstant(program.Function(index))
}
