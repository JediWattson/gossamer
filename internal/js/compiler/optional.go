package compiler

import (
	"fmt"

	"github.com/JediWattson/gossamer/internal/js/ast"
	"github.com/JediWattson/gossamer/internal/js/lexer"
	browserruntime "github.com/JediWattson/gossamer/internal/runtime"
)

func containsOptionalChain(expression ast.Expression) bool {
	switch expression := expression.(type) {
	case *ast.MemberExpression:
		return expression.Optional || containsOptionalChain(expression.Object)
	case *ast.CallExpression:
		return expression.Optional || containsOptionalChain(expression.Callee)
	default:
		return false
	}
}

func (compiler *functionCompiler) compileOptionalChain(expression ast.Expression) error {
	shortCircuit := compiler.builder.NewLabel()
	end := compiler.builder.NewLabel()
	if err := compiler.compileOptionalChainExpression(expression, shortCircuit); err != nil {
		return err
	}
	if err := compiler.emitJump(browserruntime.OpJump, end, expression.Span()); err != nil {
		return err
	}
	if err := compiler.builder.Mark(shortCircuit); err != nil {
		return fmt.Errorf("%w: %v", ErrCompile, err)
	}
	if err := compiler.emit(browserruntime.Instruction{Op: browserruntime.OpPop}, expression.Span()); err != nil {
		return err
	}
	if err := compiler.emit(browserruntime.Instruction{Op: browserruntime.OpUndefined}, expression.Span()); err != nil {
		return err
	}
	if err := compiler.builder.Mark(end); err != nil {
		return fmt.Errorf("%w: %v", ErrCompile, err)
	}
	return nil
}

func (compiler *functionCompiler) compileOptionalChainExpression(expression ast.Expression, shortCircuit browserruntime.Label) error {
	switch expression := expression.(type) {
	case *ast.MemberExpression:
		if err := compiler.compileOptionalChainOperand(expression.Object, shortCircuit); err != nil {
			return err
		}
		if expression.Optional {
			if err := compiler.emitOptionalNullishJump(shortCircuit, expression.Object.Span()); err != nil {
				return err
			}
		}
		if err := compiler.compileMemberKey(expression); err != nil {
			return err
		}
		return compiler.emit(browserruntime.Instruction{Op: browserruntime.OpGetProperty}, expression.Span())

	case *ast.CallExpression:
		if member, ok := expression.Callee.(*ast.MemberExpression); ok {
			if expression.Optional {
				return compiler.problem(expression.Span(), "optional calls on a resolved method are not implemented")
			}
			if err := compiler.compileOptionalChainOperand(member.Object, shortCircuit); err != nil {
				return err
			}
			if member.Optional {
				if err := compiler.emitOptionalNullishJump(shortCircuit, member.Object.Span()); err != nil {
					return err
				}
			}
			if err := compiler.compileMemberKey(member); err != nil {
				return err
			}
			for _, argument := range expression.Arguments {
				if err := compiler.compileExpression(argument); err != nil {
					return err
				}
			}
			return compiler.emit(browserruntime.Instruction{Op: browserruntime.OpCallMethod, A: uint32(len(expression.Arguments))}, expression.Span())
		}
		if err := compiler.compileOptionalChainOperand(expression.Callee, shortCircuit); err != nil {
			return err
		}
		if expression.Optional {
			if err := compiler.emitOptionalNullishJump(shortCircuit, expression.Callee.Span()); err != nil {
				return err
			}
		}
		for _, argument := range expression.Arguments {
			if err := compiler.compileExpression(argument); err != nil {
				return err
			}
		}
		return compiler.emit(browserruntime.Instruction{Op: browserruntime.OpCall, A: uint32(len(expression.Arguments))}, expression.Span())
	default:
		return compiler.problem(expression.Span(), "malformed optional chain")
	}
}

func (compiler *functionCompiler) compileOptionalChainOperand(expression ast.Expression, shortCircuit browserruntime.Label) error {
	if containsOptionalChain(expression) {
		return compiler.compileOptionalChainExpression(expression, shortCircuit)
	}
	return compiler.compileExpression(expression)
}

func (compiler *functionCompiler) emitOptionalNullishJump(target browserruntime.Label, span lexer.Span) error {
	if err := compiler.emit(browserruntime.Instruction{Op: browserruntime.OpDup}, span); err != nil {
		return err
	}
	return compiler.emitJump(browserruntime.OpJumpIfNullish, target, span)
}
